package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gochat/internal/model"
	"gochat/internal/service"

	"github.com/gin-gonic/gin"
)

// fakeConversationChatService 是会话处理器的假业务实现。
type fakeConversationChatService struct {
	summaries []service.ConversationSummary
	lastUser  uint64
	lastLimit int

	messages    []model.Message
	hasMore     bool
	messagesErr error

	lastConversationID uint64
	// 把原来的 lastBeforeSeq / lastAfterSeq 换成游标。
	lastCursor service.MessageCursor
}

func (f *fakeConversationChatService) ListMessages(
	ctx context.Context,
	userID uint64,
	conversationID uint64,
	cursor service.MessageCursor,
	limit int,
) ([]model.Message, bool, error) {
	f.lastConversationID = conversationID
	f.lastCursor = cursor

	if f.messagesErr != nil {
		return nil, false, f.messagesErr
	}

	return f.messages, f.hasMore, nil
}

func (f *fakeConversationChatService) ListConversations(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]service.ConversationSummary, error) {
	f.lastUser = userID
	f.lastLimit = limit

	return f.summaries, nil
}

// newTestConversationRouter 构造带 userID 注入的测试路由。
// 真实环境由 JWTAuth 中间件写入 userID，这里用最简单的中间件代替。
func newTestConversationRouter(
	chat conversationChatService,
	userID uint64,
) *gin.Engine {
	gin.SetMode(gin.TestMode)

	h := NewConversationHandler(chat)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID > 0 {
			c.Set("userID", userID)
		}

		c.Next()
	})

	r.GET("/api/v1/conversations", h.List)

	r.GET("/api/v1/conversations/:id/messages", h.Messages)

	return r
}

