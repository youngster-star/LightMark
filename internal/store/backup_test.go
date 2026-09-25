package store

import (
	"testing"

	"light_mark/internal/model"
)

func TestExportImport(t *testing.T) {
	src := openTest(t)
	src.CreateCategory("工作", "")
	src.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\a.md`, Title: "记录一", Tags: []string{"标签A"}})
	src.CreateRecord(model.RecordInput{Type: "url", PathOrURL: "https://example.com", Title: "记录二", Tags: []string{"标签B"}})

	backup, err := src.Export()
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if len(backup.Records) != 2 || len(backup.Tags) != 2 || len(backup.Categories) != 1 {
		t.Fatalf("导出数据不完整: %+v", backup)
	}

	// 导入到全新库
	dst := openTest(t)
	res, err := dst.Import(backup)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if res.Records != 2 || res.Tags != 2 || res.Categories != 1 {
		t.Fatalf("导入数量错误: %+v", res)
	}
	// 再次导入应去重
	res2, err := dst.Import(backup)
	if err != nil {
		t.Fatalf("二次导入失败: %v", err)
	}
	if res2.Records != 0 {
		t.Fatalf("重复导入应去重: %+v", res2)
	}
	// 导入后搜索可用
	list, _ := dst.ListRecords(model.ListFilter{Query: "记录一"})
	if len(list) != 1 {
		t.Fatalf("导入后搜索失败: %v", list)
	}
}

func TestExportImportRoundTripDeleted(t *testing.T) {
	src := openTest(t)
	rec, _ := src.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\x.md`, Title: "待删"})
	src.SoftDeleteRecord(rec.ID)

	backup, _ := src.Export()
	// 备份应包含回收站记录
	found := false
	for _, r := range backup.Records {
		if r.ID == rec.ID && r.Deleted {
			found = true
		}
	}
	if !found {
		t.Fatal("备份未包含回收站记录")
	}

	dst := openTest(t)
	if _, err := dst.Import(backup); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	trash, _ := dst.ListRecords(model.ListFilter{Trashed: true})
	if len(trash) != 1 {
		t.Fatalf("回收站导入数量错误: %d", len(trash))
	}
}

func TestEmptyTrash(t *testing.T) {
	s := openTest(t)
	rec, _ := s.CreateRecord(model.RecordInput{Type: "file", PathOrURL: `C:\a.md`, Title: "t"})
	s.SoftDeleteRecord(rec.ID)

	if err := s.EmptyTrash(); err != nil {
		t.Fatalf("清空回收站失败: %v", err)
	}
	if _, err := s.GetRecord(rec.ID); err == nil {
		t.Fatal("清空后记录仍存在")
	}
	trash, _ := s.ListRecords(model.ListFilter{Trashed: true})
	if len(trash) != 0 {
		t.Fatalf("清空后回收站仍有记录: %v", trash)
	}
}

// TestImportRejectsUnknownVersion 非支持版本的备份应被拒绝导入。
func TestImportRejectsUnknownVersion(t *testing.T) {
	s := openTest(t)
	backup, _ := s.Export()
	backup.Version = 99
	if _, err := s.Import(backup); err == nil {
		t.Fatal("未知备份版本应报错")
	}
}
