package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"light_mark/internal/model"
)

// TestMergeTag 覆盖标签合并：关联迁移、同名去重、源标签删除、FTS 按新名可搜。
func TestMergeTag(t *testing.T) {
	s := openTest(t)
	// 记录 A 同时挂「前端」「Vue3」，记录 B 只挂「前端」
	a, _ := s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://a.com", Title: "记录A", Tags: []string{"前端", "Vue3"}})
	b, _ := s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://b.com", Title: "记录B", Tags: []string{"前端"}})
	// 拿到「前端」的标签 ID
	frontendID, vue3ID := "", ""
	tags, _ := s.ListTags()
	for _, tg := range tags {
		switch tg.Name {
		case "前端":
			frontendID = tg.ID
		case "Vue3":
			vue3ID = tg.ID
		}
	}
	if frontendID == "" || vue3ID == "" {
		t.Fatalf("标签未创建完整: %+v", tags)
	}

	// 合并「Vue3」→「前端」：记录 A 最终只挂一个「前端」
	if err := s.MergeTag(vue3ID, frontendID); err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	tags, _ = s.ListTags()
	if len(tags) != 1 || tags[0].Name != "前端" {
		t.Fatalf("合并后标签应只剩「前端」: %+v", tags)
	}
	// 记录标签已迁移且去重
	gotA, _ := s.GetRecord(a.ID)
	if len(gotA.Tags) != 1 || gotA.Tags[0].Name != "前端" {
		t.Fatalf("记录A 标签应去重为单个「前端」: %+v", gotA.Tags)
	}
	gotB, _ := s.GetRecord(b.ID)
	if len(gotB.Tags) != 1 {
		t.Fatalf("记录B 标签异常: %+v", gotB.Tags)
	}
	// 合并后按现名可搜到（FTS 已重建）
	res, _ := s.ListRecords(model.ListFilter{Query: "Vue3"})
	if len(res) != 0 {
		t.Fatalf("旧标签名不应再命中: %d", len(res))
	}
	res, _ = s.ListRecords(model.ListFilter{Query: "前端"})
	if len(res) != 2 {
		t.Fatalf("新标签名应命中两条: %d", len(res))
	}

	// 非法输入
	if err := s.MergeTag("", ""); err == nil {
		t.Fatal("空 ID 应报错")
	}
	if err := s.MergeTag(frontendID, frontendID); err == nil {
		t.Fatal("自合并应报错")
	}
	if err := s.MergeTag("不存在", frontendID); err == nil {
		t.Fatal("源标签不存在应报错")
	}
}

// TestFindDuplicate 覆盖录入查重：精确匹配、排除自身、回收站不算重复。
func TestFindDuplicate(t *testing.T) {
	s := openTest(t)
	s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://x.com", Title: "已有"})

	dup, err := s.FindDuplicate("https://x.com", "")
	if err != nil || dup == nil {
		t.Fatalf("应检出重复: %v, %v", dup, err)
	}
	if dup.Title != "已有" {
		t.Fatalf("重复记录错误: %+v", dup)
	}
	// 不同 URL 无重复
	if dup, _ = s.FindDuplicate("https://y.com", ""); dup != nil {
		t.Fatal("不同网址不应检出重复")
	}
	// 排除自身（编辑场景）
	self, _ := s.GetRecord(dupSelfID(s, "https://x.com"))
	if dup, _ = s.FindDuplicate("https://x.com", self.ID); dup != nil {
		t.Fatal("排除自身后不应检出")
	}
	// 回收站中的记录不算重复
	id := self.ID
	s.SoftDeleteRecord(id)
	if dup, _ = s.FindDuplicate("https://x.com", ""); dup != nil {
		t.Fatal("回收站记录不应算重复")
	}
}

// dupSelfID 辅助：取指定 URL 记录的 ID。
func dupSelfID(s *Store, url string) string {
	recs, _ := s.ListRecords(model.ListFilter{Query: "已有"})
	for _, r := range recs {
		if r.PathOrURL == url {
			return r.ID
		}
	}
	return ""
}

