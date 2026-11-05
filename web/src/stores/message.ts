import { reactive, ref } from 'vue'
import { defineStore } from 'pinia'

import { http, type ApiResponse } from '../api/http'
import {
  ContentType,
  type ChatMessage,
  type MessageAckPayload,
  type MessagePayload,
  type MessageSendPayload,
} from '../types/protocol'

/** 后端历史消息接口的分页响应。 */
interface MessagePage {
  messages: MessagePayload[]
  order: 'asc' | 'desc'
  has_more: boolean
  next_before_seq: number
  next_after_seq: number
}

const ACK_TIMEOUT_MS = 8_000
const MAX_ACK_RETRY = 2
const PAGE_SIZE = 20

export const useMessageStore = defineStore('message', () => {
  const byConversation = reactive<Record<number, ChatMessage[]>>({})
  const hasMore = reactive<Record<number, boolean>>({})
  const loaded = reactive<Record<number, boolean>>({})
  const loading = ref(false)

  const ackTimers = new Map<string, number>()
  const ackRetries = new Map<string, number>()

  function list(conversationId: number): ChatMessage[] {
    return byConversation[conversationId] ?? []
  }

  function lastSeq(conversationId: number): number {
    let max = 0

    for (const message of list(conversationId)) {
      if (message.status === 'sent' && message.seq > max) {
        max = message.seq
      }
    }

    return max
  }

  function firstSeq(conversationId: number): number {
    let min = 0

    for (const message of list(conversationId)) {
      if (message.status !== 'sent' || message.seq <= 0) {
        continue
      }

      if (min === 0 || message.seq < min) {
        min = message.seq
      }
    }

    return min
  }

  function sortMessages(messages: ChatMessage[]): void {
    const sent = messages
      .filter((message) => message.status === 'sent')
      .sort((a, b) => a.seq - b.seq)

    const pending = messages.filter((message) => message.status !== 'sent')

    messages.splice(0, messages.length, ...sent, ...pending)
  }

  function upsert(conversationId: number, payload: MessagePayload): void {
    const messages =
      byConversation[conversationId] ?? (byConversation[conversationId] = [])

    const pendingIndex = messages.findIndex(
      (message) =>
        message.client_msg_id === payload.client_msg_id && message.status !== 'sent',
    )

    if (pendingIndex >= 0) {
      messages[pendingIndex] = { ...payload, status: 'sent' }
      sortMessages(messages)
      return
    }

    if (
      messages.some(
        (message) => message.status === 'sent' && message.seq === payload.seq,
      )
    ) {
      return
    }

    messages.push({ ...payload, status: 'sent' })
    sortMessages(messages)
  }

  function createPending(
    conversationId: number,
    senderId: number,
    content: string,
  ): MessageSendPayload {
    const clientMsgId = crypto.randomUUID()

    const message: ChatMessage = {
      id: 0,
      conversation_id: conversationId,
      sender_id: senderId,
      seq: 0,
      client_msg_id: clientMsgId,
      content_type: ContentType.text,
      content,
      created_at: new Date().toISOString(),
      status: 'sending',
    }

    const messages =
      byConversation[conversationId] ?? (byConversation[conversationId] = [])
    messages.push(message)

    return {
      conversation_id: conversationId,
      client_msg_id: clientMsgId,
      content_type: ContentType.text,
      content,
    }
  }

  function applyAck(ack: MessageAckPayload): void {
    clearAck(ack.client_msg_id)

    const messages = byConversation[ack.conversation_id]
    if (!messages) {
      return
    }

    const index = messages.findIndex(
      (message) => message.client_msg_id === ack.client_msg_id,
    )
    if (index === -1) {
      return
    }

    messages[index] = {
      ...messages[index],
      id: ack.id,
      seq: ack.seq,
      status: 'sent',
    }

    sortMessages(messages)
  }

  function markFailed(clientMsgId: string): void {
    for (const messages of Object.values(byConversation)) {
      const target = messages.find((message) => message.client_msg_id === clientMsgId)
      if (target) {
        target.status = 'failed'
      }
    }
  }

  function retry(clientMsgId: string): MessageSendPayload | null {
    for (const messages of Object.values(byConversation)) {
      const target = messages.find((message) => message.client_msg_id === clientMsgId)
      if (!target) {
        continue
      }

      target.status = 'sending'

      return {
        conversation_id: target.conversation_id,
        client_msg_id: target.client_msg_id,
        content_type: target.content_type,
        content: target.content,
      }
    }

    return null
  }

  function armAck(clientMsgId: string, resend: () => void): void {
    const retryCount = ackRetries.get(clientMsgId) ?? 0

    const timer = window.setTimeout(() => {
      if (retryCount >= MAX_ACK_RETRY) {
        markFailed(clientMsgId)
        ackTimers.delete(clientMsgId)
        ackRetries.delete(clientMsgId)
        return
      }

      ackRetries.set(clientMsgId, retryCount + 1)
      resend()
      armAck(clientMsgId, resend)
    }, ACK_TIMEOUT_MS)

    ackTimers.set(clientMsgId, timer)
  }

  function clearAck(clientMsgId: string): void {
    const timer = ackTimers.get(clientMsgId)
    if (timer !== undefined) {
      window.clearTimeout(timer)
      ackTimers.delete(clientMsgId)
    }

    ackRetries.delete(clientMsgId)
  }

  async function loadLatest(conversationId: number): Promise<void> {
    loading.value = true

    try {
      const { data } = await http.get<ApiResponse<MessagePage>>(
        `/conversations/${conversationId}/messages`,
        { params: { limit: PAGE_SIZE } },
      )

      const incoming = [...data.data.messages]
        .reverse()
        .map<ChatMessage>((message) => ({ ...message, status: 'sent' }))

      const pending = list(conversationId).filter(
        (message) => message.status !== 'sent',
      )

      byConversation[conversationId] = [...incoming, ...pending]
      hasMore[conversationId] = data.data.has_more
      loaded[conversationId] = true
    } finally {
      loading.value = false
    }
  }

  async function loadOlder(conversationId: number): Promise<void> {
    const beforeSeq = firstSeq(conversationId)
    if (beforeSeq === 0) {
      return
    }

    loading.value = true

    try {
      const { data } = await http.get<ApiResponse<MessagePage>>(
        `/conversations/${conversationId}/messages`,
        { params: { before_seq: beforeSeq, limit: PAGE_SIZE } },
      )

      const older = [...data.data.messages]
        .reverse()
        .map<ChatMessage>((message) => ({ ...message, status: 'sent' }))

      const messages =
        byConversation[conversationId] ?? (byConversation[conversationId] = [])
      const existing = new Set(
        messages.filter((item) => item.status === 'sent').map((item) => item.seq),
      )

      byConversation[conversationId] = [
        ...older.filter((message) => !existing.has(message.seq)),
        ...messages,
      ]

      hasMore[conversationId] = data.data.has_more
    } finally {
      loading.value = false
    }
  }

  async function catchUp(conversationId: number): Promise<void> {
    const afterSeq = lastSeq(conversationId)

    if (afterSeq === 0) {
      await loadLatest(conversationId)
      return
    }

    const { data } = await http.get<ApiResponse<MessagePage>>(
      `/conversations/${conversationId}/messages`,
      { params: { after_seq: afterSeq, limit: 100 } },
    )

    for (const message of data.data.messages) {
      upsert(conversationId, message)
    }
  }

  async function ensureLoaded(conversationId: number): Promise<void> {
    if (loaded[conversationId]) {
      return
    }

    await loadLatest(conversationId)
  }

  function reset(): void {
    for (const key of Object.keys(byConversation)) {
      delete byConversation[Number(key)]
    }

    for (const key of Object.keys(hasMore)) {
      delete hasMore[Number(key)]
    }

    for (const key of Object.keys(loaded)) {
      delete loaded[Number(key)]
    }

    for (const timer of ackTimers.values()) {
      window.clearTimeout(timer)
    }

    ackTimers.clear()
    ackRetries.clear()
  }

  return {
    byConversation,
    hasMore,
    loading,
    list,
    lastSeq,
    firstSeq,
    upsert,
    createPending,
    applyAck,
    markFailed,
    retry,
    armAck,
    clearAck,
    loadLatest,
    loadOlder,
    catchUp,
    ensureLoaded,
    reset,
  }
})
