// 与 Go 端 internal/ws/protocol.go 一一对应的协议类型。
//
// 注意：JSON 数字在 JS 里只有 53 位精度。
// 当前自增 ID 很小没问题；若将来改用雪花 ID，后端必须把 ID 序列化成字符串。

export interface Envelope<T = unknown> {
  type: string
  data?: T
}

/** 帧类型常量，对应 Go 的 ws.Type*。 */
export const FrameType = {
  ping: 'ping',
  pong: 'pong',
  error: 'error',
  messageSend: 'message.send',
  messageAck: 'message.ack',
  messageNew: 'message.new',
  messageRead: 'message.read',
} as const

export type FrameTypeValue = (typeof FrameType)[keyof typeof FrameType]

export interface UserBrief {
  id: number
  username: string
  nickname: string
  avatar: string
}

/** 对应 ws.MessagePayload */
export interface MessagePayload {
  id: number
  conversation_id: number
  sender_id: number
  seq: number
  client_msg_id: string
  content_type: number
  content: string
  created_at: string
}

/** 对应 ws.MessageSendPayload */
export interface MessageSendPayload {
  conversation_id: number
  client_msg_id: string
  content_type: number
  content: string
}

/** 对应 ws.MessageAckPayload */
export interface MessageAckPayload {
  client_msg_id: string
  id: number
  conversation_id: number
  seq: number
  duplicated: boolean
}

/** 对应 ws.MessageReadPayload */
export interface MessageReadPayload {
  conversation_id: number
  user_id: number
  last_read_seq: number
}

/** 对应 ws.ErrorPayload */
export interface ErrorPayload {
  code: number
  message: string
}

/** 对应 handler.conversationResponse */
export interface ConversationSummary {
  id: number
  type: number
  name: string
  unread_count: number
  last_read_seq: number
  peer?: UserBrief
  last_message?: MessagePayload
}

/** 会话类型 */
export const ConversationType = {
  single: 1,
  group: 2,
} as const

/** 消息内容类型 */
export const ContentType = {
  text: 1,
  image: 2,
  file: 3,
} as const

/** 本地消息状态：发送中 / 已确认 / 失败 */
export type MessageStatus = 'sending' | 'sent' | 'failed'

/** 本地消息 = 服务端消息 + 本地发送状态 */
export interface ChatMessage extends MessagePayload {
  status: MessageStatus
}
