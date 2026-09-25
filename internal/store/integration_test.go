package store

import (
	"testing"
	"time"

	"light_mark/internal/model"
)

// TestEndToEndUserJourney 模拟完整用户旅程的集成测试：
// 分类/标签 → 录入（中文标题+favicon+星标）→ 中文搜索（长词/短词）→
// 筛选/排序 → 打开统计 → 回收站恢复 → 失效标记 → 导出/导入往返。
func TestEndToEndUserJourney(t *testing.T) {
	s := openTest(t)

	// 1. 建分类与标签
	cat, err := s.CreateCategory("前端技术", "")
	if err != nil {
		t.Fatalf("创建分类失败: %v", err)
	}

	// 2. 录入网页记录（中文标题、星标、favicon、分类、双标签）
	vue, err := s.CreateRecord(model.RecordInput{
		Type:       "url",
		PathOrURL:  "https://example.com/vue-guide",
		Title:      "Vue3 组合式 API 指南",
		Note:       "值得回看的组合式 API 教程",
		CategoryID: cat.ID,
		Star:       true,
		Tags:       []string{"前端", "Vue3"},
		Favicon:    "data:image/png;base64,AAAA",
	})
	if err != nil {
		t.Fatalf("录入网页失败: %v", err)
	}
	if vue.ID == "" || vue.Favicon == "" || len(vue.Tags) != 2 {
		t.Fatalf("网页录入字段缺失: %+v", vue)
	}

	// 3. 录入文件记录
	doc, err := s.CreateRecord(model.RecordInput{
		Type:      "file",
		PathOrURL: `C:\docs\设计文档.md`,
		Title:     "设计文档",
		Note:      "系统架构设计说明",
		Tags:      []string{"文档"},
	})
	if err != nil {
		t.Fatalf("录入文件失败: %v", err)
	}

	// 4. 中文搜索：长词走 FTS trigram
	res, err := s.ListRecords(model.ListFilter{Query: "组合式"})
	if err != nil || len(res) != 1 || res[0].ID != vue.ID {
		t.Fatalf("长词搜索错误: %v, %v", res, err)
	}
	// 短词走 LIKE 回退：仅文件标题「设计文档」命中
	res, err = s.ListRecords(model.ListFilter{Query: "文档"})
	if err != nil {
		t.Fatalf("短词搜索失败: %v", err)
	}
	if len(res) != 1 || res[0].ID != doc.ID {
		t.Fatalf("短词搜索结果错误: %d", len(res))
	}
	// 短词命中备注：网页备注含「回看」
	res, err = s.ListRecords(model.ListFilter{Query: "回看"})
	if err != nil || len(res) != 1 || res[0].ID != vue.ID {
		t.Fatalf("备注短词搜索错误: %v, %v", res, err)
	}

	// 5. 筛选：类型 / 分类 / 星标
	if res, _ = s.ListRecords(model.ListFilter{Type: "url"}); len(res) != 1 {
		t.Fatalf("类型筛选错误: %d", len(res))
	}
	if res, _ = s.ListRecords(model.ListFilter{CategoryID: cat.ID}); len(res) != 1 || res[0].ID != vue.ID {
		t.Fatalf("分类筛选错误: %d", len(res))
	}
	if res, _ = s.ListRecords(model.ListFilter{StarOnly: true}); len(res) != 1 || res[0].ID != vue.ID {
		t.Fatalf("星标筛选错误: %d", len(res))
	}

	// 6. 打开统计：两次打开后 open_count=2、opened_at 刷新，updated_at 不变
	beforeUpdated := vue.UpdatedAt
	time.Sleep(15 * time.Millisecond)
	if err := s.TouchRecordOpen(vue.ID); err != nil {
		t.Fatalf("打开统计失败: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := s.TouchRecordOpen(vue.ID); err != nil {
		t.Fatalf("打开统计失败: %v", err)
	}
	got, err := s.GetRecord(vue.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.OpenCount != 2 || got.OpenedAt == 0 || got.UpdatedAt != beforeUpdated {
		t.Fatalf("打开统计异常: count=%d opened=%d", got.OpenCount, got.OpenedAt)
	}

	// 7. 回收站：软删 → 正常列表不可见 → 回收站可见 → 恢复
	if err := s.SoftDeleteRecord(doc.ID); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	if res, _ = s.ListRecords(model.ListFilter{}); len(res) != 1 {
		t.Fatalf("软删后正常列表应剩 1 条: %d", len(res))
	}
	if res, _ = s.ListRecords(model.ListFilter{Trashed: true}); len(res) != 1 || res[0].ID != doc.ID {
		t.Fatalf("回收站应可见 1 条: %d", len(res))
	}
	if err := s.RestoreRecord(doc.ID); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if res, _ = s.ListRecords(model.ListFilter{}); len(res) != 2 {
		t.Fatalf("恢复后应回到 2 条: %d", len(res))
	}

	// 8. 失效标记：文件路径删除后标记失效
	if err := s.SetRecordInvalid(doc.ID, true); err != nil {
		t.Fatalf("失效标记失败: %v", err)
	}
	got, _ = s.GetRecord(doc.ID)
	if !got.Invalid {
		t.Fatal("记录应已标记失效")
	}

	// 9. 备份往返：导出 → 新库导入 → 数据一致
	b, err := s.Export()
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	s2 := openTest(t)
	ir, err := s2.Import(b)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if ir.Records != 2 || ir.Categories != 1 {
		t.Fatalf("导入计数错误: %+v", ir)
	}
	got2, err := s2.GetRecord(vue.ID)
	if err != nil {
		t.Fatalf("导入后读取失败: %v", err)
	}
	if got2.Title != vue.Title || got2.Favicon != vue.Favicon || !got2.Star || got2.OpenCount != 2 {
		t.Fatalf("导入后数据不一致: %+v", got2)
	}
	if len(got2.Tags) != 2 {
		t.Fatalf("导入后标签数量错误: %d", len(got2.Tags))
	}
}
