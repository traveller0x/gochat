package middleware

import (
	"net/http"
	"strings"

	"gochat/internal/auth"
	"gochat/pkg/response"

	"github.com/gin-gonic/gin"
)

// JWTAuth 验证请求携带的 JWT Token。
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 从请求头获取 Authorization。
		header := c.GetHeader("Authorization")

		// 2. 检查 Bearer Token 格式。
		if header == "" {
			response.Error(
				c,
				http.StatusUnauthorized,
				4011,
				"missing authorization token",
			)
			c.Abort()
			return
		}

		parts := strings.Fields(header)

		if len(parts) != 2 ||
			!strings.EqualFold(parts[0], "Bearer") ||
			parts[1] == "" {
			response.Error(
				c,
				http.StatusUnauthorized,
				4012,
				"invalid authorization format",
			)
			c.Abort()
			return
		}

		// 3. 验证 Token 的签名、有效期和声明。
		claims, err := auth.ParseToken(parts[1], secret)
		if err != nil {
			response.Error(
				c,
				http.StatusUnauthorized,
				4013,
				"invalid or expired token",
			)
			c.Abort()
			return
		}

		// 4. 将认证后的用户身份放入 Gin Context。
		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)

		// 5. 认证通过，继续执行后续处理器。
		c.Next()
	}
}
