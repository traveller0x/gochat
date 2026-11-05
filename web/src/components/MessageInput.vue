<script setup lang="ts">
import { ref } from 'vue'
import { NButton, NInput } from 'naive-ui'

const props = defineProps<{
  disabled: boolean
}>()

const emit = defineEmits<{
  send: [content: string]
}>()

const text = ref('')

function submit(): void {
  const content = text.value.trim()
  if (!content || props.disabled) {
    return
  }

  emit('send', content)
  text.value = ''
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault()
    submit()
  }
}
</script>

<template>
  <div class="input-bar">
    <NInput
      v-model:value="text"
      type="textarea"
      :autosize="{ minRows: 1, maxRows: 4 }"
      :maxlength="4096"
      placeholder="输入消息，Enter 发送，Shift + Enter 换行"
      @keydown="onKeydown"
    />

    <NButton type="primary" :disabled="disabled || !text.trim()" @click="submit">
      发送
    </NButton>
  </div>
</template>

<style scoped>
.input-bar {
  display: flex;
  gap: 8px;
  align-items: flex-end;
  padding: 12px 16px;
  border-top: 1px solid #e8eaed;
  background: #fff;
}
</style>
