package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"gochat/internal/metrics"
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

	// IsMember 判断用户是否属于会话。
	IsMember(ctx context.Context, conversationID uint64, userID uint64) (bool, error)

	// ListByUser 返回用户加入的会话列表。
	ListByUser(
		ctx context.Context,
		userID uint64,
		limit int,
	) ([]repository.ConversationSummary, error)

	// MarkRead 更新已读位置，返回实际生效值和是否有变化。
	MarkRead(
		ctx context.Context,
		conversationID uint64,
		userID uint64,
		lastReadSeq uint64,
	) (uint64, bool, error)
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

	// ListBeforeSeq 往前翻历史消息（降序）。
	ListBeforeSeq(
		ctx context.Context,
		conversationID uint64,
		beforeSeq uint64,
		limit int,
	) ([]model.Message, error)

	// ListAfterSeq 往后补洞（升序）。
	ListAfterSeq(
		ctx context.Context,
		conversationID uint64,
		afterSeq uint64,
		limit int,
	) ([]model.Message, error)
}

// messagePusher 定义消息推送能力，由 ws.Hub 实现。
// 在 service 侧定义接口（而不是依赖具体类型），方便用假实现做单元测试。
type messagePusher interface {
	SendToUser(userID uint64, payload []byte) int
}

// ChatService 负责聊天相关的业务逻辑。
type ChatService struct {
	convRepo  chatConversationRepository
	msgRepo   chatMessageRepository
	pusher    messagePusher
	pushQueue chan pushJob
	pushOnce  sync.Once
}

// NewChatService 创建聊天业务服务。
func NewChatService(
	convRepo chatConversationRepository,
	msgRepo chatMessageRepository,
	pusher messagePusher,
) *ChatService {
	return &ChatService{
		convRepo:  convRepo,
		msgRepo:   msgRepo,
		pusher:    pusher,
		pushQueue: make(chan pushJob, pushQueueSize),
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
// 流程：参数校验 → 事务落库（含 seq 分配和幂等）→ 入队推送 → 返回。
// 推送是异步的：ack 只代表"已持久化"，实时推送失败/延迟由客户端补拉兜底。
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
	// 只有真正新落库的消息才计入发送量；重发（duplicated）不算。
	metrics.MessagesSentTotal.Inc()

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

	// 只入队，不等推送完成：ack 延迟不再受扇出规模影响。
	for _, userID := range memberIDs {
		s.enqueuePush(userID, payload)
	}

}

// MessageDirection 表示消息拉取方向。
type MessageDirection uint8

const (
	// MessageDirectionLatest 不带游标，返回最近一页（降序）。
	MessageDirectionLatest MessageDirection = iota

	// MessageDirectionBefore 往前翻历史（降序）。
	MessageDirectionBefore

	// MessageDirectionAfter 往后补洞（升序）。
	MessageDirectionAfter
)

// MessageCursor 表示消息游标。
//
// 用"方向 + 序号"显式表达，而不是用 0 当"未指定"的哨兵：
// after_seq=0 是合法语义（从最早开始补洞），不能和"没传"混淆。
type MessageCursor struct {
	Direction MessageDirection
	Seq       uint64
}

// ListMessages 拉取会话消息。
//
//   - MessageDirectionBefore：往前翻历史，返回 seq 降序
//   - MessageDirectionAfter：往后补洞，返回 seq 升序
//   - MessageDirectionLatest：最近一页，返回 seq 降序
//
// hasMore 表示同方向还有更多数据。
func (s *ChatService) ListMessages(
	ctx context.Context,
	userID uint64,
	conversationID uint64,
	cursor MessageCursor,
	limit int,
) ([]model.Message, bool, error) {
	if userID == 0 || conversationID == 0 {
		return nil, false, ErrInvalidMessage
	}

	// 历史消息属于敏感数据：非成员必须拿不到。
	member, err := s.convRepo.IsMember(ctx, conversationID, userID)
	if err != nil {
		return nil, false, fmt.Errorf("check membership: %w", err)
	}

	if !member {
		return nil, false, ErrNotConversationMember
	}

	normalizedLimit := repository.NormalizeMessageLimit(limit)

	var messages []model.Message

	switch cursor.Direction {
	case MessageDirectionAfter:
		messages, err = s.msgRepo.ListAfterSeq(ctx, conversationID, cursor.Seq, normalizedLimit)

	case MessageDirectionBefore, MessageDirectionLatest:
		messages, err = s.msgRepo.ListBeforeSeq(ctx, conversationID, cursor.Seq, normalizedLimit)

	default:
		return nil, false, ErrInvalidMessage
	}

	if err != nil {
		return nil, false, fmt.Errorf("list messages: %w", err)
	}

	return messages, len(messages) == normalizedLimit, nil
}

// MarkReadResult 是已读上报的结果。
type MarkReadResult struct {
	LastReadSeq uint64
	Changed     bool
}

// MarkRead 上报已读位置，并在位置前进时广播给该用户的其他设备。
func (s *ChatService) MarkRead(
	ctx context.Context,
	userID uint64,
	conversationID uint64,
	lastReadSeq uint64,
) (*MarkReadResult, error) {
	if userID == 0 || conversationID == 0 || lastReadSeq == 0 {
		return nil, ErrInvalidMessage
	}

	effectiveSeq, changed, err := s.convRepo.MarkRead(
		ctx,
		conversationID,
		userID,
		lastReadSeq,
	)
	if err != nil {
		if errors.Is(err, repository.ErrNotConversationMember) {
			return nil, ErrNotConversationMember
		}

		return nil, fmt.Errorf("mark read: %w", err)
	}

	// 只有位置真的前进了才广播，避免滚动到底部时重复推送。
	if changed {
		s.pushRead(userID, conversationID, effectiveSeq)
	}

	return &MarkReadResult{
		LastReadSeq: effectiveSeq,
		Changed:     changed,
	}, nil
}

// pushRead 把已读位置推给该用户的所有设备（多端同步）。
func (s *ChatService) pushRead(
	userID uint64,
	conversationID uint64,
	lastReadSeq uint64,
) {
	payload, err := ws.NewEnvelope(ws.TypeMessageRead, ws.MessageReadPayload{
		ConversationID: conversationID,
		UserID:         userID,
		LastReadSeq:    lastReadSeq,
	})
	if err != nil {
		log.Printf("build read payload failed: %v", err)
		return
	}

	s.pusher.SendToUser(userID, payload)
}
