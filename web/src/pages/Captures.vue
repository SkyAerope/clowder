<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api, type Capture } from '../api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { RefreshCw, ShieldCheck } from 'lucide-vue-next'

const route = useRoute()
const captures = ref<Capture[]>([])
const loaded = ref(false)
const ruleFilter = ref('')
const fpFilter = ref((route.query.fingerprint as string) || '')
const expanded = ref<number | null>(null)

async function load() {
  const params = new URLSearchParams()
  if (ruleFilter.value) params.set('rule', ruleFilter.value)
  if (fpFilter.value) params.set('fingerprint', fpFilter.value)
  const q = params.toString() ? `?${params}` : ''
  try {
    captures.value = (await api.get<Capture[]>(`/api/admin/captures${q}`)) ?? []
  } finally {
    loaded.value = true
  }
}

function pretty(s?: string) {
  if (!s) return ''
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between flex-wrap gap-2">
      <h1 class="text-xl font-semibold">抓取记录</h1>
      <div class="flex gap-2">
        <Input v-model="ruleFilter" class="w-44" placeholder="按规则过滤…" @keyup.enter="load" />
        <Input v-model="fpFilter" class="w-44" placeholder="按指纹过滤…" @keyup.enter="load" />
        <Button variant="outline" size="icon" @click="load"><RefreshCw class="size-4" /></Button>
      </div>
    </div>

    <div class="space-y-2">
      <template v-if="!loaded">
        <Card v-for="i in 5" :key="i" class="py-0 gap-0">
          <div class="flex items-center gap-3 p-3">
            <Skeleton class="h-4 w-8" />
            <Skeleton class="h-4 w-24" />
            <Skeleton class="h-5 w-20 rounded-full" />
            <Skeleton class="ml-auto h-4 w-32" />
          </div>
        </Card>
      </template>
      <template v-else>
        <Card v-for="c in captures" :key="c.id" class="py-0 gap-0">
        <button class="w-full flex cursor-pointer items-center gap-3 p-3 text-sm text-left hover:bg-muted/50"
          @click="expanded = expanded === c.id ? null : c.id">
          <span class="text-muted-foreground">#{{ c.id }}</span>
          <span class="font-medium">{{ c.provider }}</span>
          <Badge variant="secondary">{{ c.matched_slug }}</Badge>
          <span v-if="c.fingerprint" class="font-mono text-xs text-muted-foreground">{{ c.fingerprint.slice(0, 12) }}</span>
          <ShieldCheck v-if="c.redacted" class="size-4 text-success" title="已脱敏" />
          <span class="ml-auto text-xs text-muted-foreground whitespace-nowrap">
            {{ new Date(c.created_at + 'Z').toLocaleString() }}
          </span>
        </button>
        <div v-if="expanded === c.id" class="border-t border-border p-4 grid grid-cols-1 lg:grid-cols-2 gap-4 text-xs">
          <div v-if="c.request_headers">
            <div class="font-medium mb-1">请求头</div>
            <pre class="bg-muted p-3 overflow-x-auto max-h-60">{{ pretty(c.request_headers) }}</pre>
          </div>
          <div v-if="c.request_body">
            <div class="font-medium mb-1">请求体</div>
            <pre class="bg-muted p-3 overflow-x-auto max-h-60">{{ pretty(c.request_body) }}</pre>
          </div>
          <div v-if="c.response_headers">
            <div class="font-medium mb-1">响应头</div>
            <pre class="bg-muted p-3 overflow-x-auto max-h-60">{{ pretty(c.response_headers) }}</pre>
          </div>
          <div v-if="c.response_body">
            <div class="font-medium mb-1">响应体</div>
            <pre class="bg-muted p-3 overflow-x-auto max-h-60">{{ pretty(c.response_body) }}</pre>
          </div>
        </div>
      </Card>
        <Card v-if="!captures.length" class="p-6 text-center text-sm text-muted-foreground">暂无抓取记录</Card>
      </template>
    </div>
  </div>
</template>
