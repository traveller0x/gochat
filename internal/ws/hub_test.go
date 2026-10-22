package ws

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// receive 从连接的消息队列取一条消息，超时则测试失败。
func receive(t *testing.T, c *Client) []byte {
	t.Helper()

	select {
	case got := <-c.Messages():
		return got

	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")

		return nil
	}
}

// TestHubSendToUserAllDevices 测试同一个用户的多个设备都能收到消息。
func TestHubSendToUserAllDevices(t *testing.T) {
	hub := NewHub()

	mobile := NewClient(1, 4)
	laptop := NewClient(1, 4)
	other := NewClient(2, 4)

	hub.Register(mobile)
	hub.Register(laptop)
	hub.Register(other)

	delivered := hub.SendToUser(1, []byte("hello"))
	if delivered != 2 {
		t.Fatalf("delivered = %d, want 2", delivered)
	}

	if got := string(receive(t, mobile)); got != "hello" {
		t.Errorf("mobile received %q, want %q", got, "hello")
	}

	if got := string(receive(t, laptop)); got != "hello" {
		t.Errorf("laptop received %q, want %q", got, "hello")
	}

	// 其他用户不应该收到任何消息。
	select {
	case got := <-other.Messages():
		t.Errorf("other user received %q, want nothing", got)

	default:
	}
}

// TestHubSendToOfflineUser 测试给离线用户发消息不会 panic。
func TestHubSendToOfflineUser(t *testing.T) {
	hub := NewHub()

	if got := hub.SendToUser(99, []byte("hello")); got != 0 {
		t.Fatalf("delivered = %d, want 0", got)
	}
}

// TestHubDropsSlowClient 测试缓冲区满的慢客户端会被摘除。
func TestHubDropsSlowClient(t *testing.T) {
	hub := NewHub()

	slow := NewClient(1, 1)
	hub.Register(slow)

	// 先填满缓冲区：消息进入队列但没人消费。
	if err := slow.Send([]byte("first")); err != nil {
		t.Fatalf("fill buffer: %v", err)
	}

	// 此时再投递必然失败，Hub 应该踢掉这个连接。
	if delivered := hub.SendToUser(1, []byte("second")); delivered != 0 {
		t.Fatalf("delivered = %d, want 0", delivered)
	}

	if !slow.IsClosed() {
		t.Error("slow client should be closed")
	}

	if got := hub.ConnectionsOf(1); got != 0 {
		t.Errorf("connections of user 1 = %d, want 0", got)
	}

	// 关闭后再发送应返回 ErrClientClosed，不能 panic 也不能阻塞。
	if err := slow.Send([]byte("third")); !errors.Is(err, ErrClientClosed) {
		t.Errorf("Send() after close = %v, want %v", err, ErrClientClosed)
	}
}

// TestHubKeepsHealthyClientWhenAnotherFails 测试一个连接失败不会影响同用户的其他连接。
func TestHubKeepsHealthyClientWhenAnotherFails(t *testing.T) {
	hub := NewHub()

	slow := NewClient(1, 1)
	healthy := NewClient(1, 4)

	hub.Register(slow)
	hub.Register(healthy)

	if err := slow.Send([]byte("fill")); err != nil {
		t.Fatalf("fill buffer: %v", err)
	}

	if delivered := hub.SendToUser(1, []byte("hello")); delivered != 1 {
		t.Fatalf("delivered = %d, want 1", delivered)
	}

	if got := string(receive(t, healthy)); got != "hello" {
		t.Errorf("healthy client received %q, want %q", got, "hello")
	}

	if got := hub.ConnectionsOf(1); got != 1 {
		t.Errorf("connections of user 1 = %d, want 1", got)
	}

	if !slow.IsClosed() {
		t.Error("slow client should be closed")
	}
}

// TestHubUnregisterIdempotent 测试重复注销不会出错。
func TestHubUnregisterIdempotent(t *testing.T) {
	hub := NewHub()

	c := NewClient(1, 1)
	hub.Register(c)

	hub.Unregister(c)
	hub.Unregister(c)

	if got := hub.Connections(); got != 0 {
		t.Errorf("Connections() = %d, want 0", got)
	}

	if got := hub.OnlineUsers(); got != 0 {
		t.Errorf("OnlineUsers() = %d, want 0", got)
	}
}

// TestHubConcurrentRegisterUnregister 用并发压测验证加锁正确。
// 这个测试必须配合 -race 才有意义。
func TestHubConcurrentRegisterUnregister(t *testing.T) {
	const (
		goroutines = 100
		users      = 10
	)

	hub := NewHub()

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()

			userID := uint64(i%users + 1)
			c := NewClient(userID, 4)

			hub.Register(c)
			hub.SendToUser(userID, []byte(fmt.Sprintf("msg-%d", i)))
			hub.Unregister(c)
		}(i)
	}

	wg.Wait()

	// 所有连接都已注销，注册表必须回到空状态，否则就是泄漏。
	if got := hub.OnlineUsers(); got != 0 {
		t.Errorf("OnlineUsers() = %d, want 0", got)
	}

	if got := hub.Connections(); got != 0 {
		t.Errorf("Connections() = %d, want 0", got)
	}
}
