// Package store 封装 SQLite 数据访问层，负责建库、迁移与各类 CRUD。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Store 数据存储句柄
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）位于 dataDir 下的 SQLite 数据库，并执行迁移。
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	path := filepath.Join(dataDir, "lightmark.db")
	// 连接串启用外键与 WAL，提升并发与可靠性
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// modernc 驱动建议限制为单连接，避免并发写冲突
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}
	return s, nil
}

// Close 关闭数据库连接。
func (s *Store) Close() error {
	return s.db.Close()
}

// currentVersion 当前数据库 schema 版本。
const currentVersion = 2

// v1Statements 版本 1：初始建表与索引，均为幂等语句。
var v1Statements = []string{
	`CREATE TABLE IF NOT EXISTS records (
		id          TEXT PRIMARY KEY,
		type        TEXT NOT NULL,
		path_or_url TEXT NOT NULL,
		title       TEXT NOT NULL,
		note        TEXT NOT NULL DEFAULT '',
		category_id TEXT NOT NULL DEFAULT '',
		star        INTEGER NOT NULL DEFAULT 0,
		invalid     INTEGER NOT NULL DEFAULT 0,
		deleted     INTEGER NOT NULL DEFAULT 0,
		created_at  INTEGER NOT NULL,
		updated_at  INTEGER NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_records_type ON records(type)`,
	`CREATE INDEX IF NOT EXISTS idx_records_category ON records(category_id)`,
	`CREATE INDEX IF NOT EXISTS idx_records_deleted ON records(deleted)`,

	`CREATE TABLE IF NOT EXISTS tags (
		id   TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE
	)`,

	`CREATE TABLE IF NOT EXISTS record_tags (
		record_id TEXT NOT NULL,
		tag_id    TEXT NOT NULL,
		PRIMARY KEY (record_id, tag_id)
	)`,

	`CREATE TABLE IF NOT EXISTS categories (
		id        TEXT PRIMARY KEY,
		name      TEXT NOT NULL,
		parent_id TEXT NOT NULL DEFAULT '',
		sort      INTEGER NOT NULL DEFAULT 0
	)`,

	// 独立 FTS5 表，trigram 分词支持中文子串检索
	`CREATE VIRTUAL TABLE IF NOT EXISTS records_fts USING fts5(
		title, note, tags, path_or_url,
		record_id UNINDEXED,
		tokenize = 'trigram'
	)`,
}

// migrate 使用 PRAGMA user_version 进行版本化迁移。
func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("读取数据库版本失败: %w", err)
	}
	if version > currentVersion {
		return fmt.Errorf("数据库版本 %d 高于程序支持的 %d", version, currentVersion)
	}
	for v := version; v < currentVersion; v++ {
		next := v + 1
		if err := s.applyMigration(next); err != nil {
			return fmt.Errorf("执行迁移 v%d 失败: %w", next, err)
		}
		if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", next)); err != nil {
			return fmt.Errorf("更新数据库版本失败: %w", err)
		}
	}
	return nil
}

// applyMigration 执行指定版本的迁移。
func (s *Store) applyMigration(version int) error {
	switch version {
	case 1:
		for _, stmt := range v1Statements {
			if _, err := s.db.Exec(stmt); err != nil {
				return fmt.Errorf("执行迁移语句失败: %w", err)
			}
		}
		return nil
	case 2:
		// v2：favicon 缓存（data URL）与最近访问统计
		// ALTER TABLE 非幂等，迁移中断重放时忽略 duplicate column 错误
		for _, stmt := range v2Statements {
			if _, err := s.db.Exec(stmt); err != nil &&
				!strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("执行迁移语句失败: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("未知迁移版本: %d", version)
}

// v2Statements 版本 2：为 records 追加 favicon 与最近访问字段（幂等：列已存在时跳过）。
var v2Statements = []string{
	`ALTER TABLE records ADD COLUMN favicon TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE records ADD COLUMN opened_at INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE records ADD COLUMN open_count INTEGER NOT NULL DEFAULT 0`,
}
