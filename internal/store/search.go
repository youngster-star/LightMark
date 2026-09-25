package store

import (
	"database/sql"
	"fmt"
	"strings"

	"light_mark/internal/model"
	"light_mark/internal/search"
)

// searchRecords 按关键词搜索记录。
// 策略：全部检索词 >=3 字符时走 FTS trigram；含短词时回退 LIKE（覆盖 title/note/path_or_url 与标签）。
// 两种方式都会叠加调用方传入的基础过滤条件。
func (s *Store) searchRecords(f model.ListFilter, baseWhere []string, baseArgs []any) ([]model.Record, error) {
	matchExpr, hasShort := search.BuildMatchExpr(f.Query)

	where := append([]string{}, baseWhere...)
	args := append([]any{}, baseArgs...)

	if matchExpr != "" && !hasShort {
		// 纯长词：FTS trigram 检索，再叠加基础过滤
		where = append(where, `id IN (SELECT record_id FROM records_fts WHERE records_fts MATCH ?)`)
		args = append(args, matchExpr)
	} else {
		// 含短词：LIKE 子串匹配（title/note/path_or_url + 标签名）
		likeClause, likeArgs := buildLikeClause(f.Query)
		if likeClause == "" {
			likeClause = "1=0"
		}
		where = append(where, "("+likeClause+")")
		args = append(args, likeArgs...)
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

// queryRecords 组装并执行带排序的记录查询。
func (s *Store) queryRecords(where []string, args []any, f model.ListFilter) (*sql.Rows, error) {
	sortCol, ok := validSortColumns[f.SortBy]
	if !ok {
		sortCol = "updated_at"
	}
	order := "ASC"
	if f.SortDesc || f.SortBy == "" {
		order = "DESC"
	}
	q := `SELECT id, type, path_or_url, title, note, category_id, star, invalid, deleted,
		     favicon, opened_at, open_count, created_at, updated_at
		FROM records WHERE ` + strings.Join(where, " AND ") + ` ORDER BY ` + sortCol + ` ` + order
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("查询记录失败: %w", err)
	}
	return rows, nil
}

// rebuildFTS 全量重建 FTS 索引，用于标签重命名/删除等会改变标签文本的场景。
func rebuildFTS(tx *sql.Tx) error {
	if _, err := tx.Exec(`DELETE FROM records_fts`); err != nil {
		return fmt.Errorf("清空索引失败: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO records_fts (title, note, tags, path_or_url, record_id)
		SELECT r.title, r.note,
			COALESCE(GROUP_CONCAT(t.name, ' '), ''),
			r.path_or_url, r.id
		FROM records r
		LEFT JOIN record_tags rt ON rt.record_id = r.id
		LEFT JOIN tags t ON t.id = rt.tag_id
		GROUP BY r.id
	`); err != nil {
		return fmt.Errorf("重建索引失败: %w", err)
	}
	return nil
}

// buildLikeClause 生成覆盖 title/note/path_or_url 与标签名的 LIKE 子句。
func buildLikeClause(query string) (string, []any) {
	terms := search.Terms(query)
	if len(terms) == 0 {
		return "", nil
	}
	conds := make([]string, 0, len(terms))
	args := make([]any, 0, len(terms)*4)
	for _, t := range terms {
		like := "%" + search.EscapeLike(t) + "%"
		conds = append(conds,
			`(title LIKE ? ESCAPE '\' OR note LIKE ? ESCAPE '\' OR path_or_url LIKE ? ESCAPE '\'`+
				` OR id IN (SELECT rt.record_id FROM record_tags rt JOIN tags tg ON rt.tag_id = tg.id WHERE tg.name LIKE ? ESCAPE '\'))`)
		args = append(args, like, like, like, like)
	}
	return strings.Join(conds, " AND "), args
}
