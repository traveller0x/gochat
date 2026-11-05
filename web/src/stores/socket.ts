import { ref } from 'vue'
import { defineStore } from 'pinia'

import { ChatSocket, type SocketState } from '../api/socket'
import {
  FrameType,
  type Envelope,
  type ErrorPayload,
  type MessageAckPayload,
  type MessagePayload,
  type MessageReadPayload,
} from '../types/protocol'
import { useAuthStore } from './auth'
import { useConversationStore } from './conversation'
import { useMessageStore } from './message'

/**
 * socket store 把长连接和业务 store 撮合在一起：
 * 连接本身交给 ChatSocket，事件分发放在这里，UI 只关心 state。
 */
export const useSocketStore = defineStore('socket', () => {
  const state = ref<SocketState>('idle')
  const lastError = ref('')

  let socket: ChatSocket | null = null

  function ensureSocket(): ChatSocket {
    if (socket) {
      return socket
    }

    const auth = useAuthStore()
    const conversations = useConversationStore()
    const messages = useMessageStore()

    socket = new ChatSocket(() => auth.token, {
      onStateChange: (next) => {
        state.value = next
      },

      onEvent: (envelope: Envelope) => {
        switch (envelope.type) {
          case FrameType.messageNew: {
            const payload = envelope.data as MessagePayload

            messages.upsert(payload.conversation_id, payload)
            conversations.touchLastMessage(payload)

            // 自己发的消息也会通过 message.new 回来，不能算未读。
            if (payload.sender_id === auth.user?.id) {
              break
            }

            const isActive = conversations.activeId === payload.conversation_id
            const visible = document.visibilityState === 'visible'

            if (isActive && visible) {
              conversations.clearUnread(payload.conversation_id, payload.seq)
            } else {
              conversations.incrementUnread(payload.conversation_id)
            }

            break
          }

          case FrameType.messageAck: {
            messages.applyAck(envelope.data as MessageAckPayload)
            break
          }

          case FrameType.messageRead: {
            const payload = envelope.data as MessageReadPayload

            if (payload.user_id === auth.user?.id) {
              conversations.clearUnread(payload.conversation_id, payload.last_read_seq)
            }

            break
          }

          case FrameType.error: {
            const payload = envelope.data as ErrorPayload
            lastError.value = `${payload.code}: ${payload.message}`
            break
          }

          default:
            break
        }
      },
    })

    return socket
  }

  function connect(): void {
    ensureSocket().connect()
  }

  function send(envelope: Envelope): void {
    ensureSocket().send(envelope)
  }

  function close(): void {
    socket?.close()
    socket = null
    state.value = 'closed'
  }

  return { state, lastError, connect, send, close }
})
