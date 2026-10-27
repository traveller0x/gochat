package repository

import (
	"context"
	"errors"
	"fmt"

	"gochat/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// DefaultMessagePageSize 是历史消息的默认分页大小。
	DefaultMessagePageSize = 20

	// MaxMessagePageSize 是单页允许的最大条数，防止客户端一次拉取过多数据。
	MaxMessagePageSize = 100
)

var (
	// ErrDuplicateMessage 表示同一会话内 client_msg_id 已存在，属于重发。
	ErrDuplicateMessage = errors.New("duplicate client message id")

	// ErrSeqConflict 表示同一会话内 seq 已被占用，属于并发写入冲突。
	ErrSeqConflict = errors.New("conversation seq conflict")

	// ErrConversationNotFound 表示会话不存在。
	ErrConversationNotFound = errors.New("conversation not found")

	// ErrNotConversationMember 表示发送者不是会话成员。
	ErrNotConversationMember = errors.New("not a conversation member")
)

// MessageRepository 负责消息数据的持久化操作。
type MessageRepository struct {
	db *gorm.DB
}

// NewMessageRepository 创建消息数据访问对象。
func NewMessageRepository(db *gorm.DB) *MessageRepository {
	return &MessageRepository{db: db}
}

// Create 插入一条消息。
//
// 违反唯一键时区分两种原因：
//   - client_msg_id 重复 → ErrDuplicateMessage（调用方应视为发送成功，返回原消息）
//   - seq 重复 → ErrSeqConflict（调用方应重新分配序号后重试）
func (r *MessageRepository) Create(
	ctx context.Context,
	msg *model.Message,
) error {
	err := r.db.WithContext(ctx).Create(msg).Error
	if err == nil {
		return nil
	}

	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		return fmt.Errorf("create message: %w", err)
	}

	// 唯一键冲突有两种可能，用 client_msg_id 能否查到区分。
	existing, getErr := r.GetByClientMsgID(ctx, msg.ConversationID, msg.ClientMsgID)
	if getErr == nil && existing != nil {
		return ErrDuplicateMessage
	}

	return ErrSeqConflict
}

// GetByClientMsgID 按会话 + 客户端消息 ID 查询消息。
func (r *MessageRepository) GetByClientMsgID(
	ctx context.Context,
	conversationID uint64,
	clientMsgID string,
) (*model.Message, error) {
	var msg model.Message

	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND client_msg_id = ?", conversationID, clientMsgID).
		First(&msg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}

		return nil, fmt.Errorf("get message by client msg id: %w", err)
	}

	return &msg, nil
}

// ListBeforeSeq 按 seq 游标分页拉取历史消息。
//
// beforeSeq = 0 表示从最新一条开始；返回结果按 seq 降序（最新在前）。
//
// 用游标而不是 OFFSET 的原因：
//   - OFFSET 深翻页必须扫描并丢弃前 N 行，越翻越慢
//   - 新消息插入会改变行号，导致重复或漏读
func (r *MessageRepository) ListBeforeSeq(
	ctx context.Context,
	conversationID uint64,
	beforeSeq uint64,
	limit int,
) ([]model.Message, error) {
	if conversationID == 0 {
		return nil, errors.New("invalid conversation id")
	}

	query := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID)

	if beforeSeq > 0 {
		query = query.Where("seq < ?", beforeSeq)
	}

	var messages []model.Message

	err := query.
		Order("seq DESC").
		Limit(NormalizeMessageLimit(limit)).
		Find(&messages).Error
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	return messages, nil
}

// ListAfterSeq 拉取 afterSeq 之后的消息，按 seq 升序返回。
//
// 用于断线重连补洞：客户端保存本地最大 seq，重连后从那里往后拉。
func (r *MessageRepository) ListAfterSeq(
	ctx context.Context,
	conversationID uint64,
	afterSeq uint64,
	limit int,
) ([]model.Message, error) {
	if conversationID == 0 {
		return nil, errors.New("invalid conversation id")
	}

	query := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID)

	if afterSeq > 0 {
		query = query.Where("seq > ?", afterSeq)
	}

	var messages []model.Message

	err := query.
		Order("seq ASC").
		Limit(NormalizeMessageLimit(limit)).
		Find(&messages).Error
	if err != nil {
		return nil, fmt.Errorf("list messages after seq: %w", err)
	}

	return messages, nil
}

