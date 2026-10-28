package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"gochat/internal/model"
	"gochat/internal/service"
	"gochat/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// conversationChatService 抽象会话相关的业务能力，便于测试替换。
type conversationChatService interface {
	CreateSingleConversation(
		ctx context.Context,
		userA uint64,
		userB uint64,
	) (*model.Conversation, error)

	ListConversations(
		ctx context.Context,
		userID uint64,
		limit int,
	) ([]service.ConversationSummary, error)

	ListMessages(
		ctx context.Context,
		userID uint64,
		conversationID uint64,
		cursor service.MessageCursor,
		limit int,
	) ([]model.Message, bool, error)

	MarkRead(
		ctx context.Context,
		userID uint64,
		conversationID uint64,
		lastReadSeq uint64,
	) (*service.MarkReadResult, error)
}

// userReader 是会话处理器需要的用户查询能力（由 UserService 实现）。
type userReader interface {
	GetByID(ctx context.Context, id uint64) (*model.User, error)
}

// ConversationHandler 处理会话相关的 HTTP 请求。
type ConversationHandler struct {
	chat  conversationChatService
	users userReader
}

// NewConversationHandler 创建会话处理器。
func NewConversationHandler(chat conversationChatService, users userReader) *ConversationHandler {
	return &ConversationHandler{
		chat:  chat,
		users: users,
	}
}

// userBrief 是响应里的用户摘要。
type userBrief struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
}

// messageResponse 是响应里的消息结构。
type messageResponse struct {
	ID             uint64 `json:"id"`
	ConversationID uint64 `json:"conversation_id"`
	SenderID       uint64 `json:"sender_id"`
	Seq            uint64 `json:"seq"`
	ClientMsgID    string `json:"client_msg_id"`
	ContentType    uint8  `json:"content_type"`
	Content        string `json:"content"`
	CreatedAt      any    `json:"created_at"`
}

// newMessageResponse 把消息模型转换成响应结构。
func newMessageResponse(msg *model.Message) *messageResponse {
	if msg == nil {
		return nil
	}

	return &messageResponse{
		ID:             msg.ID,
		ConversationID: msg.ConversationID,
		SenderID:       msg.SenderID,
		Seq:            msg.Seq,
		ClientMsgID:    msg.ClientMsgID,
		ContentType:    msg.ContentType,
		Content:        msg.Content,
		CreatedAt:      msg.CreatedAt,
	}
}

// conversationResponse 是会话列表项的响应结构。
//
// 单聊的 name 为空，客户端应展示 peer.nickname；
// 群聊的 name 是群名称，peer 为 null。
type conversationResponse struct {
	ID          uint64           `json:"id"`
	Type        uint8            `json:"type"`
	Name        string           `json:"name"`
	UnreadCount int64            `json:"unread_count"`
	LastReadSeq uint64           `json:"last_read_seq"`
	Peer        *userBrief       `json:"peer,omitempty"`
	LastMessage *messageResponse `json:"last_message,omitempty"`
}

// List 处理 GET /api/v1/conversations。
func (h *ConversationHandler) List(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 4011, "unauthorized")
		return
	}

	limit, err := parsePositiveInt(c.Query("limit"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 4001, "invalid limit")
		return
	}

	summaries, err := h.chat.ListConversations(c.Request.Context(), userID, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 5000, "internal server error")
		return
	}

	items := make([]conversationResponse, 0, len(summaries))

	for _, item := range summaries {
		var peer *userBrief

		if item.Peer != nil {
			peer = &userBrief{
				ID:       item.Peer.ID,
				Username: item.Peer.Username,
				Nickname: item.Peer.Nickname,
				Avatar:   item.Peer.Avatar,
			}
		}

		items = append(items, conversationResponse{
			ID:          item.ID,
			Type:        item.Type,
			Name:        item.Name,
			UnreadCount: item.UnreadCount,
			LastReadSeq: item.LastReadSeq,
			Peer:        peer,
			LastMessage: newMessageResponse(item.LastMessage),
		})
	}

	response.Success(c, gin.H{
		"conversations": items,
	})
}

// parsePositiveInt 解析可选的 int 查询参数，空字符串视为 0（由 service 归一化）。
func parsePositiveInt(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, errors.New("invalid positive int")
	}

	return value, nil
}

// Messages 处理 GET /api/v1/conversations/:id/messages。
//
// 游标分页：
//   - before_seq：往前翻历史，返回 desc
//   - after_seq：往后补洞，返回 asc（after_seq=0 表示从最早开始）
//   - 都不传：返回最近一页（desc）
func (h *ConversationHandler) Messages(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 4011, "unauthorized")
		return
	}

	conversationID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || conversationID == 0 {
		response.Error(c, http.StatusBadRequest, 4001, "invalid conversation id")
		return
	}

	cursor, err := parseMessageCursor(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 4001, err.Error())
		return
	}

	limit, err := parsePositiveInt(c.Query("limit"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 4001, "invalid limit")
		return
	}

	messages, hasMore, err := h.chat.ListMessages(
		c.Request.Context(),
		userID,
		conversationID,
		cursor,
		limit,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotConversationMember):
			response.Error(c, http.StatusForbidden, 4031, "not a conversation member")

		case errors.Is(err, service.ErrInvalidMessage):
			response.Error(c, http.StatusBadRequest, 4001, "invalid request")

		default:
			response.Error(c, http.StatusInternalServerError, 5000, "internal server error")
		}

		return
	}

	items := make([]messageResponse, 0, len(messages))
	for i := range messages {
		items = append(items, *newMessageResponse(&messages[i]))
	}

	order := "desc"
	if cursor.Direction == service.MessageDirectionAfter {
		order = "asc"
	}

	var nextBeforeSeq, nextAfterSeq uint64

	if len(messages) > 0 {
		last := messages[len(messages)-1].Seq

		if order == "desc" {
			nextBeforeSeq = last
		} else {
			nextAfterSeq = last
		}
	}

	response.Success(c, gin.H{
		"messages":        items,
		"order":           order,
		"has_more":        hasMore,
		"next_before_seq": nextBeforeSeq,
		"next_after_seq":  nextAfterSeq,
	})
}

