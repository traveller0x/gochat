import { FrameType, type Envelope } from '../types/protocol'

export type SocketState = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed'

export interface SocketHandlers {
  onEvent: (envelope: Envelope) => void
  onStateChange?: (state: SocketState) => void
}

/**
 * ChatSocket 负责长连接的全部"脏活"：
 *   - 指数退避 + 抖动重连
 *   - 业务层心跳（30s 一次 ping）
 *   - 断线期间发送排队，连上后按序补发
 *   - 状态机对外暴露，UI 用来显示"重连中"
 */
export class ChatSocket {
  private readonly getToken: () => string
  private readonly handlers: SocketHandlers

  private ws: WebSocket | null = null
  private state: SocketState = 'idle'
  private retry = 0
  private queue: string[] = []
  private heartbeatTimer: number | null = null
  private reconnectTimer: number | null = null
  private manualClose = false

  constructor(getToken: () => string, handlers: SocketHandlers) {
    this.getToken = getToken
    this.handlers = handlers
  }

  getState(): SocketState {
    return this.state
  }

  connect(): void {
    if (this.state === 'connecting' || this.state === 'open') {
      return
    }

    const token = this.getToken()
    if (!token) {
      return
    }

    this.manualClose = false
    this.setState(this.retry === 0 ? 'connecting' : 'reconnecting')

    const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${scheme}://${window.location.host}/api/v1/ws?token=${encodeURIComponent(token)}`

    const ws = new WebSocket(url)
    this.ws = ws

    ws.onopen = () => {
      this.retry = 0
      this.setState('open')
      this.flushQueue()
      this.startHeartbeat()
    }

    ws.onmessage = (event) => {
      let envelope: Envelope

      try {
        envelope = JSON.parse(event.data as string) as Envelope
      } catch {
        return
      }

      // pong 只用于保活，不需要业务处理。
      if (envelope.type === FrameType.pong) {
        return
      }

      this.handlers.onEvent(envelope)
    }

    ws.onclose = () => {
      this.stopHeartbeat()

      if (this.manualClose) {
        this.setState('closed')
        return
      }

      this.scheduleReconnect()
    }

    ws.onerror = () => {
      // onerror 之后一定会有 onclose，重连逻辑统一放在 onclose。
      ws.close()
    }
  }

  /** 发送帧；连接未就绪时进入队列，连上后自动补发。 */
  send(envelope: Envelope): void {
    const raw = JSON.stringify(envelope)

    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(raw)
      return
    }

    this.queue.push(raw)
    this.connect()
  }

  /** 主动关闭，不再重连（退出登录时调用）。 */
  close(): void {
    this.manualClose = true
    this.stopHeartbeat()

    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }

    this.ws?.close()
    this.ws = null
    this.queue = []
    this.setState('closed')
  }

  private flushQueue(): void {
    const ws = this.ws
    if (!ws) {
      return
    }

    while (this.queue.length > 0) {
      const raw = this.queue.shift()
      if (raw !== undefined) {
        ws.send(raw)
      }
    }
  }

  private scheduleReconnect(): void {
    // 指数退避：1s、2s、4s……上限 30s。
    // 乘上随机抖动，避免服务端重启后所有客户端同时重连（惊群）。
    const base = Math.min(30_000, 1_000 * 2 ** this.retry)
    const delay = Math.round(base * (0.8 + Math.random() * 0.4))

    this.retry += 1
    this.setState('reconnecting')

    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, delay)
  }

  private startHeartbeat(): void {
    this.stopHeartbeat()

    this.heartbeatTimer = window.setInterval(() => {
      this.send({ type: FrameType.ping })
    }, 30_000)
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer !== null) {
      window.clearInterval(this.heartbeatTimer)
      this.heartbeatTimer = null
    }
  }

  private setState(state: SocketState): void {
    if (this.state === state) {
      return
    }

    this.state = state
    this.handlers.onStateChange?.(state)
  }
}
