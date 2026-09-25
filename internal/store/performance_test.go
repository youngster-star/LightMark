package store

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"light_mark/internal/model"
)

// hzTable 一段稳定的中文+英文标题样本，用于批量造数（不含 FTS/SQL 特殊字符）。
var hzTable = [][]byte{
	[]byte("项目文档说明书"), []byte("架构设计方案合集"), []byte("前端技术调研"),
	[]byte("数据库索引优化指南"), []byte("网络故障排查手册"), []byte("部署运维记录"),
	[]byte("性能压测报告汇总"), []byte("安全漏洞修复清单"), []byte("产品需求评审纪要"),
}

// safeText 保留中文字符与字母数字，剔除可能影响 SQL/FTS 语法的字符，
// 此处仅做兜底防御，避免测试数据自身构造出命令式词条。
func safeText(src []byte) []byte {
	return bytes.Map(func(r rune) rune {
		if (r >= 0x4E00 && r <= 0x9FFF) || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, src)
}

// insertBulk 在单事务内批量插入 n 条记录及 FTS 行（性能测试专用，绕开逐条事务开销）。
// 记录按 FTS 可索引的构造写入：标题取中文样本轮换，URL/备注注入稳定词。
func insertBulk(s *Store, n int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := 0; i < n; i++ {
		titleSafe := string(safeText(hzTable[i%len(hzTable)]))
		title := fmt.Sprintf("%s 第%d篇", titleSafe, i)
		id := fmt.Sprintf("perf-%06d", i)
		url := fmt.Sprintf("https://docs.example.com/articles/%d", i)
		if _, err := tx.Exec(
			`INSERT INTO records (id, type, path_or_url, title, note, category_id, star, invalid, deleted, favicon, created_at, updated_at)
			 VALUES (?, 'url', ?, ?, '项目文档与架构设计方案的要点摘录', '', 0, 0, 0, '', ?, ?)`,
			id, url, title, time.Now().UnixMilli(), time.Now().UnixMilli(),
		); err != nil {
			return err
		}
		if i%2 == 0 {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO tags (id, name) VALUES (?, ?)`, "t-tech", "技术"); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO record_tags (record_id, tag_id) VALUES (?, 't-tech')`, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO records_fts (title, note, tags, path_or_url, record_id) VALUES (?, '项目文档与架构设计方案的要点摘录', ?, ?, ?)`,
			title, "技术", url, id,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TestSearchPerformance10k 万条级记录下的搜索耗时验证（PRD 非功能：搜索 < 200ms）。
// 措辞覆盖：中文长词（FTS trigram）、中文短词（LIKE 回退）、过滤组合搜索。
func TestSearchPerformance10k(t *testing.T) {
	s := openTest(t)
	start := time.Now()
	if err := insertBulk(s, 10000); err != nil {
		t.Fatalf("批量造数失败: %v", err)
	}
	t.Logf("插入 10000 条耗时: %v", time.Since(start))

	cases := []struct {
		name string
		q    string
		f    model.ListFilter
	}{
		{"中文长词FTS", "项目文档说明书", model.ListFilter{}},
		{"中文短词LIKE", "文档", model.ListFilter{}},
		{"英文FTS", "articles", model.ListFilter{}},
		{"多词组合", "文档 方案", model.ListFilter{}},
		{"类型过滤+FTS", "项目", model.ListFilter{Type: "url"}},
		{"标签过滤", "", model.ListFilter{Tag: "技术"}},
	}
	const threshold = 200 * time.Millisecond
	for _, c := range cases {
		f := c.f
		f.Query = c.q
		var maxCost time.Duration
		for i := 0; i < 3; i++ {
			st := time.Now()
			recs, err := s.ListRecords(f)
			cost := time.Since(st)
			if err != nil || len(recs) == 0 {
				t.Fatalf("%s 查询异常: %v, %d", c.name, err, len(recs))
			}
			if cost > maxCost {
				maxCost = cost
			}
		}
		if maxCost >= threshold {
			t.Errorf("%s 最差耗时 %v 超过阈值 %v", c.name, maxCost, threshold)
		} else {
			t.Logf("%s 最差耗时: %v (阈值 %v)", c.name, maxCost, threshold)
		}
	}
}
