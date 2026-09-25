package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"light_mark/internal/model"
)

// ListTags 返回全部标签，按名称排序。
func (s *Store) ListTags() ([]model.Tag, error) {
	rows, err := s.db.Query(`SELECT id, name FROM tags ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询标签失败: %w", err)
	}
	defer rows.Close()

	tags := []model.Tag{}
	for rows.Next() {
		var t model.Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("扫描标签失败: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// CreateTag 创建标签，若同名已存在则返回已存在标签。
func (s *Store) CreateTag(name string) (*model.Tag, error) {
	if name == "" {
		return nil, errors.New("标签名称不能为空")
	}
	var id string
	err := s.db.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return &model.Tag{ID: id, Name: name}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("查询标签失败: %w", err)
	}
	id = uuid.NewString()
	if _, err := s.db.Exec(`INSERT INTO tags (id, name) VALUES (?, ?)`, id, name); err != nil {
		return nil, fmt.Errorf("创建标签失败: %w", err)
	}
	return &model.Tag{ID: id, Name: name}, nil
}

// RenameTag 重命名标签，并重建受影响记录的 FTS 索引。
// 目标名称与其他标签重名时返回明确错误（tags.name 有 UNIQUE 约束，
// 直接执行会把 SQLite 原始错误抛给用户，信息不友好）。
func (s *Store) RenameTag(id, name string) error {
	if id == "" {
		return errors.New("标签 ID 不能为空")
	}
	if name == "" {
		return errors.New("标签名称不能为空")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	var dupID string
	err = tx.QueryRow(`SELECT id FROM tags WHERE name = ? AND id <> ?`, name, id).Scan(&dupID)
	if err == nil {
		return fmt.Errorf("已存在同名标签: %s", name)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("查询标签失败: %w", err)
	}

	res, err := tx.Exec(`UPDATE tags SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("重命名标签失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("标签不存在: %s", id)
	}
	if err := rebuildFTS(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// MergeTag 将源标签的全部记录关联迁移到目标标签（已存在同记录同标签的关联自动去重），
// 之后删除源标签并重建 FTS 索引。源标签与目标标签不能是同一个。
func (s *Store) MergeTag(srcID, dstID string) error {
	if srcID == "" || dstID == "" {
		return errors.New("标签 ID 不能为空")
	}
	if srcID == dstID {
		return errors.New("不能合并到标签自身")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 两边标签都必须存在，避免静默丢失关联
	var srcName, dstName string
	if err := tx.QueryRow(`SELECT name FROM tags WHERE id = ?`, srcID).Scan(&srcName); err != nil {
		return fmt.Errorf("源标签不存在: %w", err)
	}
	if err := tx.QueryRow(`SELECT name FROM tags WHERE id = ?`, dstID).Scan(&dstName); err != nil {
		return fmt.Errorf("目标标签不存在: %w", err)
	}
	// 迁移关联：INSERT OR IGNORE 跳过已存在组合，防止主键冲突
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO record_tags (record_id, tag_id)
		 SELECT record_id, ? FROM record_tags WHERE tag_id = ?`, dstID, srcID,
	); err != nil {
		return fmt.Errorf("迁移标签关联失败: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM record_tags WHERE tag_id = ?`, srcID); err != nil {
		return fmt.Errorf("清理源标签关联失败: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, srcID); err != nil {
		return fmt.Errorf("删除源标签失败: %w", err)
	}
	// 标签名变化会影响记录的 FTS tags 列，统一重建
	if err := rebuildFTS(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteTag 删除标签并清理关联，重建受影响记录的 FTS 索引。
func (s *Store) DeleteTag(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM record_tags WHERE tag_id = ?`, id); err != nil {
		return fmt.Errorf("清理标签关联失败: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, id); err != nil {
		return fmt.Errorf("删除标签失败: %w", err)
	}
	if err := rebuildFTS(tx); err != nil {
		return err
	}
	return tx.Commit()
}
