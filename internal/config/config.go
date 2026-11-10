package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	MySQL  MySQLConfig  `yaml:"mysql"`
	Redis  RedisConfig  `yaml:"redis"`
	JWT    JWTConfig    `yaml:"jwt"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	Database int    `yaml:"database"`
}

type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpireHours int    `yaml:"expire_hours"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	// 从环境变量读取数据库密码，避免把真实密码写入配置文件。
	if password, exists := os.LookupEnv("GOCHAT_MYSQL_PASSWORD"); exists {
		cfg.MySQL.Password = password
	}

	// 从环境变量读取 JWT 密钥，避免将真实密钥写入配置文件。
	if secret, exists := os.LookupEnv("GOCHAT_JWT_SECRET"); exists {
		cfg.JWT.Secret = secret
	}

	// 从环境变量读取 Redis 密码，避免把真实密码写入配置文件。
	if password, exists := os.LookupEnv("GOCHAT_REDIS_PASSWORD"); exists {
		cfg.Redis.Password = password
	}

	// 容器部署时用服务名替代 127.0.0.1。
	if host, exists := os.LookupEnv("GOCHAT_MYSQL_HOST"); exists {
		cfg.MySQL.Host = host
	}

	if raw, exists := os.LookupEnv("GOCHAT_MYSQL_PORT"); exists {
		if port, err := strconv.Atoi(raw); err == nil && port > 0 {
			cfg.MySQL.Port = port
		}
	}

	if host, exists := os.LookupEnv("GOCHAT_REDIS_HOST"); exists {
		cfg.Redis.Host = host
	}

	if raw, exists := os.LookupEnv("GOCHAT_REDIS_PORT"); exists {
		if port, err := strconv.Atoi(raw); err == nil && port > 0 {
			cfg.Redis.Port = port
		}
	}

	// 多实例演示时用环境变量覆盖端口，避免复制配置文件。
	if rawPort, exists := os.LookupEnv("GOCHAT_SERVER_PORT"); exists {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port <= 0 {
			return nil, fmt.Errorf("invalid GOCHAT_SERVER_PORT: %q", rawPort)
		}

		cfg.Server.Port = port
	}

	return &cfg, nil
}
