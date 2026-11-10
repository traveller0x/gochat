package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// DefaultPerSecond 每个用户每秒允许的消息数。
	DefaultPerSecond = 20

	// DefaultBurst 允许的瞬时突发量。
	DefaultBurst = 40

	// idleTTL 超过这个时间没活动的限流器会被清理，避免 map 无限增长。
	idleTTL = 10 * time.Minute
)

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// UserLimiter 按用户维度做令牌桶限流。
//
// 为什么按用户而不是按连接：同一个账号多开（手机+电脑）共享一份额度，
// 否则开 10 个连接就能绕过限流。
type UserLimiter struct {
	mu      sync.Mutex
	limit   rate.Limit
	burst   int
	entries map[uint64]*entry
}

// NewUserLimiter 创建限流器；参数非法时使用默认值。
func NewUserLimiter(perSecond int, burst int) *UserLimiter {
	if perSecond <= 0 {
		perSecond = DefaultPerSecond
	}

	if burst <= 0 {
		burst = DefaultBurst
	}

	return &UserLimiter{
		limit:   rate.Limit(perSecond),
		burst:   burst,
		entries: make(map[uint64]*entry),
	}
}

// Allow 判断该用户当前是否允许发送一条消息。
func (l *UserLimiter) Allow(userID uint64) bool {
	l.mu.Lock()

	item, exists := l.entries[userID]
	if !exists {
		item = &entry{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.entries[userID] = item
	}

	item.lastSeen = time.Now()
	limiter := item.limiter

	l.mu.Unlock()

	// rate.Limiter 本身是并发安全的，放在锁外调用，缩短临界区。
	return limiter.Allow()
}

// Run 定期清理长期不活跃的限流器，直到 ctx 取消。
func (l *UserLimiter) Run(ctx context.Context) {
	ticker := time.NewTicker(idleTTL)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			l.cleanup(time.Now().Add(-idleTTL))
		}
	}
}

func (l *UserLimiter) cleanup(before time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for userID, item := range l.entries {
		if item.lastSeen.Before(before) {
			delete(l.entries, userID)
		}
	}
}
