<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, refreshSession } from '../api'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Cat } from '@lucide/vue'

const router = useRouter()
const route = useRoute()
const adminKey = ref('')
const error = ref('')
const loading = ref(false)

async function login() {
  loading.value = true
  error.value = ''
  try {
    await api.post('/api/login', { admin_key: adminKey.value })
    await refreshSession()
    const redirect = (route.query.redirect as string) || '/dashboard'
    router.push(redirect)
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex items-center justify-center min-h-[70vh]">
    <Card class="p-8 w-full max-w-sm">
      <form class="space-y-4" @submit.prevent="login">
        <div class="flex items-center justify-center gap-2 mb-2">
          <Cat class="size-8 text-primary" />
          <span class="text-xl font-semibold">clowder 管理</span>
        </div>
        <Input v-model="adminKey" type="password" placeholder="admin key" autofocus />
        <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
        <Button type="submit" class="w-full" :disabled="loading || !adminKey">登录</Button>
        <p class="text-xs text-muted-foreground text-center">admin key 位于 ~/.clowder/config.yaml</p>
      </form>
    </Card>
  </div>
</template>
