<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NForm, NFormItem, NInput, useMessage } from 'naive-ui'

import { apiErrorMessage } from '../api/http'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()
const toast = useMessage()

const username = ref('')
const password = ref('')
const nickname = ref('')
const registerMode = ref(false)
const submitting = ref(false)

async function submit(): Promise<void> {
  const name = username.value.trim()

  if (!name || !password.value) {
    toast.warning('请输入用户名和密码')
    return
  }

  submitting.value = true

  try {
    if (registerMode.value) {
      await auth.register(name, password.value, nickname.value.trim())
    }

    await auth.login(name, password.value)
    await router.push({ name: 'chat' })
  } catch (error) {
    toast.error(apiErrorMessage(error))
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="page">
    <div class="card">
      <h1>GoChat</h1>
      <p class="sub">{{ registerMode ? '注册新账号' : '登录后开始聊天' }}</p>

      <NForm @submit.prevent="submit">
        <NFormItem label="用户名">
          <NInput v-model:value="username" placeholder="3~32 位字母、数字或下划线" />
        </NFormItem>

        <NFormItem label="密码">
          <NInput
            v-model:value="password"
            type="password"
            show-password-on="click"
            placeholder="至少 8 位"
            @keyup.enter="submit"
          />
        </NFormItem>

        <NFormItem v-if="registerMode" label="昵称">
          <NInput v-model:value="nickname" placeholder="可选" />
        </NFormItem>

        <NButton type="primary" block :loading="submitting" @click="submit">
          {{ registerMode ? '注册并登录' : '登录' }}
        </NButton>
      </NForm>

      <button class="switch" type="button" @click="registerMode = !registerMode">
        {{ registerMode ? '已有账号？去登录' : '没有账号？去注册' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
}

.card {
  width: 360px;
  padding: 32px;
  border-radius: 12px;
  background: #fff;
  box-shadow: 0 8px 24px rgba(31, 34, 37, 0.08);
}

h1 {
  margin: 0;
  font-size: 24px;
}

.sub {
  margin: 6px 0 20px;
  font-size: 13px;
  color: #909399;
}

.switch {
  width: 100%;
  margin-top: 12px;
  border: none;
  background: none;
  color: #3a6ea5;
  cursor: pointer;
  font-size: 13px;
}
</style>
