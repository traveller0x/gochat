package presence

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultTTL 是在线状态的存活时间。
	// 必须大于心跳续期间隔的 2 倍，否则正常网络抖动就会被误判为离线。
	DefaultTTL = 90 * time.Second

	keyPrefix = "online:"
)

// onlineScript 原子地写入在线成员并续期。
// 拆成两条命令的话，可能出现"SADD 成功、EXPIRE 失败"→ key 永不过期的幽灵在线。
var onlineScript = redis.NewScript(`
redis.call("SADD", KEYS[1], ARGV[1])
redis.call("EXPIRE", KEYS[1], ARGV[2])
return 1
`)

// offlineScript 原子地移除成员，并在集合为空时删除 key。
var offlineScript = redis.NewScript(`
redis.call("SREM", KEYS[1], ARGV[1])
if redis.call("SCARD", KEYS[1]) == 0 then
  redis.call("DEL", KEYS[1])
  return 0
end
return 1
`)

// Presence 维护用户的在线状态。
//
// 每个用户一个集合：online:{userID} = {instanceID...}
// 用集合而不是单个 key，是为了支持"同一用户连在不同实例上"。
// 过期时间挂在集合上：进程被 kill -9 后没有机会执行 Offline，
// 靠 TTL 自愈，不会留下永久的幽灵在线。
type Presence struct {
	client     *redis.Client
	instanceID string
	ttl        time.Duration
}

// New 创建在线状态管理器。
func New(client *redis.Client, instanceID string) *Presence {
	return &Presence{
		client:     client,
		instanceID: instanceID,
		ttl:        DefaultTTL,
	}
}

func key(userID uint64) string {
	return fmt.Sprintf("%s%d", keyPrefix, userID)
}

// Online 标记用户在本实例在线，并刷新过期时间。
func (p *Presence) Online(ctx context.Context, userID uint64) error {
	err := onlineScript.Run(
		ctx,
		p.client,
		[]string{key(userID)},
		p.instanceID,
		int(p.ttl.Seconds()),
	).Err()
	if err != nil {
		return fmt.Errorf("mark online: %w", err)
	}

	return nil
}

// KeepAlive 续期。
//
// 内部复用 Online：如果 key 已经因为长时间没有心跳而过期，
// 这里会重新写入，避免"连接还在、在线状态却丢了"。
func (p *Presence) KeepAlive(ctx context.Context, userID uint64) error {
	return p.Online(ctx, userID)
}

// Offline 移除本实例的在线标记；集合为空时删除 key。
func (p *Presence) Offline(ctx context.Context, userID uint64) error {
	err := offlineScript.Run(ctx, p.client, []string{key(userID)}, p.instanceID).Err()
	if err != nil {
		return fmt.Errorf("mark offline: %w", err)
	}

	return nil
}

// IsOnline 判断用户是否至少有一个在线连接。
func (p *Presence) IsOnline(ctx context.Context, userID uint64) (bool, error) {
	count, err := p.client.Exists(ctx, key(userID)).Result()
	if err != nil {
		return false, fmt.Errorf("check presence: %w", err)
	}

	return count > 0, nil
}

// OnlineUsers 批量判断在线状态。
// 用 pipeline 一次往返，避免 N 次网络调用（好友列表场景必备）。
func (p *Presence) OnlineUsers(
	ctx context.Context,
	userIDs []uint64,
) (map[uint64]bool, error) {
	result := make(map[uint64]bool, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	pipe := p.client.Pipeline()
	commands := make([]*redis.IntCmd, 0, len(userIDs))

	for _, id := range userIDs {
		commands = append(commands, pipe.Exists(ctx, key(id)))
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("batch check presence: %w", err)
	}

	for i, command := range commands {
		result[userIDs[i]] = command.Val() > 0
	}

	return result, nil
}
