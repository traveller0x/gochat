package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 定义 JWT 认证过程中可能出现的错误。
var (
	ErrInvalidToken = errors.New("invalid token")
	ErrInvalidKey   = errors.New("invalid JWT secret")
)

// Claims 定义 JWT 中保存的用户身份信息。
type Claims struct {
	UserID   uint64 `json:"user_id"`
	Username string `json:"username"`

	jwt.RegisteredClaims
}

// GenerateToken 根据用户信息生成 JWT。
//
// userID: 用户 ID
// username: 用户名
// secret: JWT 签名密钥
// ttl: 令牌有效期
func GenerateToken(
	userID uint64,
	username string,
	secret string,
	ttl time.Duration,
) (string, error) {
	// 校验密钥，避免使用空密钥或过短的密钥。
	if len(secret) < 32 {
		return "", ErrInvalidKey
	}

	if userID == 0 || username == "" || ttl <= 0 {
		return "", errors.New("invalid token parameters")
	}

	now := time.Now()

	// 构建 JWT Claims。
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			// 标识令牌的签发者。
			Issuer: "gochat",

			// 使用用户 ID 作为令牌主题。
			Subject: strconv.FormatUint(userID, 10),

			// 令牌签发时间。
			IssuedAt: jwt.NewNumericDate(now),

			// 令牌生效时间。
			NotBefore: jwt.NewNumericDate(now),

			// 令牌过期时间。
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	// 使用 HS256 算法创建 JWT。
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 使用密钥签名，生成最终的令牌字符串。
	signedToken, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return signedToken, nil
}

// ParseToken 验证 JWT，并返回解析后的 Claims。
func ParseToken(tokenString string, secret string) (*Claims, error) {
	if len(secret) < 32 {
		return nil, ErrInvalidKey
	}

	if tokenString == "" {
		return nil, ErrInvalidToken
	}

	claims := &Claims{}

	// 解析令牌，并严格限制允许使用的签名算法。
	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwt.Token) (interface{}, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, ErrInvalidToken
			}

			return []byte(secret), nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer("gochat"),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	// 防止缺少关键身份信息的令牌被当作有效身份。
	if claims.UserID == 0 ||
		claims.Username == "" ||
		claims.Subject != strconv.FormatUint(claims.UserID, 10) {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