// TestRestoreRecordSnapshot 覆盖快照还原：全字段还原、生效于撤销场景。
func TestRestoreRecordSnapshot(t *testing.T) {
	s := openTest(t)
	orig, _ := s.CreateRecord(model.RecordInput{
		Type: "url", PathOrURL: "https://o.com", Title: "原标题", Note: "原备注",
		Tags: []string{"旧标签"},
	})
	oldSnap, _ := s.GetRecord(orig.ID)

	// 内容改得面目全非
	s.UpdateRecord(model.RecordInput{
		ID: orig.ID, Type: "url", PathOrURL: "https://changed.com", Title: "新标题",
		Note: "新备注", Tags: []string{"新标签"}, Favicon: "data:image/png;base64,ZZZZ",
	})

	// 撤销 = 还原旧快照
	if err := s.RestoreRecordSnapshot(oldSnap); err != nil {
		t.Fatalf("还原快照失败: %v", err)
	}
	got, _ := s.GetRecord(orig.ID)
	if got.Title != "原标题" || got.Note != "原备注" || got.PathOrURL != "https://o.com" {
		t.Fatalf("字段未还原: %+v", got)
	}
	if got.Favicon != "" {
		t.Fatalf("favicon 应还原为空: %q", got.Favicon)
	}
	if len(got.Tags) != 1 || got.Tags[0].Name != "旧标签" {
		t.Fatalf("标签未还原: %+v", got.Tags)
	}
	// 还原后按旧标题可搜到、新标题搜不到
	if res, _ := s.ListRecords(model.ListFilter{Query: "新标题"}); len(res) != 0 {
		t.Fatal("新标题不应再命中")
	}
	if res, _ := s.ListRecords(model.ListFilter{Query: "原标题"}); len(res) != 1 {
		t.Fatal("旧标题应可搜到（索引已还原）")
	}

	// 还原一个 deleted=1 的快照 → 索引移除、正常列表不可见
	oldSnap.Deleted = true
	if err := s.RestoreRecordSnapshot(oldSnap); err != nil {
		t.Fatalf("还原删除态快照失败: %v", err)
	}
	if res, _ := s.ListRecords(model.ListFilter{}); len(res) != 0 {
		t.Fatal("删除态快照应从正常列表消失")
	}

	// 彻底删除后仍可按快照整行重建
	s.RestoreRecordSnapshot(oldSnap) // 恢复正常
	s.PurgeRecord(orig.ID)
	if err := s.RestoreRecordSnapshot(oldSnap); err != nil {
		t.Fatalf("快照重建行失败: %v", err)
	}
	if _, err := s.GetRecord(orig.ID); err != nil {
		t.Fatalf("重建后应可读取: %v", err)
	}
}

// TestSnapshotTo 验证 VACUUM INTO 快照生成与覆盖写入。
func TestSnapshotTo(t *testing.T) {
	s := openTest(t)
	s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\a.md`, Title: "快照测试"})

	// 快照写入独立目录并命名为 lightmark.db，之后直接用 Open 打开该目录验证
	snapDir := filepath.Join(t.TempDir(), "snapdir")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("创建快照目录失败: %v", err)
	}
	snapPath := filepath.Join(snapDir, "lightmark.db")
	if err := s.SnapshotTo(snapPath); err != nil {
		t.Fatalf("快照失败: %v", err)
	}
	// 覆盖写入应成功（内部先移除旧文件）
	if err := s.SnapshotTo(snapPath); err != nil {
		t.Fatalf("覆盖快照失败: %v", err)
	}

	// 打开快照验证内容完整
	s2, err := Open(snapDir)
	if err != nil {
		t.Fatalf("打开快照失败: %v", err)
	}
	defer s2.Close()
	recs, _ := s2.ListRecords(model.ListFilter{})
	if len(recs) != 1 || recs[0].Title != "快照测试" {
		t.Fatalf("快照内容不完整: %v", recs)
	}
}

// TestFindDuplicateNormalized 覆盖查重规范化：大小写不敏感与尾部斜杠。
func TestFindDuplicateNormalized(t *testing.T) {
	s := openTest(t)
	s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://x.com/a", Title: "网页"})
	s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\Docs\A.md`, Title: "文件"})

	// 大小写不同仍应检出（域名/Windows 路径不区分大小写）
	if dup, _ := s.FindDuplicate("https://X.COM/a", ""); dup == nil {
		t.Fatal("大小写不同的网址应检出重复")
	}
	if dup, _ := s.FindDuplicate(`c:\docs\a.md`, ""); dup == nil {
		t.Fatal("大小写不同的路径应检出重复")
	}
	// 尾部斜杠在入库时已规范化，查询同值应一致
	if dup, _ := s.FindDuplicate("https://x.com/a/", ""); dup == nil {
		t.Fatal("尾部斜杠查询应命中已规范化记录")
	}
}

