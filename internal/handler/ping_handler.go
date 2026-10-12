package handler

import (
	"gochat/pkg/response"

	"github.com/gin-gonic/gin"
)

// Ping 用于检查 HTTP 服务是否正常运行。
func Ping(c *gin.Context) {
	response.Success(c, "pong")
}
