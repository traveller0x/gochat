package main

import (
	"fmt"
	"log"

	"gochat/internal/config"
	"gochat/internal/database"
	"gochat/internal/repository"
	"gochat/internal/router"
	"gochat/internal/service"
)

func main() {
	// 1. 加载配置。
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	// 2. 检查 JWT 配置。
	if len(cfg.JWT.Secret) < 32 {
		log.Fatal("JWT secret must contain at least 32 bytes; configure GOCHAT_JWT_SECRET")
	}

	if cfg.JWT.ExpireHours <= 0 {
		log.Fatal("JWT expire_hours must be greater than 0")
	}

	// 3. 初始化 MySQL。
	db, err := database.NewMySQL(cfg.MySQL)
	if err != nil {
		log.Fatalf("initialize MySQL failed: %v", err)
	}

	// 4. 获取底层数据库连接，用于管理连接池生命周期。
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("get SQL DB failed: %v", err)
	}

	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("close MySQL connection failed: %v", err)
		}
	}()

	// 5. 初始化 Repository。
	userRepo := repository.NewUserRepository(db)

	// 6. 初始化 Service。
	userService := service.NewUserService(userRepo)

	// 7. 初始化路由，注入业务服务和 JWT 配置。
	r := router.SetupRouter(
		userService,
		cfg.JWT.Secret,
		cfg.JWT.ExpireHours,
	)

	// 8. 启动 HTTP 服务。
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("GoChat server starting on %s", addr)

	if err := r.Run(addr); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
