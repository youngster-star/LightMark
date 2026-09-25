package fileops

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPathExists(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.md")
	if PathExists(f) {
		t.Fatal("不存在文件应返回 false")
	}
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !PathExists(f) {
		t.Fatal("已存在文件应返回 true")
	}
	if PathExists("") {
		t.Fatal("空路径应返回 false")
	}
}

func TestURLReachable(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()

	nf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer nf.Close()

	if !URLReachable(ok.URL) {
		t.Fatal("正常站点应可达")
	}
	if URLReachable(nf.URL) {
		t.Fatal("404 站点应不可达")
	}
	if URLReachable("not a url") {
		t.Fatal("非法 URL 应不可达")
	}
}

// TestURLReachableHeadFallback HEAD 不被支持（405）时应回退 GET 判定可达。
func TestURLReachableHeadFallback(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if !URLReachable(srv.URL) {
		t.Fatal("HEAD 被拒但 GET 正常的站点应可达")
	}
	if len(methods) < 2 || methods[0] != http.MethodHead || methods[1] != http.MethodGet {
		t.Fatalf("应先 HEAD 再回退 GET: %v", methods)
	}
}
