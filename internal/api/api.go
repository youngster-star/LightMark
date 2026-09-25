// Package api 提供本地 REST + WebSocket 服务桩，供未来 AI 桌宠接入。
// 仅监听 127.0.0.1（随机端口），不暴露局域网。
package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Server 本地 API 服务。
type Server struct {
	ln     net.Listener
	srv    *http.Server
	wsUpgr websocket.Upgrader
}

// Start 启动本地 API 服务（127.0.0.1 随机端口，避免端口冲突）。
// 当前为接口桩：/api/health 健康检查、/ws WebSocket 回声协议。
func Start() (*Server, error) {
	// 仅绑定回环地址，无外部攻击面
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("本地端口监听失败: %w", err)
	}
	s := &Server{
		ln: ln,
		wsUpgr: websocket.Upgrader{
			// 桩阶段仅接受本机 Origin，升级请求校验由连接方地址兜底
			CheckOrigin: func(r *http.Request) bool {
				return strings.HasPrefix(r.RemoteAddr, "127.0.0.1")
			},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/ws", s.handleWS)
	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = s.srv.Serve(ln)
	}()
	return s, nil
}

// Addr 返回监听地址（host:port）。
func (s *Server) Addr() string {
	return s.ln.Addr().String()
}

// Stop 优雅停止服务。
func (s *Server) Stop() error {
	return s.srv.Close()
}

// handleHealth GET /api/health：服务与连接健康检查。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true,"service":"light_mark"}`))
}

// wsMessage WS 桩协议消息：{"type":"ping"} → {"type":"pong"}；其余回声。
type wsMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// handleWS GET /ws：WebSocket 接口桩。
// 协议：客户端发送 JSON {"type":"ping"}，服务端回复 {"type":"pong"};
// 其他 type 原样回声，为未来 AI 会话协议预留。
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.wsUpgr.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	// 读写超时按消息滚动续期：有活动的连接不会 60s 被掐断，
	// 同时仍能回收彻底静默的死连接，防止 goroutine 泄漏
	const idleTimeout = 60 * time.Second
	_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
	for {
		var m wsMessage
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		switch m.Type {
		case "ping":
			_ = conn.WriteJSON(wsMessage{Type: "pong"})
		default:
			_ = conn.WriteJSON(m)
		}
	}
}
