package ws

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestRouter 用内存版 Redis 构造一个路由器。
func newTestRouter(
	t *testing.T,
	server *miniredis.Miniredis,
	instanceID string,
) (*RedisRouter, *Hub) {
	t.Helper()

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	hub := NewHub()

	return NewRedisRouter(hub, client, instanceID), hub
}

// TestRedisRouterLocalFirst 测试本地有连接时不经过 Redis。
func TestRedisRouterLocalFirst(t *testing.T) {
	server := miniredis.RunT(t)
	router, hub := newTestRouter(t, server, "inst-1")

	client := NewClient(42, nil, 4)
	hub.Register(client)
	t.Cleanup(func() {
		hub.Unregister(client)
	})

	// 注意：routeMessage.Payload 是 json.RawMessage，
	// 跨实例路径要求内容必须是合法 JSON（线上就是 ws.NewEnvelope 的产物）。
	payload := []byte(`{"type":"message.new","data":{"seq":1}}`)

	if delivered := router.SendToUser(42, payload); delivered != 1 {
		t.Fatalf("delivered = %d, want 1", delivered)
	}

	select {
	case got := <-client.Messages():
		if string(got) != string(payload) {
			t.Errorf("payload = %q, want %q", got, payload)
		}

	case <-time.After(time.Second):
		t.Fatal("local message not delivered")
	}
}

// TestRedisRouterCrossInstance 测试跨实例投递。
func TestRedisRouterCrossInstance(t *testing.T) {
	server := miniredis.RunT(t)

	routerA, _ := newTestRouter(t, server, "inst-a")
	routerB, hubB := newTestRouter(t, server, "inst-b")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	if err := routerA.Start(ctx); err != nil {
		t.Fatalf("start routerA failed: %v", err)
	}

	if err := routerB.Start(ctx); err != nil {
		t.Fatalf("start routerB failed: %v", err)
	}

	t.Cleanup(func() {
		_ = routerA.Close()
		_ = routerB.Close()
	})

	// 用户 43 只连在实例 B 上。
	clientB := NewClient(43, nil, 4)
	hubB.Register(clientB)
	t.Cleanup(func() {
		hubB.Unregister(clientB)
	})

	// 必须是合法 JSON：跨实例会走 json.Marshal/json.RawMessage。
	payload := []byte(`{"type":"message.new","data":{"seq":7,"content":"cross"}}`)

	// 实例 A 本地没有 43 → 发布到 Redis。
	if delivered := routerA.SendToUser(43, payload); delivered != 0 {
		t.Fatalf("delivered = %d, want 0 (local miss)", delivered)
	}

	select {
	case got := <-clientB.Messages():
		if string(got) != string(payload) {
			t.Errorf("payload = %q, want %q", got, payload)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("cross-instance message not delivered")
	}
}
