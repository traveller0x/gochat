package router

import (
	"gochat/internal/handler"
	"gochat/internal/service"

	"github.com/gin-gonic/gin"
)

// SetupRouter 初始化 HTTP 路由。
func SetupRouter(userService *service.UserService) *gin.Engine {
	r := gin.Default()

	// 健康检查。
	r.GET("/ping", handler.Ping)

	// 身份认证接口。
	authHandler := handler.NewAuthHandler(userService)

	api := r.Group("/api/v1")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
		}
	}

	return r
}
