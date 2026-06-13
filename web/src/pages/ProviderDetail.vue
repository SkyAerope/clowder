<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api, type KeyRow } from '../api'
import Card from '../components/Card.vue'
import StatusBadge from '../components/StatusBadge.vue'
import CopyButton from '../components/CopyButton.vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { RefreshCw, Plus, Stethoscope, Ban, CircleCheck, ArrowLeft } from 'lucide-vue-next'
import { RouterLink } from 'vue-router'

const route = useRoute()
const name = route.params.name as string

interface Detail {
  manifest: Record<string, any>
  models: string[]
  models_fetched_at?: string
  keys: KeyRow[]
  last_register_at?: string
  last_register_duration: number
  last_register_error?: string
}

const detail = ref<Detail | null>(null)
const msg = ref('')
const registering = ref(false)
const refreshing = ref(false)
const checking = ref(false)
const busy = ref<Record<number, boolean>>({})

async function load() {
  detail.value = await api.get<Detail>(`/api/admin/providers/${name}`)
}

async function register() {
  registering.value = true
  msg.value = ''
  try {
    const r = await api.post<{ registered: number; error?: string }>('/api/admin/register', { provider: name, count: 1 })
    msg.value = r.error ? `注册失败: ${r.error}` : `已注册 ${r.registered} 个 key`
    await load()
  } catch (e: any) {
    msg.value = `注册失败: ${e.message}`
  } finally {
    registering.value = false
  }
}

async function refreshModels() {
  refreshing.value = true
  msg.value = ''
  try {
    await api.post(`/api/admin/providers/${name}/refresh-models`)
    msg.value = '模型已刷新'
    await load()
  } catch (e: any) {
    msg.value = `刷新失败: ${e.message}`
  } finally {
    refreshing.value = false
  }
}

