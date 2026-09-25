package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"light_mark/internal/model"
)

// ListCategories 返回全部分类，按 sort 升序、名称排序。
func (s *Store) ListCategories() ([]model.Category, error) {
	rows, err := s.db.Query(`SELECT id, name, parent_id, sort FROM categories ORDER BY sort ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询分类失败: %w", err)
	}
	defer rows.Close()

	cats := []model.Category{}
	for rows.Next() {
		var c model.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.ParentID, &c.Sort); err != nil {
			return nil, fmt.Errorf("扫描分类失败: %w", err)
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

// CreateCategory 创建分类。
func (s *Store) CreateCategory(name, parentID string) (*model.Category, error) {
	if name == "" {
		return nil, errors.New("分类名称不能为空")
	}
	c := &model.Category{
		ID:       uuid.NewString(),
		Name:     name,
		ParentID: parentID,
	}
	if _, err := s.db.Exec(
		`INSERT INTO categories (id, name, parent_id, sort) VALUES (?, ?, ?, 0)`,
		c.ID, c.Name, c.ParentID,
	); err != nil {
		return nil, fmt.Errorf("创建分类失败: %w", err)
	}
	return c, nil
}

// UpdateCategory 更新分类名称、父级与排序。
func (s *Store) UpdateCategory(id, name, parentID string, sort int) (*model.Category, error) {
	if id == "" {
		return nil, errors.New("分类 ID 不能为空")
	}
	if name == "" {
		return nil, errors.New("分类名称不能为空")
	}
	if id == parentID {
		return nil, errors.New("分类不能以自身为父级")
	}
	res, err := s.db.Exec(
		`UPDATE categories SET name=?, parent_id=?, sort=? WHERE id=?`,
		name, parentID, sort, id,
	)
	if err != nil {
		return nil, fmt.Errorf("更新分类失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("分类不存在: %s", id)
	}
	return &model.Category{ID: id, Name: name, ParentID: parentID, Sort: sort}, nil
}

// DeleteCategory 删除分类，其下记录的分类字段置空，子分类上移一级。
func (s *Store) DeleteCategory(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 先读取父级，删除后供子分类上移使用
	var parentID string
	err = tx.QueryRow(`SELECT parent_id FROM categories WHERE id = ?`, id).Scan(&parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("分类不存在: %s", id)
	}
	if err != nil {
		return fmt.Errorf("查询父级失败: %w", err)
	}

	res, err := tx.Exec(`DELETE FROM categories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除分类失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("分类不存在: %s", id)
	}
	if _, err := tx.Exec(`UPDATE records SET category_id = '' WHERE category_id = ?`, id); err != nil {
		return fmt.Errorf("清理记录分类失败: %w", err)
	}
	// 子分类上移：其 parent_id 指向被删分类的父级
	if _, err := tx.Exec(`UPDATE categories SET parent_id = ? WHERE parent_id = ?`, parentID, id); err != nil {
		return fmt.Errorf("上移子分类失败: %w", err)
	}
	return tx.Commit()
}
