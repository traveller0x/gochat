package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

// shutdownTimeout 是等待在途请求处理完成的最长时间。
// 超过这个时间仍未结束的请求会被强制断开，避免进程永远退不出去。
const shutdownTimeout = 5 * time.Second

// Run 监听 srv.Addr 并在 ctx 取消时优雅关闭。
func Run(ctx context.Context, srv *http.Server) error {
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", srv.Addr, err)
	}

	return Serve(ctx, ln, srv)
}

// Serve 在调用方提供的 listener 上提供服务，并在 ctx 取消时优雅关闭。
//
// listener 由外部传入而不是内部创建，这样测试可以用 :0 随机端口，
// 并且在服务启动前就能知道真实地址。
func Serve(ctx context.Context, ln net.Listener, srv *http.Server) error {
	errCh := make(chan error, 1)

	go func() {
		log.Printf("GoChat server starting on %s", ln.Addr())

		// Serve 在 Shutdown 被调用后会返回 http.ErrServerClosed。
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		// 启动阶段就失败（例如端口被占用）。
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve: %w", err)

	case <-ctx.Done():
	}

	// 1. 停止接收新连接，并等待在途请求处理完成。
	//    这里必须新建 ctx：传入的 ctx 已经因为取消而 Done，直接使用会立刻超时。
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		// 超时后强制关闭剩余连接，避免进程卡死。
		_ = srv.Close()

		return fmt.Errorf("graceful shutdown: %w", err)
	}

	// 2. 等 Serve 真正返回，确保 goroutine 已经收尾。
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}

	log.Println("http server stopped gracefully")

	return nil
}
