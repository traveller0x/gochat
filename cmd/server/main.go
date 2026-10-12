package main

import (
	"fmt"
	"log"

	"gochat/internal/config"
	"gochat/internal/database"
	"gochat/internal/router"
)

func main() {
	// 加载配置
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	// 初始化 MySQL
	db, err := database.NewMySQL(cfg.MySQL)
	if err != nil {
		log.Fatalf("initialize MySQL failed: %v", err)
	}

	// 获取底层数据库连接，用于管理连接池生命周期
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("get SQL DB failed: %v", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("close MySQL connection failed: %v", err)
		}
	}()

	// 初始化路由
	r := router.SetupRouter()

	// 启动 HTTP 服务
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("GoChat server starting on %s", addr)

	if err := r.Run(addr); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
