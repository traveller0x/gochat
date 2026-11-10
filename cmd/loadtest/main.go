// loadtest 是 GoChat 的 WebSocket 压测客户端。
//
// 它会用同一个账号建立 N 条连接，按固定速率发送消息，
// 统计 ack 延迟（P50/P95/P99）、错误码分布和吞吐。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type ackPayload struct {
	ClientMsgID string `json:"client_msg_id"`
}

type errorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type stats struct {
	sent     atomic.Int64
	acked    atomic.Int64
	errors   atomic.Int64
	connOK   atomic.Int64
	connFail atomic.Int64

	mu        sync.Mutex
	latencies []time.Duration

	errorCodes sync.Map // map[int]*atomic.Int64
	pending    sync.Map // clientMsgID -> time.Time
}

func (s *stats) recordLatency(start time.Time) {
	latency := time.Since(start)

	s.mu.Lock()
	s.latencies = append(s.latencies, latency)
	s.mu.Unlock()
}

func (s *stats) recordError(code int) {
	value, _ := s.errorCodes.LoadOrStore(code, &atomic.Int64{})
	value.(*atomic.Int64).Add(1)
}

func (s *stats) percentiles() (time.Duration, time.Duration, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.latencies) == 0 {
		return 0, 0, 0
	}

	sorted := append([]time.Duration(nil), s.latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	pick := func(p float64) time.Duration {
		index := int(float64(len(sorted)-1) * p)
		return sorted[index]
	}

	return pick(0.50), pick(0.95), pick(0.99)
}

func login(baseURL, username, password string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Post(
		baseURL+"/api/v1/auth/login",
		"application/json",
		strings.NewReader(string(body)),
	)
	if err != nil {
		return "", fmt.Errorf("login request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode login response: %w", err)
	}

	if result.Code != 0 || result.Data.AccessToken == "" {
		return "", fmt.Errorf("login failed: code=%d message=%s", result.Code, result.Message)
	}

	return result.Data.AccessToken, nil
}

func runConn(
	ctx context.Context,
	wsURL string,
	conversationID uint64,
	messagesPerSecond float64,
	st *stats,
) {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		st.connFail.Add(1)
		return
	}
	defer func() { _ = conn.Close() }()

	st.connOK.Add(1)

	readerDone := make(chan struct{})

	go func() {
		defer close(readerDone)

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var env envelope
			if err := json.Unmarshal(data, &env); err != nil {
				continue
			}

			switch env.Type {
			case "message.ack":
				var ack ackPayload
				if err := json.Unmarshal(env.Data, &ack); err != nil {
					continue
				}

				if start, ok := st.pending.LoadAndDelete(ack.ClientMsgID); ok {
					st.acked.Add(1)
					st.recordLatency(start.(time.Time))
				}

			case "error":
				var e errorPayload
				if err := json.Unmarshal(env.Data, &e); err != nil {
					continue
				}

				st.errors.Add(1)
				st.recordError(e.Code)
			}
		}
	}()

	interval := time.Duration(float64(time.Second) / messagesPerSecond)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(time.Second),
			)
			<-readerDone
			return

		case <-ticker.C:
			clientMsgID := fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Uint32())

			payload, err := json.Marshal(map[string]any{
				"type": "message.send",
				"data": map[string]any{
					"conversation_id": conversationID,
					"client_msg_id":   clientMsgID,
					"content_type":    1,
					"content":         "loadtest",
				},
			})
			if err != nil {
				return
			}

			st.pending.Store(clientMsgID, time.Now())

			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}

			st.sent.Add(1)
		}
	}
}

func main() {
	var (
		baseURL        = flag.String("base", "http://127.0.0.1:8080", "后端地址")
		username       = flag.String("user", "alice", "登录用户名")
		password       = flag.String("password", "Test123456", "登录密码")
		conns          = flag.Int("conns", 100, "并发连接数")
		ratePerConn    = flag.Float64("rate", 1, "每条连接每秒发送的消息数")
		duration       = flag.Duration("duration", 30*time.Second, "压测持续时间")
		conversationID = flag.Uint64("conversation", 238, "目标会话 ID")
	)
	flag.Parse()

	if *conns <= 0 || *ratePerConn <= 0 {
		fmt.Fprintln(os.Stderr, "conns 和 rate 必须大于 0")
		os.Exit(2)
	}

	token, err := login(*baseURL, *username, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "登录失败: %v\n", err)
		os.Exit(1)
	}

	wsBase := strings.Replace(*baseURL, "http", "ws", 1)
	wsURL := fmt.Sprintf("%s/api/v1/ws?token=%s", wsBase, url.QueryEscape(token))

	fmt.Printf(
		"开始压测: conns=%d rate=%.1f/s/conn total≈%.0f/s duration=%s conversation=%d\n",
		*conns, *ratePerConn, float64(*conns)*(*ratePerConn), *duration, *conversationID,
	)

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	// Ctrl+C 也能提前结束
	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = signalCtx

	st := &stats{}
	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(*conns)

	for i := 0; i < *conns; i++ {
		go func() {
			defer wg.Done()
			runConn(ctx, wsURL, *conversationID, *ratePerConn, st)
		}()
	}

	// 每 5 秒打印一次进度
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-progressDone:
				return
			case <-ticker.C:
				fmt.Printf(
					"  [进行中] 连接 ok=%d fail=%d 发送=%d 确认=%d 错误=%d\n",
					st.connOK.Load(), st.connFail.Load(),
					st.sent.Load(), st.acked.Load(), st.errors.Load(),
				)
			}
		}
	}()

	wg.Wait()
	close(progressDone)

	elapsed := time.Since(start)
	p50, p95, p99 := st.percentiles()

	fmt.Println()
	fmt.Println("===== 压测结果 =====")
	fmt.Printf("连接:      成功 %d / 失败 %d\n", st.connOK.Load(), st.connFail.Load())
	fmt.Printf("时长:      %.1fs\n", elapsed.Seconds())
	fmt.Printf("发送:      %d 条 (%.1f/s)\n", st.sent.Load(), float64(st.sent.Load())/elapsed.Seconds())
	fmt.Printf("确认:      %d 条 (%.1f/s)\n", st.acked.Load(), float64(st.acked.Load())/elapsed.Seconds())
	fmt.Printf("错误:      %d 条\n", st.errors.Load())

	st.errorCodes.Range(func(key, value any) bool {
		fmt.Printf("  - code %v: %d 次\n", key, value.(*atomic.Int64).Load())
		return true
	})

	if st.acked.Load() > 0 {
		fmt.Printf("ack 延迟:  P50=%s P95=%s P99=%s\n",
			p50.Round(100*time.Microsecond),
			p95.Round(100*time.Microsecond),
			p99.Round(100*time.Microsecond),
		)
	}

	if st.errors.Load() > 0 {
		fmt.Println()
		fmt.Println("提示: 如果错误码是 4290，说明被限流；请用更高的限流参数启动服务端，见 README。")
	}

	if st.connFail.Load() > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		fmt.Println("提示: 连接失败较多时，检查服务端 GOCHAT_MAX_CONNECTIONS 是否够大。")
	}
}
