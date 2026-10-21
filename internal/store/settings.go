package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// GetSetting 读取应用偏好设置，键不存在时返回空串。
func (s *Store) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取设置失败: %w", err)
	}
	return value, nil
}

// SetSetting 写入应用偏好设置（存在则覆盖）。
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO app_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}
	return nil
}
