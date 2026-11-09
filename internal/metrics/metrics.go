package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 全部指标注册到 Prometheus 默认 Registry，
// /metrics 用 promhttp.Handler() 直接暴露。
var (
	// WSConnections 当前活跃连接数（Gauge，可增可减）。
	WSConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gochat_ws_connections",
		Help: "当前活跃的 WebSocket 连接数。",
	})

	// WSConnectionsTotal 累计建立过的连接数（Counter，只增）。
	WSConnectionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gochat_ws_connections_total",
		Help: "累计建立的 WebSocket 连接数。",
	})

	// MessagesSentTotal 成功落库并推送的消息数（不含幂等重发）。
	MessagesSentTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gochat_messages_sent_total",
		Help: "累计成功落库并推送的消息数。",
	})

	// RoutePublishedTotal 发布到其他实例的消息数。
	RoutePublishedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gochat_route_published_total",
		Help: "发布到 Redis 供其他实例投递的消息数。",
	})

	// RouteDeliveredTotal 从其他实例接收并成功投递的消息数。
	RouteDeliveredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gochat_route_delivered_total",
		Help: "从其他实例接收并成功投递的消息数。",
	})

	// HTTPRequestsTotal 按 方法/路由模板/状态码 统计请求数。
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gochat_http_requests_total",
		Help: "HTTP 请求总数。",
	}, []string{"method", "path", "status"})

	// HTTPRequestDuration HTTP 请求耗时分布。
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gochat_http_request_duration_seconds",
		Help:    "HTTP 请求耗时分布（秒）。",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)
