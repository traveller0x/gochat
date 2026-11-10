// loadtest 是 GoChat 的 WebSocket 压测客户端（多用户多会话版）。
//
// 每个虚拟用户：
//  1. 注册 / 登录，拿到 token
//  2. 和 -peer 指定的账号创建一个独立会话
//  3. 建立 1 条 WebSocket 连接，按 -rate 频率往自己的会话发消息
//
// 因此 N 个用户 = N 个会话 = N 条连接，扇出只有 1~2，
// 能测出服务端真实的写入与推送能力。
package main

import (
	"bytes"
	"context"
	"encoding/json"
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
		return sorted[int(float64(len(sorted)-1)*p)]
	}

	return pick(0.50), pick(0.95), pick(0.99)
}

// ---------- HTTP 辅助 ----------

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func callAPI(
	client *http.Client,
	method string,
	endpoint string,
	token string,
	body any,
	out any,
) (int, error) {
	var payload []byte

	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}

		payload = raw
	}

	req, err := http.NewRequest(method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	var env apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return resp.StatusCode, err
	}

	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return resp.StatusCode, err
		}
	}

	return resp.StatusCode, nil
}

func register(client *http.Client, base, username, password string) error {
	status, err := callAPI(
		client,
		http.MethodPost,
		base+"/api/v1/auth/register",
		"",
		map[string]string{
			"username": username,
			"password": password,
			"nickname": username,
		},
		nil,
	)
	if err != nil {
		return err
	}

	// 201 = 新建成功；409 = 已存在（可重跑）
	if status != http.StatusCreated && status != http.StatusConflict {
		return fmt.Errorf("register %s: status=%d", username, status)
	}

	return nil
}

func login(client *http.Client, base, username, password string) (string, error) {
	var out struct {
		AccessToken string `json:"access_token"`
	}

	status, err := callAPI(
		client,
		http.MethodPost,
		base+"/api/v1/auth/login",
		"",
		map[string]string{"username": username, "password": password},
		&out,
	)
	if err != nil {
		return "", err
	}

	if status != http.StatusOK || out.AccessToken == "" {
		return "", fmt.Errorf("login %s: status=%d", username, status)
	}

	return out.AccessToken, nil
}

func currentUserID(client *http.Client, base, token string) (uint64, error) {
	var out struct {
		User struct {
			ID uint64 `json:"id"`
		} `json:"user"`
	}

	if _, err := callAPI(client, http.MethodGet, base+"/api/v1/users/me", token, nil, &out); err != nil {
		return 0, err
	}

	if out.User.ID == 0 {
		return 0, fmt.Errorf("empty user id")
	}

	return out.User.ID, nil
}

func createConversation(
	client *http.Client,
	base string,
	token string,
	peerID uint64,
) (uint64, error) {
	var out struct {
		Conversation struct {
			ID uint64 `json:"id"`
		} `json:"conversation"`
	}

	status, err := callAPI(
		client,
		http.MethodPost,
		base+"/api/v1/conversations",
		token,
		map[string]uint64{"peer_user_id": peerID},
		&out,
	)
	if err != nil {
		return 0, err
	}

	if status != http.StatusOK || out.Conversation.ID == 0 {
		return 0, fmt.Errorf("create conversation: status=%d", status)
	}

	return out.Conversation.ID, nil
}

// ---------- WebSocket ----------

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

	ticker := time.NewTicker(time.Duration(float64(time.Second) / messagesPerSecond))
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

// ---------- main ----------

func main() {
	var (
		baseURL     = flag.String("base", "http://127.0.0.1:8080", "后端地址")
		password    = flag.String("password", "Test123456", "压测用户密码")
		peer        = flag.String("peer", "bob", "每个用户聊天的对端账号")
		users       = flag.Int("users", 50, "虚拟用户数（每个用户 1 条连接 + 1 个会话）")
		ratePerUser = flag.Float64("rate", 2, "每个用户每秒发送的消息数")
		duration    = flag.Duration("duration", 30*time.Second, "压测持续时间")
		prefix      = flag.String("prefix", "lt", "压测用户名前缀")
	)
	flag.Parse()

	if *users <= 0 || *ratePerUser <= 0 {
		fmt.Fprintln(os.Stderr, "users 和 rate 必须大于 0")
		os.Exit(2)
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}

	// 1. 找到对端用户 ID
	peerToken, err := login(httpClient, *baseURL, *peer, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "登录对端 %s 失败: %v\n", *peer, err)
		os.Exit(1)
	}

	peerID, err := currentUserID(httpClient, *baseURL, peerToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取对端 user id 失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("对端 %s 的 user id = %d\n", *peer, peerID)

	// 2. 准备虚拟用户
	runID := time.Now().Unix() % 100000

	type clientSpec struct {
		token          string
		conversationID uint64
	}

	specs := make([]clientSpec, 0, *users)

	for i := 0; i < *users; i++ {
		username := fmt.Sprintf("%s%d_%03d", *prefix, runID, i)

		if err := register(httpClient, *baseURL, username, *password); err != nil {
			fmt.Fprintf(os.Stderr, "注册 %s 失败: %v\n", username, err)
			os.Exit(1)
		}

		token, err := login(httpClient, *baseURL, username, *password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "登录 %s 失败: %v\n", username, err)
			os.Exit(1)
		}

		convID, err := createConversation(httpClient, *baseURL, token, peerID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "为 %s 创建会话失败: %v\n", username, err)
			os.Exit(1)
		}

		specs = append(specs, clientSpec{token: token, conversationID: convID})

		if (i+1)%10 == 0 {
			fmt.Printf("  已准备 %d/%d 个用户\n", i+1, *users)
		}
	}

	wsBase := "ws" + strings.TrimPrefix(*baseURL, "http")
	_ = wsBase

	// 3. 开始压测
	totalRate := float64(*users) * (*ratePerUser)

	fmt.Printf(
		"开始压测: users=%d rate=%.1f/s/user total≈%.0f/s duration=%s\n",
		*users, *ratePerUser, totalRate, *duration,
	)

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = signalCtx

	st := &stats{}
	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(len(specs))

	for _, spec := range specs {
		go func(spec clientSpec) {
			defer wg.Done()

			wsURL := fmt.Sprintf(
				"%s/api/v1/ws?token=%s",
				wsBase, url.QueryEscape(spec.token),
			)

			runConn(ctx, wsURL, spec.conversationID, *ratePerUser, st)
		}(spec)
	}

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
}
