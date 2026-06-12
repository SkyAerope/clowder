<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, refreshSession } from '../api'
import { Cat } from 'lucide-vue-next'

const router = useRouter()
const adminKey = ref('')
const error = ref('')
const loading = ref(false)

async function login() {
  loading.value = true
  error.value = ''
  try {
    await api.post('/api/login', { admin_key: adminKey.value })
    await refreshSession()
    router.push('/dashboard')
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex items-center justify-center min-h-[70vh]">
    <form class="card p-8 w-full max-w-sm space-y-4" @submit.prevent="login">
      <div class="flex items-center justify-center gap-2 mb-2">
        <Cat class="size-8 text-primary" />
        <span class="text-xl font-semibold">clowder 管理</span>
      </div>
      <input v-model="adminKey" type="password" class="input" placeholder="admin key" autofocus />
      <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
      <button class="btn-primary w-full justify-center" :disabled="loading || !adminKey">登录</button>
      <p class="text-xs text-muted-foreground text-center">admin key 位于 ~/.clowder/config.yaml</p>
    </form>
  </div>
</template>
