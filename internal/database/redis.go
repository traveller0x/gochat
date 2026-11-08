package database

import (
	"context"
	"fmt"
	"time"

	"gochat/internal/config"

	"github.com/redis/go-redis/v9"
)

// NewRedis 创建 Redis 客户端并检查连通性。
//
// 启动时 Ping 一次：连不上就直接失败，避免服务"看起来启动了"、
// 实际每次用到 Redis 才报错（在线状态和跨实例推送都会依赖它）。
func NewRedis(cfg config.RedisConfig) (*redis.Client, error) {
	if cfg.Host == "" || cfg.Port <= 0 {
		return nil, fmt.Errorf("invalid Redis configuration")
	}

	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.Database,

		// IM 的压力主要来自 Pub/Sub 和在线状态读写，先给保守的连接池。
		PoolSize:     20,
		MinIdleConns: 5,

		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping Redis: %w", err)
	}

	return client, nil
}
