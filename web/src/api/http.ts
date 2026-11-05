import axios, { type AxiosError } from 'axios'

/** 后端统一响应结构：{ code, message, data } */
export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

export const TOKEN_KEY = 'gochat.token'

export const http = axios.create({
  baseURL: '/api/v1',
  timeout: 10_000,
})

// 请求拦截：自动带上 JWT。
http.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers.set('Authorization', `Bearer ${token}`)
  }

  return config
})

// 响应拦截：401 时清理登录态并回到登录页。
http.interceptors.response.use(
  (response) => response,
  (error: AxiosError) => {
    if (error.response?.status === 401) {
      localStorage.removeItem(TOKEN_KEY)

      if (window.location.pathname !== '/login') {
        window.location.assign('/login')
      }
    }

    return Promise.reject(error)
  },
)

/** 从后端错误响应里取出业务错误码。 */
export function apiErrorCode(error: unknown): number | undefined {
  if (!axios.isAxiosError(error)) {
    return undefined
  }

  const data = error.response?.data as { code?: number } | undefined

  return data?.code
}

/** 从后端错误响应里取出可读的错误信息。 */
export function apiErrorMessage(error: unknown): string {
  if (axios.isAxiosError(error)) {
    const data = error.response?.data as { message?: string } | undefined

    return data?.message ?? error.message
  }

  return error instanceof Error ? error.message : 'unknown error'
}
