package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"gochat/internal/metrics"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// routeChannel 是跨实例消息路由的 Pub/Sub 频道。
const routeChannel = "chat:route"

// routePublishTimeout 限制发布耗时：推送路径不能因为 Redis 抖动而阻塞业务。
const routePublishTimeout = 2 * time.Second

// routeMessage 是跨实例投递的消息体。
type routeMessage struct {
	UserID uint64 `json:"user_id"`

	// Payload 必须是合法 JSON（即 ws.NewEnvelope 的产物）。
	// 用 json.RawMessage 是为了避免"JSON 套 JSON"的转义开销；
	// 代价是传入非 JSON 字节时 Marshal 会失败，消息不会被路由。
	Payload json.RawMessage `json:"payload"`

	Origin string `json:"origin"`
}

// RedisRouter 把本地 Hub 和 Redis Pub/Sub 组合成一个 messagePusher：
//
//	本地有连接 → 直接投递（快路径，不经 Redis）
//	本地没有   → 发布到 chat:route，由持有该用户连接的实例投递
//
// 这样 ChatService 完全不需要知道当前是单机还是多实例。
type RedisRouter struct {
	local      *Hub
	client     *redis.Client
	instanceID string
	sub        *redis.PubSub
}

// NewRedisRouter 创建跨实例路由器。
func NewRedisRouter(local *Hub, client *redis.Client, instanceID string) *RedisRouter {
	return &RedisRouter{
		local:      local,
		client:     client,
		instanceID: instanceID,
	}
}

// SendToUser 实现 messagePusher 接口。
func (r *RedisRouter) SendToUser(userID uint64, payload []byte) int {
	// 1. 快路径：用户连在本实例上，直接投递。
	if delivered := r.local.SendToUser(userID, payload); delivered > 0 {
		return delivered
	}

	if userID == 0 || len(payload) == 0 {
		return 0
	}

	// 2. 慢路径：发布给其他实例。
	body, err := json.Marshal(routeMessage{
		UserID:  userID,
		Payload: payload,
		Origin:  r.instanceID,
	})
	if err != nil {
		log.Printf("encode route message failed: user=%d err=%v", userID, err)
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), routePublishTimeout)
	defer cancel()

	if err := r.client.Publish(ctx, routeChannel, body).Err(); err != nil {
		// Pub/Sub 不持久化：这里失败只影响实时推送，
		// 消息已在库里，客户端重连后靠 after_seq 补拉。
		log.Printf("publish route message failed: user=%d err=%v", userID, err)
	} else {
		metrics.RoutePublishedTotal.Inc()
	}

	return 0
}

// Start 订阅 chat:route 并启动消费协程。
//
// 返回时订阅已经确认（Receive），之后的发布不会丢。
func (r *RedisRouter) Start(ctx context.Context) error {
	sub := r.client.Subscribe(ctx, routeChannel)

	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return fmt.Errorf("subscribe %s: %w", routeChannel, err)
	}

	r.sub = sub
	go r.consume(ctx, sub.Channel())

	return nil
}

// Close 关闭订阅。
func (r *RedisRouter) Close() error {
	if r.sub == nil {
		return nil
	}

	return r.sub.Close()
}

func (r *RedisRouter) consume(
	ctx context.Context,
	messages <-chan *redis.Message,
) {
	for {
		select {
		case <-ctx.Done():
			return

		case message, ok := <-messages:
			if !ok {
				return
			}

			r.deliver(message.Payload)
		}
	}
}

func (r *RedisRouter) deliver(raw string) {
	var message routeMessage

	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		log.Printf("decode route message failed: %v", err)
		return
	}

	// 自己发布的已经在快路径投递过，跳过，避免重复（防回环）。
	if message.Origin == r.instanceID {
		return
	}

	if message.UserID == 0 || len(message.Payload) == 0 {
		return
	}

	if delivered := r.local.SendToUser(message.UserID, message.Payload); delivered > 0 {
		metrics.RouteDeliveredTotal.Inc()
	}

}
