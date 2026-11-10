package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"gochat/internal/auth"
	"gochat/internal/logging"
	"gochat/internal/service"
	"gochat/internal/ws"
	"gochat/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// newUpgrader 构造 WebSocket 升级器。
//
// allowedOrigins 为空表示不校验（开发环境）；
// 非浏览器客户端（websocat、移动端）不带 Origin 头，直接放行。
func newUpgrader(allowedOrigins []string) websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  4 << 10,
		WriteBufferSize: 4 << 10,

		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}

			if len(allowedOrigins) == 0 {
				return true
			}

			for _, allowed := range allowedOrigins {
				if allowed == "*" || strings.EqualFold(origin, allowed) {
					return true
				}
			}

			return false
		},
	}
}

// messageSender 抽象发送消息的业务能力，便于测试替换。
type messageSender interface {
	SendMessage(
		ctx context.Context,
		senderID uint64,
		conversationID uint64,
		clientMsgID string,
		contentType uint8,
		content string,
	) (*service.SendMessageResult, error)
}

// presenceTracker 抽象在线状态（由 presence.Presence 实现）。
type presenceTracker interface {
	Online(ctx context.Context, userID uint64) error
	Offline(ctx context.Context, userID uint64) error
	KeepAlive(ctx context.Context, userID uint64) error
}

// messageLimiter 抽象消息限流（由 ratelimit.UserLimiter 实现）。
type messageLimiter interface {
	Allow(userID uint64) bool
}

// WSConfig 汇总 WebSocket 处理器的依赖。
type WSConfig struct {
	Hub            *ws.Hub
	Chat           messageSender
	Presence       presenceTracker
	Limiter        messageLimiter
	JWTSecret      string
	MaxConnections int
	AllowedOrigins []string
}

// WSHandler 处理 WebSocket 连接。
type WSHandler struct {
	hub            *ws.Hub
	chat           messageSender
	presence       presenceTracker
	limiter        messageLimiter
	jwtSecret      string
	maxConnections int
	upgrader       websocket.Upgrader
}

// NewWSHandler 创建 WebSocket 处理器。
func NewWSHandler(cfg WSConfig) *WSHandler {
	return &WSHandler{
		hub:            cfg.Hub,
		chat:           cfg.Chat,
		presence:       cfg.Presence,
		limiter:        cfg.Limiter,
		jwtSecret:      cfg.JWTSecret,
		maxConnections: cfg.MaxConnections,
		upgrader:       newUpgrader(cfg.AllowedOrigins),
	}
}

// Serve 处理 GET /api/v1/ws。
//
// 该处理器会一直阻塞到连接断开，因此一次请求对应一条长期连接。
func (h *WSHandler) Serve(c *gin.Context) {
	// 1. 从 query 读取 token。
	//    浏览器的 WebSocket API 不能自定义请求头，
	//    所以不能复用 Authorization: Bearer，只能放在 query 参数里。
	token := c.Query("token")
	if token == "" {
		response.Error(c, http.StatusUnauthorized, 4011, "missing token")
		return
	}

	// 2. 校验 token。
	claims, err := auth.ParseToken(token, h.jwtSecret)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, 4013, "invalid or expired token")
		return
	}

	// 连接数上限：软限制，先检查再注册（极端并发下可能略微超出）。
	if h.maxConnections > 0 && h.hub.Connections() >= h.maxConnections {
		response.Error(c, http.StatusServiceUnavailable, 5031, "too many connections")
		return
	}

	// 3. 升级为 WebSocket 连接。
	//    升级失败时 upgrader 已经写过 HTTP 响应，这里只需要返回。
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logging.FromContext(c.Request.Context()).Error("ws upgrade failed", "error", err)
		return
	}

	// 4. 注册连接。
	client := ws.NewClient(claims.UserID, conn, ws.DefaultSendBufferSize)
	h.hub.Register(client)

	// 4.1 标记在线（失败不影响连接，只记录日志）。
	if h.presence != nil {
		if err := h.presence.Online(c.Request.Context(), client.UserID()); err != nil {
			logging.FromContext(c.Request.Context()).Error("presence online failed", "user_id", client.UserID(), "error", err)
		}
	}

	logging.FromContext(c.Request.Context()).Info("ws connected",
		"user_id", client.UserID(),
		"online_users", h.hub.OnlineUsers(),
		"connections", h.hub.Connections(),
	)

	// 5. 启动写循环：它是这条连接唯一的写者。
	go client.WritePump(ws.PingInterval)

	// 6. 读循环阻塞在这里，直到连接断开。
	//    使用请求级 context：连接断开时会被取消，
	//    下游的数据库操作也会随之中断，不会在断连后继续写库。
	ctx := c.Request.Context()
	// 6.1 心跳续期：连接存活期间定期刷新在线状态的 TTL。
	go h.keepPresenceAlive(ctx, client.UserID())

	client.ReadPump(func(payload []byte) {
		h.handleMessage(ctx, client, payload)
	})

	// 7. 连接已断开，清理资源。
	//    Unregister 和 Close 都是幂等的，Hub 踢人时重复调用也没问题。
	h.hub.Unregister(client)
	client.Close()

	if h.presence != nil {
		// 注意：请求的 ctx 此时已经被取消，清理必须用独立 ctx，
		// 否则 Redis 调用会立刻返回 context canceled。
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		if err := h.presence.Offline(cleanupCtx, client.UserID()); err != nil {
			logging.FromContext(ctx).Error("presence offline failed", "user_id", client.UserID(), "error", err)
		}

		cancel()
	}

	logging.FromContext(ctx).Info("ws disconnected",
		"user_id", client.UserID(),
		"online_users", h.hub.OnlineUsers(),
		"connections", h.hub.Connections(),
	)

}

