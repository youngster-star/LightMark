package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"light_mark/internal/model"
)

// openTest 在临时目录打开一个测试用 Store。
func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开存储失败: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGetRecord(t *testing.T) {
	s := openTest(t)
	in := model.RecordInput{
		Type:      string(model.TypeFile),
		PathOrURL: `C:\notes\a.md`,
		Title:     "我的笔记",
		Note:      "关于项目的备注",
		Tags:      []string{"工作", "学习"},
	}
	rec, err := s.CreateRecord(in)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if rec.ID == "" {
		t.Fatal("ID 为空")
	}
	if rec.Type != model.TypeFile {
		t.Fatalf("类型错误: %v", rec.Type)
	}
	if len(rec.Tags) != 2 {
		t.Fatalf("标签数量错误: %v", rec.Tags)
	}

	got, err := s.GetRecord(rec.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.Title != "我的笔记" {
		t.Fatalf("标题错误: %q", got.Title)
	}
	if len(got.Tags) != 2 {
		t.Fatalf("读取标签数量错误: %v", got.Tags)
	}
}

func TestValidateRecordInput(t *testing.T) {
	if _, _, err := validateRecordInput(model.RecordInput{Title: "", PathOrURL: "x", Type: "file"}); err == nil {
		t.Fatal("空标题应报错")
	}
	if _, _, err := validateRecordInput(model.RecordInput{Title: "x", PathOrURL: "", Type: "file"}); err == nil {
		t.Fatal("空路径应报错")
	}
	if _, _, err := validateRecordInput(model.RecordInput{Title: "x", PathOrURL: "x", Type: "bad"}); err == nil {
		t.Fatal("非法类型应报错")
	}
}

func TestUpdateRecord(t *testing.T) {
	s := openTest(t)
	rec, _ := s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\a.md`, Title: "旧标题", Tags: []string{"旧"}})

	upd, err := s.UpdateRecord(model.RecordInput{
		ID:        rec.ID,
		Type:      "file",
		PathOrURL: `C:\b.md`,
		Title:     "新标题",
		Note:      "更新备注",
		Tags:      []string{"新标签"},
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if upd.Title != "新标题" || upd.PathOrURL != `C:\b.md` {
		t.Fatalf("更新结果错误: %+v", upd)
	}
	if len(upd.Tags) != 1 || upd.Tags[0].Name != "新标签" {
		t.Fatalf("标签更新错误: %v", upd.Tags)
	}
}

func TestRecycleBin(t *testing.T) {
	s := openTest(t)
	rec, _ := s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\a.md`, Title: "t"})

	if err := s.SoftDeleteRecord(rec.ID); err != nil {
		t.Fatalf("软删除失败: %v", err)
	}
	// 删除后默认列表不应出现
	list, err := s.ListRecords(model.ListFilter{})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("软删除后仍可见: %v", list)
	}
	// 回收站应出现
	trash, _ := s.ListRecords(model.ListFilter{Trashed: true})
	if len(trash) != 1 {
		t.Fatalf("回收站数量错误: %d", len(trash))
	}
	// 恢复
	if err := s.RestoreRecord(rec.ID); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	list, _ = s.ListRecords(model.ListFilter{})
	if len(list) != 1 {
		t.Fatalf("恢复后数量错误: %d", len(list))
	}
	// 彻底删除
	if err := s.PurgeRecord(rec.ID); err != nil {
		t.Fatalf("彻底删除失败: %v", err)
	}
	if _, err := s.GetRecord(rec.ID); err == nil {
		t.Fatal("彻底删除后仍可读取")
	}
}

func TestSearchLongTerm(t *testing.T) {
	s := openTest(t)
	s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\a.md`, Title: "项目文档说明", Note: ""})
	s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\b.md`, Title: "无关内容", Note: ""})

	// 长词走 FTS trigram
	res, err := s.ListRecords(model.ListFilter{Query: "项目文档"})
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(res) != 1 || res[0].Title != "项目文档说明" {
		t.Fatalf("长词搜索结果错误: %v", res)
	}
}

