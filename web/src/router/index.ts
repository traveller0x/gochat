import { createRouter, createWebHistory } from 'vue-router'

import ChatView from '../views/ChatView.vue'
import LoginView from '../views/LoginView.vue'
import { useAuthStore } from '../stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/chat' },
    { path: '/login', name: 'login', component: LoginView },
    { path: '/chat', name: 'chat', component: ChatView },
  ],
})

// 未登录只能去登录页；已登录不再回登录页。
router.beforeEach((to) => {
  const auth = useAuthStore()

  if (to.name !== 'login' && !auth.isLoggedIn) {
    return { name: 'login' }
  }

  if (to.name === 'login' && auth.isLoggedIn) {
    return { name: 'chat' }
  }

  return true
})

export default router
