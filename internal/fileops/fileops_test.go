package fileops

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchURLMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			w.Write([]byte("ico"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html><html><head>
			<title>测试标题</title>
			<link rel="shortcut icon" href="/favicon.ico">
		</head><body></body></html>`))
	}))
	defer srv.Close()

	meta, err := FetchURLMeta(srv.URL)
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if meta.Title != "测试标题" {
		t.Fatalf("标题错误: %q", meta.Title)
	}
	if meta.Favicon != srv.URL+"/favicon.ico" {
		t.Fatalf("favicon 错误: %q", meta.Favicon)
	}
}

func TestFetchURLMetaFallbackTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>no title</body></html>`))
	}))
	defer srv.Close()

	meta, err := FetchURLMeta(srv.URL)
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if meta.Title != srv.URL {
		t.Fatalf("无标题时应回退为 URL: %q", meta.Title)
	}
}

func TestFetchURLMetaRejectsNonHTTP(t *testing.T) {
	if _, err := FetchURLMeta("ftp://example.com/x"); err == nil {
		t.Fatal("非 http 协议应报错")
	}
	if _, err := FetchURLMeta("not a url"); err == nil {
		t.Fatal("非法 URL 应报错")
	}
}

// TestFetchFaviconData 验证 favicon 下载转 data URL、404 与非法地址报错。
func TestFetchFaviconData(t *testing.T) {
	// 1x1 PNG
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.ico":
			w.Header().Set("Content-Type", "image/x-icon")
			w.Write(png)
		case "/404.ico":
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dataURL, err := FetchFaviconData(srv.URL + "/ok.ico")
	if err != nil {
		t.Fatalf("下载 favicon 失败: %v", err)
	}
	if !strings.HasPrefix(dataURL, "data:image/x-icon;base64,") &&
		!strings.HasPrefix(dataURL, "data:image/png;base64,") {
		t.Fatalf("data URL 前缀异常: %q", dataURL[:40])
	}
	encoded := strings.TrimPrefix(dataURL, "data:image/x-icon;base64,")
	encoded = strings.TrimPrefix(encoded, "data:image/png;base64,")
	if got, err := base64.StdEncoding.DecodeString(encoded); err != nil || string(got) != string(png) {
		t.Fatalf("base64 解码不一致: %v", err)
	}

	if _, err := FetchFaviconData(srv.URL + "/404.ico"); err == nil {
		t.Fatal("404 应报错")
	}
	if _, err := FetchFaviconData("ftp://x/y.ico"); err == nil {
		t.Fatal("非 http 地址应报错")
	}
	if _, err := FetchFaviconData("   "); err == nil {
		t.Fatal("空地址应报错")
	}
}
