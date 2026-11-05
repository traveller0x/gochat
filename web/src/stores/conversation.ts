import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { http, type ApiResponse } from '../api/http'
import type { ConversationSummary, MessagePayload, UserBrief } from '../types/protocol'

export const useConversationStore = defineStore('conversation', () => {
  const conversations = ref<ConversationSummary[]>([])
  const activeId = ref(0)
  const loading = ref(false)

  const active = computed(
    () => conversations.value.find((item) => item.id === activeId.value) ?? null,
  )

  /** 单聊显示对方昵称，群聊显示群名。 */
  function displayName(item: ConversationSummary): string {
    if (item.type === 1) {
      return item.peer?.nickname || item.peer?.username || `会话 #${item.id}`
    }

    return item.name || `群聊 #${item.id}`
  }

  async function fetchConversations(): Promise<void> {
    loading.value = true

    try {
      const { data } = await http.get<
        ApiResponse<{ conversations: ConversationSummary[] }>
      >('/conversations')

      conversations.value = data.data.conversations
    } finally {
      loading.value = false
    }
  }

  async function searchUsers(keyword: string): Promise<UserBrief[]> {
    const { data } = await http.get<ApiResponse<{ users: UserBrief[] }>>(
      '/users/search',
      { params: { keyword } },
    )

    return data.data.users
  }

  /** 创建或获取与某人的单聊，并切换到该会话。 */
  async function openWithPeer(peerUserId: number): Promise<ConversationSummary> {
    const { data } = await http.post<
      ApiResponse<{ conversation: ConversationSummary }>
    >('/conversations', { peer_user_id: peerUserId })

    const conv = data.data.conversation

    if (!conversations.value.some((item) => item.id === conv.id)) {
      conversations.value.unshift({
        ...conv,
        unread_count: 0,
        last_read_seq: 0,
      })
    }

    activeId.value = conv.id
    await fetchConversations()

    return conv
  }

  function setActive(id: number): void {
    activeId.value = id

    const conv = conversations.value.find((item) => item.id === id)
    if (conv) {
      conv.unread_count = 0
    }
  }

  /** 收到新消息时更新会话摘要并置顶。 */
  function touchLastMessage(payload: MessagePayload): void {
    const conv = conversations.value.find((item) => item.id === payload.conversation_id)
    if (!conv) {
      return
    }

    conv.last_message = payload

    const index = conversations.value.indexOf(conv)
    if (index > 0) {
      conversations.value.splice(index, 1)
      conversations.value.unshift(conv)
    }
  }

  function incrementUnread(conversationId: number): void {
    const conv = conversations.value.find((item) => item.id === conversationId)
    if (conv) {
      conv.unread_count += 1
    }
  }

  function clearUnread(conversationId: number, lastReadSeq: number): void {
    const conv = conversations.value.find((item) => item.id === conversationId)
    if (!conv) {
      return
    }

    conv.unread_count = 0

    if (lastReadSeq > conv.last_read_seq) {
      conv.last_read_seq = lastReadSeq
    }
  }

  return {
    conversations,
    activeId,
    active,
    loading,
    displayName,
    fetchConversations,
    searchUsers,
    openWithPeer,
    setActive,
    touchLastMessage,
    incrementUnread,
    clearUnread,
  }
})