// parseMessageCursor 把查询参数转换成显式游标。
//
// 关键：用 GetQuery 判断参数"是否存在"，而不是用 0 判断"是否指定"。
// after_seq=0 与"没传 after_seq"是两种不同语义。
func parseMessageCursor(c *gin.Context) (service.MessageCursor, error) {
	beforeRaw, hasBefore := c.GetQuery("before_seq")
	afterRaw, hasAfter := c.GetQuery("after_seq")

	if hasBefore && hasAfter {
		return service.MessageCursor{},
			errors.New("before_seq and after_seq cannot be used together")
	}

	if hasAfter {
		seq, err := parseUintQuery(afterRaw)
		if err != nil {
			return service.MessageCursor{}, errors.New("invalid after_seq")
		}

		return service.MessageCursor{
			Direction: service.MessageDirectionAfter,
			Seq:       seq,
		}, nil
	}

	if hasBefore {
		seq, err := parseUintQuery(beforeRaw)
		if err != nil {
			return service.MessageCursor{}, errors.New("invalid before_seq")
		}

		return service.MessageCursor{
			Direction: service.MessageDirectionBefore,
			Seq:       seq,
		}, nil
	}

	return service.MessageCursor{
		Direction: service.MessageDirectionLatest,
	}, nil
}

// parseUintQuery 解析可选的 uint 查询参数，空字符串视为 0。
func parseUintQuery(raw string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}

	return strconv.ParseUint(raw, 10, 64)
}

// markReadRequest 定义已读上报的请求体。
type markReadRequest struct {
	LastReadSeq uint64 `json:"last_read_seq" binding:"required"`
}

// MarkRead 处理 POST /api/v1/conversations/:id/read。
func (h *ConversationHandler) MarkRead(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 4011, "unauthorized")
		return
	}

	conversationID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || conversationID == 0 {
		response.Error(c, http.StatusBadRequest, 4001, "invalid conversation id")
		return
	}

	var req markReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 4001, "invalid request body")
		return
	}

	result, err := h.chat.MarkRead(
		c.Request.Context(),
		userID,
		conversationID,
		req.LastReadSeq,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotConversationMember):
			response.Error(c, http.StatusForbidden, 4031, "not a conversation member")

		case errors.Is(err, service.ErrInvalidMessage):
			response.Error(c, http.StatusBadRequest, 4001, "invalid request")

		default:
			response.Error(c, http.StatusInternalServerError, 5000, "internal server error")
		}

		return
	}

	response.Success(c, gin.H{
		"conversation_id": conversationID,
		"last_read_seq":   result.LastReadSeq,
		"changed":         result.Changed,
	})
}

// createConversationRequest 定义创建单聊的请求体。
type createConversationRequest struct {
	PeerUserID uint64 `json:"peer_user_id" binding:"required"`
}

// Create 处理 POST /api/v1/conversations。
//
// 语义是"创建或获取"：重复调用返回同一条会话，所以用 200 而不是 201。
func (h *ConversationHandler) Create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 4011, "unauthorized")
		return
	}

	var req createConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 4001, "invalid request body")
		return
	}

	// 自己和自己建会话没有意义。
	if req.PeerUserID == userID {
		response.Error(c, http.StatusBadRequest, 4002, "cannot create conversation with yourself")
		return
	}

	// 对端必须存在，否则会留下指向不存在用户的会话。
	peer, err := h.users.GetByID(c.Request.Context(), req.PeerUserID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, http.StatusNotFound, 4040, "user not found")
			return
		}

		response.Error(c, http.StatusInternalServerError, 5000, "internal server error")
		return
	}

	conv, err := h.chat.CreateSingleConversation(
		c.Request.Context(),
		userID,
		req.PeerUserID,
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidConversation) {
			response.Error(c, http.StatusBadRequest, 4002, "invalid conversation")
			return
		}

		response.Error(c, http.StatusInternalServerError, 5000, "internal server error")
		return
	}

	response.Success(c, gin.H{
		"conversation": conversationResponse{
			ID:   conv.ID,
			Type: conv.Type,
			Name: conv.Name,
			Peer: &userBrief{
				ID:       peer.ID,
				Username: peer.Username,
				Nickname: peer.Nickname,
				Avatar:   peer.Avatar,
			},
		},
	})
}
