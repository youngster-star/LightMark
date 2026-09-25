// Package fileops 封装本机文件/网页的打开与网页元信息抓取。
package fileops

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// Meta 网页元信息。
type Meta struct {
	Title   string `json:"title"`
	Favicon string `json:"favicon"`
}

// maxFaviconSize favicon 内容大小上限（64KB），超出即放弃缓存。
const maxFaviconSize = 64 << 10

// FetchFaviconData 下载 favicon 并转为 data URL，便于前端 <img> 直接展示。
// 仅支持 http/https；内容超限或为空时返回错误。
func FetchFaviconData(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", errors.New("favicon 地址为空")
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("仅支持 http/https 的 favicon 地址")
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("下载 favicon 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("下载 favicon 返回状态码 %d", resp.StatusCode)
	}
	// 多读 1 字节用于判断超限
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFaviconSize+1))
	if err != nil {
		return "", fmt.Errorf("读取 favicon 失败: %w", err)
	}
	if len(data) > maxFaviconSize {
		return "", errors.New("favicon 内容过大")
	}
	if len(data) == 0 {
		return "", errors.New("favicon 内容为空")
	}
	// 优先使用响应头类型，缺失或二进制流时嗅探实际内容
	mime := resp.Header.Get("Content-Type")
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	if mime == "" || mime == "application/octet-stream" {
		mime = http.DetectContentType(data)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// Open 用本机默认关联应用打开本地文件或网址。
func Open(target string) error {
	if target == "" {
		return errors.New("打开目标为空")
	}
	// FileProtocolHandler 可统一处理本地路径与 http(s) 网址
	err := exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	if err != nil {
		return fmt.Errorf("打开失败: %w", err)
	}
	return nil
}

// FetchURLMeta 抓取网页标题与 favicon 地址。
func FetchURLMeta(rawURL string) (Meta, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return Meta{}, errors.New("网址为空")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return Meta{}, fmt.Errorf("无效的网址: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Meta{}, errors.New("仅支持 http/https 网址")
	}

	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return Meta{}, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return Meta{}, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Meta{}, fmt.Errorf("请求返回状态码 %d", resp.StatusCode)
	}

	// 限制读取大小，防止异常响应拖垮内存
	reader, err := charset.NewReader(io.LimitReader(resp.Body, 1<<20), resp.Header.Get("Content-Type"))
	if err != nil {
		reader = io.LimitReader(resp.Body, 1<<20)
	}
	doc, err := html.Parse(reader)
	if err != nil {
		return Meta{}, fmt.Errorf("解析页面失败: %w", err)
	}

	title := strings.TrimSpace(extractTitle(doc))
	if title == "" {
		title = rawURL
	}
	return Meta{Title: title, Favicon: extractFavicon(doc, u)}, nil
}

// extractTitle 提取 <title> 文本内容。
func extractTitle(doc *html.Node) string {
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if title != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "title" {
			if n.FirstChild != nil {
				title = n.FirstChild.Data
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return title
}

// extractFavicon 提取 favicon 链接，缺失时回退 /favicon.ico。
func extractFavicon(doc *html.Node, base *url.URL) string {
	var href string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if href != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "link" {
			var rel, h string
			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "rel":
					rel = strings.ToLower(a.Val)
				case "href":
					h = a.Val
				}
			}
			if strings.Contains(rel, "icon") && h != "" {
				href = h
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if href == "" {
		href = "/favicon.ico"
	}
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil || ref.String() == "" {
		return ""
	}
	return base.ResolveReference(ref).String()
}
