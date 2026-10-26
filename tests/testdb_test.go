package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"gochat/internal/config"
	"gochat/internal/database"
	"gochat/internal/model"
	"gochat/internal/repository"

	"gorm.io/gorm"
)

// openTestDB 打开真实 MySQL 连接；未配置密码时跳过测试。
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	password, exists := os.LookupEnv("GOCHAT_MYSQL_PASSWORD")
	if !exists || password == "" {
		t.Skip("GOCHAT_MYSQL_PASSWORD is not set; skipping MySQL integration test")
	}

	cfg, err := config.Load("../configs/config.yaml")
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	db, err := database.NewMySQL(cfg.MySQL)
	if err != nil {
		t.Fatalf("connect to MySQL failed: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get SQL DB failed: %v", err)
	}

	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close MySQL connection failed: %v", err)
		}
	})

	return db
}

// cleanupSingleConversation 删除指定 single_key 的会话、成员和消息。
func cleanupSingleConversation(t *testing.T, db *gorm.DB, key string) {
	t.Helper()

	var ids []uint64

	if err := db.Model(&model.Conversation{}).
		Where("single_key = ?", key).
		Pluck("id", &ids).Error; err != nil {
		t.Fatalf("query conversations for cleanup failed: %v", err)
	}

	for _, id := range ids {
		if err := db.Where("conversation_id = ?", id).
			Delete(&model.Message{}).Error; err != nil {
			t.Fatalf("cleanup messages failed: %v", err)
		}

		if err := db.Where("conversation_id = ?", id).
			Delete(&model.ConversationMember{}).Error; err != nil {
			t.Fatalf("cleanup members failed: %v", err)
		}

		if err := db.Where("id = ?", id).
			Delete(&model.Conversation{}).Error; err != nil {
			t.Fatalf("cleanup conversation failed: %v", err)
		}
	}
}

// newTestConversationWithMembers 创建一条测试单聊，返回会话和两个成员 ID。
// 测试结束后自动清理会话、成员和消息。
func newTestConversationWithMembers(
	t *testing.T,
	db *gorm.DB,
) (*model.Conversation, uint64, uint64) {
	t.Helper()

	base := uint64(time.Now().UnixNano())
	userA, userB := base, base+1
	key := repository.SingleKey(userA, userB)

	conv, err := repository.NewConversationRepository(db).
		CreateSingleConversation(context.Background(), userA, userB)
	if err != nil {
		t.Fatalf("create test conversation failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupSingleConversation(t, db, key)
	})

	return conv, userA, userB
}
