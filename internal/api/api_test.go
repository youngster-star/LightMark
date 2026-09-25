package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestHealthCheck 验证 /api/health 返回 200 与 JSON 体。
func TestHealthCheck(t *testing.T) {
	s, err := Start()
	if err != nil {
		t.Fatalf("启动服务失败: %v", err)
	}
	defer s.Stop()

	url := "http://" + s.Addr() + "/api/health"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("请求健康检查失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("健康检查状态码: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("健康检查响应异常: %s", body)
	}
}

// TestWSPingPong 验证 WS 桩：连接建立、ping/pong 往返。
func TestWSPingPong(t *testing.T) {
	s, err := Start()
	if err != nil {
		t.Fatalf("启动服务失败: %v", err)
	}
	defer s.Stop()

	wsURL := "ws://" + s.Addr() + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS 连接失败: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(wsMessage{Type: "ping"}); err != nil {
		t.Fatalf("发送 ping 失败: %v", err)
	}
	var m wsMessage
	if err := conn.ReadJSON(&m); err != nil {
		t.Fatalf("读取回复失败: %v", err)
	}
	if m.Type != "pong" {
		t.Fatalf("期望 pong，实际: %s", m.Type)
	}
}

// TestEchoOtherTypes 非ping消息原样回声。
func TestEchoOtherTypes(t *testing.T) {
	s, err := Start()
	if err != nil {
		t.Fatalf("启动服务失败: %v", err)
	}
	defer s.Stop()

	wsURL := "ws://" + s.Addr() + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS 连接失败: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(wsMessage{Type: "chat", Payload: json.RawMessage(`"hello"`)}); err != nil {
		t.Fatalf("发送消息失败: %v", err)
	}
	var m wsMessage
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := conn.ReadJSON(&m); err != nil {
		t.Fatalf("读取回复失败: %v", err)
	}
	if m.Type != "chat" {
		t.Fatalf("期望回声 chat，实际: %s", m.Type)
	}
}
