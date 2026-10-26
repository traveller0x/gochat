package repository

import (
	"context"
	"errors"
	"fmt"

	"gochat/internal/model"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// CreateSingleMaxRetries 是并发创建单聊时允许的最大重试次数。
const CreateSingleMaxRetries = 3

var (
	// ErrInvalidConversation 表示创建会话的参数非法。
	ErrInvalidConversation = errors.New("invalid conversation")

	// ErrCreateConversationRetryExceeded 表示并发冲突重试次数耗尽。
	ErrCreateConversationRetryExceeded = errors.New("create conversation retry exceeded")
)

// ConversationRepository 负责会话数据的持久化操作。
type ConversationRepository struct {
	db *gorm.DB
}

// NewConversationRepository 创建会话数据访问对象。
func NewConversationRepository(db *gorm.DB) *ConversationRepository {
	return &ConversationRepository{db: db}
}

// SingleKey 生成单聊会话的唯一键。
//
// 约定小 ID 在前，保证 A→B 和 B→A 得到同一个键。
func SingleKey(userA uint64, userB uint64) string {
	if userA > userB {
		userA, userB = userB, userA
	}

	return fmt.Sprintf("%d:%d", userA, userB)
}

// CreateSingleConversation 幂等地创建（或获取）两人之间的单聊会话。
//
// 幂等性由 conversations.single_key 的唯一索引保证：
// 并发请求只有一个能插入成功，其余会收到唯一键冲突，
// 冲突后重新查询即可拿到同一条会话。
func (r *ConversationRepository) CreateSingleConversation(
	ctx context.Context,
	userA uint64,
	userB uint64,
) (*model.Conversation, error) {
	if userA == 0 || userB == 0 || userA == userB {
		return nil, ErrInvalidConversation
	}

	key := SingleKey(userA, userB)

	for attempt := 0; attempt < CreateSingleMaxRetries; attempt++ {
		// 1. 先查：绝大多数请求在这里命中。
		conv, err := r.GetBySingleKey(ctx, key)
		if err == nil {
			return conv, nil
		}

		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}

		// 2. 不存在则在一个事务里创建会话和两个成员。
		conv, err = r.createSingle(ctx, key, userA, userB)
		if err == nil {
			return conv, nil
		}

		// 3. 并发冲突（唯一键冲突或死锁）：别的请求先创建成功，重查即可。
		if isRetryableCreateError(err) {
			continue
		}

		return nil, err
	}

	return nil, ErrCreateConversationRetryExceeded
}

// GetBySingleKey 根据单聊唯一键查询会话。
func (r *ConversationRepository) GetBySingleKey(
	ctx context.Context,
	key string,
) (*model.Conversation, error) {
	var conv model.Conversation

	err := r.db.WithContext(ctx).
		Where("single_key = ?", key).
		First(&conv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}

		return nil, fmt.Errorf("get conversation by single key: %w", err)
	}

	return &conv, nil
}

// GetByID 根据会话 ID 查询会话。
func (r *ConversationRepository) GetByID(
	ctx context.Context,
	id uint64,
) (*model.Conversation, error) {
	var conv model.Conversation

	err := r.db.WithContext(ctx).First(&conv, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}

		return nil, fmt.Errorf("get conversation by id: %w", err)
	}

	return &conv, nil
}

// IsMember 判断用户是否属于该会话。
//
// 发送消息前必须校验，否则任何登录用户都能往别人的会话里发消息。
func (r *ConversationRepository) IsMember(
	ctx context.Context,
	conversationID uint64,
	userID uint64,
) (bool, error) {
	if conversationID == 0 || userID == 0 {
		return false, nil
	}

	var count int64

	err := r.db.WithContext(ctx).
		Model(&model.ConversationMember{}).
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check conversation member: %w", err)
	}

	return count > 0, nil
}

// createSingle 在一个事务里创建会话和两个成员。
func (r *ConversationRepository) createSingle(
	ctx context.Context,
	key string,
	userA uint64,
	userB uint64,
) (*model.Conversation, error) {
	conv := &model.Conversation{
		Type:      model.ConversationTypeSingle,
		SingleKey: &key,
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(conv).Error; err != nil {
			return err
		}

		members := []model.ConversationMember{
			{ConversationID: conv.ID, UserID: userA},
			{ConversationID: conv.ID, UserID: userB},
		}

		if err := tx.Create(&members).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create single conversation: %w", err)
	}

	return conv, nil
}

// isRetryableCreateError 判断失败是否由并发竞争引起，可以安全重试。
//
// 1062 = 唯一键冲突；1213 = 死锁（并发插入同一唯一键时偶尔出现）。
func isRetryableCreateError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1213 {
		return true
	}

	return false
}
