<script setup lang="ts">
import dayjs from 'dayjs'

import type { ChatMessage } from '../types/protocol'

const props = defineProps<{
  message: ChatMessage
  own: boolean
}>()

const emit = defineEmits<{
  retry: [clientMsgId: string]
}>()

function onRetry(): void {
  if (props.message.status === 'failed') {
    emit('retry', props.message.client_msg_id)
  }
}

function statusText(): string {
  switch (props.message.status) {
    case 'sending':
      return '发送中'
    case 'failed':
      return '发送失败，点击重试'
    default:
      return dayjs(props.message.created_at).format('HH:mm')
  }
}
</script>

<template>
  <div class="row" :class="{ own }">
    <div class="bubble" :class="message.status">
      <p class="content">{{ message.content }}</p>

      <span class="meta" :class="{ clickable: message.status === 'failed' }" @click="onRetry">
        {{ statusText() }}
      </span>
    </div>
  </div>
</template>

<style scoped>
.row {
  display: flex;
  margin-bottom: 12px;
}

.row.own {
  justify-content: flex-end;
}

.bubble {
  max-width: 68%;
  padding: 8px 12px;
  border-radius: 10px;
  background: #fff;
  box-shadow: 0 1px 2px rgba(31, 34, 37, 0.08);
}

.own .bubble {
  background: #d7eaff;
}

.bubble.sending {
  opacity: 0.65;
}

.bubble.failed {
  background: #ffe2e2;
}

.content {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.5;
}

.meta {
  display: block;
  margin-top: 4px;
  font-size: 11px;
  color: #909399;
  text-align: right;
}

.clickable {
  color: #d03050;
  cursor: pointer;
}
</style>