// TestRestoreRecordSnapshotWithDeletedTag 撤销场景：快照中的标签已被删除，
// 还原仍应成功（跳过失效标签），不因外键约束失败。
func TestRestoreRecordSnapshotWithDeletedTag(t *testing.T) {
	s := openTest(t)
	orig, _ := s.CreateRecord(model.RecordInput{
		Type: "url", PathOrURL: "https://o.com", Title: "标题", Tags: []string{"临时标签"},
	})
	oldSnap, _ := s.GetRecord(orig.ID)

	// 删除快照引用的标签
	tags, _ := s.ListTags()
	if len(tags) != 1 {
		t.Fatalf("标签数量异常: %+v", tags)
	}
	if err := s.DeleteTag(tags[0].ID); err != nil {
		t.Fatalf("删除标签失败: %v", err)
	}

	// 还原含已删标签的快照：应成功且不再恢复该标签
	if err := s.RestoreRecordSnapshot(oldSnap); err != nil {
		t.Fatalf("含已删标签的快照还原应成功: %v", err)
	}
	got, _ := s.GetRecord(orig.ID)
	if len(got.Tags) != 0 {
		t.Fatalf("已删标签不应被恢复: %+v", got.Tags)
	}
}

// TestRenameTagDuplicate 覆盖重命名重名校验。
func TestRenameTagDuplicate(t *testing.T) {
	s := openTest(t)
	s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://a.com", Title: "A", Tags: []string{"前端"}})
	s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://b.com", Title: "B", Tags: []string{"后端"}})
	tags, _ := s.ListTags()
	var frontID, backID string
	for _, tg := range tags {
		if tg.Name == "前端" {
			frontID = tg.ID
		} else if tg.Name == "后端" {
			backID = tg.ID
		}
	}
	// 重命名为已有名应报明确错误
	if err := s.RenameTag(backID, "前端"); err == nil {
		t.Fatal("重名重命名应报错")
	} else if !strings.Contains(err.Error(), "同名") {
		t.Fatalf("错误信息应友好提示同名: %v", err)
	}
	// 重命名为自身当前名（id 排除自身）应成功
	if err := s.RenameTag(frontID, "前端"); err != nil {
		t.Fatalf("同名不变重命名应成功: %v", err)
	}
}

// TestNormalizeInput 覆盖录入规范化：URL 尾斜杠裁剪、文件路径 Clean。
func TestNormalizeInput(t *testing.T) {
	s := openTest(t)
	u, _ := s.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://x.com/a/", Title: "网页"})
	if u.PathOrURL != "https://x.com/a" {
		t.Fatalf("URL 尾斜杠应被去除: %q", u.PathOrURL)
	}
	f, _ := s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\docs\.\a.md`, Title: "文件"})
	if f.PathOrURL != `C:\docs\a.md` {
		t.Fatalf("文件路径应被 Clean: %q", f.PathOrURL)
	}
}
