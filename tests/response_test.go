package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gochat/pkg/response"

	"github.com/gin-gonic/gin"
)

func TestSuccessResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()

	r.GET("/test", func(c *gin.Context) {
		response.Success(c, "pong")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	recorder := httptest.NewRecorder()

	r.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP status 200, got %d", recorder.Code)
	}

	var result response.Response

	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result.Code != 0 {
		t.Fatalf("expected business code 0, got %d", result.Code)
	}

	if result.Message != "success" {
		t.Fatalf("expected message success, got %s", result.Message)
	}

	if result.Data != "pong" {
		t.Fatalf("expected data pong, got %v", result.Data)
	}
}
