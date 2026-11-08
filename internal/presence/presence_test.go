package presence

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestPresence 用内存版 Redis 构造一个实例。
func newTestPresence(t *testing.T) (*Presence, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	return New(client, "inst-1"), server
}

// TestPresenceOnlineAndOffline 测试多实例上下线。
func TestPresenceOnlineAndOffline(t *testing.T) {
	store, server := newTestPresence(t)
	ctx := context.Background()

	if err := store.Online(ctx, 12); err != nil {
		t.Fatalf("Online failed: %v", err)
	}

	online, err := store.IsOnline(ctx, 12)
	if err != nil {
		t.Fatalf("IsOnline failed: %v", err)
	}
	if !online {
		t.Fatal("user 12 should be online")
	}

	// 第二个实例也上线。
	client2 := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client2.Close()
	})

	store2 := New(client2, "inst-2")
	if err := store2.Online(ctx, 12); err != nil {
		t.Fatalf("Online on inst-2 failed: %v", err)
	}

	// 实例 1 下线，实例 2 仍在线 → 用户仍在线。
	if err := store.Offline(ctx, 12); err != nil {
		t.Fatalf("Offline failed: %v", err)
	}

	online, err = store.IsOnline(ctx, 12)
	if err != nil {
		t.Fatalf("IsOnline failed: %v", err)
	}
	if !online {
		t.Error("user should still be online on another instance")
	}

	// 实例 2 也下线 → 用户离线且 key 被删除。
	if err := store2.Offline(ctx, 12); err != nil {
		t.Fatalf("Offline on inst-2 failed: %v", err)
	}

	online, err = store.IsOnline(ctx, 12)
	if err != nil {
		t.Fatalf("IsOnline failed: %v", err)
	}
	if online {
		t.Error("user should be offline")
	}

	if server.Exists("online:12") {
		t.Error("online key should be deleted when the set becomes empty")
	}
}

// TestPresenceTTLExpires 测试进程被 kill -9 后靠 TTL 自愈。
func TestPresenceTTLExpires(t *testing.T) {
	store, server := newTestPresence(t)
	ctx := context.Background()

	if err := store.Online(ctx, 12); err != nil {
		t.Fatalf("Online failed: %v", err)
	}

	// 快进超过 TTL：模拟进程没机会执行 Offline。
	server.FastForward(DefaultTTL + time.Second)

	online, err := store.IsOnline(ctx, 12)
	if err != nil {
		t.Fatalf("IsOnline failed: %v", err)
	}
	if online {
		t.Fatal("presence should expire after TTL")
	}
}

// TestPresenceOnlineUsers 测试批量查询。
func TestPresenceOnlineUsers(t *testing.T) {
	store, _ := newTestPresence(t)
	ctx := context.Background()

	for _, id := range []uint64{1, 3} {
		if err := store.Online(ctx, id); err != nil {
			t.Fatalf("Online(%d) failed: %v", id, err)
		}
	}

	got, err := store.OnlineUsers(ctx, []uint64{1, 2, 3})
	if err != nil {
		t.Fatalf("OnlineUsers failed: %v", err)
	}

	if !got[1] || got[2] || !got[3] {
		t.Errorf("OnlineUsers = %+v, want 1:true 2:false 3:true", got)
	}
}
