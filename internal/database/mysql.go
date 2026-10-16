package database

import (
	"fmt"
	"time"

	"gochat/internal/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// NewMySQL 创建 MySQL 数据库连接。
func NewMySQL(cfg config.MySQLConfig) (*gorm.DB, error) {
	if cfg.Host == "" || cfg.Port <= 0 || cfg.User == "" || cfg.Database == "" {
		return nil, fmt.Errorf("invalid MySQL configuration")
	}

	if cfg.Password == "" {
		return nil, fmt.Errorf("MySQL password is empty")
	}

	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
	)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to MySQL: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get underlying SQL DB: %w", err)
	}

	// 配置连接池。
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 显式检查数据库连接是否可用。
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping MySQL: %w", err)
	}

	return db, nil
}