// handleMessage 处理客户端发来的业务消息。
func (h *WSHandler) handleMessage(
	ctx context.Context,
	client *ws.Client,
	payload []byte,
) {
	var env ws.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		h.sendError(client, 4001, "invalid message format")
		return
	}

	switch env.Type {
	case ws.TypePing:
		reply, err := ws.NewEnvelope(ws.TypePong, nil)
		if err != nil {
			return
		}

		_ = client.Send(reply)

	case ws.TypeMessageSend:
		h.handleSendMessage(ctx, client, env.Data)

	case "":
		h.sendError(client, 4001, "missing message type")

	default:
		h.sendError(client, 4004, "unsupported message type")
	}
}

// handleSendMessage 处理 message.send：落库后返回 message.ack。
//
// 注意：message.new 的推送由 ChatService 完成，Handler 只负责 ack。
func (h *WSHandler) handleSendMessage(
	ctx context.Context,
	client *ws.Client,
	data json.RawMessage,
) {
	var req ws.MessageSendPayload
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendError(client, 4001, "invalid message payload")
		return
	}
	// 限流：按用户维度，超限直接回错误帧，不写库。
	if h.limiter != nil && !h.limiter.Allow(client.UserID()) {
		h.sendError(client, 4290, "too many requests")
		return
	}

	result, err := h.chat.SendMessage(
		ctx,
		client.UserID(),
		req.ConversationID,
		req.ClientMsgID,
		req.ContentType,
		req.Content,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidMessage):
			h.sendError(client, 4001, "invalid message")

		case errors.Is(err, service.ErrNotConversationMember):
			h.sendError(client, 4031, "not a conversation member")

		case errors.Is(err, service.ErrConversationNotFound):
			h.sendError(client, 4040, "conversation not found")

		default:
			logging.FromContext(ctx).Error("send message failed", "user_id", client.UserID(), "error", err)
			h.sendError(client, 5000, "internal server error")
		}

		return
	}

	ack, err := ws.NewEnvelope(ws.TypeMessageAck, ws.MessageAckPayload{
		ClientMsgID:    result.Message.ClientMsgID,
		ID:             result.Message.ID,
		ConversationID: result.Message.ConversationID,
		Seq:            result.Message.Seq,
		Duplicated:     result.Duplicated,
	})
	if err != nil {
		logging.FromContext(ctx).Error("build ack payload failed", "error", err)
		return
	}

	_ = client.Send(ack)
}

// sendError 向客户端推送错误帧。
func (h *WSHandler) sendError(client *ws.Client, code int, message string) {
	payload, err := ws.NewEnvelope(ws.TypeError, ws.ErrorPayload{
		Code:    code,
		Message: message,
	})
	if err != nil {
		return
	}

	_ = client.Send(payload)
}

// presenceKeepAliveInterval 是在线状态续期间隔。
// 必须明显小于 presence.DefaultTTL（当前 90s），留出 2 倍容错。
const presenceKeepAliveInterval = 30 * time.Second

// keepPresenceAlive 在连接存活期间定期续期在线状态。
func (h *WSHandler) keepPresenceAlive(ctx context.Context, userID uint64) {
	if h.presence == nil {
		return
	}

	ticker := time.NewTicker(presenceKeepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			if err := h.presence.KeepAlive(ctx, userID); err != nil {
				logging.FromContext(ctx).Error("presence keepalive failed", "user_id", userID, "error", err)
			}
		}
	}
}
