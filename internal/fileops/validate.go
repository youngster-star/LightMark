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

// PathKind 判断本地路径类型：真实存在的目录返回 "folder"，其余返回 "file"。
// 用于拖拽录入时区分文件夹与文件（前端无法直接读取文件系统）。
func PathKind(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return "folder"
	}
	return "file"
}

// newValidateClient 创建校验专用 HTTP 客户端。
// 超时放宽到 10s：部分站点首包较慢，6s 容易在网络波动时产生误判。
func newValidateClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// statusReachable 依据状态码判断「可达」。
// 只有明确的客户端错误才视为失效（404/410 等）；
// 401/403/405/408/429 与 5xx 属于反爬、限流或服务端临时故障，
// 不代表资源已不存在，一律按可达处理，避免网络因素造成误判。
func statusReachable(code int) bool {
	switch {
	case code < 400:
		return true
	case code == http.StatusUnauthorized, // 401 需要登录
		code == http.StatusForbidden, // 403 反爬/权限
		code == http.StatusMethodNotAllowed,
		code == http.StatusRequestTimeout,
		code == http.StatusTooManyRequests, // 429 限流
		code >= 500:                        // 服务端临时故障
		return true
	default:
		return false
	}
}

// URLReachable 判断网址是否可访问。
// 优先 HEAD 请求避免下载页面正文；部分站点不支持 HEAD（405/501）时回退 GET。
// 网络层错误（超时、连接重置等）重试一次，仍失败才判定不可达。
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
	// try 发起请求：网络层错误重试一次（间隔 800ms）
	try := func(method string) (int, error) {
		code, err := do(method)
		if err == nil {
			return code, nil
		}
		time.Sleep(800 * time.Millisecond)
		return do(method)
	}

	// HEAD 不被支持（405 Method Not Allowed / 501 Not Implemented）时回退 GET
	if code, err := try(http.MethodHead); err == nil && code != http.StatusMethodNotAllowed && code != http.StatusNotImplemented {
		return statusReachable(code)
	}
	code, err := try(http.MethodGet)
	if err != nil {
		return false
	}
	return statusReachable(code)
}
