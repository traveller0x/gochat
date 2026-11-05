import { computed, ref } from 'vue'
import { defineStore } from 'pinia'

import { TOKEN_KEY, http, type ApiResponse } from '../api/http'

export interface AuthUser {
  id: number
  username: string
  nickname: string
  avatar?: string
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem(TOKEN_KEY) ?? '')
  const user = ref<AuthUser | null>(null)

  const isLoggedIn = computed(() => token.value !== '')

  function setToken(value: string): void {
    token.value = value

    if (value) {
      localStorage.setItem(TOKEN_KEY, value)
    } else {
      localStorage.removeItem(TOKEN_KEY)
    }
  }

  async function login(username: string, password: string): Promise<void> {
    const { data } = await http.post<
      ApiResponse<{ access_token: string; user: AuthUser }>
    >('/auth/login', { username, password })

    setToken(data.data.access_token)
    user.value = data.data.user
  }

  async function register(
    username: string,
    password: string,
    nickname: string,
  ): Promise<void> {
    await http.post('/auth/register', { username, password, nickname })
  }

  async function fetchMe(): Promise<void> {
    if (!token.value) {
      return
    }

    const { data } = await http.get<ApiResponse<{ user: AuthUser }>>('/users/me')
    user.value = data.data.user
  }

  function logout(): void {
    setToken('')
    user.value = null
  }

  return { token, user, isLoggedIn, login, register, fetchMe, logout }
})