// TestConversationHandlerList 测试会话列表包含未读数、对方信息和最后一条消息。
func TestConversationHandlerList(t *testing.T) {
	chat := &fakeConversationChatService{
		summaries: []service.ConversationSummary{
			{
				ID:          238,
				Type:        model.ConversationTypeSingle,
				UnreadCount: 1,
				LastReadSeq: 0,
				Peer: &service.UserSummary{
					ID:       12,
					Username: "alice",
					Nickname: "Alice",
				},
				LastMessage: &model.Message{
					ID:             138,
					ConversationID: 238,
					SenderID:       12,
					Seq:            1,
					ClientMsgID:    "demo-1",
					ContentType:    model.MessageContentTypeText,
					Content:        "你好 Bob",
					CreatedAt:      time.Now(),
				},
			},
		},
	}

	r := newTestConversationRouter(chat, 27)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations?limit=20", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result struct {
		Code int `json:"code"`
		Data struct {
			Conversations []conversationResponse `json:"conversations"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if len(result.Data.Conversations) != 1 {
		t.Fatalf("conversations = %d, want 1", len(result.Data.Conversations))
	}

	item := result.Data.Conversations[0]

	if item.UnreadCount != 1 {
		t.Errorf("unread_count = %d, want 1", item.UnreadCount)
	}

	if item.Peer == nil || item.Peer.Nickname != "Alice" {
		t.Errorf("peer = %+v, want nickname Alice", item.Peer)
	}

	if item.LastMessage == nil || item.LastMessage.Seq != 1 {
		t.Errorf("last_message = %+v, want seq 1", item.LastMessage)
	}

	if chat.lastUser != 27 {
		t.Errorf("userID = %d, want 27", chat.lastUser)
	}

	if chat.lastLimit != 20 {
		t.Errorf("limit = %d, want 20", chat.lastLimit)
	}
}

// TestConversationHandlerListUnauthorized 测试缺少身份时返回 401。
func TestConversationHandlerListUnauthorized(t *testing.T) {
	r := newTestConversationRouter(&fakeConversationChatService{}, 0)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if result["code"] != float64(4011) {
		t.Errorf("code = %v, want 4011", result["code"])
	}
}

// TestConversationHandlerMessagesBefore 测试往前翻历史的响应字段。
func TestConversationHandlerMessagesBefore(t *testing.T) {
	chat := &fakeConversationChatService{
		messages: []model.Message{
			{ID: 9, ConversationID: 238, Seq: 9, Content: "9"},
			{ID: 8, ConversationID: 238, Seq: 8, Content: "8"},
		},
		hasMore: true,
	}
	r := newTestConversationRouter(chat, 27)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/conversations/238/messages?before_seq=10&limit=2",
		nil,
	)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result struct {
		Data struct {
			Messages      []messageResponse `json:"messages"`
			Order         string            `json:"order"`
			HasMore       bool              `json:"has_more"`
			NextBeforeSeq uint64            `json:"next_before_seq"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if result.Data.Order != "desc" {
		t.Errorf("order = %q, want desc", result.Data.Order)
	}

	if result.Data.NextBeforeSeq != 8 {
		t.Errorf("next_before_seq = %d, want 8", result.Data.NextBeforeSeq)
	}

	if !result.Data.HasMore {
		t.Error("has_more = false, want true")
	}
}

// TestConversationHandlerMessagesAfter 测试断线补洞走升序。
func TestConversationHandlerMessagesAfter(t *testing.T) {
	chat := &fakeConversationChatService{
		messages: []model.Message{
			{ID: 11, ConversationID: 238, Seq: 11},
			{ID: 12, ConversationID: 238, Seq: 12},
		},
	}
	r := newTestConversationRouter(chat, 27)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/conversations/238/messages?after_seq=10&limit=20",
		nil,
	)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var result struct {
		Data struct {
			Order        string `json:"order"`
			NextAfterSeq uint64 `json:"next_after_seq"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if result.Data.Order != "asc" {
		t.Errorf("order = %q, want asc", result.Data.Order)
	}

	if result.Data.NextAfterSeq != 12 {
		t.Errorf("next_after_seq = %d, want 12", result.Data.NextAfterSeq)
	}

	if chat.lastCursor.Direction != service.MessageDirectionAfter || chat.lastCursor.Seq != 10 {
		t.Errorf("cursor = %+v, want after 10", chat.lastCursor)
	}

}

// TestConversationHandlerMessagesBothCursors 测试同时传两个游标被拒绝。
func TestConversationHandlerMessagesBothCursors(t *testing.T) {
	r := newTestConversationRouter(&fakeConversationChatService{}, 27)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/conversations/238/messages?before_seq=5&after_seq=1",
		nil,
	)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestConversationHandlerMessagesNotMember 测试非成员拿不到历史消息。
func TestConversationHandlerMessagesNotMember(t *testing.T) {
	chat := &fakeConversationChatService{
		messagesErr: service.ErrNotConversationMember,
	}
	r := newTestConversationRouter(chat, 99)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/238/messages", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if result["code"] != float64(4031) {
		t.Errorf("code = %v, want 4031", result["code"])
	}
}

// TestConversationHandlerMessagesAfterZero 回归测试：
// after_seq=0 必须被识别为"补洞模式"，返回升序，而不是当成未传。
func TestConversationHandlerMessagesAfterZero(t *testing.T) {
	chat := &fakeConversationChatService{
		messages: []model.Message{
			{ID: 1, ConversationID: 238, Seq: 1},
			{ID: 2, ConversationID: 238, Seq: 2},
		},
	}
	r := newTestConversationRouter(chat, 27)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/conversations/238/messages?after_seq=0&limit=20",
		nil,
	)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var result struct {
		Data struct {
			Order        string `json:"order"`
			NextAfterSeq uint64 `json:"next_after_seq"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if result.Data.Order != "asc" {
		t.Errorf("order = %q, want asc", result.Data.Order)
	}

	if result.Data.NextAfterSeq != 2 {
		t.Errorf("next_after_seq = %d, want 2", result.Data.NextAfterSeq)
	}

	if chat.lastCursor.Direction != service.MessageDirectionAfter || chat.lastCursor.Seq != 0 {
		t.Errorf("cursor = %+v, want after 0", chat.lastCursor)
	}
}
