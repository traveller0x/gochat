package tests

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"gochat/internal/config"
	"gochat/internal/database"
	"gochat/internal/model"
	"gochat/internal/repository"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
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

	db, err := database.NewMySQLWithLogger(cfg.MySQL, gormlogger.Discard)

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

// newTestUser 创建一个测试用户，并注册清理逻辑。
func newTestUser(t *testing.T, db *gorm.DB, prefix string) *model.User {
	t.Helper()

	repo := repository.NewUserRepository(db)

	user := &model.User{
		Username:     fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()),
		PasswordHash: "test-password-hash",
		Nickname:     prefix,
	}

	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("create test user failed: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Where("id = ?", user.ID).Delete(&model.User{}).Error; err != nil {
			t.Errorf("cleanup user failed: %v", err)
		}
	})

	return user
}

// newTestConversationBetween 创建两个真实用户之间的单聊并注册清理。
func newTestConversationBetween(
	t *testing.T,
	db *gorm.DB,
	userA *model.User,
	userB *model.User,
) *model.Conversation {
	t.Helper()

	key := repository.SingleKey(userA.ID, userB.ID)

	conv, err := repository.NewConversationRepository(db).
		CreateSingleConversation(context.Background(), userA.ID, userB.ID)
	if err != nil {
		t.Fatalf("create test conversation failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupSingleConversation(t, db, key)
	})

	return conv
}
