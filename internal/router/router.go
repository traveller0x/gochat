package router

import (
	"gochat/internal/handler"
	"gochat/internal/middleware"
	"gochat/internal/service"

	"github.com/gin-gonic/gin"
)

// SetupRouter 初始化 HTTP 路由。
func SetupRouter(
	userService *service.UserService,
	jwtSecret string,
	jwtExpireHours int,
) *gin.Engine {
	r := gin.Default()

	// 健康检查。
	r.GET("/ping", handler.Ping)

	// 身份认证接口。
	authHandler := handler.NewAuthHandler(
		userService,
		jwtSecret,
		jwtExpireHours,
	)

	// 用户资料接口。
	userHandler := handler.NewUserHandler(userService)

	api := r.Group("/api/v1")
	{
		// 注册和登录不需要提前登录。
		auth := api.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
		}

		// 用户资料接口需要通过 JWT 身份认证。
		users := api.Group("/users")
		users.Use(middleware.JWTAuth(jwtSecret))
		{
			// 获取当前登录用户的信息。
			users.GET("/me", userHandler.Me)
		}
	}

	return r
}