func TestSearchShortTerm(t *testing.T) {
	s := openTest(t)
	s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\a.md`, Title: "项目文档", Note: ""})
	s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\b.md`, Title: "其他", Note: ""})

	// 短词（2 字）走 LIKE 回退
	res, err := s.ListRecords(model.ListFilter{Query: "文档"})
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(res) != 1 || res[0].Title != "项目文档" {
		t.Fatalf("短词搜索结果错误: %v", res)
	}
}

func TestSearchByTag(t *testing.T) {
	s := openTest(t)
	s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://example.com", Title: "网页一", Tags: []string{"技术"}})
	s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://example.org", Title: "网页二", Tags: []string{"生活"}})

	// 标签名 3 字走 FTS
	res, err := s.ListRecords(model.ListFilter{Query: "技术"})
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(res) != 1 || res[0].Title != "网页一" {
		t.Fatalf("标签搜索结果错误: %v", res)
	}
}

func TestListFilterByTypeAndStar(t *testing.T) {
	s := openTest(t)
	s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\a.md`, Title: "文件", Star: true})
	s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://x.com", Title: "网页"})

	res, err := s.ListRecords(model.ListFilter{Type: "url"})
	if err != nil || len(res) != 1 || res[0].Title != "网页" {
		t.Fatalf("类型筛选错误: %v", res)
	}
	res, _ = s.ListRecords(model.ListFilter{StarOnly: true})
	if len(res) != 1 || res[0].Title != "文件" {
		t.Fatalf("星标筛选错误: %v", res)
	}
}

func TestCategoriesCRUD(t *testing.T) {
	s := openTest(t)
	c, err := s.CreateCategory("工作", "")
	if err != nil || c.Name != "工作" {
		t.Fatalf("创建分类失败: %v", err)
	}
	// 更新
	if _, err := s.UpdateCategory(c.ID, "学习", "", 1); err != nil {
		t.Fatalf("更新分类失败: %v", err)
	}
	// 删除（有记录引用时记录分类置空）
	rec, _ := s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\a.md`, Title: "t", CategoryID: c.ID})
	if err := s.DeleteCategory(c.ID); err != nil {
		t.Fatalf("删除分类失败: %v", err)
	}
	got, _ := s.GetRecord(rec.ID)
	if got.CategoryID != "" {
		t.Fatalf("删除分类后记录分类未清空: %q", got.CategoryID)
	}
}

func TestTagsCRUD(t *testing.T) {
	s := openTest(t)
	tag, err := s.CreateTag("工作")
	if err != nil || tag.Name != "工作" {
		t.Fatalf("创建标签失败: %v", err)
	}
	// 同名应返回已存在
	tag2, _ := s.CreateTag("工作")
	if tag2.ID != tag.ID {
		t.Fatalf("同名标签应复用: %v vs %v", tag2.ID, tag.ID)
	}
	if err := s.RenameTag(tag.ID, "工作事项"); err != nil {
		t.Fatalf("重命名失败: %v", err)
	}
	if err := s.DeleteTag(tag.ID); err != nil {
		t.Fatalf("删除标签失败: %v", err)
	}
}

