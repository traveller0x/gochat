package router

import (
	"gochat/internal/handler"
	"gochat/internal/middleware"
	"gochat/internal/presence"
	"gochat/internal/ratelimit"
	"gochat/internal/service"
	"gochat/internal/ws"

	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Deps 汇总路由需要的所有依赖。
//
// 参数超过三个时用结构体传递：
// 调用方写字段名不会传错顺序，将来新增依赖也不用改所有调用点。
type Deps struct {
	UserService    *service.UserService
	ChatService    *service.ChatService
	Hub            *ws.Hub
	Presence       *presence.Presence
	Limiter        *ratelimit.UserLimiter
	Logger         *slog.Logger
	JWTSecret      string
	JWTExpireHours int
}

// SetupRouter 初始化 HTTP 路由。
func SetupRouter(deps Deps) *gin.Engine {
	// 依赖缺失时立即失败，避免运行到一半才 panic。
	if deps.Hub == nil || deps.ChatService == nil {
		panic("router: Deps.Hub and Deps.ChatService must not be nil")
	}

	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	// gin.Default() 自带 Logger + Recovery；这里换成我们的 RequestLogger，
	// 避免两套日志格式并存。
	r := gin.New()
	r.Use(
		gin.Recovery(),
		middleware.RequestLogger(deps.Logger),
		middleware.Metrics(),
	)

	// 健康检查。
	r.GET("/ping", handler.Ping)
	// Prometheus 抓取端点。
	// 注意：这里没有鉴权，生产环境应只允许内网/监控系统访问。
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// 身份认证接口。
	authHandler := handler.NewAuthHandler(
		deps.UserService,
		deps.JWTSecret,
		deps.JWTExpireHours,
	)

	// 用户资料接口。
	userHandler := handler.NewUserHandler(deps.UserService)

	// WebSocket 接口。
	wsHandler := handler.NewWSHandler(deps.Hub, deps.ChatService, deps.Presence, deps.Limiter, deps.JWTSecret)

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
			// 用户搜索（按用户名或昵称模糊匹配）。
			users.GET("/search", userHandler.Search)

		}

		// 会话接口需要登录。
		conversationHandler := handler.NewConversationHandler(deps.ChatService, deps.UserService)

		conversations := api.Group("/conversations")
		conversations.Use(middleware.JWTAuth(deps.JWTSecret))
		{

			// 创建（或获取）单聊会话。
			conversations.POST("", conversationHandler.Create)
			// 会话列表（含未读数、最后一条消息、单聊对方信息）。
			conversations.GET("", conversationHandler.List)
			// 历史消息与补洞，seq 游标分页。
			conversations.GET("/:id/messages", conversationHandler.Messages)

			// 已读上报。
			conversations.POST("/:id/read", conversationHandler.MarkRead)

		}

		// WebSocket 连接不能挂 JWTAuth 中间件：
		// 浏览器无法给 WebSocket 请求设置自定义 Header，
		// 所以 token 通过 query 传递，由处理器自己校验。
		api.GET("/ws", wsHandler.Serve)
	}

	return r
}
