package fileops

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// PathExists 判断本地路径是否存在（文件或目录均可）。
func PathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// newValidateClient 创建校验专用 HTTP 客户端（短超时，不跟随过多重定向）。
func newValidateClient() *http.Client {
	return &http.Client{Timeout: 6 * time.Second}
}

// URLReachable 判断网址是否可访问（状态码 < 400 视为可达）。
// 优先 HEAD 请求避免下载页面正文；部分站点不支持 HEAD（405/501）时回退 GET。
func URLReachable(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	client := newValidateClient()
	do := func(method string) (int, error) {
		req, err := http.NewRequest(method, rawURL, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		// 读完少量正文以便连接复用，避免悬挂
		_, _ = io.CopyN(io.Discard, resp.Body, 4<<10)
		return resp.StatusCode, nil
	}
	// HEAD 不被支持（405 Method Not Allowed / 501 Not Implemented）时回退 GET
	if code, err := do(http.MethodHead); err == nil && code != http.StatusMethodNotAllowed && code != http.StatusNotImplemented {
		return code < 400
	}
	code, err := do(http.MethodGet)
	if err != nil {
		return false
	}
	return code < 400
}
