package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// contextKey 是私有类型，避免和其他包的 context key 冲突。
type contextKey struct{}

// New 构建根 logger。
//
// GOCHAT_ENV=production 输出 JSON（便于 Loki/ELK 解析），
// 其他情况输出 Text（本地开发可读性更好）。
func New(env string) *slog.Logger {
	level := slog.LevelInfo

	switch strings.ToLower(env) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	options := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if strings.EqualFold(env, "production") {
		handler = slog.NewJSONHandler(os.Stdout, options)
	} else {
		handler = slog.NewTextHandler(os.Stdout, options)
	}

	return slog.New(handler)
}

// WithLogger 把请求级 logger 放进 context。
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext 取出请求级 logger；取不到时退回默认 logger。
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}

	return slog.Default()
}
