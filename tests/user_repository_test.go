package tests

import (
	"context"
	"errors"
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

// TestUserRepository 测试用户数据访问层。
func TestUserRepository(t *testing.T) {
	// 1. 检查数据库密码是否配置。
	password, exists := os.LookupEnv("GOCHAT_MYSQL_PASSWORD")
	if !exists || password == "" {
		t.Skip("GOCHAT_MYSQL_PASSWORD is not set; skipping MySQL integration test")
	}

	// 2. 加载项目配置。
	cfg, err := config.Load("../configs/config.yaml")
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	// 3. 创建数据库连接。
	db, err := database.NewMySQLWithLogger(cfg.MySQL, gormlogger.Discard)

	if err != nil {
		t.Fatalf("connect to MySQL failed: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get SQL DB failed: %v", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close MySQL connection failed: %v", err)
		}
	}()

	// 4. 创建 Repository。
	userRepo := repository.NewUserRepository(db)
	ctx := context.Background()

	// 使用唯一用户名，避免与其他测试数据冲突。
	username := fmt.Sprintf("repo_test_%d", time.Now().UnixNano())

	user := &model.User{
		Username:     username,
		PasswordHash: "test-password-hash",
		Nickname:     "Repository测试",
		Avatar:       "",
	}

	// 5. 测试创建用户。
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	if user.ID == 0 {
		t.Fatal("expected user ID to be generated")
	}

	// 测试完成后删除本次创建的用户。
	defer func() {
		if err := db.WithContext(ctx).
			Where("username = ?", username).
			Delete(&model.User{}).Error; err != nil {
			t.Errorf("cleanup test user failed: %v", err)
		}
	}()

	// 6. 测试根据用户名查询用户。
	got, err := userRepo.GetByUsername(ctx, username)
	if err != nil {
		t.Fatalf("get user by username failed: %v", err)
	}

	if got.Username != username {
		t.Errorf("expected username %q, got %q", username, got.Username)
	}

	if got.Nickname != user.Nickname {
		t.Errorf("expected nickname %q, got %q", user.Nickname, got.Nickname)
	}

	if got.ID != user.ID {
		t.Errorf("expected user ID %d, got %d", user.ID, got.ID)
	}

	// 7. 测试重复用户名。
	duplicateUser := &model.User{
		Username:     username,
		PasswordHash: "another-test-hash",
		Nickname:     "重复用户",
	}

	err = userRepo.Create(ctx, duplicateUser)
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Errorf("expected gorm.ErrDuplicatedKey, got %v", err)
	}

	// 8. 测试查询不存在的用户。
	missingUsername := fmt.Sprintf("missing_%d", time.Now().UnixNano())

	_, err = userRepo.GetByUsername(ctx, missingUsername)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected gorm.ErrRecordNotFound, got %v", err)
	}
}

// TestUserRepositorySearch 测试模糊搜索、排除自己和 LIKE 通配符转义。
func TestUserRepositorySearch(t *testing.T) {
	db := openTestDB(t)

	repo := repository.NewUserRepository(db)
	ctx := context.Background()

	// 每次运行使用唯一前缀，两个用户共享它，便于"用同一关键词搜到两人"。
	prefix := fmt.Sprintf("search_%d", time.Now().UnixNano())

	users := []*model.User{
		{
			Username:     prefix + "_a",
			PasswordHash: "test-hash",
			Nickname:     prefix + "甲",
		},
		{
			Username:     prefix + "_b",
			PasswordHash: "test-hash",
			Nickname:     prefix + "乙",
		},
	}

	for _, user := range users {
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("create user failed: %v", err)
		}
	}

	t.Cleanup(func() {
		for _, user := range users {
			if err := db.Where("id = ?", user.ID).Delete(&model.User{}).Error; err != nil {
				t.Errorf("cleanup user failed: %v", err)
			}
		}
	})

	// ① 按用户名片段搜索，命中第一个用户。
	found, err := repo.Search(ctx, prefix+"_a", 0, 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if !containsUser(found, users[0].ID) {
		t.Fatalf("search %q = %+v, want user %d", prefix+"_a", found, users[0].ID)
	}

	// ② 用共同前缀搜索并排除自己：看不到自己，但能看到对方。
	found, err = repo.Search(ctx, prefix, users[0].ID, 10)
	if err != nil {
		t.Fatalf("search exclude failed: %v", err)
	}

	if containsUser(found, users[0].ID) {
		t.Errorf("exclude result contains self %d", users[0].ID)
	}

	if !containsUser(found, users[1].ID) {
		t.Errorf("exclude result = %+v, want user %d", found, users[1].ID)
	}

	// ③ "%" 必须被转义，否则会返回全表。
	found, err = repo.Search(ctx, "%", 0, 50)
	if err != nil {
		t.Fatalf("search wildcard failed: %v", err)
	}

	if containsUser(found, users[0].ID) || containsUser(found, users[1].ID) {
		t.Fatalf("wildcard %% should not match test users: %+v", found)
	}

	// ④ 中文昵称搜索。
	found, err = repo.Search(ctx, prefix+"乙", 0, 10)
	if err != nil {
		t.Fatalf("search nickname failed: %v", err)
	}

	if !containsUser(found, users[1].ID) {
		t.Fatalf("nickname search = %+v, want user %d", found, users[1].ID)
	}
}

// containsUser 判断搜索结果中是否包含指定用户。
func containsUser(users []model.User, id uint64) bool {
	for _, user := range users {
		if user.ID == id {
			return true
		}
	}

	return false
}
