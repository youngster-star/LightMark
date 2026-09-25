package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"light_mark/internal/model"
)

// validSortColumns 白名单：允许排序的列，防止 SQL 注入。
var validSortColumns = map[string]string{
	"":           "updated_at",
	"updated_at": "updated_at",
	"created_at": "created_at",
	"opened_at":  "opened_at",
	"title":      "title",
}

// validateRecordInput 校验记录入参，返回规范化后的类型与路径/网址。
// 规范化规则：
//   - file：filepath.Clean 折叠冗余分隔符；Windows 路径大小写不敏感，比较环节另行折叠
//   - url：去除协议后尾部多余的 "/"（根路径除外），便于重复检测与展示一致性
func validateRecordInput(in model.RecordInput) (model.RecordType, string, error) {
	if in.Title == "" {
		return "", "", errors.New("标题不能为空")
	}
	var rt model.RecordType
	switch in.Type {
	case string(model.TypeFile), "":
		rt = model.TypeFile
	case string(model.TypeURL):
		rt = model.TypeURL
	default:
		return "", "", fmt.Errorf("未知的记录类型: %s", in.Type)
	}
	in.PathOrURL = strings.TrimSpace(in.PathOrURL)
	if in.PathOrURL == "" {
		return "", "", errors.New("路径或网址不能为空")
	}
	if rt == model.TypeFile {
		in.PathOrURL = filepath.Clean(in.PathOrURL)
	} else {
		// 仅裁剪末尾斜杠，保留查询串与路径结构
		in.PathOrURL = strings.TrimRight(in.PathOrURL, "/")
		if in.PathOrURL == "" {
			return "", "", errors.New("路径或网址不能为空")
		}
	}
	return rt, in.PathOrURL, nil
}

// resolveTags 在事务内按名称查找或创建标签，返回对应 Tag 列表。
func resolveTags(tx *sql.Tx, names []string) ([]model.Tag, error) {
	seen := make(map[string]struct{}, len(names))
	var tags []model.Tag
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		var id string
		err := tx.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			id = uuid.NewString()
			if _, err := tx.Exec(`INSERT INTO tags (id, name) VALUES (?, ?)`, id, name); err != nil {
				return nil, fmt.Errorf("创建标签失败: %w", err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("查询标签失败: %w", err)
		}
		tags = append(tags, model.Tag{ID: id, Name: name})
	}
	return tags, nil
}

// replaceRecordTags 删除记录原有标签关联并按新标签重建。
// 仅关联数据库中仍存在的标签：撤销快照还原时，快照中的标签可能已被
// 删除/合并，直接插入会因外键约束导致整个撤销事务失败。
func replaceRecordTags(tx *sql.Tx, recordID string, tags []model.Tag) error {
	if _, err := tx.Exec(`DELETE FROM record_tags WHERE record_id = ?`, recordID); err != nil {
		return fmt.Errorf("清除标签关联失败: %w", err)
	}
	for _, t := range tags {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(1) FROM tags WHERE id = ?`, t.ID).Scan(&exists); err != nil {
			return fmt.Errorf("查询标签失败: %w", err)
		}
		if exists == 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO record_tags (record_id, tag_id) VALUES (?, ?)`, recordID, t.ID); err != nil {
			return fmt.Errorf("写入标签关联失败: %w", err)
		}
	}
	return nil
}

