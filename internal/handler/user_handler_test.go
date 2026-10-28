package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gochat/internal/model"
	"gochat/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// mockSearchUserRepository 是用户搜索测试用的假仓储。
type mockSearchUserRepository struct {
	users []model.User
}

func (m *mockSearchUserRepository) Create(ctx context.Context, user *model.User) error {
	return nil
}

func (m *mockSearchUserRepository) GetByUsername(
	ctx context.Context,
	username string,
) (*model.User, error) {
	for i := range m.users {
		if m.users[i].Username == username {
			return &m.users[i], nil
		}
	}

	return nil, gorm.ErrRecordNotFound
}

func (m *mockSearchUserRepository) GetByID(
	ctx context.Context,
	id uint64,
) (*model.User, error) {
	for i := range m.users {
		if m.users[i].ID == id {
			return &m.users[i], nil
		}
	}

	return nil, gorm.ErrRecordNotFound
}

func (m *mockSearchUserRepository) Search(
	ctx context.Context,
	keyword string,
	excludeUserID uint64,
	limit int,
) ([]model.User, error) {
	var result []model.User

	for _, user := range m.users {
		if user.ID == excludeUserID {
			continue
		}

		if strings.Contains(user.Username, keyword) ||
			strings.Contains(user.Nickname, keyword) {
			result = append(result, user)
		}
	}

	return result, nil
}

// newTestUserRouter 构造带 userID 注入的用户路由。
func newTestUserRouter(users []model.User, userID uint64) *gin.Engine {
	gin.SetMode(gin.TestMode)

	userService := service.NewUserService(&mockSearchUserRepository{users: users})
	h := NewUserHandler(userService)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID > 0 {
			c.Set("userID", userID)
		}

		c.Next()
	})

	r.GET("/api/v1/users/search", h.Search)

	return r
}

// TestUserHandlerSearch 测试搜索返回结果并排除自己。
func TestUserHandlerSearch(t *testing.T) {
	users := []model.User{
		{ID: 12, Username: "alice", Nickname: "Alice"},
		{ID: 27, Username: "bob", Nickname: "Bob"},
	}

	r := newTestUserRouter(users, 27)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/search?keyword=ali", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var result struct {
		Data struct {
			Users []userBrief `json:"users"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if len(result.Data.Users) != 1 {
		t.Fatalf("users = %d, want 1", len(result.Data.Users))
	}

	if result.Data.Users[0].ID != 12 {
		t.Errorf("user id = %d, want 12", result.Data.Users[0].ID)
	}
}

// TestUserHandlerSearchMissingKeyword 测试缺少关键词。
func TestUserHandlerSearchMissingKeyword(t *testing.T) {
	r := newTestUserRouter(nil, 27)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/search", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestUserHandlerSearchUnauthorized 测试未登录。
func TestUserHandlerSearchUnauthorized(t *testing.T) {
	r := newTestUserRouter(nil, 0)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/search?keyword=a", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
