package router

import (
	"gochat/internal/handler"
	"gochat/internal/middleware"
	"gochat/internal/service"
	"gochat/internal/ws"

	"github.com/gin-gonic/gin"
)

// Deps 汇总路由需要的所有依赖。
//
// 参数超过三个时用结构体传递：
// 调用方写字段名不会传错顺序，将来新增依赖也不用改所有调用点。
type Deps struct {
	UserService    *service.UserService
	ChatService    *service.ChatService
	Hub            *ws.Hub
	JWTSecret      string
	JWTExpireHours int
}

// SetupRouter 初始化 HTTP 路由。
func SetupRouter(deps Deps) *gin.Engine {
	// 依赖缺失时立即失败，避免运行到一半才 panic。
	if deps.Hub == nil || deps.ChatService == nil {
		panic("router: Deps.Hub and Deps.ChatService must not be nil")
	}

	r := gin.Default()

	// 健康检查。
	r.GET("/ping", handler.Ping)

	// 身份认证接口。
	authHandler := handler.NewAuthHandler(
		deps.UserService,
		deps.JWTSecret,
		deps.JWTExpireHours,
	)

	// 用户资料接口。
	userHandler := handler.NewUserHandler(deps.UserService)

	// WebSocket 接口。
	wsHandler := handler.NewWSHandler(deps.Hub, deps.ChatService, deps.JWTSecret)

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
		users.Use(middleware.JWTAuth(deps.JWTSecret))
		{
			// 获取当前登录用户的信息。
			users.GET("/me", userHandler.Me)
		}

		// WebSocket 连接不能挂 JWTAuth 中间件：
		// 浏览器无法给 WebSocket 请求设置自定义 Header，
		// 所以 token 通过 query 传递，由处理器自己校验。
		api.GET("/ws", wsHandler.Serve)
	}

	return r
}
