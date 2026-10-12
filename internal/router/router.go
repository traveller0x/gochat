package router

import (
	"github.com/gin-gonic/gin"

	"gochat/internal/handler"
)

func SetupRouter() *gin.Engine {
	r := gin.Default()

	r.GET("/ping", handler.Ping)

	return r
}
