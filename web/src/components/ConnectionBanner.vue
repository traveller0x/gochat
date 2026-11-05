<script setup lang="ts">
import { computed } from 'vue'

import { useSocketStore } from '../stores/socket'

const socket = useSocketStore()

const text = computed(() => {
  switch (socket.state) {
    case 'open':
      return '已连接'
    case 'connecting':
      return '连接中…'
    case 'reconnecting':
      return '连接已断开，正在重连…'
    case 'closed':
      return '已断开'
    default:
      return '未连接'
  }
})

const tone = computed(() => {
  if (socket.state === 'open') {
    return 'ok'
  }

  return socket.state === 'reconnecting' ? 'warn' : 'muted'
})
</script>

<template>
  <div class="banner" :class="tone">
    <span class="dot" />
    <span>{{ text }}</span>
    <span v-if="socket.lastError" class="error">{{ socket.lastError }}</span>
  </div>
</template>

<style scoped>
.banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 16px;
  font-size: 12px;
  border-bottom: 1px solid #e8eaed;
}

.banner.ok {
  background: #f0f9eb;
  color: #4b8b3b;
}

.banner.warn {
  background: #fdf6ec;
  color: #b88230;
}

.banner.muted {
  background: #f4f4f5;
  color: #909399;
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.error {
  margin-left: auto;
  opacity: 0.8;
}
</style>
