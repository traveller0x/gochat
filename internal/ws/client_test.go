package ws

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/goleak"
)

// fakeConn 是测试用的内存连接，替代真实 WebSocket，
// 让心跳、超时、错误退出等场景可以确定性地触发。
type fakeConn struct {
	mu sync.Mutex

	written    [][]byte
	writeTypes []int
	closed     bool

	// writeErr 非空时，所有写操作都返回该错误。
	writeErr error

	// reads 保存预置的入站数据，ReadMessage 依次返回。
	reads chan fakeRead

	pongHandler  func(string) error
	readDeadline time.Time
}

type fakeRead struct {
	payload []byte
	err     error
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		reads: make(chan fakeRead, 8),
	}
}

func (f *fakeConn) ReadMessage() (int, []byte, error) {
	r, ok := <-f.reads
	if !ok {
		return 0, nil, errors.New("fake conn: reads closed")
	}

	return websocket.TextMessage, r.payload, r.err
}

func (f *fakeConn) WriteMessage(messageType int, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.writeErr != nil {
		return f.writeErr
	}

	cp := make([]byte, len(payload))
	copy(cp, payload)

	f.written = append(f.written, cp)
	f.writeTypes = append(f.writeTypes, messageType)

	return nil
}

func (f *fakeConn) SetReadDeadline(t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.readDeadline = t

	return nil
}

func (f *fakeConn) SetWriteDeadline(time.Time) error {
	return nil
}

func (f *fakeConn) SetReadLimit(int64) {}

func (f *fakeConn) SetPongHandler(handler func(string) error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.pongHandler = handler
}

func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed = true

	return nil
}

// push 预置一条入站消息。
func (f *fakeConn) push(payload []byte) {
	f.reads <- fakeRead{payload: payload}
}

// pushError 预置一条入站错误，模拟对端断开。
func (f *fakeConn) pushError(err error) {
	f.reads <- fakeRead{err: err}
}

func (f *fakeConn) writtenCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.written)
}

func (f *fakeConn) countType(messageType int) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	total := 0
	for _, t := range f.writeTypes {
		if t == messageType {
			total++
		}
	}

	return total
}

func (f *fakeConn) lastWritten() (int, []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.written) == 0 {
		return 0, nil
	}

	return f.writeTypes[len(f.writeTypes)-1], f.written[len(f.written)-1]
}

func (f *fakeConn) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.closed
}

func (f *fakeConn) deadline() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.readDeadline
}

func (f *fakeConn) invokePong() error {
	f.mu.Lock()
	handler := f.pongHandler
	f.mu.Unlock()

	if handler == nil {
		return errors.New("pong handler not registered")
	}

	return handler("")
}

// waitFor 轮询等待条件成立，避免依赖固定的 time.Sleep。
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(time.Second)

	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal(msg)
}

// waitDone 等待 goroutine 结束。
func waitDone(t *testing.T, done <-chan struct{}, msg string) {
	t.Helper()

	select {
	case <-done:

	case <-time.After(time.Second):
		t.Fatal(msg)
	}
}

// TestWritePumpWritesQueuedMessages 测试发送队列里的消息会被写到连接上。
func TestWritePumpWritesQueuedMessages(t *testing.T) {
	conn := newFakeConn()
	client := NewClient(1, conn, 4)

	writerDone := make(chan struct{})
	go func() {
		client.WritePump(time.Hour) // 心跳间隔取很大，避免干扰本测试
		close(writerDone)
	}()

	if err := client.Send([]byte("hello")); err != nil {
		t.Fatalf("Send() = %v", err)
	}

	waitFor(t, func() bool {
		return conn.writtenCount() == 1
	}, "message was not written to conn")

	messageType, payload := conn.lastWritten()
	if messageType != websocket.TextMessage {
		t.Errorf("message type = %d, want %d", messageType, websocket.TextMessage)
	}

	if string(payload) != "hello" {
		t.Errorf("payload = %q, want %q", payload, "hello")
	}

	// 关闭后写循环必须退出。
	client.Close()
	waitDone(t, writerDone, "WritePump did not exit after Close")

	if !conn.isClosed() {
		t.Error("WritePump should close the underlying conn on exit")
	}
}

