package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gochat/internal/auth"
	"gochat/internal/model"
	"gochat/internal/repository"
	"gochat/internal/service"
	"gochat/internal/ws"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const wsTestSecret = "test-secret-for-gochat-ws-handler-tests-2026"

// fakeConvRepo 满足 service 需要的会话仓储接口。
type fakeConvRepo struct {
	conv *model.Conversation
}

func (f *fakeConvRepo) CreateSingleConversation(
	ctx context.Context,
	userA uint64,
	userB uint64,
) (*model.Conversation, error) {
	return f.conv, nil
}

func (f *fakeConvRepo) ListByUser(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]repository.ConversationSummary, error) {
	return nil, nil
}

func (f *fakeConvRepo) IsMember(
	ctx context.Context,
	conversationID uint64,
	userID uint64,
) (bool, error) {
	return true, nil
}

func (f *fakeConvRepo) MarkRead(
	ctx context.Context,
	conversationID uint64,
	userID uint64,
	lastReadSeq uint64,
) (uint64, bool, error) {
	return 0, false, nil
}

// fakeMsgRepo 是内存版消息仓储：实现 seq 分配和 client_msg_id 幂等，
// 让 handler 的端到端测试不依赖 MySQL。
type fakeMsgRepo struct {
	mu sync.Mutex

	members       []uint64
	seq           uint64
	byClientMsgID map[string]*model.Message
}

func newFakeMsgRepo(members ...uint64) *fakeMsgRepo {
	return &fakeMsgRepo{
		members:       members,
		byClientMsgID: make(map[string]*model.Message),
	}
}

func (f *fakeMsgRepo) ListBeforeSeq(
	ctx context.Context,
	conversationID uint64,
	beforeSeq uint64,
	limit int,
) ([]model.Message, error) {
	return nil, nil
}

func (f *fakeMsgRepo) ListAfterSeq(
	ctx context.Context,
	conversationID uint64,
	afterSeq uint64,
	limit int,
) ([]model.Message, error) {
	return nil, nil
}

func (f *fakeMsgRepo) AppendMessage(
	ctx context.Context,
	conversationID uint64,
	senderID uint64,
	clientMsgID string,
	contentType uint8,
	content string,
) (*model.Message, []uint64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	isMember := false

	for _, id := range f.members {
		if id == senderID {
			isMember = true
			break
		}
	}

	if !isMember {
		return nil, nil, false, repository.ErrNotConversationMember
	}

	if msg, exists := f.byClientMsgID[clientMsgID]; exists {
		return msg, f.members, true, nil
	}

	f.seq++

	msg := &model.Message{
		ID:             f.seq,
		ConversationID: conversationID,
		SenderID:       senderID,
		Seq:            f.seq,
		ClientMsgID:    clientMsgID,
		ContentType:    contentType,
		Content:        content,
		CreatedAt:      time.Now(),
	}
	f.byClientMsgID[clientMsgID] = msg

	return msg, f.members, false, nil
}

// newTestWSServer 启动只包含 WebSocket 路由的测试服务。
func newTestWSServer(t *testing.T) (*httptest.Server, *ws.Hub) {
	t.Helper()

	hub := ws.NewHub()

	msgRepo := newFakeMsgRepo(42, 43)
	chatService := service.NewChatService(
		&fakeConvRepo{conv: &model.Conversation{ID: 7}},
		msgRepo,
		hub,
	)

	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/api/v1/ws", NewWSHandler(hub, chatService, wsTestSecret).Serve)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return srv, hub
}

