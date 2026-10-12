package tests

import (
	"os"
	"testing"

	"gochat/internal/config"
	"gochat/internal/database"
)

func TestMySQLConnection(t *testing.T) {
	password, exists := os.LookupEnv("GOCHAT_MYSQL_PASSWORD")
	if !exists || password == "" {
		t.Skip("GOCHAT_MYSQL_PASSWORD is not set; skipping MySQL integration test")
	}

	cfg, err := config.Load("../configs/config.yaml")
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	db, err := database.NewMySQL(cfg.MySQL)
	if err != nil {
		t.Fatalf("connect to MySQL failed: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get SQL DB failed: %v", err)
	}

	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close MySQL connection failed: %v", err)
	}
}
