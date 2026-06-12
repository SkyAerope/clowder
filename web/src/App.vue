<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { session, refreshSession, api } from './api'
import {
  LayoutDashboard, KeyRound, ScrollText, AlertTriangle,
  Camera, SlidersHorizontal, Settings, LogOut, LogIn, Cat,
} from 'lucide-vue-next'

const router = useRouter()

onMounted(refreshSession)

async function logout() {
  await api.post('/api/logout')
  await refreshSession()
  router.push('/dashboard')
}

const navItems = [
  { to: '/dashboard', label: '看板', icon: LayoutDashboard, admin: false },
  { to: '/keys', label: 'Key 管理', icon: KeyRound, admin: true },
  { to: '/requests', label: '请求日志', icon: ScrollText, admin: true },
  { to: '/errors', label: '错误聚合', icon: AlertTriangle, admin: true },
  { to: '/captures', label: '抓取记录', icon: Camera, admin: true },
  { to: '/rules', label: '规则', icon: SlidersHorizontal, admin: true },
  { to: '/settings', label: '设置', icon: Settings, admin: true },
]
</script>

<template>
  <div class="flex min-h-screen">
    <aside class="w-52 shrink-0 border-r border-border bg-card flex flex-col">
      <div class="flex items-center gap-2 px-4 py-4 border-b border-border">
        <Cat class="size-6 text-primary" />
        <span class="font-semibold text-lg">clowder</span>
      </div>
      <nav class="flex-1 p-2 space-y-0.5">
        <template v-for="item in navItems" :key="item.to">
          <RouterLink
            v-if="!item.admin || session.admin"
            :to="item.to"
            class="flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted transition-colors"
            active-class="bg-muted font-medium text-primary"
          >
            <component :is="item.icon" class="size-4" />
            {{ item.label }}
          </RouterLink>
        </template>
      </nav>
      <div class="p-2 border-t border-border">
        <button v-if="session.admin" class="btn-ghost w-full justify-start text-muted-foreground" @click="logout">
          <LogOut class="size-4" /> 退出管理
        </button>
        <RouterLink v-else to="/login" class="btn-ghost w-full justify-start text-muted-foreground">
          <LogIn class="size-4" /> 管理登录
        </RouterLink>
      </div>
    </aside>
    <main class="flex-1 min-w-0 p-6">
      <RouterView />
    </main>
  </div>
</template>
