package ws

import (
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultSendBufferSize 是单个连接发送缓冲区的默认容量。
//
// 缓冲区太小，正常网络抖动就会被误杀；太大，慢客户端会占用过多内存。
// 256 条消息在 IM 场景下足够吸收一次短暂卡顿。
const DefaultSendBufferSize = 256

var (
	// ErrClientClosed 表示连接已经关闭。
	ErrClientClosed = errors.New("ws: client closed")

	// ErrSendBufferFull 表示客户端消费速度跟不上，缓冲区已满。
	ErrSendBufferFull = errors.New("ws: send buffer full")
)

// Client 表示一条在线连接。
//
// 一个用户可能同时有多条连接（手机 + 电脑），
// 所以身份由 userID 和 Client 指针共同决定。
type Client struct {
	userID uint64

	// conn 是底层网络连接。
	// 测试中可以传 nil 或假实现，只要不调用 ReadPump / WritePump 即可。
	conn Conn

	// send 保存待发送的消息，由写 goroutine 消费。
	send chan []byte

	// done 在连接关闭时被关闭，用于通知所有 goroutine 退出。
	done chan struct{}

	// once 保证 done 只被关闭一次，重复 Close 是安全的。
	once sync.Once
}

// NewClient 创建一条在线连接。
// bufferSize <= 0 时使用默认值。
func NewClient(userID uint64, conn Conn, bufferSize int) *Client {
	if bufferSize <= 0 {
		bufferSize = DefaultSendBufferSize
	}

	return &Client{
		userID: userID,
		conn:   conn,
		send:   make(chan []byte, bufferSize),
		done:   make(chan struct{}),
	}
}

// UserID 返回连接所属用户。
func (c *Client) UserID() uint64 {
	return c.userID
}

// Messages 返回只读的消息队列，写 goroutine 从这里取数据。
func (c *Client) Messages() <-chan []byte {
	return c.send
}

// Done 返回关闭信号通道。
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// Send 非阻塞地把消息放进发送队列。
//
// 返回 ErrClientClosed 表示连接已关闭，
// 返回 ErrSendBufferFull 表示客户端消费过慢，调用方应断开该连接。
func (c *Client) Send(payload []byte) error {
	// 1. 先检查连接是否已关闭，避免往废弃连接里塞数据。
	select {
	case <-c.done:
		return ErrClientClosed
	default:
	}

	// 2. 非阻塞投递：缓冲区满时立即返回，绝不阻塞调用方。
	select {
	case c.send <- payload:
		return nil

	case <-c.done:
		return ErrClientClosed

	default:
		return ErrSendBufferFull
	}
}

// Close 关闭连接，重复调用安全。
func (c *Client) Close() {
	c.once.Do(func() {
		close(c.done)
	})
}

// IsClosed 报告连接是否已经关闭。
func (c *Client) IsClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// ReadPump 持续读取客户端消息，收到一条就调用一次 onMessage。
//
// 它是阻塞的，通常由 Handler 在主 goroutine 中调用：
// 函数返回即代表连接已断开。
//
// 读循环退出时会关闭 Client，通知写循环和业务侧收尾。
func (c *Client) ReadPump(onMessage func(payload []byte)) {
	defer func() {
		c.Close()
	}()

	// 1. 限制单条消息大小，防止恶意客户端发送超大消息。
	c.conn.SetReadLimit(MaxMessageSize)

	// 2. 设置初始读超时：pongWait 内没有任何数据就判定连接已死。
	_ = c.conn.SetReadDeadline(time.Now().Add(PongWait))

	// 3. 收到 pong 说明对端还活着，续期读超时。
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(PongWait))
	})

	for {
		_, payload, err := c.conn.ReadMessage()
		if err != nil {
			// 连接断开、读超时、消息超限都会走到这里。
			return
		}

		if onMessage != nil {
			onMessage(payload)
		}
	}
}

// WritePump 是唯一的写者：从发送队列取消息写到底层连接，
// 并定时发送 ping 心跳。
//
// 单一写者保证了底层连接不会被并发写（gorilla 要求调用方保证）。
func (c *Client) WritePump(interval time.Duration) {
	if interval <= 0 {
		interval = PingInterval
	}

	ticker := time.NewTicker(interval)

	defer func() {
		ticker.Stop()

		// 写循环退出时关闭底层连接，
		// 这样阻塞在 ReadMessage 上的读循环也会立刻返回。
		_ = c.conn.Close()
	}()

	for {
		select {
		case <-c.done:
			return

		case payload := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(WriteTimeout))

			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				// 写失败说明连接不可用，主动关闭并退出。
				c.Close()
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(WriteTimeout))

			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.Close()
				return
			}
		}
	}
}
