package ws

import (
	"time"

	"github.com/gorilla/websocket"
)

const (
	// WriteTimeout 是单次写操作的最长耗时。
	// 对端不读数据时，写会一直阻塞，必须靠 deadline 打断。
	WriteTimeout = 10 * time.Second

	// PongWait 是服务端等待客户端 pong 的最长时间。
	// 超过这个时间没有收到任何数据（包括 pong），就判定连接已死。
	PongWait = 60 * time.Second

	// PingInterval 是心跳发送间隔，必须小于 PongWait。
	PingInterval = 54 * time.Second

	// MaxMessageSize 是单条入站消息的最大字节数，防止超大消息打爆内存。
	MaxMessageSize = 4 << 10
)

// Conn 抽象一条可读写的连接。
//
// 生产环境使用 *websocket.Conn，单元测试使用内存假实现。
// 方法签名刻意与 gorilla/websocket 保持一致，因此不需要额外的适配器。
type Conn interface {
	ReadMessage() (messageType int, payload []byte, err error)
	WriteMessage(messageType int, payload []byte) error

	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error

	// 注意：gorilla 的 SetReadLimit 没有返回值。
	SetReadLimit(limit int64)

	SetPongHandler(handler func(appData string) error)

	Close() error
}

// 编译期断言：*websocket.Conn 必须满足 Conn 接口。
// 如果将来升级 gorilla 后方法签名变化，这里会直接编译失败。
var _ Conn = (*websocket.Conn)(nil)