// TestWritePumpSendsPing 测试心跳会定期发送。
func TestWritePumpSendsPing(t *testing.T) {
	conn := newFakeConn()
	client := NewClient(1, conn, 1)

	writerDone := make(chan struct{})
	go func() {
		client.WritePump(10 * time.Millisecond)
		close(writerDone)
	}()

	waitFor(t, func() bool {
		return conn.countType(websocket.PingMessage) >= 2
	}, "expected at least two ping frames")

	client.Close()
	waitDone(t, writerDone, "WritePump did not exit")
}

// TestWritePumpStopsOnWriteError 测试写失败时连接会被关闭并停止写循环。
func TestWritePumpStopsOnWriteError(t *testing.T) {
	conn := newFakeConn()
	conn.writeErr = errors.New("broken pipe")

	client := NewClient(1, conn, 4)

	writerDone := make(chan struct{})
	go func() {
		client.WritePump(time.Hour)
		close(writerDone)
	}()

	if err := client.Send([]byte("hello")); err != nil {
		t.Fatalf("Send() = %v", err)
	}

	waitDone(t, writerDone, "WritePump did not exit after write error")

	if !client.IsClosed() {
		t.Error("client should be closed after write error")
	}
}

// TestReadPumpDeliversMessages 测试入站消息会被交给回调，且读错误时退出。
func TestReadPumpDeliversMessages(t *testing.T) {
	conn := newFakeConn()
	client := NewClient(1, conn, 1)

	conn.push([]byte(`{"type":"ping"}`))

	received := make(chan []byte, 1)
	readerDone := make(chan struct{})

	go func() {
		client.ReadPump(func(payload []byte) {
			received <- payload
		})
		close(readerDone)
	}()

	select {
	case got := <-received:
		if string(got) != `{"type":"ping"}` {
			t.Errorf("payload = %q, want %q", got, `{"type":"ping"}`)
		}

	case <-time.After(time.Second):
		t.Fatal("message was not delivered")
	}

	// 模拟对端断开。
	conn.pushError(errors.New("connection reset"))

	waitDone(t, readerDone, "ReadPump did not exit after read error")

	if !client.IsClosed() {
		t.Error("ReadPump should close client on exit")
	}
}

// TestReadPumpExtendsDeadlineOnPong 测试收到 pong 后会续期读超时。
func TestReadPumpExtendsDeadlineOnPong(t *testing.T) {
	conn := newFakeConn()
	client := NewClient(1, conn, 1)

	readerDone := make(chan struct{})
	go func() {
		client.ReadPump(nil)
		close(readerDone)
	}()

	// 等待 pong handler 注册完成。
	var first time.Time
	waitFor(t, func() bool {
		first = conn.deadline()
		return !first.IsZero()
	}, "read deadline was not set")

	time.Sleep(2 * time.Millisecond)

	if err := conn.invokePong(); err != nil {
		t.Fatalf("invoke pong handler: %v", err)
	}

	second := conn.deadline()
	if !second.After(first) {
		t.Errorf("deadline was not extended: first=%v second=%v", first, second)
	}

	conn.pushError(errors.New("closed"))
	waitDone(t, readerDone, "ReadPump did not exit")
}

// TestNoGoroutineLeak 测试连接关闭后没有 goroutine 残留。
func TestNoGoroutineLeak(t *testing.T) {
	defer goleak.VerifyNone(t)

	conn := newFakeConn()
	client := NewClient(1, conn, 4)

	writerDone := make(chan struct{})
	go func() {
		client.WritePump(5 * time.Millisecond)
		close(writerDone)
	}()

	client.Close()
	waitDone(t, writerDone, "WritePump leaked")

	readerDone := make(chan struct{})
	conn.pushError(errors.New("closed"))

	go func() {
		client.ReadPump(nil)
		close(readerDone)
	}()

	waitDone(t, readerDone, "ReadPump leaked")
}

// TestReadPumpStopsOnFrameFlood 测试垃圾帧洪泛会断开连接。
func TestReadPumpStopsOnFrameFlood(t *testing.T) {
	conn := newFakeConn()
	client := NewClient(1, conn, 4)

	done := make(chan struct{})

	go func() {
		client.ReadPump(nil)
		close(done)
	}()

	// 持续投喂空帧，直到 ReadPump 退出。
	go func() {
		for i := 0; i < MaxFrameBurst+50; i++ {
			select {
			case conn.reads <- fakeRead{payload: []byte(`{}`)}:
			case <-done:
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ReadPump should stop after frame flood")
	}
}
