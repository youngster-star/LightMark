// Package search 提供 FTS5 查询构建与中文搜索辅助。
// 由于 FTS5 的 trigram 分词器对 1~2 字符的查询词无法命中，
// 短词需回退到 LIKE 子串匹配，保证中文搜索的正确性。
package search

import (
	"strings"
	"unicode"
)

// minTrigramLen trigram 分词器能命中的最短词长（按字符数）。
const minTrigramLen = 3

// Terms 将输入按空白与标点切分为多个检索词，并过滤空串。
func Terms(query string) []string {
	f := strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	out := make([]string, 0, len(f))
	for _, t := range f {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// BuildMatchExpr 将查询词构造为 FTS5 MATCH 表达式。
// 返回 match 表达式；若存在短词（<3 字符）则 hasShort 为 true，
// 表示需要额外使用 LIKE 回退补齐结果。
func BuildMatchExpr(query string) (expr string, hasShort bool) {
	terms := Terms(query)
	var ftsTerms []string
	for _, t := range terms {
		if len([]rune(t)) < minTrigramLen {
			hasShort = true
			continue
		}
		ftsTerms = append(ftsTerms, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	if len(ftsTerms) == 0 {
		return "", hasShort
	}
	return strings.Join(ftsTerms, " AND "), hasShort
}

// EscapeLike 转义 LIKE 通配符，防止用户输入被当作通配符。
func EscapeLike(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return r.Replace(s)
}
