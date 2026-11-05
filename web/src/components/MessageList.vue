<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { NButton } from 'naive-ui'

import { useAuthStore } from '../stores/auth'
import { useMessageStore } from '../stores/message'
import MessageBubble from './MessageBubble.vue'

const props = defineProps<{
  conversationId: number
}>()

const emit = defineEmits<{
  retry: [clientMsgId: string]
}>()

const auth = useAuthStore()
const store = useMessageStore()
const container = ref<HTMLDivElement | null>(null)

const messages = computed(() => store.list(props.conversationId))
const canLoadOlder = computed(() => store.hasMore[props.conversationId] === true)

function scrollToBottom(): void {
  nextTick(() => {
    const el = container.value
    if (el) {
      el.scrollTop = el.scrollHeight
    }
  })
}

function onRetry(clientMsgId: string): void {
  emit('retry', clientMsgId)
}

watch(
  () => props.conversationId,
  async () => {
    await store.ensureLoaded(props.conversationId)
    scrollToBottom()
  },
  { immediate: true },
)

watch(
  () => messages.value.length,
  (next, prev) => {
    if (next > prev) {
      scrollToBottom()
    }
  },
)

async function loadOlder(): Promise<void> {
  const el = container.value
  const previousHeight = el?.scrollHeight ?? 0
  const previousTop = el?.scrollTop ?? 0

  await store.loadOlder(props.conversationId)

  await nextTick()

  if (el) {
    el.scrollTop = previousTop + (el.scrollHeight - previousHeight)
  }
}
</script>

<template>
  <div ref="container" class="messages">
    <div v-if="canLoadOlder" class="load-more">
      <NButton size="tiny" :loading="store.loading" @click="loadOlder">
        加载更早的消息
      </NButton>
    </div>

    <MessageBubble
      v-for="message in messages"
      :key="message.client_msg_id || String(message.id)"
      :message="message"
      :own="message.sender_id === auth.user?.id"
      @retry="onRetry"
    />

    <div v-if="!messages.length" class="empty">还没有消息，发第一条吧</div>
  </div>
</template>

<style scoped>
.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  background: #f5f6f8;
}

.load-more {
  display: flex;
  justify-content: center;
  margin-bottom: 12px;
}

.empty {
  margin-top: 48px;
  text-align: center;
  color: #a0a4a8;
  font-size: 13px;
}
</style>
