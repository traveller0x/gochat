package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"gochat/internal/logging"

	"github.com/gin-gonic/gin"
)

const (
	headerRequestID     = "X-Request-ID"
	contextKeyRequestID = "requestID"
)

// RequestLogger 为每个请求生成 request_id，并注入带该 id 的 logger。
//
// 同时替代 gin.Default() 自带的 Logger，输出统一为 slog 格式。
func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(headerRequestID)
		if requestID == "" {
			requestID = newRequestID()
		}

		requestLogger := logger.With("request_id", requestID)
		c.Set(contextKeyRequestID, requestID)
		c.Header(headerRequestID, requestID)

		// 把 logger 带进 request context，下游 handler / service 都能取到。
		ctx := logging.WithLogger(c.Request.Context(), requestLogger)
		c.Request = c.Request.WithContext(ctx)

		start := time.Now()
		c.Next()

		attributes := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		}

		// JWT 中间件在 c.Next() 期间写入，所以这里能取到。
		if userID, ok := c.Get("userID"); ok {
			attributes = append(attributes, "user_id", userID)
		}

		requestLogger.Info("http request", attributes...)
	}
}

func newRequestID() string {
	buf := make([]byte, 8)

	if _, err := rand.Read(buf); err != nil {
		return "unknown"
	}

	return hex.EncodeToString(buf)
}
