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
