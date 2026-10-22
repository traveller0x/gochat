package service

import (
	"context"
	"errors"
	"fmt"
	"gochat/internal/model"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// mockUserRepository 是测试专用的模拟 Repository。

// 它不连接 MySQL，而是在内存中模拟用户创建操作。

type mockUserRepository struct {
	users map[string]*model.User
}

// Create 模拟创建用户，并检查用户名是否重复。

func (m *mockUserRepository) Create(

	ctx context.Context,
	user *model.User,

) error {

	if err := ctx.Err(); err != nil {
		return err
	}
	if _, exists := m.users[user.Username]; exists {
		return fmt.Errorf("username already exists: %w", gorm.ErrDuplicatedKey)
	}
	// 模拟数据库自增 ID。
	user.ID = uint64(len(m.users) + 1)
	// 保存用户副本，避免测试中修改原对象影响模拟数据。
	userCopy := *user
	m.users[user.Username] = &userCopy
	return nil

}

// GetByUsername 根据用户名查询模拟用户。
func (m *mockUserRepository) GetByUsername(
	ctx context.Context,
	username string,
) (*model.User, error) {
	user, exists := m.users[username]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}

	return user, nil
}

// GetByID 根据用户 ID 查询模拟用户。
func (m *mockUserRepository) GetByID(
	ctx context.Context,
	id uint64,
) (*model.User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for _, user := range m.users {
		if user.ID == id {
			return user, nil
		}
	}

	return nil, gorm.ErrRecordNotFound
}

// newTestUserService 创建使用模拟 Repository 的 UserService。

func newTestUserService() *UserService {

	repo := &mockUserRepository{
		users: make(map[string]*model.User),
	}
	return NewUserService(repo)

}

func TestUserServiceRegisterSuccess(t *testing.T) {

	svc := newTestUserService()
	user, err := svc.Register(
		context.Background(),
		"testuser",
		"Test123456",
		"测试用户",
	)
	if err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}
	if user == nil {
		t.Fatal("Register() returned nil user")
	}
	if user.ID == 0 {
		t.Error("expected user ID to be non-zero")
	}
	if user.Username != "testuser" {
		t.Errorf("username = %q, want %q", user.Username, "testuser")
	}
	if user.Nickname != "测试用户" {
		t.Errorf("nickname = %q, want %q", user.Nickname, "测试用户")
	}
	if user.PasswordHash == "Test123456" {
		t.Fatal("password must not be stored as plaintext")
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte("Test123456"),
	); err != nil {
		t.Errorf("password hash cannot verify original password: %v", err)
	}

}

func TestUserServiceRegisterInvalidUsername(t *testing.T) {

	tests := []struct {
		name     string
		username string
	}{
		{name: "too short", username: "ab"},
		{name: "contains special character", username: "test-user"},
		{name: "empty", username: ""},
		{name: "too long", username: strings.Repeat("a", 33)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestUserService()
			_, err := svc.Register(
				context.Background(),
				tt.username,
				"Test123456",
				"测试用户",
			)
			if !errors.Is(err, ErrInvalidUsername) {
				t.Errorf("error = %v, want %v", err, ErrInvalidUsername)
			}
		})
	}

}

func TestUserServiceRegisterInvalidPassword(t *testing.T) {

	tests := []struct {
		name     string
		password string
	}{
		{name: "too short", password: "1234567"},
		{name: "empty", password: ""},
		{name: "too long", password: strings.Repeat("a", 73)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestUserService()
			_, err := svc.Register(
				context.Background(),
				"testuser",
				tt.password,
				"测试用户",
			)
			if !errors.Is(err, ErrInvalidPassword) {
				t.Errorf("error = %v, want %v", err, ErrInvalidPassword)
			}
		})
	}

}

func TestUserServiceRegisterInvalidNickname(t *testing.T) {

	svc := newTestUserService()
	// 65 个中文字符，超过昵称允许的 64 个字符。
	nickname := strings.Repeat("测", 65)
	_, err := svc.Register(
		context.Background(),
		"testuser",
		"Test123456",
		nickname,
	)
	if !errors.Is(err, ErrInvalidNickname) {
		t.Errorf("error = %v, want %v", err, ErrInvalidNickname)
	}

}

func TestUserServiceRegisterDuplicateUsername(t *testing.T) {

	svc := newTestUserService()
	ctx := context.Background()
	_, err := svc.Register(ctx, "testuser", "Test123456", "用户一")
	if err != nil {
		t.Fatalf("first Register() unexpected error: %v", err)
	}
	_, err = svc.Register(ctx, "testuser", "Another123", "用户二")
	if !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("error = %v, want %v", err, ErrUsernameTaken)
	}

}

// TestUserServiceLoginSuccess 测试正确的用户名和密码可以登录。
func TestUserServiceLoginSuccess(t *testing.T) {
	svc := newTestUserService()
	ctx := context.Background()

	// 先注册一个用户，模拟数据库中已经存在该用户。
	_, err := svc.Register(ctx, "testuser", "Test123456", "测试用户")
	if err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}

	// 使用正确的用户名和密码登录。
	user, err := svc.Login(ctx, "testuser", "Test123456")
	if err != nil {
		t.Fatalf("Login() unexpected error: %v", err)
	}

	if user == nil {
		t.Fatal("Login() returned nil user")
	}

	if user.Username != "testuser" {
		t.Errorf("username = %q, want %q", user.Username, "testuser")
	}

	if user.PasswordHash == "Test123456" {
		t.Error("password must not be stored as plaintext")
	}
}

// TestUserServiceLoginWrongPassword 测试密码错误时登录失败。
func TestUserServiceLoginWrongPassword(t *testing.T) {
	svc := newTestUserService()
	ctx := context.Background()

	_, err := svc.Register(ctx, "testuser", "Test123456", "测试用户")
	if err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}

	_, err = svc.Login(ctx, "testuser", "Wrong123456")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("error = %v, want %v", err, ErrInvalidCredentials)
	}
}

// TestUserServiceLoginUserNotFound 测试用户不存在时登录失败。
func TestUserServiceLoginUserNotFound(t *testing.T) {
	svc := newTestUserService()

	_, err := svc.Login(context.Background(), "unknownuser", "Test123456")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("error = %v, want %v", err, ErrInvalidCredentials)
	}
}

// TestUserServiceLoginEmptyCredentials 测试用户名或密码为空时登录失败。
func TestUserServiceLoginEmptyCredentials(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{
			name:     "empty username",
			username: "",
			password: "Test123456",
		},
		{
			name:     "empty password",
			username: "testuser",
			password: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestUserService()

			_, err := svc.Login(
				context.Background(),
				tt.username,
				tt.password,
			)

			if !errors.Is(err, ErrInvalidCredentials) {
				t.Errorf("error = %v, want %v", err, ErrInvalidCredentials)
			}
		})
	}
}
