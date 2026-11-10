package ratelimit

import (
	"testing"
	"time"
)

// TestUserLimiterBurstThenBlock 测试突发用完后被拒绝，且不影响其他用户。
func TestUserLimiterBurstThenBlock(t *testing.T) {
	limiter := NewUserLimiter(1, 2)

	if !limiter.Allow(1) || !limiter.Allow(1) {
		t.Fatal("first two messages should be allowed")
	}

	if limiter.Allow(1) {
		t.Error("third message should be blocked")
	}

	if !limiter.Allow(2) {
		t.Error("other user should not be affected")
	}
}

// TestUserLimiterRefill 测试令牌会随时间恢复。
func TestUserLimiterRefill(t *testing.T) {
	limiter := NewUserLimiter(10, 1)

	if !limiter.Allow(1) {
		t.Fatal("first message should be allowed")
	}

	if limiter.Allow(1) {
		t.Fatal("second message should be blocked")
	}

	// 10 个/秒 → 100ms 恢复一个令牌。
	time.Sleep(150 * time.Millisecond)

	if !limiter.Allow(1) {
		t.Error("token should be refilled after waiting")
	}
}

// TestUserLimiterCleanup 测试不活跃条目会被清理。
func TestUserLimiterCleanup(t *testing.T) {
	limiter := NewUserLimiter(10, 10)

	limiter.Allow(1)
	limiter.Allow(2)

	if len(limiter.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(limiter.entries))
	}

	// 把清理时间点设到未来，模拟长时间无活动。
	limiter.cleanup(time.Now().Add(time.Hour))

	if len(limiter.entries) != 0 {
		t.Errorf("entries = %d, want 0 after cleanup", len(limiter.entries))
	}
}
