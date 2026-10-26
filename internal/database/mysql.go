package database

import (
	"fmt"
	"log"
	"os"
	"time"

	"gochat/internal/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// NewMySQL 创建 MySQL 数据库连接，使用默认日志配置。
func NewMySQL(cfg config.MySQLConfig) (*gorm.DB, error) {
	return NewMySQLWithLogger(cfg, newGormLogger())
}

// NewMySQLWithLogger 创建 MySQL 连接，并允许调用方指定 GORM 日志实现。
//
// 测试可以传入 gormlogger.Discard 完全关闭 SQL 日志，
// 让并发测试里"预期中的唯一键冲突"不再刷屏。
func NewMySQLWithLogger(
	cfg config.MySQLConfig,
	gormLogger gormlogger.Interface,
) (*gorm.DB, error) {
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
		Logger:         gormLogger,
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

// newGormLogger 构建默认的 GORM 日志配置。
//
// 三个关键设置：
//   - LogLevel = Warn：不打印每条正常 SQL，只记录慢查询和错误
//   - SlowThreshold = 200ms：超过这个耗时的查询才值得关注
//   - IgnoreRecordNotFoundError = true：忽略"查不到"这种预期内的结果，
//     否则"先查后插"的每个请求都会刷一行 record not found
func newGormLogger() gormlogger.Interface {
	return gormlogger.New(
		log.New(os.Stderr, "", log.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)
}
