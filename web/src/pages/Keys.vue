<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { api, type KeyRow, type Provider } from '../api'
import StatusBadge from '../components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { RefreshCw, Plus, Stethoscope, Ban, CircleCheck } from 'lucide-vue-next'

const keys = ref<KeyRow[]>([])
const providers = ref<Provider[]>([])
const filter = ref('all')
const busy = ref<Record<number, boolean>>({})
const registering = ref(false)
const msg = ref('')

const filtered = computed(() =>
  filter.value !== 'all' ? keys.value.filter(k => k.provider === filter.value) : keys.value
)

async function load() {
  const [k, p] = await Promise.all([
    api.get<KeyRow[]>('/api/admin/keys'),
    api.get<Provider[]>('/api/providers'),
  ])
  keys.value = k ?? []
  providers.value = p ?? []
}

async function action(k: KeyRow, act: 'disable' | 'enable' | 'check') {
  busy.value[k.id] = true
  msg.value = ''
  try {
    await api.post(`/api/admin/keys/${k.id}/${act}`)
    await load()
  } catch (e: any) {
    msg.value = `key #${k.id} ${act} 失败: ${e.message}`
  } finally {
    busy.value[k.id] = false
  }
}

async function register() {
  if (filter.value === 'all') {
    msg.value = '请先在筛选框选择 provider'
    return
  }
  registering.value = true
  msg.value = ''
  try {
    const r = await api.post<{ registered: number; error?: string }>('/api/admin/register', {
      provider: filter.value, count: 1,
    })
    msg.value = r.error ? `注册失败: ${r.error}` : `已注册 ${r.registered} 个 key`
    await load()
  } catch (e: any) {
    msg.value = `注册失败: ${e.message}`
  } finally {
    registering.value = false
  }
}

function fmtTime(s?: string) {
  return s ? new Date(s).toLocaleString() : '—'
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between flex-wrap gap-2">
      <h1 class="text-xl font-semibold">Key 管理</h1>
      <div class="flex items-center gap-2">
        <Select v-model="filter">
          <SelectTrigger class="w-44"><SelectValue placeholder="全部 provider" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部 provider</SelectItem>
            <SelectItem v-for="p in providers" :key="p.name" :value="p.name">{{ p.name }}</SelectItem>
          </SelectContent>
        </Select>
        <Button :disabled="registering || filter === 'all'" @click="register">
          <Plus class="size-4" :class="registering && 'animate-pulse'" /> 注册新 Key
        </Button>
        <Button variant="outline" size="icon" @click="load"><RefreshCw class="size-4" /></Button>
      </div>
    </div>

    <Alert v-if="msg" :variant="msg.includes('失败') ? 'destructive' : 'default'">
      <AlertDescription>{{ msg }}</AlertDescription>
    </Alert>

    <div class="rounded-none border bg-card overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead><TableHead>Provider</TableHead><TableHead>Key</TableHead>
            <TableHead>Email</TableHead><TableHead>状态</TableHead><TableHead>限流次数</TableHead>
            <TableHead>限流至</TableHead><TableHead>最后使用</TableHead><TableHead>创建时间</TableHead>
            <TableHead class="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="k in filtered" :key="k.id">
            <TableCell class="text-muted-foreground">#{{ k.id }}</TableCell>
            <TableCell>{{ k.provider }}</TableCell>
            <TableCell class="font-mono text-xs break-all max-w-64">{{ k.api_key }}</TableCell>
            <TableCell class="font-mono text-xs">{{ k.email || '—' }}</TableCell>
            <TableCell><StatusBadge :status="k.status" /></TableCell>
            <TableCell>{{ k.rate_limit_count }}</TableCell>
            <TableCell class="text-xs">{{ fmtTime(k.rate_limit_until) }}</TableCell>
            <TableCell class="text-xs">{{ fmtTime(k.last_used_at) }}</TableCell>
            <TableCell class="text-xs">{{ fmtTime(k.created_at) }}</TableCell>
            <TableCell>
              <div class="flex justify-end gap-1">
                <Button variant="ghost" size="icon-sm" title="一键 check" :disabled="busy[k.id]" @click="action(k, 'check')">
                  <Stethoscope class="size-4" :class="busy[k.id] && 'animate-pulse'" />
                </Button>
                <Button v-if="k.status !== 'invalid'" variant="ghost" size="icon-sm" class="text-destructive" title="禁用"
                  :disabled="busy[k.id]" @click="action(k, 'disable')">
                  <Ban class="size-4" />
                </Button>
                <Button v-else variant="ghost" size="icon-sm" class="text-success" title="启用"
                  :disabled="busy[k.id]" @click="action(k, 'enable')">
                  <CircleCheck class="size-4" />
                </Button>
              </div>
            </TableCell>
          </TableRow>
          <TableEmpty v-if="!filtered.length" :colspan="10">暂无 key</TableEmpty>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
