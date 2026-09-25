package store

import (
	"fmt"
	"time"

	"light_mark/internal/model"
)

// Export 导出全部数据（含回收站）。
func (s *Store) Export() (*model.Backup, error) {
	cats, err := s.ListCategories()
	if err != nil {
		return nil, err
	}
	tags, err := s.ListTags()
	if err != nil {
		return nil, err
	}
	recs, err := s.listAllRecords()
	if err != nil {
		return nil, err
	}
	return &model.Backup{
		Version:    1,
		ExportedAt: time.Now().UnixMilli(),
		Categories: cats,
		Tags:       tags,
		Records:    recs,
	}, nil
}

// listAllRecords 返回所有记录（含已删除），用于导出。
func (s *Store) listAllRecords() ([]model.Record, error) {
	rows, err := s.db.Query(
		`SELECT id, type, path_or_url, title, note, category_id, star, invalid, deleted,
		       favicon, opened_at, open_count, created_at, updated_at
		 FROM records ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("查询记录失败: %w", err)
	}
	defer rows.Close()
	records, err := scanRecords(rows)
	if err != nil {
		return nil, err
	}
	return s.fillTags(records)
}

// Import 合并导入备份数据，按 ID 去重，返回新增数量统计。
// 仅支持当前备份格式版本，防止未来格式变更后被静默误导入。
func (s *Store) Import(b *model.Backup) (model.ImportResult, error) {
	if b == nil {
		return model.ImportResult{}, fmt.Errorf("备份数据为空")
	}
	if b.Version != 1 {
		return model.ImportResult{}, fmt.Errorf("不支持的备份版本: %d", b.Version)
	}
	var res model.ImportResult
	tx, err := s.db.Begin()
	if err != nil {
		return res, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 分类：按 ID 去重
	for _, c := range b.Categories {
		if c.ID == "" || c.Name == "" {
			continue
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(1) FROM categories WHERE id = ?`, c.ID).Scan(&exists); err != nil {
			return res, fmt.Errorf("查询分类失败: %w", err)
		}
		if exists > 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO categories (id, name, parent_id, sort) VALUES (?, ?, ?, ?)`,
			c.ID, c.Name, c.ParentID, c.Sort); err != nil {
			return res, fmt.Errorf("导入分类失败: %w", err)
		}
		res.Categories++
	}

	// 标签：按 ID 去重，重名保留已有
	for _, t := range b.Tags {
		if t.ID == "" || t.Name == "" {
			continue
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(1) FROM tags WHERE id = ? OR name = ?`, t.ID, t.Name).Scan(&exists); err != nil {
			return res, fmt.Errorf("查询标签失败: %w", err)
		}
		if exists > 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO tags (id, name) VALUES (?, ?)`, t.ID, t.Name); err != nil {
			return res, fmt.Errorf("导入标签失败: %w", err)
		}
		res.Tags++
	}

	// 记录：按 ID 去重，标签按名称解析
	for _, r := range b.Records {
		if r.ID == "" {
			continue
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(1) FROM records WHERE id = ?`, r.ID).Scan(&exists); err != nil {
			return res, fmt.Errorf("查询记录失败: %w", err)
		}
		if exists > 0 {
			continue
		}
		rt := r.Type
		if rt != model.TypeFile && rt != model.TypeURL {
			rt = model.TypeFile
		}
		if _, err := tx.Exec(
			`INSERT INTO records (id, type, path_or_url, title, note, category_id, star, invalid, deleted,
			                      favicon, opened_at, open_count, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, rt, r.PathOrURL, r.Title, r.Note, r.CategoryID,
			boolToInt(r.Star), boolToInt(r.Invalid), boolToInt(r.Deleted),
			r.Favicon, r.OpenedAt, r.OpenCount, r.CreatedAt, r.UpdatedAt,
		); err != nil {
			return res, fmt.Errorf("导入记录失败: %w", err)
		}
		names := make([]string, 0, len(r.Tags))
		for _, t := range r.Tags {
			names = append(names, t.Name)
		}
		tags, err := resolveTags(tx, names)
		if err != nil {
			return res, err
		}
		if err := replaceRecordTags(tx, r.ID, tags); err != nil {
			return res, err
		}
		res.Records++
	}

	// 全量重建索引，保证一致性
	if err := rebuildFTS(tx); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("提交事务失败: %w", err)
	}
	return res, nil
}

// EmptyTrash 清空回收站，彻底删除所有已软删除的记录。
func (s *Store) EmptyTrash() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM record_tags WHERE record_id IN (SELECT id FROM records WHERE deleted = 1)`); err != nil {
		return fmt.Errorf("清理标签关联失败: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM records WHERE deleted = 1`); err != nil {
		return fmt.Errorf("清空回收站失败: %w", err)
	}
	return tx.Commit()
}