async function keyAction(k: KeyRow, act: 'disable' | 'enable' | 'check') {
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

async function checkAll() {
  checking.value = true
  msg.value = ''
  let ok = 0, fail = 0
  for (const k of detail.value?.keys ?? []) {
    try {
      await api.post(`/api/admin/keys/${k.id}/check`)
      ok++
    } catch {
      fail++
    }
  }
  msg.value = `check 完成：成功 ${ok}，失败 ${fail}`
  checking.value = false
  await load()
}

function fmtTime(s?: string) {
  return s ? new Date(s).toLocaleString() : '—'
}

onMounted(load)
</script>

<template>
  <div v-if="detail" class="space-y-4">
    <div class="flex items-center justify-between flex-wrap gap-2">
      <div class="flex items-center gap-2">
        <Button as-child variant="ghost" size="icon-sm" title="返回提供商列表">
          <RouterLink to="/providers"><ArrowLeft class="size-4" /></RouterLink>
        </Button>
        <h1 class="text-xl font-semibold">{{ detail.manifest.display_name || name }}</h1>
      </div>
      <div class="flex gap-2">
        <Button v-if="detail.manifest.has_check" variant="outline" size="sm" :disabled="checking" @click="checkAll">
          <Stethoscope class="size-4" :class="checking && 'animate-pulse'" /> 全部 check
        </Button>
        <Button variant="outline" size="sm" :disabled="refreshing" @click="refreshModels">
          <RefreshCw class="size-4" :class="refreshing && 'animate-spin'" /> 刷新模型
        </Button>
        <Button v-if="detail.manifest.key_source === 'script'" size="sm" :disabled="registering" @click="register">
          <Plus class="size-4" :class="registering && 'animate-pulse'" /> 注册新 Key
        </Button>
      </div>
    </div>

    <Alert v-if="msg" :variant="msg.includes('失败') ? 'destructive' : 'default'">
      <AlertDescription>{{ msg }}</AlertDescription>
    </Alert>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <Card title="Manifest">
        <dl class="grid grid-cols-2 gap-y-2 text-sm">
          <dt class="text-muted-foreground">base_url</dt><dd class="font-mono text-xs break-all">{{ detail.manifest.base_url }}</dd>
          <dt class="text-muted-foreground">key_source</dt><dd>{{ detail.manifest.key_source }}</dd>
          <dt class="text-muted-foreground">key_strategy</dt><dd>{{ detail.manifest.key_strategy }}</dd>
          <dt class="text-muted-foreground">priority / weight</dt><dd>{{ detail.manifest.priority }} / {{ detail.manifest.weight }}</dd>
          <dt class="text-muted-foreground">enabled</dt><dd>{{ detail.manifest.enabled }}</dd>
          <dt class="text-muted-foreground">check 脚本</dt><dd>{{ detail.manifest.has_check ? '有' : '无' }}</dd>
        </dl>
      </Card>

      <Card title="注册状态">
        <dl class="grid grid-cols-2 gap-y-2 text-sm">
          <dt class="text-muted-foreground">上次注册</dt><dd>{{ fmtTime(detail.last_register_at) }}</dd>
          <dt class="text-muted-foreground">耗时</dt><dd>{{ detail.last_register_duration > 0 ? detail.last_register_duration + 'ms' : '—' }}</dd>
          <dt class="text-muted-foreground">上次错误</dt>
          <dd :class="detail.last_register_error && 'text-destructive'">{{ detail.last_register_error || '无' }}</dd>
        </dl>
      </Card>
    </div>

    <Card :title="`模型 (${detail.models?.length ?? 0})，拉取于 ${fmtTime(detail.models_fetched_at)}`">
      <div class="flex flex-wrap gap-1.5">
        <Badge v-for="m in detail.models" :key="m" variant="secondary" class="font-mono">{{ m }}</Badge>
        <span v-if="!detail.models?.length" class="text-sm text-muted-foreground">无模型</span>
      </div>
    </Card>

    <Card :title="`Keys (${detail.keys?.length ?? 0})`">
      <div class="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead><TableHead>Key</TableHead><TableHead>Email</TableHead>
              <TableHead>状态</TableHead><TableHead>限流次数</TableHead><TableHead>最后使用</TableHead>
              <TableHead class="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="k in detail.keys" :key="k.id">
              <TableCell class="text-muted-foreground">#{{ k.id }}</TableCell>
              <TableCell>
                <div class="flex items-center gap-1">
                  <span class="font-mono text-xs truncate max-w-48" :title="k.api_key">{{ k.api_key }}</span>
                  <CopyButton :value="k.api_key" label="Key" />
                </div>
              </TableCell>
              <TableCell>
                <div v-if="k.email" class="flex items-center gap-1">
                  <span class="font-mono text-xs truncate max-w-48" :title="k.email">{{ k.email }}</span>
                  <CopyButton :value="k.email" label="Email" />
                </div>
                <span v-else class="text-muted-foreground">—</span>
              </TableCell>
              <TableCell><StatusBadge :status="k.status" /></TableCell>
              <TableCell>{{ k.rate_limit_count }}</TableCell>
              <TableCell class="text-xs">{{ fmtTime(k.last_used_at) }}</TableCell>
              <TableCell>
                <div class="flex justify-end gap-1">
                  <Button variant="ghost" size="icon-sm" title="一键 check" :disabled="busy[k.id]" @click="keyAction(k, 'check')">
                    <Stethoscope class="size-4" :class="busy[k.id] && 'animate-pulse'" />
                  </Button>
                  <Button v-if="k.status !== 'invalid'" variant="ghost" size="icon-sm" class="text-destructive" title="禁用"
                    :disabled="busy[k.id]" @click="keyAction(k, 'disable')">
                    <Ban class="size-4" />
                  </Button>
                  <Button v-else variant="ghost" size="icon-sm" class="text-success" title="启用"
                    :disabled="busy[k.id]" @click="keyAction(k, 'enable')">
                    <CircleCheck class="size-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
            <TableEmpty v-if="!detail.keys?.length" :colspan="7">暂无 key</TableEmpty>
          </TableBody>
        </Table>
      </div>
    </Card>
  </div>
</template>
