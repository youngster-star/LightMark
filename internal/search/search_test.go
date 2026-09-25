package search

import "testing"

func TestTerms(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"项目 文档", []string{"项目", "文档"}},
		{"hello,world", []string{"hello", "world"}},
		{"a b c", []string{"a", "b", "c"}},
		{"项目文档", []string{"项目文档"}},
	}
	for _, c := range cases {
		got := Terms(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("Terms(%q) = %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("Terms(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestBuildMatchExpr(t *testing.T) {
	cases := []struct {
		query     string
		wantExpr  string
		wantShort bool
	}{
		{"项目文档", `"项目文档"`, false},
		{"文档", "", true}, // 2 字短词
		{"abc", `"abc"`, false},
		{"ab", "", true},
		{"hello world", `"hello" AND "world"`, false},
		{"项目 文档", "", true}, // 含短词
	}
	for _, c := range cases {
		expr, short := BuildMatchExpr(c.query)
		if expr != c.wantExpr || short != c.wantShort {
			t.Fatalf("BuildMatchExpr(%q) = (%q, %v), want (%q, %v)", c.query, expr, short, c.wantExpr, c.wantShort)
		}
	}
}

func TestEscapeLike(t *testing.T) {
	if got := EscapeLike("a%b_c"); got != `a\%b\_c` {
		t.Fatalf("EscapeLike = %q", got)
	}
}
