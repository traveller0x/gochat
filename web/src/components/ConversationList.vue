<script setup lang="ts">
import { ref } from 'vue'
import dayjs from 'dayjs'
import { NBadge, NInput } from 'naive-ui'

import { useConversationStore } from '../stores/conversation'
import type { UserBrief } from '../types/protocol'

const emit = defineEmits<{
  selected: [conversationId: number]
}>()

const store = useConversationStore()

const keyword = ref('')
const results = ref<UserBrief[]>([])
const searching = ref(false)

async function search(): Promise<void> {
  const value = keyword.value.trim()
  if (!value) {
    results.value = []
    return
  }

  searching.value = true

  try {
    results.value = await store.searchUsers(value)
  } finally {
    searching.value = false
  }
}

async function startChat(user: UserBrief): Promise<void> {
  const conv = await store.openWithPeer(user.id)

  keyword.value = ''
  results.value = []

  emit('selected', conv.id)
}

function select(id: number): void {
  store.setActive(id)
  emit('selected', id)
}
</script>

<template>
  <div class="panel">
    <div class="search">
      <NInput
        v-model:value="keyword"
        size="small"
        placeholder="搜索用户，开始聊天"
        clearable
        @keyup.enter="search"
      />
    </div>

    <div v-if="searching" class="hint">搜索中…</div>

    <div v-if="results.length" class="results">
      <div
        v-for="user in results"
        :key="user.id"
        class="result"
        @click="startChat(user)"
      >
        <span class="name">{{ user.nickname || user.username }}</span>
        <span class="username">@{{ user.username }}</span>
      </div>
    </div>

    <div class="items">
      <div
        v-for="conv in store.conversations"
        :key="conv.id"
        class="item"
        :class="{ active: conv.id === store.activeId }"
        @click="select(conv.id)"
      >
        <div class="avatar">{{ store.displayName(conv).slice(0, 1) }}</div>

        <div class="info">
          <div class="line">
            <span class="name">{{ store.displayName(conv) }}</span>
            <span v-if="conv.last_message" class="time">
              {{ dayjs(conv.last_message.created_at).format('HH:mm') }}
            </span>
          </div>

          <div class="line">
            <span class="preview">{{ conv.last_message?.content || '暂无消息' }}</span>
            <NBadge v-if="conv.unread_count > 0" :value="conv.unread_count" :max="99" />
          </div>
        </div>
      </div>

      <div v-if="!store.conversations.length" class="hint">还没有会话</div>
    </div>
  </div>
</template>

<style scoped>
.panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  width: 280px;
  border-right: 1px solid #e8eaed;
  background: #fff;
}

.search {
  padding: 12px;
  border-bottom: 1px solid #f0f1f2;
}

.results {
  border-bottom: 1px solid #f0f1f2;
  max-height: 200px;
  overflow-y: auto;
}

.result {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 8px 12px;
  cursor: pointer;
}

.result:hover {
  background: #f5f7fa;
}

.result .username {
  font-size: 12px;
  color: #909399;
}

.items {
  flex: 1;
  overflow-y: auto;
}

.item {
  display: flex;
  gap: 10px;
  padding: 10px 12px;
  cursor: pointer;
  border-bottom: 1px solid #f7f8f9;
}

.item:hover {
  background: #f5f7fa;
}

.item.active {
  background: #ecf5ff;
}

.avatar {
  width: 36px;
  height: 36px;
  flex: none;
  border-radius: 50%;
  background: #d9e7ff;
  color: #3a6ea5;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
}

.info {
  flex: 1;
  min-width: 0;
}

.line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.name {
  font-size: 14px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.time {
  font-size: 11px;
  color: #a0a4a8;
  flex: none;
}

.preview {
  font-size: 12px;
  color: #8a8f94;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.hint {
  padding: 16px;
  font-size: 12px;
  color: #a0a4a8;
  text-align: center;
}
</style>
