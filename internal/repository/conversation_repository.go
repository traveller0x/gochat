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

const (
	// DefaultConversationPageSize 是会话列表的默认条数。
	DefaultConversationPageSize = 50

	// MaxConversationPageSize 是会话列表单页最大条数。
	MaxConversationPageSize = 100
)

// ConversationSummary 是会话列表需要的聚合数据。
type ConversationSummary struct {
	Conversation model.Conversation

	// LastReadSeq 是该用户在这个会话里的已读位置。
	LastReadSeq uint64

	// UnreadCount 是未读数，只统计别人发的消息。
	UnreadCount int64

	// LastMessage 是会话最后一条消息，新会话可能为 nil。
	LastMessage *model.Message

	// Peer 是单聊的对方用户；群聊为 nil。
	Peer *model.User
}

// NormalizeConversationLimit 把会话列表分页大小限制在合理区间。
func NormalizeConversationLimit(limit int) int {
	if limit <= 0 {
		return DefaultConversationPageSize
	}

	if limit > MaxConversationPageSize {
		return MaxConversationPageSize
	}

	return limit
}

// ListByUser 返回用户加入的会话列表，附带未读数、最后一条消息和单聊对方信息。
//
// 固定 4 条 SQL，避免 N+1：
//  1. 查会话列表（按最近活跃排序）
//  2. 一次分组统计所有会话的未读数
//  3. 一次 IN 查询批量加载最后一条消息
//  4. 一次 IN 查询批量加载单聊的对方用户
func (r *ConversationRepository) ListByUser(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]ConversationSummary, error) {
	if userID == 0 {
		return nil, ErrInvalidConversation
	}

	limit = NormalizeConversationLimit(limit)

	// 1. 该用户加入的会话。
	var conversations []model.Conversation

	err := r.db.WithContext(ctx).
		Select("conversations.*").
		Joins("JOIN conversation_members ON conversation_members.conversation_id = conversations.id").
		Where("conversation_members.user_id = ?", userID).
		Order("COALESCE(conversations.last_message_at, conversations.created_at) DESC, conversations.id DESC").
		Limit(limit).
		Find(&conversations).Error
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}

	if len(conversations) == 0 {
		return []ConversationSummary{}, nil
	}

	conversationIDs := make([]uint64, 0, len(conversations))
	singleConversationIDs := make([]uint64, 0, len(conversations))
	lastMessageIDs := make([]uint64, 0, len(conversations))

	for _, conv := range conversations {
		conversationIDs = append(conversationIDs, conv.ID)

		if conv.Type == model.ConversationTypeSingle {
			singleConversationIDs = append(singleConversationIDs, conv.ID)
		}

		if conv.LastMessageID > 0 {
			lastMessageIDs = append(lastMessageIDs, conv.LastMessageID)
		}
	}

	// 2. 一次统计所有会话的未读数。
	//    过滤条件写在 JOIN 里：只统计别人发的、且 seq 大于自己已读位置的消息。
	type unreadRow struct {
		ConversationID uint64
		LastReadSeq    uint64
		UnreadCount    int64
	}

	var unreadRows []unreadRow

	err = r.db.WithContext(ctx).
		Model(&model.ConversationMember{}).
		Select(`
			conversation_members.conversation_id AS conversation_id,
			conversation_members.last_read_seq AS last_read_seq,
			COUNT(messages.id) AS unread_count
		`).
		Joins(`
			LEFT JOIN messages
				ON messages.conversation_id = conversation_members.conversation_id
				AND messages.seq > conversation_members.last_read_seq
				AND messages.sender_id <> conversation_members.user_id
		`).
		Where(
			"conversation_members.user_id = ? AND conversation_members.conversation_id IN ?",
			userID,
			conversationIDs,
		).
		Group("conversation_members.conversation_id, conversation_members.last_read_seq").
		Scan(&unreadRows).Error
	if err != nil {
		return nil, fmt.Errorf("count unread messages: %w", err)
	}

	unreadByConversation := make(map[uint64]unreadRow, len(unreadRows))
	for _, row := range unreadRows {
		unreadByConversation[row.ConversationID] = row
	}

	// 3. 一次加载全部最后一条消息。
	lastMessages := make(map[uint64]*model.Message, len(lastMessageIDs))

	if len(lastMessageIDs) > 0 {
		var messages []model.Message

		if err := r.db.WithContext(ctx).
			Where("id IN ?", lastMessageIDs).
			Find(&messages).Error; err != nil {
			return nil, fmt.Errorf("load last messages: %w", err)
		}

		for i := range messages {
			lastMessages[messages[i].ID] = &messages[i]
		}
	}

	// 4. 一次加载单聊的对方用户信息。
	//    显式指定字段，绝不把 password_hash 读进内存。
	type peerRow struct {
		ConversationID uint64
		ID             uint64
		Username       string
		Nickname       string
		Avatar         string
	}

	peerByConversation := make(map[uint64]*model.User, len(singleConversationIDs))

	if len(singleConversationIDs) > 0 {
		var peerRows []peerRow

		err = r.db.WithContext(ctx).
			Table("conversation_members").
			Select(`
				conversation_members.conversation_id AS conversation_id,
				users.id AS id,
				users.username AS username,
				users.nickname AS nickname,
				users.avatar AS avatar
			`).
			Joins("JOIN users ON users.id = conversation_members.user_id").
			Where(
				"conversation_members.conversation_id IN ? AND conversation_members.user_id <> ?",
				singleConversationIDs,
				userID,
			).
			Scan(&peerRows).Error
		if err != nil {
			return nil, fmt.Errorf("load conversation peers: %w", err)
		}

		for _, row := range peerRows {
			peerByConversation[row.ConversationID] = &model.User{
				ID:       row.ID,
				Username: row.Username,
				Nickname: row.Nickname,
				Avatar:   row.Avatar,
			}
		}
	}

	summaries := make([]ConversationSummary, 0, len(conversations))

	for _, conv := range conversations {
		row := unreadByConversation[conv.ID]

		summaries = append(summaries, ConversationSummary{
			Conversation: conv,
			LastReadSeq:  row.LastReadSeq,
			UnreadCount:  row.UnreadCount,
			LastMessage:  lastMessages[conv.LastMessageID],
			Peer:         peerByConversation[conv.ID],
		})
	}

	return summaries, nil
}