// mustToken 为指定用户生成测试 token。
func mustToken(t *testing.T, userID uint64) string {
	t.Helper()

	token, err := auth.GenerateToken(
		userID,
		fmt.Sprintf("user-%d", userID),
		wsTestSecret,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	return token
}

// dialWS 建立一条已鉴权的 WebSocket 连接。
func dialWS(t *testing.T, srv *httptest.Server, userID uint64) *websocket.Conn {
	t.Helper()

	conn, resp, err := websocket.DefaultDialer.Dial(
		wsURL(srv)+"?token="+url.QueryEscape(mustToken(t, userID)),
		nil,
	)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}

		t.Fatalf("dial user %d failed: %v", userID, err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// readEnvelope 读取下一帧。
func readEnvelope(t *testing.T, conn *websocket.Conn) ws.Envelope {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var env ws.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		t.Fatalf("read envelope failed: %v", err)
	}

	return env
}

// readEnvelopeOfType 持续读取直到拿到指定类型的帧（跳过心跳等无关帧）。
func readEnvelopeOfType(
	t *testing.T,
	conn *websocket.Conn,
	wantType string,
) ws.Envelope {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		env := readEnvelope(t, conn)
		if env.Type == wantType {
			return env
		}
	}

	t.Fatalf("did not receive %q frame", wantType)

	return ws.Envelope{}
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
	srv, hub := newTestWSServer(t)

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
	srv, hub := newTestWSServer(t)

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
	srv, hub := newTestWSServer(t)
	token := mustToken(t, 42)

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
	srv, _ := newTestWSServer(t)
	token := mustToken(t, 42)

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

// TestWSHandlerSendMessageDeliversToBothMembers 测试一条消息的完整链路：
// A 发送 → 落库 → ack 给 A → message.new 推给 B 和 A 的其他设备。
func TestWSHandlerSendMessageDeliversToBothMembers(t *testing.T) {
	srv, hub := newTestWSServer(t)

	alice := dialWS(t, srv, 42)
	bob := dialWS(t, srv, 43)
	aliceSecondDevice := dialWS(t, srv, 42)

	waitForCondition(t, func() bool {
		return hub.Connections() == 3
	}, "connections were not registered in hub")

	payload, err := ws.NewEnvelope(ws.TypeMessageSend, ws.MessageSendPayload{
		ConversationID: 7,
		ClientMsgID:    "client-msg-1",
		ContentType:    model.MessageContentTypeText,
		Content:        "hello bob",
	})
	if err != nil {
		t.Fatalf("build send payload failed: %v", err)
	}

	if err := alice.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("alice send failed: %v", err)
	}

	// 1. Bob 收到 message.new。
	bobEnv := readEnvelopeOfType(t, bob, ws.TypeMessageNew)

	var bobMsg ws.MessagePayload
	if err := json.Unmarshal(bobEnv.Data, &bobMsg); err != nil {
		t.Fatalf("unmarshal message payload failed: %v", err)
	}

	if bobMsg.Seq != 1 || bobMsg.Content != "hello bob" {
		t.Errorf("bob received seq=%d content=%q", bobMsg.Seq, bobMsg.Content)
	}

	// 2. Alice 的另一台设备也收到 message.new（多端同步）。
	secondEnv := readEnvelopeOfType(t, aliceSecondDevice, ws.TypeMessageNew)

	var secondMsg ws.MessagePayload
	if err := json.Unmarshal(secondEnv.Data, &secondMsg); err != nil {
		t.Fatalf("unmarshal second device payload failed: %v", err)
	}

	if secondMsg.Seq != 1 {
		t.Errorf("second device seq = %d, want 1", secondMsg.Seq)
	}

	// 3. Alice 自己收到 ack。
	ackEnv := readEnvelopeOfType(t, alice, ws.TypeMessageAck)

	var ack ws.MessageAckPayload
	if err := json.Unmarshal(ackEnv.Data, &ack); err != nil {
		t.Fatalf("unmarshal ack payload failed: %v", err)
	}

	if ack.ClientMsgID != "client-msg-1" || ack.Seq != 1 || ack.Duplicated {
		t.Errorf("ack = %+v, want client_msg_id=client-msg-1 seq=1 duplicated=false", ack)
	}
}

// TestWSHandlerSendMessageDuplicateIsIdempotent 测试重发不会产生第二条消息。
func TestWSHandlerSendMessageDuplicateIsIdempotent(t *testing.T) {
	srv, _ := newTestWSServer(t)

	alice := dialWS(t, srv, 42)
	bob := dialWS(t, srv, 43)

	sendPayload, err := ws.NewEnvelope(ws.TypeMessageSend, ws.MessageSendPayload{
		ConversationID: 7,
		ClientMsgID:    "client-msg-dup",
		ContentType:    model.MessageContentTypeText,
		Content:        "hello",
	})
	if err != nil {
		t.Fatalf("build send payload failed: %v", err)
	}

	// 第一次发送。
	if err := alice.WriteMessage(websocket.TextMessage, sendPayload); err != nil {
		t.Fatalf("first send failed: %v", err)
	}

	readEnvelopeOfType(t, alice, ws.TypeMessageAck)
	readEnvelopeOfType(t, bob, ws.TypeMessageNew)

	// 第二次发送同一 client_msg_id。
	if err := alice.WriteMessage(websocket.TextMessage, sendPayload); err != nil {
		t.Fatalf("second send failed: %v", err)
	}

	ackEnv := readEnvelopeOfType(t, alice, ws.TypeMessageAck)

	var ack ws.MessageAckPayload
	if err := json.Unmarshal(ackEnv.Data, &ack); err != nil {
		t.Fatalf("unmarshal ack payload failed: %v", err)
	}

	if !ack.Duplicated {
		t.Error("Duplicated = false, want true")
	}

	if ack.Seq != 1 {
		t.Errorf("seq = %d, want 1 (复用第一次的序号)", ack.Seq)
	}

	// Bob 不应该再收到第二条 message.new。
	_ = bob.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := bob.ReadMessage(); err == nil {
		t.Fatal("bob received a duplicate message")
	}
}

// TestWSHandlerSendMessageNotMember 测试非成员发送消息会收到错误帧。
func TestWSHandlerSendMessageNotMember(t *testing.T) {
	srv, _ := newTestWSServer(t)

	outsider := dialWS(t, srv, 99)

	payload, err := ws.NewEnvelope(ws.TypeMessageSend, ws.MessageSendPayload{
		ConversationID: 7,
		ClientMsgID:    "hack-msg",
		ContentType:    model.MessageContentTypeText,
		Content:        "let me in",
	})
	if err != nil {
		t.Fatalf("build send payload failed: %v", err)
	}

	if err := outsider.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("outsider send failed: %v", err)
	}

	env := readEnvelopeOfType(t, outsider, ws.TypeError)

	var errPayload ws.ErrorPayload
	if err := json.Unmarshal(env.Data, &errPayload); err != nil {
		t.Fatalf("unmarshal error payload failed: %v", err)
	}

	if errPayload.Code != 4031 {
		t.Errorf("error code = %d, want 4031", errPayload.Code)
	}
}
