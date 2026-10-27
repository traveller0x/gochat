package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"unicode/utf8"

	"gochat/internal/model"
	"gochat/internal/repository"
	"gochat/internal/ws"
)

const (
	// MaxMessageContentRunes 是单条文本消息的最大字符数。
	// 限制在服务端，避免超长消息打爆数据库和客户端。
	MaxMessageContentRunes = 4096
)

var (
	// ErrInvalidMessage 表示消息参数非法。
	ErrInvalidMessage = errors.New("invalid message")

	// ErrInvalidConversation 表示会话参数非法。
	ErrInvalidConversation = errors.New("invalid conversation")

	// ErrNotConversationMember 表示操作者不是会话成员。
	ErrNotConversationMember = errors.New("not a conversation member")

	// ErrConversationNotFound 表示会话不存在。
	ErrConversationNotFound = errors.New("conversation not found")
)

// clientMsgIDPattern 限制客户端消息 ID 的字符集和长度，
// 防止超长或带特殊字符的值污染唯一索引。
var clientMsgIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// chatConversationRepository 定义聊天业务需要的会话操作。
type chatConversationRepository interface {
	CreateSingleConversation(
		ctx context.Context,
		userA uint64,
		userB uint64,
	) (*model.Conversation, error)

	// ListByUser 返回用户加入的会话列表。
	ListByUser(
		ctx context.Context,
		userID uint64,
		limit int,
	) ([]repository.ConversationSummary, error)
}

// chatMessageRepository 定义聊天业务需要的消息操作。
type chatMessageRepository interface {
	AppendMessage(
		ctx context.Context,
		conversationID uint64,
		senderID uint64,
		clientMsgID string,
		contentType uint8,
		content string,
	) (*model.Message, []uint64, bool, error)
}

// messagePusher 定义消息推送能力，由 ws.Hub 实现。
// 在 service 侧定义接口（而不是依赖具体类型），方便用假实现做单元测试。
type messagePusher interface {
	SendToUser(userID uint64, payload []byte) int
}

// ChatService 负责聊天相关的业务逻辑。
type ChatService struct {
	convRepo chatConversationRepository
	msgRepo  chatMessageRepository
	pusher   messagePusher
}

// NewChatService 创建聊天业务服务。
func NewChatService(
	convRepo chatConversationRepository,
	msgRepo chatMessageRepository,
	pusher messagePusher,
) *ChatService {
	return &ChatService{
		convRepo: convRepo,
		msgRepo:  msgRepo,
		pusher:   pusher,
	}
}

// SendMessageResult 是发送消息的结果。
type SendMessageResult struct {
	Message *model.Message

	// Duplicated 表示这是一次重发，消息已存在，未被重复推送。
	Duplicated bool
}

// UserSummary 是会话列表里展示的用户摘要（不含任何敏感字段）。
type UserSummary struct {
	ID       uint64
	Username string
	Nickname string
	Avatar   string
}

// ConversationSummary 是会话列表项的对外结构。
// 不含 JSON 标签：序列化由 handler 负责。
type ConversationSummary struct {
	ID          uint64
	Type        uint8
	Name        string
	UnreadCount int64
	LastReadSeq uint64
	LastMessage *model.Message

	// Peer 是单聊的对方用户；群聊为 nil。
	Peer *UserSummary
}

// ListConversations 返回用户的会话列表。
func (s *ChatService) ListConversations(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]ConversationSummary, error) {
	if userID == 0 {
		return nil, ErrInvalidMessage
	}

	summaries, err := s.convRepo.ListByUser(
		ctx,
		userID,
		repository.NormalizeConversationLimit(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}

	result := make([]ConversationSummary, 0, len(summaries))

	for _, item := range summaries {
		var peer *UserSummary

		if item.Peer != nil {
			peer = &UserSummary{
				ID:       item.Peer.ID,
				Username: item.Peer.Username,
				Nickname: item.Peer.Nickname,
				Avatar:   item.Peer.Avatar,
			}
		}

		result = append(result, ConversationSummary{
			ID:          item.Conversation.ID,
			Type:        item.Conversation.Type,
			Name:        item.Conversation.Name,
			UnreadCount: item.UnreadCount,
			LastReadSeq: item.LastReadSeq,
			LastMessage: item.LastMessage,
			Peer:        peer,
		})
	}

	return result, nil
}

// CreateSingleConversation 创建或获取两人之间的单聊会话。
func (s *ChatService) CreateSingleConversation(
	ctx context.Context,
	userA uint64,
	userB uint64,
) (*model.Conversation, error) {
	if userA == 0 || userB == 0 || userA == userB {
		return nil, ErrInvalidConversation
	}

	conv, err := s.convRepo.CreateSingleConversation(ctx, userA, userB)
	if err != nil {
		if errors.Is(err, repository.ErrInvalidConversation) {
			return nil, ErrInvalidConversation
		}

		return nil, fmt.Errorf("create single conversation: %w", err)
	}

	return conv, nil
}

// SendMessage 发送一条文本消息。
//
// 流程：参数校验 → 事务落库（含 seq 分配和幂等）→ 推送给全部成员。
// 推送失败不影响消息一致性：消息已在库里，客户端可通过 seq 补拉。
func (s *ChatService) SendMessage(
	ctx context.Context,
	senderID uint64,
	conversationID uint64,
	clientMsgID string,
	contentType uint8,
	content string,
) (*SendMessageResult, error) {
	// 1. 参数校验。
	if senderID == 0 || conversationID == 0 {
		return nil, ErrInvalidMessage
	}

	clientMsgID = strings.TrimSpace(clientMsgID)
	if !clientMsgIDPattern.MatchString(clientMsgID) {
		return nil, ErrInvalidMessage
	}

	if contentType != model.MessageContentTypeText {
		return nil, ErrInvalidMessage
	}

	if strings.TrimSpace(content) == "" ||
		utf8.RuneCountInString(content) > MaxMessageContentRunes {
		return nil, ErrInvalidMessage
	}

	// 2. 事务落库。
	message, memberIDs, duplicated, err := s.msgRepo.AppendMessage(
		ctx,
		conversationID,
		senderID,
		clientMsgID,
		contentType,
		content,
	)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrConversationNotFound):
			return nil, ErrConversationNotFound

		case errors.Is(err, repository.ErrNotConversationMember):
			return nil, ErrNotConversationMember

		default:
			return nil, fmt.Errorf("append message: %w", err)
		}
	}

	// 3. 重发不重复推送，避免接收方出现重复消息。
	if duplicated {
		return &SendMessageResult{
			Message:    message,
			Duplicated: true,
		}, nil
	}

	// 4. 推送给会话全部成员（包含发送者自己，实现多端同步）。
	s.pushMessage(message, memberIDs)

	return &SendMessageResult{
		Message:    message,
		Duplicated: false,
	}, nil
}

// pushMessage 把新消息序列化后推送给会话成员。
func (s *ChatService) pushMessage(message *model.Message, memberIDs []uint64) {
	payload, err := ws.NewEnvelope(ws.TypeMessageNew, ws.MessagePayload{
		ID:             message.ID,
		ConversationID: message.ConversationID,
		SenderID:       message.SenderID,
		Seq:            message.Seq,
		ClientMsgID:    message.ClientMsgID,
		ContentType:    message.ContentType,
		Content:        message.Content,
		CreatedAt:      message.CreatedAt,
	})
	if err != nil {
		// 序列化失败属于程序错误，消息已落库，不阻塞后续流程。
		log.Printf("build message payload failed: %v", err)
		return
	}

	for _, userID := range memberIDs {
		s.pusher.SendToUser(userID, payload)
	}
}
