package ws

import (
	"encoding/json"
	"time"
)

// WebSocket 帧的类型。客户端和服务端共用同一套 envelope。
const (
	// TypePing 客户端保活探测。
	TypePing = "ping"

	// TypePong 服务端对 ping 的回应。
	TypePong = "pong"

	// TypeError 服务端返回错误。
	TypeError = "error"

	// TypeMessageSend 客户端发送消息。
	TypeMessageSend = "message.send"

	// TypeMessageAck 服务端确认消息已落库。
	TypeMessageAck = "message.ack"

	// TypeMessageNew 服务端推送新消息。
	TypeMessageNew = "message.new"
)

// Envelope 是 WebSocket 帧的统一结构。
//
// Data 使用 json.RawMessage，先解析外层拿到 type，
// 再把 Data 交给对应的处理器解析，避免消息类型和结构耦合。
type Envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// ErrorPayload 是 TypeError 帧的数据体。
type ErrorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MessagePayload 是 message.new 帧的数据体。
type MessagePayload struct {
	ID             uint64    `json:"id"`
	ConversationID uint64    `json:"conversation_id"`
	SenderID       uint64    `json:"sender_id"`
	Seq            uint64    `json:"seq"`
	ClientMsgID    string    `json:"client_msg_id"`
	ContentType    uint8     `json:"content_type"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

// NewEnvelope 把业务数据序列化成可直接发送的帧。
func NewEnvelope(messageType string, payload any) ([]byte, error) {
	var data json.RawMessage

	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		data = raw
	}

	return json.Marshal(Envelope{
		Type: messageType,
		Data: data,
	})
}
