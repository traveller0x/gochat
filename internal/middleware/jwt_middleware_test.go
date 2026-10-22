package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"gochat/internal/auth"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const middlewareTestSecret = "gochat-middleware-test-secret-2026"

func init() {
	gin.SetMode(gin.TestMode)
}

// executeProtectedRequest 模拟访问一个受 JWT 保护的接口。
func executeProtectedRequest(token string) *httptest.ResponseRecorder {
	r := gin.New()

	r.GET(
		"/protected",
		JWTAuth(middlewareTestSecret),
		func(c *gin.Context) {
			userID, exists := c.Get("userID")
			if !exists {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "userID missing",
				})
				return
			}

			username, exists := c.Get("username")
			if !exists {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "username missing",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"userID":   userID,
				"username": username,
			})
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)

	return recorder
}

// TestJWTAuthMissingToken 测试未携带 Token 的请求。
func TestJWTAuthMissingToken(t *testing.T) {
	recorder := executeProtectedRequest("")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusUnauthorized,
			recorder.Code,
		)
	}
}

// TestJWTAuthInvalidToken 测试无效 Token。
func TestJWTAuthInvalidToken(t *testing.T) {
	recorder := executeProtectedRequest("invalid.token.value")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusUnauthorized,
			recorder.Code,
		)
	}
}

// TestJWTAuthMalformedAuthorization 测试错误的 Authorization 格式。
func TestJWTAuthMalformedAuthorization(t *testing.T) {
	r := gin.New()

	r.GET(
		"/protected",
		JWTAuth(middlewareTestSecret),
		func(c *gin.Context) {
			c.Status(http.StatusOK)
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "InvalidFormat")

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusUnauthorized,
			recorder.Code,
		)
	}
}

// TestJWTAuthExpiredToken 测试过期 Token。
func TestJWTAuthExpiredToken(t *testing.T) {
	now := time.Now()

	claims := auth.Claims{
		UserID:   1,
		Username: "testuser",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "gochat",
			Subject:   strconv.FormatUint(1, 10),
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(now.Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString([]byte(middlewareTestSecret))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}

	recorder := executeProtectedRequest(tokenString)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusUnauthorized,
			recorder.Code,
		)
	}
}

// TestJWTAuthValidToken 测试有效 Token。
func TestJWTAuthValidToken(t *testing.T) {
	tokenString, err := auth.GenerateToken(
		7,
		"testuser",
		middlewareTestSecret,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	recorder := executeProtectedRequest(tokenString)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d; body: %s",
			http.StatusOK,
			recorder.Code,
			recorder.Body.String(),
		)
	}

	expectedUserID := `"userID":7`
	expectedUsername := `"username":"testuser"`

	body := recorder.Body.String()

	if !contains(body, expectedUserID) {
		t.Errorf("response does not contain %s: %s", expectedUserID, body)
	}

	if !contains(body, expectedUsername) {
		t.Errorf(
			"response does not contain %s: %s",
			expectedUsername,
			body,
		)
	}
}

// contains 判断字符串是否包含指定子串。
func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
