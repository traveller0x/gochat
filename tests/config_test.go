package tests

import (
	"testing"

	"gochat/internal/config"
)

func TestLoadConfig(t *testing.T) {
	cfg, err := config.Load("../configs/config.yaml")
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Fatalf("unexpected server port: %d", cfg.Server.Port)
	}

	if cfg.MySQL.Database != "gochat" {
		t.Fatalf("unexpected database: %s", cfg.MySQL.Database)
	}

	if cfg.Redis.Port != 6379 {
		t.Fatalf("unexpected redis port: %d", cfg.Redis.Port)
	}
}
