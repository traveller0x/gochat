package middleware

import (
	"strconv"
	"time"

	"gochat/internal/metrics"

	"github.com/gin-gonic/gin"
)

// Metrics 记录 HTTP 请求量与时延。
//
// path 用 c.FullPath()（路由模板，如 /api/v1/conversations/:id/messages），
// 而不是真实 URL——否则 /conversations/238、/conversations/239 会各成一条时间序列，
// 指标基数爆炸（高基数标签是 Prometheus 最常见的踩坑）。
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}

		status := strconv.Itoa(c.Writer.Status())

		metrics.HTTPRequestsTotal.
			WithLabelValues(c.Request.Method, path, status).
			Inc()

		// WebSocket 是长连接，耗时等于连接存续时长，统计进直方图会严重失真。
		if path != "/api/v1/ws" {
			metrics.HTTPRequestDuration.
				WithLabelValues(c.Request.Method, path).
				Observe(time.Since(start).Seconds())
		}
	}
}
