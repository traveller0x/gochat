package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"gochat/internal/auth"
	"gochat/internal/ws"
	"gochat/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// upgrader 负责把 HTTP 请求升级为 WebSocket 连接。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4 << 10,
	WriteBufferSize: 4 << 10,

	// CheckOrigin 防止跨站 WebSocket 劫持（CSWSH）。
	// 开发阶段为了方便本地调试允许所有来源；
	// 上线前必须改为校验白名单域名。
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSHandler 处理 WebSocket 连接。
type WSHandler struct {
	hub       *ws.Hub
	jwtSecret string
}

// NewWSHandler 创建 WebSocket 处理器。
func NewWSHandler(hub *ws.Hub, jwtSecret string) *WSHandler {
	return &WSHandler{
		hub:       hub,
		jwtSecret: jwtSecret,
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

	// 3. 升级为 WebSocket 连接。
	//    升级失败时 upgrader 已经写过 HTTP 响应，这里只需要返回。
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}

	// 4. 注册连接。
	client := ws.NewClient(claims.UserID, conn, ws.DefaultSendBufferSize)
	h.hub.Register(client)

	log.Printf(
		"ws connected: user=%d online_users=%d connections=%d",
		client.UserID(),
		h.hub.OnlineUsers(),
		h.hub.Connections(),
	)

	// 5. 启动写循环：它是这条连接唯一的写者。
	go client.WritePump(ws.PingInterval)

	// 6. 读循环阻塞在这里，直到连接断开。
	client.ReadPump(func(payload []byte) {
		h.handleMessage(client, payload)
	})

	// 7. 连接已断开，清理资源。
	//    Unregister 和 Close 都是幂等的，Hub 踢人时重复调用也没问题。
	h.hub.Unregister(client)
	client.Close()

	log.Printf(
		"ws disconnected: user=%d online_users=%d connections=%d",
		client.UserID(),
		h.hub.OnlineUsers(),
		h.hub.Connections(),
	)
}

// handleMessage 处理客户端发来的业务消息。
//
// 当前只支持 ping/pong，真正的消息收发在第 8 步接入 ChatService。
func (h *WSHandler) handleMessage(client *ws.Client, payload []byte) {
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

	case "":
		h.sendError(client, 4001, "missing message type")

	default:
		h.sendError(client, 4004, "unsupported message type")
	}
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
