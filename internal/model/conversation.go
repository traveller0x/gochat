package model

import "time"

// 会话类型。
const (
	ConversationTypeSingle uint8 = 1 // 单聊
	ConversationTypeGroup  uint8 = 2 // 群聊
)

// 会话成员角色。
const (
	MemberRoleNormal uint8 = 0 // 普通成员
	MemberRoleOwner  uint8 = 1 // 群主
)

// Conversation 表示一个单聊或群聊会话。
type Conversation struct {
	ID   uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	Type uint8  `gorm:"not null;default:1" json:"type"`

	// SingleKey 是单聊唯一键，格式为 "小用户ID:大用户ID"。
	// 群聊为 nil：依赖 MySQL 唯一索引允许多个 NULL 的特性。
	SingleKey *string `gorm:"size:64" json:"-"`

	Name    string `gorm:"size:64;not null;default:''" json:"name"`
	OwnerID uint64 `gorm:"not null;default:0" json:"owner_id"`

	// 会话列表页的冗余字段。
	LastMessageID uint64     `gorm:"not null;default:0" json:"last_message_id"`
	LastMessageAt *time.Time `json:"last_message_at"`

	// LastSeq 是分配消息序号的锚点，发消息时由行锁保护。
	LastSeq uint64 `gorm:"not null;default:0" json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定数据库表名。
func (Conversation) TableName() string {
	return "conversations"
}

// ConversationMember 表示会话与用户的关联关系。
type ConversationMember struct {
	ConversationID uint64 `gorm:"primaryKey" json:"conversation_id"`
	UserID         uint64 `gorm:"primaryKey" json:"user_id"`

	Role        uint8  `gorm:"not null;default:0" json:"role"`
	LastReadSeq uint64 `gorm:"not null;default:0" json:"last_read_seq"`

	// autoCreateTime 让 GORM 在插入时自动填入当前时间。
	// 字段名不是约定的 CreatedAt，必须显式声明，否则会写入零值时间。
	JoinedAt time.Time `gorm:"autoCreateTime" json:"joined_at"`
}

// TableName 指定数据库表名。
func (ConversationMember) TableName() string {
	return "conversation_members"
}