// NormalizeMessageLimit 把分页大小限制在合理区间。
func NormalizeMessageLimit(limit int) int {
	if limit <= 0 {
		return DefaultMessagePageSize
	}

	if limit > MaxMessagePageSize {
		return MaxMessagePageSize
	}

	return limit
}

// AppendMessage 在事务中写入一条消息并更新会话冗余字段。
//
// 返回值：
//   - msg: 写入的消息；重发时是数据库里已存在的那条
//   - memberIDs: 会话全部成员，调用方据此推送
//   - duplicated: true 表示 client_msg_id 重复，调用方不应再次推送
//
// 事务内做四件事，保证强一致：
//  1. SELECT ... FOR UPDATE 锁住会话行，串行化 seq 分配
//  2. 校验发送者是会话成员
//  3. 幂等检查 client_msg_id
//  4. 写消息 + 更新 conversations.last_seq/last_message_id/last_message_at
func (r *MessageRepository) AppendMessage(
	ctx context.Context,
	conversationID uint64,
	senderID uint64,
	clientMsgID string,
	contentType uint8,
	content string,
) (*model.Message, []uint64, bool, error) {
	if conversationID == 0 || senderID == 0 || clientMsgID == "" {
		return nil, nil, false, errors.New("invalid append message params")
	}

	var (
		message    model.Message
		duplicated bool
	)

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 锁住会话行。同一会话的并发发送在这里排队，
		//    这是 seq 不重复、不空洞的根本保证。
		var conv model.Conversation

		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&conv, conversationID).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrConversationNotFound
			}

			return fmt.Errorf("lock conversation: %w", err)
		}

		// 2. 校验发送者身份：与写入处于同一事务，避免校验后被踢出会话的竞态。
		var memberCount int64

		if err := tx.Model(&model.ConversationMember{}).
			Where("conversation_id = ? AND user_id = ?", conversationID, senderID).
			Count(&memberCount).Error; err != nil {
			return fmt.Errorf("check membership: %w", err)
		}

		if memberCount == 0 {
			return ErrNotConversationMember
		}

		// 3. 幂等：client_msg_id 已存在，直接复用已有消息。
		var existing model.Message

		lookupErr := tx.Where(
			"conversation_id = ? AND client_msg_id = ?",
			conversationID,
			clientMsgID,
		).First(&existing).Error
		if lookupErr == nil {
			message = existing
			duplicated = true

			return nil
		}

		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("lookup existing message: %w", lookupErr)
		}

		// 4. 分配会话内序号并写入消息。
		nextSeq := conv.LastSeq + 1

		message = model.Message{
			ConversationID: conversationID,
			SenderID:       senderID,
			Seq:            nextSeq,
			ClientMsgID:    clientMsgID,
			ContentType:    contentType,
			Content:        content,
		}

		if err := tx.Create(&message).Error; err != nil {
			// 极端情况：两个请求同时通过了第 3 步检查。
			// 交给唯一索引兜底，提交后再查一次拿到已存在的那条。
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				duplicated = true

				return nil
			}

			return fmt.Errorf("create message: %w", err)
		}

		// 5. 更新会话冗余字段，供会话列表页直接读取。
		if err := tx.Model(&model.Conversation{}).
			Where("id = ?", conversationID).
			Updates(map[string]any{
				"last_seq":        nextSeq,
				"last_message_id": message.ID,
				"last_message_at": message.CreatedAt,
			}).Error; err != nil {
			return fmt.Errorf("update conversation: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, nil, false, err
	}

	// 兜底：上面第 4 步因唯一索引冲突而 duplicated 时，message 还是空的。
	if duplicated && message.ID == 0 {
		existing, err := r.GetByClientMsgID(ctx, conversationID, clientMsgID)
		if err != nil {
			return nil, nil, false, fmt.Errorf("load duplicated message: %w", err)
		}

		message = *existing
	}

	memberIDs, err := r.listMemberIDs(ctx, conversationID)
	if err != nil {
		return nil, nil, false, err
	}

	return &message, memberIDs, duplicated, nil
}

// listMemberIDs 返回会话全部成员 ID，用于消息推送。
func (r *MessageRepository) listMemberIDs(
	ctx context.Context,
	conversationID uint64,
) ([]uint64, error) {
	var ids []uint64

	err := r.db.WithContext(ctx).
		Model(&model.ConversationMember{}).
		Where("conversation_id = ?", conversationID).
		Order("user_id").
		Pluck("user_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("list conversation members: %w", err)
	}

	return ids, nil
}