func TestTagRenameUpdatesSearch(t *testing.T) {
	s := openTest(t)
	rec, _ := s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\a.md`, Title: "t", Tags: []string{"旧标签"}})
	tagID := rec.Tags[0].ID

	if err := s.RenameTag(tagID, "新标签名"); err != nil {
		t.Fatalf("重命名失败: %v", err)
	}
	// 用新标签名可搜到
	res, _ := s.ListRecords(model.ListFilter{Query: "新标签名"})
	if len(res) != 1 {
		t.Fatalf("重命名后搜索失败: %v", res)
	}
	// 旧标签名搜不到
	res, _ = s.ListRecords(model.ListFilter{Query: "旧标签"})
	if len(res) != 0 {
		t.Fatalf("旧标签仍可搜到: %v", res)
	}
}

func TestDatabaseFileLocation(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	defer s.Close()
	// 数据库文件应位于指定目录
	if !fileExists(filepath.Join(dir, "lightmark.db")) {
		t.Fatal("数据库文件未创建于指定目录")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestListRecordsEmptyReturnsNonNil 空库查询必须返回空切片（非 nil）：
// nil 会被 JSON 序列化成 null，前端 records.length 抛 TypeError 导致整页白屏。
func TestListRecordsEmptyReturnsNonNil(t *testing.T) {
	s := openTest(t)

	recs, err := s.ListRecords(model.ListFilter{})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if recs == nil {
		t.Fatal("空库 ListRecords 返回了 nil，应返回空切片")
	}
	if len(recs) != 0 {
		t.Fatalf("空库记录数应为 0: %v", recs)
	}

	// 带搜索词的路径同样不允许返回 nil
	recs, err = s.ListRecords(model.ListFilter{Query: "不存在"})
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if recs == nil {
		t.Fatal("空搜索结果返回了 nil，应返回空切片")
	}
}

// TestRecordFaviconAndTouchOpen 覆盖 favicon 存取与最近访问统计。
func TestRecordFaviconAndTouchOpen(t *testing.T) {
	s := openTest(t)
	rec, err := s.CreateRecord(model.RecordInput{
		Type:      string(model.TypeURL),
		PathOrURL: "https://example.com",
		Title:     "示例网页",
		Favicon:   "data:image/png;base64,AAAA",
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if rec.Favicon != "data:image/png;base64,AAAA" {
		t.Fatalf("favicon 未写入: %q", rec.Favicon)
	}

	// 打开两次：opened_at 刷新、open_count=2，updated_at 不受影响
	before := rec.UpdatedAt
	if err := s.TouchRecordOpen(rec.ID); err != nil {
		t.Fatalf("TouchRecordOpen 失败: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := s.TouchRecordOpen(rec.ID); err != nil {
		t.Fatalf("TouchRecordOpen 失败: %v", err)
	}
	got, err := s.GetRecord(rec.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.OpenCount != 2 {
		t.Fatalf("打开次数应为 2: %d", got.OpenCount)
	}
	if got.OpenedAt == 0 {
		t.Fatal("opened_at 应已刷新")
	}
	if got.UpdatedAt != before {
		t.Fatal("打开不应修改 updated_at")
	}

	// 更新记录时 favicon 传空应保持原值，非空应覆盖
	if _, err := s.UpdateRecord(model.RecordInput{
		ID: rec.ID, Type: string(model.TypeURL), PathOrURL: "https://example.com",
		Title: "示例网页", Favicon: "",
	}); err != nil {
		t.Fatalf("空 favicon 更新失败: %v", err)
	}
	got, _ = s.GetRecord(rec.ID)
	if got.Favicon != "data:image/png;base64,AAAA" {
		t.Fatalf("空 favicon 更新应保持原值: %q", got.Favicon)
	}
	if _, err := s.UpdateRecord(model.RecordInput{
		ID: rec.ID, Type: string(model.TypeURL), PathOrURL: "https://example.com",
		Title: "示例网页", Favicon: "data:image/png;base64,BBBB",
	}); err != nil {
		t.Fatalf("带 favicon 更新失败: %v", err)
	}
	got, _ = s.GetRecord(rec.ID)
	if got.Favicon != "data:image/png;base64,BBBB" {
		t.Fatalf("favicon 应已覆盖: %q", got.Favicon)
	}
}

// TestListRecordsSortByOpenedAt 按 opened_at 排序时未打开记录应排在末尾。
func TestListRecordsSortByOpenedAt(t *testing.T) {
	s := openTest(t)
	a, _ := s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://a.com", Title: "A"})
	b, _ := s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://b.com", Title: "B"})
	if err := s.TouchRecordOpen(b.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := s.TouchRecordOpen(a.ID); err != nil {
		t.Fatal(err)
	}
	// 创建一条从未打开的
	s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://c.com", Title: "C"})

	recs, err := s.ListRecords(model.ListFilter{SortBy: "opened_at", SortDesc: true})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("记录数应为 3: %d", len(recs))
	}
	if recs[0].ID != a.ID || recs[1].ID != b.ID {
		t.Fatalf("打开时间新→旧排序异常: %s,%s", recs[0].Title, recs[1].Title)
	}
	if recs[2].ID == a.ID || recs[2].ID == b.ID {
		t.Fatal("未打开记录应排在末尾")
	}
}
