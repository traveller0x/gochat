package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// waitForListenerClosed 轮询直到监听端口不再接受连接。
// 用轮询代替 time.Sleep，测试既稳定又快。
func waitForListenerClosed(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err != nil {
			// 连接失败说明 listener 已关闭，优雅关闭已经开始。
			return
		}

		_ = conn.Close()

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("listener %s is still accepting connections", addr)
}

func TestServeGracefulShutdownWaitsForInflightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		// 通知测试：请求已经进入 handler，此时关闭流程还没开始。
		close(started)

		// 挂起，模拟一个正在处理中的业务请求。
		<-release

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	})

	// 1. 用 :0 让系统分配空闲端口。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	addr := ln.Addr().String()

	// 2. 启动服务。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := &http.Server{Handler: mux}
	errCh := make(chan error, 1)

	go func() {
		errCh <- Serve(ctx, ln, srv)
	}()

	// 3. 发送请求，此时 handler 会阻塞在 <-release。
	respCh := make(chan string, 1)

	go func() {
		client := &http.Client{Timeout: 3 * time.Second}

		resp, err := client.Get("http://" + addr + "/slow")
		if err != nil {
			respCh <- "request error: " + err.Error()
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			respCh <- "read body error: " + err.Error()
			return
		}

		respCh <- fmt.Sprintf("%d:%s", resp.StatusCode, body)
	}()

	// 4. 等请求真正进入 handler，再触发关闭。
	<-started
	cancel()

	// 5. 确认监听器已关闭：优雅关闭不会接受新连接。
	waitForListenerClosed(t, addr)

	// 6. 放行在途请求，它应该能完整返回 200。
	close(release)

	select {
	case got := <-respCh:
		if got != "200:done" {
			t.Fatalf("in-flight request = %q, want %q", got, "200:done")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight request did not complete")
	}

	// 7. Serve 必须返回 nil，且不能有 goroutine 残留。
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve() = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not return after shutdown")
	}
}

// TestRunReturnsErrorWhenListenFails 测试端口被占用时 Run 立即返回错误，
// 而不是卡住或者 panic。
func TestRunReturnsErrorWhenListenFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer func() {
		_ = ln.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := &http.Server{Addr: ln.Addr().String()}

	err = Run(ctx, srv)
	if err == nil {
		t.Fatal("Run() = nil, want listen error")
	}
}
