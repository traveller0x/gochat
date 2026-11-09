package ws

import (
	"gochat/internal/metrics"
	"sync"
)

// Hub 维护所有在线连接。
//
// 它是整个进程内唯一的连接注册表，所有读写都必须经过这里的锁。
type Hub struct {
	mu sync.RWMutex

	// clients: 用户 ID -> 该用户的连接集合。
	clients map[uint64]map[*Client]struct{}
}

// NewHub 创建连接中心。
func NewHub() *Hub {
	return &Hub{
		clients: make(map[uint64]map[*Client]struct{}),
	}
}

// Register 注册一条新连接。
func (h *Hub) Register(c *Client) {
	if c == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.clients[c.UserID()] == nil {
		h.clients[c.UserID()] = make(map[*Client]struct{})
	}

	h.clients[c.UserID()][c] = struct{}{}
	metrics.WSConnections.Inc()
	metrics.WSConnectionsTotal.Inc()

}

// Unregister 移除一条连接，重复调用安全。
func (h *Hub) Unregister(c *Client) {
	if c == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	conns, ok := h.clients[c.UserID()]
	if !ok {
		return
	}

	// 只有确实删掉了才减 Gauge：Unregister 是幂等的，
	// 重复调用不能把连接数减成负数。
	if _, existed := conns[c]; !existed {
		return
	}

	delete(conns, c)
	metrics.WSConnections.Dec()

	// 用户的最后一条连接断开后删除外层 key。
	// 不删除的话，map 会随着登录过的用户数量无限增长，造成内存泄漏。
	if len(conns) == 0 {
		delete(h.clients, c.UserID())
	}
}

// SendToUser 向指定用户的所有在线连接投递消息，
// 返回成功送达的连接数。
func (h *Hub) SendToUser(userID uint64, payload []byte) int {
	// 1. 锁内只做快照，不做任何可能阻塞的操作。
	snapshot := h.snapshot(userID)
	if len(snapshot) == 0 {
		return 0
	}

	// 2. 锁外投递，缩短临界区。
	delivered := 0
	var failed []*Client

	for _, c := range snapshot {
		if err := c.Send(payload); err != nil {
			failed = append(failed, c)
			continue
		}

		delivered++
	}

	// 3. 统一处理投递失败的连接。
	//
	// 注意：这里必须在锁外调用 Unregister。
	// 如果在 RLock 期间调用需要写锁的 Unregister，会直接死锁。
	for _, c := range failed {
		c.Close()
		h.Unregister(c)
	}

	return delivered
}

// snapshot 复制一份用户当前的连接列表。
func (h *Hub) snapshot(userID uint64) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()

	conns := h.clients[userID]
	if len(conns) == 0 {
		return nil
	}

	result := make([]*Client, 0, len(conns))
	for c := range conns {
		result = append(result, c)
	}

	return result
}

// OnlineUsers 返回当前有连接的用户数。
func (h *Hub) OnlineUsers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.clients)
}

// Connections 返回当前在线连接总数。
func (h *Hub) Connections() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	total := 0
	for _, conns := range h.clients {
		total += len(conns)
	}

	return total
}

// ConnectionsOf 返回指定用户的在线连接数。
func (h *Hub) ConnectionsOf(userID uint64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.clients[userID])
}
