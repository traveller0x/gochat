package model

import "time"

// 消息内容类型。
const (
	MessageContentTypeText uint8 = 1 // 文本
)

// Message 表示一条聊天消息。
type Message struct {
	ID uint64 `gorm:"primaryKey;autoIncrement" json:"id"`

	ConversationID uint64 `gorm:"not null" json:"conversation_id"`
	SenderID       uint64 `gorm:"not null" json:"sender_id"`

	// Seq 是会话内连续递增的序号，由 ChatService 在事务中分配。
	Seq uint64 `gorm:"not null" json:"seq"`

	// ClientMsgID 由客户端生成，用于重发幂等。
	ClientMsgID string `gorm:"size:64;not null" json:"client_msg_id"`

	ContentType uint8  `gorm:"not null;default:1" json:"content_type"`
	Content     string `gorm:"type:text;not null" json:"content"`

	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定数据库表名。
func (Message) TableName() string {
	return "messages"
}