// indexRecord 写入或更新 FTS 索引行。
func indexRecord(tx *sql.Tx, r *model.Record) error {
	tagNames := make([]string, 0, len(r.Tags))
	for _, t := range r.Tags {
		tagNames = append(tagNames, t.Name)
	}
	if _, err := tx.Exec(`DELETE FROM records_fts WHERE record_id = ?`, r.ID); err != nil {
		return fmt.Errorf("删除索引失败: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO records_fts (title, note, tags, path_or_url, record_id) VALUES (?, ?, ?, ?, ?)`,
		r.Title, r.Note, strings.Join(tagNames, " "), r.PathOrURL, r.ID,
	); err != nil {
		return fmt.Errorf("写入索引失败: %w", err)
	}
	return nil
}

// removeIndex 删除 FTS 索引行。
func removeIndex(tx *sql.Tx, recordID string) error {
	if _, err := tx.Exec(`DELETE FROM records_fts WHERE record_id = ?`, recordID); err != nil {
		return fmt.Errorf("删除索引失败: %w", err)
	}
	return nil
}

// CreateRecord 创建一条记录并同步标签与索引。
func (s *Store) CreateRecord(in model.RecordInput) (*model.Record, error) {
	rt, pathOrURL, err := validateRecordInput(in)
	if err != nil {
		return nil, err
	}
	in.PathOrURL = pathOrURL
	now := time.Now().UnixMilli()
	id := in.ID
	if id == "" {
		id = uuid.NewString()
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	tags, err := resolveTags(tx, in.Tags)
	if err != nil {
		return nil, err
	}
	rec := &model.Record{
		ID:         id,
		Type:       rt,
		PathOrURL:  in.PathOrURL,
		Title:      in.Title,
		Note:       in.Note,
		CategoryID: in.CategoryID,
		Star:       in.Star,
		Favicon:    in.Favicon,
		CreatedAt:  now,
		UpdatedAt:  now,
		Tags:       tags,
	}
	if _, err := tx.Exec(
		`INSERT INTO records (id, type, path_or_url, title, note, category_id, star, invalid, deleted, favicon, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0, ?, ?, ?)`,
		rec.ID, rec.Type, rec.PathOrURL, rec.Title, rec.Note, rec.CategoryID, boolToInt(rec.Star), rec.Favicon, rec.CreatedAt, rec.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("写入记录失败: %w", err)
	}
	if err := replaceRecordTags(tx, rec.ID, tags); err != nil {
		return nil, err
	}
	if err := indexRecord(tx, rec); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交事务失败: %w", err)
	}
	return rec, nil
}

// UpdateRecord 更新记录内容、标签与索引。
func (s *Store) UpdateRecord(in model.RecordInput) (*model.Record, error) {
	if in.ID == "" {
		return nil, errors.New("记录 ID 不能为空")
	}
	rt, pathOrURL, err := validateRecordInput(in)
	if err != nil {
		return nil, err
	}
	in.PathOrURL = pathOrURL

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	tags, err := resolveTags(tx, in.Tags)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	// favicon 为空时保持原值，非空时覆盖（支持重新抓取图标）
	res, err := tx.Exec(
		`UPDATE records SET type=?, path_or_url=?, title=?, note=?, category_id=?, star=?,
		     favicon=CASE WHEN ?<>'' THEN ? ELSE favicon END, updated_at=? WHERE id=?`,
		rt, in.PathOrURL, in.Title, in.Note, in.CategoryID, boolToInt(in.Star),
		in.Favicon, in.Favicon, now, in.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("更新记录失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("记录不存在: %s", in.ID)
	}
	if err := replaceRecordTags(tx, in.ID, tags); err != nil {
		return nil, err
	}

	rec, err := getRecordTx(tx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := indexRecord(tx, rec); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("提交事务失败: %w", err)
	}
	return rec, nil
}

// GetRecord 按 ID 读取单条记录（含标签）。
func (s *Store) GetRecord(id string) (*model.Record, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()
	rec, err := getRecordTx(tx, id)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// SoftDeleteRecord 软删除：移入回收站，同时移除索引。
func (s *Store) SoftDeleteRecord(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE records SET deleted=1, updated_at=? WHERE id=?`, time.Now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("删除记录失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("记录不存在: %s", id)
	}
	if err := removeIndex(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// RestoreRecord 从回收站恢复，并重建索引。
// 恢复属于状态变更，不刷新 updated_at：内容时间保持原值，
// 「按更新时间」排序与撤销语义（完整还原）保持一致。
func (s *Store) RestoreRecord(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE records SET deleted=0 WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("恢复记录失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("记录不存在: %s", id)
	}
	rec, err := getRecordTx(tx, id)
	if err != nil {
		return err
	}
	if err := indexRecord(tx, rec); err != nil {
		return err
	}
	return tx.Commit()
}

// PurgeRecord 彻底删除记录及其标签关联与索引。
func (s *Store) PurgeRecord(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM record_tags WHERE record_id = ?`, id); err != nil {
		return fmt.Errorf("清除标签关联失败: %w", err)
	}
	if err := removeIndex(tx, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM records WHERE id = ?`, id); err != nil {
		return fmt.Errorf("删除记录失败: %w", err)
	}
	return tx.Commit()
}

// FindDuplicate 查找与给定路径或网址相同且未删除的记录（排除 excludeID），
// 用于录入时的重复提示；未找到返回 nil。
// 比较对大小写不敏感（SQLite LOWER 仅作用于 ASCII，对中文无影响）：
// Windows 文件路径与域名/协议均不区分大小写，精确区分大小写会产生漏报。
// 尾部斜杠已在入库前规范化，此处只需大小写折叠。
func (s *Store) FindDuplicate(pathOrURL, excludeID string) (*model.Record, error) {
	pathOrURL = strings.TrimSpace(pathOrURL)
	if pathOrURL == "" {
		return nil, nil
	}
	// 查询值同样裁剪尾部斜杠，与入库规范化对齐（存量数据可能带尾斜杠的场景无法
	// 在 SQL 侧简单处理，规范化入库后新数据不再产生）
	pathOrURL = strings.TrimRight(pathOrURL, "/\\")
	if pathOrURL == "" {
		return nil, nil
	}
	rows, err := s.queryRecords(
		[]string{"deleted = 0", "LOWER(path_or_url) = LOWER(?)", "id <> ?"},
		[]any{pathOrURL, excludeID},
		model.ListFilter{},
	)
	if err != nil {
		return nil, fmt.Errorf("重复检测查询失败: %w", err)
	}
	defer rows.Close()
	recs, err := scanRecords(rows)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, nil
	}
	recs, err = s.fillTags(recs)
	if err != nil {
		return nil, err
	}
	return &recs[0], nil
}

// RestoreRecordSnapshot 应用完整记录快照：全字段还原 + 标签关联重建 + 索引同步。
// 供撤销功能使用：快照中 deleted 状态决定索引重建还是移除；
// 若记录行已被彻底删除（Purge），则按快照重建整行。
func (s *Store) RestoreRecordSnapshot(rec *model.Record) error {
	if rec == nil || rec.ID == "" {
		return errors.New("记录快照无效")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE records SET type=?, path_or_url=?, title=?, note=?, category_id=?, star=?,
		     invalid=?, deleted=?, favicon=?, opened_at=?, open_count=?, created_at=?, updated_at=?
		 WHERE id=?`,
		string(rec.Type), rec.PathOrURL, rec.Title, rec.Note, rec.CategoryID, boolToInt(rec.Star),
		boolToInt(rec.Invalid), boolToInt(rec.Deleted), rec.Favicon, rec.OpenedAt, rec.OpenCount,
		rec.CreatedAt, rec.UpdatedAt, rec.ID,
	)
	if err != nil {
		return fmt.Errorf("还原记录失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 行已不存在（被彻底删除）：按快照重建整行
		if _, err := tx.Exec(
			`INSERT INTO records (id, type, path_or_url, title, note, category_id, star, invalid, deleted, favicon, opened_at, open_count, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rec.ID, string(rec.Type), rec.PathOrURL, rec.Title, rec.Note, rec.CategoryID,
			boolToInt(rec.Star), boolToInt(rec.Invalid), boolToInt(rec.Deleted), rec.Favicon,
			rec.OpenedAt, rec.OpenCount, rec.CreatedAt, rec.UpdatedAt,
		); err != nil {
			return fmt.Errorf("重建记录失败: %w", err)
		}
	}
	if err := replaceRecordTags(tx, rec.ID, rec.Tags); err != nil {
		return err
	}
	if rec.Deleted {
		if err := removeIndex(tx, rec.ID); err != nil {
			return err
		}
	} else {
		if err := indexRecord(tx, rec); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetRecordInvalid 标记记录失效状态，并同步索引。
// 校验属于状态检测，不刷新 updated_at，避免打乱「按更新时间」排序。
func (s *Store) SetRecordInvalid(id string, invalid bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE records SET invalid=? WHERE id=?`, boolToInt(invalid), id)
	if err != nil {
		return fmt.Errorf("更新失效状态失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("记录不存在: %s", id)
	}
	return tx.Commit()
}

// TouchRecordOpen 记录打开行为：刷新最近打开时间并累加打开次数。
// 打开不改变 updated_at，避免影响内容更新排序。
func (s *Store) TouchRecordOpen(id string) error {
	res, err := s.db.Exec(
		`UPDATE records SET opened_at=?, open_count=open_count+1 WHERE id=?`,
		time.Now().UnixMilli(), id,
	)
	if err != nil {
		return fmt.Errorf("记录打开状态失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("记录不存在: %s", id)
	}
	return nil
}

// SetRecordStar 切换星标状态。
// 星标不参与 FTS 索引，无需重建索引。
func (s *Store) SetRecordStar(id string, star bool) error {
	res, err := s.db.Exec(`UPDATE records SET star=? WHERE id=?`, boolToInt(star), id)
	if err != nil {
		return fmt.Errorf("更新星标失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("记录不存在: %s", id)
	}
	return nil
}

// ListRecords 按过滤条件查询记录列表（含标签、排序）。
func (s *Store) ListRecords(f model.ListFilter) ([]model.Record, error) {
	where := []string{}
	args := []any{}
	if !f.Trashed {
		where = append(where, "deleted = 0")
	} else {
		where = append(where, "deleted = 1")
	}
	if f.Type != "" {
		where = append(where, "type = ?")
		args = append(args, f.Type)
	}
	if f.CategoryID != "" {
		where = append(where, "category_id = ?")
		args = append(args, f.CategoryID)
	}
	if f.StarOnly {
		where = append(where, "star = 1")
	}
	if f.Tag != "" {
		where = append(where, `id IN (SELECT record_id FROM record_tags rt JOIN tags t ON rt.tag_id = t.id WHERE t.name = ?)`)
		args = append(args, f.Tag)
	}
	// 搜索：优先 FTS，短词回退 LIKE，详见 searchRecords 实现
	if f.Query != "" {
		return s.searchRecords(f, where, args)
	}

	rows, err := s.queryRecords(where, args, f)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records, err := scanRecords(rows)
	if err != nil {
		return nil, err
	}
	return s.fillTags(records)
}

// scanRecords 从结果集扫描基础字段。
// 调用方 SELECT 列顺序必须与 Scan 一致：
// id, type, path_or_url, title, note, category_id, star, invalid, deleted, favicon, opened_at, open_count, created_at, updated_at
func scanRecords(rows *sql.Rows) ([]model.Record, error) {
	// 初始化为空切片而非 var 声明的 nil：空结果集序列化为 JSON null 会让前端
	// records.length 抛 TypeError，导致整个界面渲染中断白屏
	records := make([]model.Record, 0)
	for rows.Next() {
		var r model.Record
		var star, invalid, deleted int
		if err := rows.Scan(&r.ID, &r.Type, &r.PathOrURL, &r.Title, &r.Note, &r.CategoryID,
			&star, &invalid, &deleted, &r.Favicon, &r.OpenedAt, &r.OpenCount, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("扫描记录失败: %w", err)
		}
		r.Star = star != 0
		r.Invalid = invalid != 0
		r.Deleted = deleted != 0
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历记录失败: %w", err)
	}
	return records, nil
}

// fillTags 批量填充记录标签，避免 N+1 查询。
func (s *Store) fillTags(records []model.Record) ([]model.Record, error) {
	if len(records) == 0 {
		return records, nil
	}
	ids := make([]string, 0, len(records))
	idSet := make(map[string]struct{}, len(records))
	for _, r := range records {
		if _, ok := idSet[r.ID]; !ok {
			ids = append(ids, r.ID)
			idSet[r.ID] = struct{}{}
		}
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	q := `SELECT rt.record_id, t.id, t.name FROM record_tags rt
		JOIN tags t ON rt.tag_id = t.id WHERE rt.record_id IN (` + placeholders + `) ORDER BY t.name`
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("查询标签失败: %w", err)
	}
	defer rows.Close()

	tagMap := make(map[string][]model.Tag, len(ids))
	for rows.Next() {
		var recordID, tagID, name string
		if err := rows.Scan(&recordID, &tagID, &name); err != nil {
			return nil, fmt.Errorf("扫描标签失败: %w", err)
		}
		tagMap[recordID] = append(tagMap[recordID], model.Tag{ID: tagID, Name: name})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历标签失败: %w", err)
	}
	for i := range records {
		records[i].Tags = tagMap[records[i].ID]
		if records[i].Tags == nil {
			records[i].Tags = []model.Tag{}
		}
	}
	return records, nil
}

// getRecordTx 在事务内读取单条记录（含标签）。
func getRecordTx(tx *sql.Tx, id string) (*model.Record, error) {
	var r model.Record
	var star, invalid, deleted int
	err := tx.QueryRow(
		`SELECT id, type, path_or_url, title, note, category_id, star, invalid, deleted,
		       favicon, opened_at, open_count, created_at, updated_at
		 FROM records WHERE id = ?`, id,
	).Scan(&r.ID, &r.Type, &r.PathOrURL, &r.Title, &r.Note, &r.CategoryID, &star, &invalid, &deleted,
		&r.Favicon, &r.OpenedAt, &r.OpenCount, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("记录不存在: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询记录失败: %w", err)
	}
	r.Star = star != 0
	r.Invalid = invalid != 0
	r.Deleted = deleted != 0

	rows, err := tx.Query(
		`SELECT t.id, t.name FROM record_tags rt JOIN tags t ON rt.tag_id = t.id WHERE rt.record_id = ? ORDER BY t.name`, id)
	if err != nil {
		return nil, fmt.Errorf("查询标签失败: %w", err)
	}
	defer rows.Close()
	r.Tags = []model.Tag{}
	for rows.Next() {
		var t model.Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("扫描标签失败: %w", err)
		}
		r.Tags = append(r.Tags, t)
	}
	return &r, rows.Err()
}

// boolToInt 将布尔转为 0/1。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
