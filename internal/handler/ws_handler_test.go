package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"gochat/internal/auth"
	"gochat/internal/ws"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const wsTestSecret = "test-secret-for-gochat-ws-handler-tests-2026"

// newTestWSServer 启动一个只包含 WebSocket 路由的测试服务。
//
// 这里刻意不使用 router.SetupRouter：
// 它需要 UserService，而 UserService 需要数据库。
// WebSocket 鉴权只依赖 JWT 和 Hub，单独组装可以让测试不依赖 MySQL。
func newTestWSServer(t *testing.T) (*httptest.Server, *ws.Hub, string) {
	t.Helper()

	token, err := auth.GenerateToken(42, "tester", wsTestSecret, time.Hour)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	hub := ws.NewHub()

	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/api/v1/ws", NewWSHandler(hub, wsTestSecret).Serve)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return srv, hub, token
}

// wsURL 把 HTTP 地址转换成 WebSocket 地址。
func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws"
}

// waitForCondition 轮询等待条件成立，避免依赖固定 sleep。
func waitForCondition(t *testing.T, cond func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal(msg)
}

// TestWSHandlerRejectsMissingToken 测试没有 token 时握手被拒绝。
func TestWSHandlerRejectsMissingToken(t *testing.T) {
	srv, hub, _ := newTestWSServer(t)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(srv), nil)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected handshake to fail")
	}

	if resp == nil {
		t.Fatal("expected an HTTP response")
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	if got := hub.Connections(); got != 0 {
		t.Errorf("Connections() = %d, want 0", got)
	}
}

// TestWSHandlerRejectsInvalidToken 测试伪造 token 被拒绝。
func TestWSHandlerRejectsInvalidToken(t *testing.T) {
	srv, hub, _ := newTestWSServer(t)

	conn, resp, err := websocket.DefaultDialer.Dial(
		wsURL(srv)+"?token=invalid.token.value",
		nil,
	)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected handshake to fail")
	}

	if resp == nil {
		t.Fatal("expected an HTTP response")
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	if got := hub.Connections(); got != 0 {
		t.Errorf("Connections() = %d, want 0", got)
	}
}

// TestWSHandlerPingPong 测试合法连接可以升级、注册、收发消息，并在断开后清理。
func TestWSHandlerPingPong(t *testing.T) {
	srv, hub, token := newTestWSServer(t)

	conn, resp, err := websocket.DefaultDialer.Dial(
		wsURL(srv)+"?token="+url.QueryEscape(token),
		nil,
	)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}

		t.Fatalf("dial failed: %v", err)
	}

	defer func() {
		_ = conn.Close()
	}()

	// 1. 连接应该被注册到 Hub。
	waitForCondition(t, func() bool {
		return hub.ConnectionsOf(42) == 1
	}, "connection was not registered in hub")

	// 2. 发 ping 应该收到 pong。
	if err := conn.WriteJSON(ws.Envelope{Type: ws.TypePing}); err != nil {
		t.Fatalf("write ping failed: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var env ws.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatalf("read pong failed: %v", err)
	}

	if env.Type != ws.TypePong {
		t.Fatalf("message type = %q, want %q", env.Type, ws.TypePong)
	}

	// 3. 客户端断开后，Hub 必须清理干净。
	_ = conn.Close()

	waitForCondition(t, func() bool {
		return hub.Connections() == 0
	}, "connection was not unregistered from hub")
}

// TestWSHandlerRejectsUnknownMessageType 测试未知消息类型会收到错误帧。
func TestWSHandlerRejectsUnknownMessageType(t *testing.T) {
	srv, _, token := newTestWSServer(t)

	conn, resp, err := websocket.DefaultDialer.Dial(
		wsURL(srv)+"?token="+url.QueryEscape(token),
		nil,
	)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}

		t.Fatalf("dial failed: %v", err)
	}

	defer func() {
		_ = conn.Close()
	}()

	if err := conn.WriteJSON(ws.Envelope{Type: "unknown"}); err != nil {
		t.Fatalf("write message failed: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var env ws.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatalf("read error frame failed: %v", err)
	}

	if env.Type != ws.TypeError {
		t.Fatalf("message type = %q, want %q", env.Type, ws.TypeError)
	}

	var payload ws.ErrorPayload
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		t.Fatalf("decode error payload failed: %v", err)
	}

	if payload.Code != 4004 {
		t.Errorf("error code = %d, want 4004", payload.Code)
	}
}
