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
)

// conversationChatService 抽象会话相关的业务能力，便于测试替换。
type conversationChatService interface {
	ListConversations(
		ctx context.Context,
		userID uint64,
		limit int,
	) ([]service.ConversationSummary, error)
}

// ConversationHandler 处理会话相关的 HTTP 请求。
type ConversationHandler struct {
	chat conversationChatService
}

// NewConversationHandler 创建会话处理器。
func NewConversationHandler(chat conversationChatService) *ConversationHandler {
	return &ConversationHandler{
		chat: chat,
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
