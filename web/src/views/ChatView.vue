<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NEmpty } from 'naive-ui'

import { http, type ApiResponse } from '../api/http'
import ConnectionBanner from '../components/ConnectionBanner.vue'
import ConversationList from '../components/ConversationList.vue'
import MessageInput from '../components/MessageInput.vue'
import MessageList from '../components/MessageList.vue'
import { useAuthStore } from '../stores/auth'
import { useConversationStore } from '../stores/conversation'
import { useMessageStore } from '../stores/message'
import { useSocketStore } from '../stores/socket'
import { FrameType } from '../types/protocol'

const router = useRouter()
const auth = useAuthStore()
const conversations = useConversationStore()
const messages = useMessageStore()
const socket = useSocketStore()

const active = computed(() => conversations.active)
const connected = computed(() => socket.state === 'open')
const activeLastSeq = computed(() =>
  conversations.activeId ? messages.lastSeq(conversations.activeId) : 0,
)

/** 上报已读：只在前进时发请求，失败不阻塞聊天。 */
async function markRead(conversationId: number, seq: number): Promise<void> {
  const conv = conversations.conversations.find((item) => item.id === conversationId)
  if (!conv || seq <= conv.last_read_seq) {
    return
  }

  try {
    const { data } = await http.post<ApiResponse<{ last_read_seq: number }>>(
      `/conversations/${conversationId}/read`,
      { last_read_seq: seq },
    )

    conversations.clearUnread(conversationId, data.data.last_read_seq)
  } catch {
    // 下次切换会话或重连时会再次上报。
  }
}

function onSend(content: string): void {
  const conv = active.value
  const user = auth.user

  if (!conv || !user) {
    return
  }

  const payload = messages.createPending(conv.id, user.id, content)
  const resend = (): void => {
    socket.send({ type: FrameType.messageSend, data: payload })
  }

  resend()
  messages.armAck(payload.client_msg_id, resend)
}

function onRetry(clientMsgId: string): void {
  const payload = messages.retry(clientMsgId)
  if (!payload) {
    return
  }

  const resend = (): void => {
    socket.send({ type: FrameType.messageSend, data: payload })
  }

  resend()
  messages.armAck(payload.client_msg_id, resend)
}

function onSelected(conversationId: number): void {
  const seq = messages.lastSeq(conversationId)
  if (seq > 0) {
    void markRead(conversationId, seq)
  }
}

// 重连成功后：刷新会话列表 + 对当前会话补洞。
watch(
  () => socket.state,
  async (state) => {
    if (state !== 'open') {
      return
    }

    await conversations.fetchConversations()

    if (conversations.activeId) {
      await messages.catchUp(conversations.activeId)
    }
  },
)

// 当前会话出现新消息时上报已读（页面可见才报）。
watch([() => conversations.activeId, activeLastSeq], ([conversationId, seq]) => {
  if (!conversationId || !seq || document.visibilityState !== 'visible') {
    return
  }

  void markRead(conversationId, seq)
})

onMounted(async () => {
  if (!auth.user) {
    await auth.fetchMe()
  }

  socket.connect()
  await conversations.fetchConversations()
})

onBeforeUnmount(() => {
  socket.close()
})

function logout(): void {
  socket.close()
  auth.logout()
  messages.reset()
  void router.push({ name: 'login' })
}
</script>

<template>
  <div class="layout">
    <ConversationList @selected="onSelected" />

    <main class="main">
      <ConnectionBanner />

      <template v-if="active">
        <header class="head">
          <span class="title">{{ conversations.displayName(active) }}</span>
          <span class="me">{{ auth.user?.nickname || auth.user?.username }}</span>
          <NButton size="tiny" quaternary @click="logout">退出登录</NButton>
        </header>

        <MessageList :conversation-id="active.id" @retry="onRetry" />
        <MessageInput :disabled="!connected" @send="onSend" />
      </template>

      <NEmpty v-else class="empty" description="选择左侧会话，或搜索用户开始聊天" />
    </main>
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  height: 100%;
}

.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  border-bottom: 1px solid #e8eaed;
  background: #fff;
}

.title {
  font-size: 15px;
  font-weight: 600;
}

.me {
  margin-left: auto;
  font-size: 12px;
  color: #909399;
}

.empty {
  margin-top: 120px;
}
</style>
