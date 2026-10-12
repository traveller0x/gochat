package model

import "time"

// User 表示 GoChat 的用户。
type User struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string    `gorm:"size:32;not null;uniqueIndex" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Nickname     string    `gorm:"size:64;not null;default:''" json:"nickname"`
	Avatar       string    `gorm:"size:255;not null;default:''" json:"avatar"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName 指定数据库表名。
func (User) TableName() string {
	return "users"
}
