package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testJWTSecret = "test-secret-for-gochat-jwt-unit-tests-2026"

// 测试正常生成和解析 JWT。
func TestGenerateAndParseToken(t *testing.T) {
	tokenString, err := GenerateToken(
		1,
		"testuser",
		testJWTSecret,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	claims, err := ParseToken(tokenString, testJWTSecret)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}

	if claims.UserID != 1 {
		t.Errorf("UserID = %d, want 1", claims.UserID)
	}

	if claims.Username != "testuser" {
		t.Errorf("Username = %q, want testuser", claims.Username)
	}

	if claims.Issuer != "gochat" {
		t.Errorf("Issuer = %q, want gochat", claims.Issuer)
	}

	if claims.Subject != "1" {
		t.Errorf("Subject = %q, want 1", claims.Subject)
	}
}

// 测试使用错误的密钥解析令牌。
func TestParseTokenWithWrongSecret(t *testing.T) {
	tokenString, err := GenerateToken(
		1,
		"testuser",
		testJWTSecret,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	_, err = ParseToken(
		tokenString,
		"another-test-secret-for-gochat-jwt-2026",
	)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

// 测试解析格式错误的令牌。
func TestParseInvalidToken(t *testing.T) {
	_, err := ParseToken("not-a-valid-jwt", testJWTSecret)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

// 测试令牌过期后不能通过验证。
func TestParseExpiredToken(t *testing.T) {
	claims := Claims{
		UserID:   1,
		Username: "testuser",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "gochat",
			Subject:   "1",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	_, err = ParseToken(tokenString, testJWTSecret)
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("error = %v, want ErrInvalidToken", err)
	}
}

// 测试空密钥和过短密钥会被拒绝。
func TestGenerateTokenWithInvalidSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{name: "empty secret", secret: ""},
		{name: "short secret", secret: "too-short"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GenerateToken(1, "testuser", tt.secret, time.Hour)
			if !errors.Is(err, ErrInvalidKey) {
				t.Errorf("error = %v, want ErrInvalidKey", err)
			}
		})
	}
}

// 测试无效的用户 ID、用户名和有效期会被拒绝。
func TestGenerateTokenWithInvalidParameters(t *testing.T) {
	tests := []struct {
		name     string
		userID   uint64
		username string
		ttl      time.Duration
	}{
		{
			name:     "zero user ID",
			userID:   0,
			username: "testuser",
			ttl:      time.Hour,
		},
		{
			name:     "empty username",
			userID:   1,
			username: "",
			ttl:      time.Hour,
		},
		{
			name:     "zero expiration",
			userID:   1,
			username: "testuser",
			ttl:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GenerateToken(
				tt.userID,
				tt.username,
				testJWTSecret,
				tt.ttl,
			)
			if err == nil {
				t.Fatal("GenerateToken() expected an error")
			}
		})
	}
}

// 确保测试使用的密钥不会为空。
func TestJWTTestSecret(t *testing.T) {
	if strings.TrimSpace(testJWTSecret) == "" {
		t.Fatal("test JWT secret must not be empty")
	}
}
