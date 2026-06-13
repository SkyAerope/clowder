import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import './style.css'
import { ensureSession, session } from './api'

import Dashboard from './pages/Dashboard.vue'
import Login from './pages/Login.vue'
import Providers from './pages/Providers.vue'
import ProviderDetail from './pages/ProviderDetail.vue'
import Requests from './pages/Requests.vue'
import Errors from './pages/Errors.vue'
import Captures from './pages/Captures.vue'
import Rules from './pages/Rules.vue'
import Settings from './pages/Settings.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/dashboard' },
    { path: '/dashboard', component: Dashboard },
    { path: '/login', component: Login },
    { path: '/providers', component: Providers, meta: { admin: true } },
    { path: '/providers/:name', component: ProviderDetail, meta: { admin: true } },
    { path: '/requests', component: Requests, meta: { admin: true } },
    { path: '/errors', component: Errors, meta: { admin: true } },
    { path: '/captures', component: Captures, meta: { admin: true } },
    { path: '/rules', component: Rules, meta: { admin: true } },
    { path: '/settings', component: Settings, meta: { admin: true } },
  ],
})

// 未登录访问管理页面时跳转到登录页，并记录原始目标用于登录后回跳。
router.beforeEach(async (to) => {
  if (!to.meta.admin) return true
  await ensureSession()
  if (session.value.admin) return true
  return { path: '/login', query: { redirect: to.fullPath } }
})

createApp(App).use(router).mount('#app')
