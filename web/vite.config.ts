import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// 开发期通过 Vite 代理访问后端，避免 CORS 与 cookie 问题。
// ws: true 是 WebSocket 必需项，否则 /api/v1/ws 会被当成普通 HTTP 请求。
export default defineConfig({
  plugins: [vue()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
