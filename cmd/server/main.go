package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gochat/internal/config"
	"gochat/internal/database"
	"gochat/internal/repository"
	"gochat/internal/router"
	"gochat/internal/server"
	"gochat/internal/service"
	"gochat/internal/ws"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("server exited with error: %v", err)
	}
}

// run 负责组装依赖并启动服务。
// 返回 nil 表示收到了退出信号并已完成优雅关闭。
//
// 把逻辑从 main 拆出来的原因：
// 1. main 里不能写 defer（os.Exit 不会执行 defer）；
// 2. run 可以被测试直接调用，而 main 只能靠启动进程测试。
func run() error {
	// 1. 加载配置。
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. 检查 JWT 配置。
	if len(cfg.JWT.Secret) < 32 {
		return fmt.Errorf(
			"JWT secret must contain at least 32 bytes; configure GOCHAT_JWT_SECRET",
		)
	}

	if cfg.JWT.ExpireHours <= 0 {
		return fmt.Errorf("JWT expire_hours must be greater than 0")
	}

	// 3. 初始化 MySQL。
	db, err := database.NewMySQL(cfg.MySQL)
	if err != nil {
		return fmt.Errorf("initialize MySQL: %w", err)
	}

	// 4. 获取底层数据库连接，用于管理连接池生命周期。
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get SQL DB: %w", err)
	}

	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("close MySQL connection failed: %v", err)
		}
	}()

	// 5. 初始化 Redis。
	redisClient, err := database.NewRedis(cfg.Redis)
	if err != nil {
		return fmt.Errorf("initialize Redis: %w", err)
	}

	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Printf("close Redis client failed: %v", err)
		}
	}()

	// 6. 初始化 Repository。
	userRepo := repository.NewUserRepository(db)
	conversationRepo := repository.NewConversationRepository(db)
	messageRepo := repository.NewMessageRepository(db)

	// 7. 初始化 Service。
	userService := service.NewUserService(userRepo)

	// 8. 创建连接中心。
	//    Hub 是整个进程唯一的在线连接注册表，生命周期与 HTTP 服务一致。
	hub := ws.NewHub()

	// 9. 初始化聊天服务。
	//    Hub 实现了 SendToUser，因此可以直接作为消息推送器注入。
	chatService := service.NewChatService(conversationRepo, messageRepo, hub)

	// 10. 初始化路由，注入业务服务、连接中心和 JWT 配置。
	r := router.SetupRouter(router.Deps{
		UserService:    userService,
		ChatService:    chatService,
		Hub:            hub,
		JWTSecret:      cfg.JWT.Secret,
		JWTExpireHours: cfg.JWT.ExpireHours,
	})

	// 11. 构造 HTTP 服务。
	// ReadHeaderTimeout 用于防止慢速请求头攻击：客户端只发一半请求头就不动了，
	// 连接会一直被占用。设置上限后，超时会自动断开。
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// 12. 监听 Ctrl+C 和 SIGTERM，收到信号后取消 ctx。
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// 13. 启动服务并阻塞，直到出错或被信号中断。
	if err := server.Run(ctx, srv); err != nil {
		return err
	}

	log.Println("shutdown complete")

	return nil
}
