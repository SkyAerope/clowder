<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { session, refreshSession, ensureSession, api } from './api'
import {
  LayoutDashboard, Boxes, ScrollText, AlertTriangle,
  Camera, SlidersHorizontal, Settings, LogOut, LogIn, Cat, Menu,
} from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Sheet, SheetContent, SheetTrigger, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'

const router = useRouter()
const mobileOpen = ref(false)

onMounted(ensureSession)

async function logout() {
  await api.post('/api/logout')
  await refreshSession()
  router.push('/dashboard')
}

const navItems = [
  { to: '/dashboard', label: '看板', icon: LayoutDashboard, admin: false },
  { to: '/providers', label: '提供商管理', icon: Boxes, admin: true },
  { to: '/requests', label: '请求日志', icon: ScrollText, admin: true },
  { to: '/errors', label: '错误聚合', icon: AlertTriangle, admin: true },
  { to: '/captures', label: '抓取记录', icon: Camera, admin: true },
  { to: '/rules', label: '规则', icon: SlidersHorizontal, admin: true },
  { to: '/settings', label: '设置', icon: Settings, admin: true },
]
</script>

<template>
  <div class="flex min-h-screen flex-col">
    <header class="sticky top-0 z-30 border-b border-border bg-card/80 backdrop-blur">
      <div class="flex h-14 items-center gap-2 px-4">
        <!-- 移动端：汉堡 + 可展开左栏 -->
        <Sheet v-model:open="mobileOpen">
          <SheetTrigger as-child>
            <Button variant="ghost" size="icon" class="md:hidden">
              <Menu class="size-5" />
            </Button>
          </SheetTrigger>
          <SheetContent side="left" class="w-64 p-0">
            <SheetHeader class="border-b border-border">
              <SheetTitle class="flex items-center gap-2">
                <Cat class="size-5 text-primary" /> clowder
              </SheetTitle>
            </SheetHeader>
            <nav class="flex flex-col gap-0.5 p-2">
              <template v-for="item in navItems" :key="item.to">
                <RouterLink
                  v-if="!item.admin || session.admin"
                  :to="item.to"
                  class="flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted transition-colors"
                  active-class="bg-muted font-medium text-primary"
                  @click="mobileOpen = false"
                >
                  <component :is="item.icon" class="size-4" />
                  {{ item.label }}
                </RouterLink>
              </template>
              <Separator class="my-2" />
              <button v-if="session.admin"
                class="flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-muted transition-colors"
                @click="logout(); mobileOpen = false">
                <LogOut class="size-4" /> 退出管理
              </button>
              <RouterLink v-else to="/login"
                class="flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-muted transition-colors"
                @click="mobileOpen = false">
                <LogIn class="size-4" /> 管理登录
              </RouterLink>
            </nav>
          </SheetContent>
        </Sheet>

        <RouterLink to="/dashboard" class="flex items-center gap-2 font-semibold text-lg">
          <Cat class="size-6 text-primary" />
          <span>clowder</span>
        </RouterLink>

        <!-- 桌面端顶栏导航 -->
        <nav class="ml-4 hidden items-center gap-0.5 md:flex">
          <template v-for="item in navItems" :key="item.to">
            <RouterLink
              v-if="!item.admin || session.admin"
              :to="item.to"
              class="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm hover:bg-muted transition-colors"
              active-class="bg-muted font-medium text-primary"
            >
              <component :is="item.icon" class="size-4" />
              {{ item.label }}
            </RouterLink>
          </template>
        </nav>

        <div class="ml-auto">
          <Button v-if="session.admin" variant="ghost" size="sm" class="text-muted-foreground" @click="logout">
            <LogOut class="size-4" /> 退出管理
          </Button>
          <Button v-else as-child variant="ghost" size="sm" class="text-muted-foreground">
            <RouterLink to="/login"><LogIn class="size-4" /> 管理登录</RouterLink>
          </Button>
        </div>
      </div>
    </header>

    <main class="mx-auto w-full max-w-7xl flex-1 p-4 md:p-6">
      <TooltipProvider :delay-duration="400">
        <RouterView />
      </TooltipProvider>
    </main>

    <Toaster rich-colors close-button />
  </div>
</template>
