package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gochat/internal/model"
	"gochat/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// mockAuthUserRepository 是 Handler 测试使用的模拟 Repository。
// 它在内存中模拟数据库，不需要连接 MySQL。
type mockAuthUserRepository struct {
	users map[string]*model.User
}

// Create 模拟创建用户，并检查用户名是否重复。
func (m *mockAuthUserRepository) Create(
	ctx context.Context,
	user *model.User,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if _, exists := m.users[user.Username]; exists {
		return fmt.Errorf(
			"username already exists: %w",
			gorm.ErrDuplicatedKey,
		)
	}

	// 模拟数据库自增 ID。
	user.ID = uint64(len(m.users) + 1)

	// 保存用户副本，避免外部修改影响模拟数据。
	userCopy := *user
	m.users[user.Username] = &userCopy

	return nil
}

// GetByUsername 根据用户名查询模拟用户。
func (m *mockAuthUserRepository) GetByUsername(
	ctx context.Context,
	username string,
) (*model.User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	user, exists := m.users[username]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}

	return user, nil
}

// GetByID 根据用户 ID 查询模拟用户。
func (m *mockAuthUserRepository) GetByID(
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

// newTestAuthHandler 创建使用模拟 Repository 的 Handler。
func newTestAuthHandler() *AuthHandler {
	repo := &mockAuthUserRepository{
		users: make(map[string]*model.User),
	}

	userService := service.NewUserService(repo)
	return NewAuthHandler(
		userService,
		"test-secret-for-gochat-handler-tests-2026",
		24,
	)
}

// performRegister 模拟发送注册 HTTP 请求。
func performRegister(
	handler *AuthHandler,
	body string,
) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/register",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = req

	handler.Register(ctx)

	return recorder
}

// decodeResponse 将 HTTP 响应解析为通用 JSON 对象。
func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()

	var result map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response JSON: %v; body=%s", err, recorder.Body.String())
	}

	return result
}

func TestAuthHandlerRegisterSuccess(t *testing.T) {
	handler := newTestAuthHandler()

	recorder := performRegister(
		handler,
		`{"username":"testuser","password":"Test123456","nickname":"测试用户"}`,
	)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s",
			recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	result := decodeResponse(t, recorder)

	if result["code"] != float64(0) {
		t.Errorf("code = %v, want 0", result["code"])
	}

	if result["message"] != "success" {
		t.Errorf("message = %v, want success", result["message"])
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data has unexpected type: %T", result["data"])
	}

	if data["username"] != "testuser" {
		t.Errorf("username = %v, want testuser", data["username"])
	}

	if data["nickname"] != "测试用户" {
		t.Errorf("nickname = %v, want 测试用户", data["nickname"])
	}

	if _, exists := data["password_hash"]; exists {
		t.Error("response must not contain password_hash")
	}

	if _, exists := data["password"]; exists {
		t.Error("response must not contain password")
	}
}

func TestAuthHandlerRegisterInvalidRequestBody(t *testing.T) {
	handler := newTestAuthHandler()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "malformed JSON",
			body: `{"username":`,
		},
		{
			name: "missing username",
			body: `{"password":"Test123456"}`,
		},
		{
			name: "missing password",
			body: `{"username":"testuser"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := performRegister(handler, tt.body)

			if recorder.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d",
					recorder.Code, http.StatusBadRequest)
			}

			result := decodeResponse(t, recorder)

			if result["code"] != float64(4001) {
				t.Errorf("code = %v, want 4001", result["code"])
			}
		})
	}
}

func TestAuthHandlerRegisterInvalidUsername(t *testing.T) {
	handler := newTestAuthHandler()

	recorder := performRegister(
		handler,
		`{"username":"ab","password":"Test123456","nickname":"测试用户"}`,
	)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d",
			recorder.Code, http.StatusBadRequest)
	}

	result := decodeResponse(t, recorder)

	if result["code"] != float64(4002) {
		t.Errorf("code = %v, want 4002", result["code"])
	}
}

func TestAuthHandlerRegisterInvalidPassword(t *testing.T) {
	handler := newTestAuthHandler()

	recorder := performRegister(
		handler,
		`{"username":"testuser","password":"1234567","nickname":"测试用户"}`,
	)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d",
			recorder.Code, http.StatusBadRequest)
	}

	result := decodeResponse(t, recorder)

	if result["code"] != float64(4003) {
		t.Errorf("code = %v, want 4003", result["code"])
	}
}

func TestAuthHandlerRegisterInvalidNickname(t *testing.T) {
	handler := newTestAuthHandler()

	nickname := strings.Repeat("测", 65)
	bodyBytes, err := json.Marshal(RegisterRequest{
		Username: "testuser",
		Password: "Test123456",
		Nickname: nickname,
	})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	recorder := performRegister(handler, string(bodyBytes))

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d",
			recorder.Code, http.StatusBadRequest)
	}

	result := decodeResponse(t, recorder)

	if result["code"] != float64(4004) {
		t.Errorf("code = %v, want 4004", result["code"])
	}
}

func TestAuthHandlerRegisterDuplicateUsername(t *testing.T) {
	handler := newTestAuthHandler()

	body := `{"username":"testuser","password":"Test123456","nickname":"测试用户"}`

	// 第一次注册应该成功。
	first := performRegister(handler, body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first registration status = %d, want %d",
			first.Code, http.StatusCreated)
	}

	// 第二次注册相同用户名应该失败。
	second := performRegister(handler, body)

	if second.Code != http.StatusConflict {
		t.Fatalf("second registration status = %d, want %d; body=%s",
			second.Code, http.StatusConflict, second.Body.String())
	}

	result := decodeResponse(t, second)

	if result["code"] != float64(4005) {
		t.Errorf("code = %v, want 4005", result["code"])
	}
}
