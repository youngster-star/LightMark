package store

import (
	"fmt"
	"os"
)

// SnapshotTo 使用 SQLite 的 VACUUM INTO 生成当前数据库的完整快照文件。
// 相比直接复制 db 文件，VACUUM INTO 在 WAL 模式下也能保证快照内容一致完整，
// 且不中断当前连接的读写。
func (s *Store) SnapshotTo(path string) error {
	if path == "" {
		return fmt.Errorf("快照路径不能为空")
	}
	// VACUUM INTO 要求目标文件不存在； 与保存对话框「覆盖保存」的语义对齐：先移除旧快照
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("移除旧快照失败: %w", err)
	}
	if _, err := s.db.Exec(`VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("生成数据库快照失败: %w", err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return fmt.Errorf("快照文件写入异常: %w", err)
	}
	return nil
}
