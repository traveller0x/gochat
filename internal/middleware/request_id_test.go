package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestRequestLoggerGeneratesID 测试自动生成 request_id。
func TestRequestLoggerGeneratesID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(RequestLogger(newSilentLogger()))
	r.GET("/ping", func(c *gin.Context) {
		if id := c.GetString(contextKeyRequestID); id == "" {
			t.Error("request id should be set in gin context")
		}

		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Header().Get(headerRequestID) == "" {
		t.Error("response should contain X-Request-ID header")
	}
}

// TestRequestLoggerKeepsProvidedID 测试透传调用方传入的 request_id。
func TestRequestLoggerKeepsProvidedID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(RequestLogger(newSilentLogger()))
	r.GET("/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(headerRequestID, "trace-123")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get(headerRequestID); got != "trace-123" {
		t.Errorf("X-Request-ID = %q, want trace-123", got)
	}
}
