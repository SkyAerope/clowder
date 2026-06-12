<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api, type KeyRow } from '../api'
import Card from '../components/Card.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { RefreshCw, Plus, Stethoscope } from 'lucide-vue-next'

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
      <h1 class="text-xl font-semibold">{{ detail.manifest.display_name || name }}</h1>
      <div class="flex gap-2">
        <button v-if="detail.manifest.has_check" class="btn-outline" :disabled="checking" @click="checkAll">
          <Stethoscope class="size-4" :class="checking && 'animate-pulse'" /> 全部 check
        </button>
        <button class="btn-outline" :disabled="refreshing" @click="refreshModels">
          <RefreshCw class="size-4" :class="refreshing && 'animate-spin'" /> 刷新模型
        </button>
        <button v-if="detail.manifest.key_source === 'script'" class="btn-primary" :disabled="registering" @click="register">
          <Plus class="size-4" :class="registering && 'animate-pulse'" /> 注册新 Key
        </button>
      </div>
    </div>

    <p v-if="msg" class="text-sm" :class="msg.includes('失败') ? 'text-destructive' : 'text-success'">{{ msg }}</p>

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
        <span v-for="m in detail.models" :key="m" class="badge bg-muted text-foreground font-mono">{{ m }}</span>
        <span v-if="!detail.models?.length" class="text-sm text-muted-foreground">无模型</span>
      </div>
    </Card>

    <Card :title="`Keys (${detail.keys?.length ?? 0})`">
      <div class="overflow-x-auto">
        <table class="table-base">
          <thead>
            <tr><th>ID</th><th>Key</th><th>Email</th><th>状态</th><th>限流次数</th><th>最后使用</th></tr>
          </thead>
          <tbody>
            <tr v-for="k in detail.keys" :key="k.id">
              <td class="text-muted-foreground">#{{ k.id }}</td>
              <td class="font-mono text-xs">{{ k.api_key_masked }}</td>
              <td class="font-mono text-xs">{{ k.email_masked || '—' }}</td>
              <td><StatusBadge :status="k.status" /></td>
              <td>{{ k.rate_limit_count }}</td>
              <td class="text-xs">{{ fmtTime(k.last_used_at) }}</td>
            </tr>
            <tr v-if="!detail.keys?.length"><td colspan="6" class="text-center text-muted-foreground py-4">暂无 key</td></tr>
          </tbody>
        </table>
      </div>
    </Card>
  </div>
</template>
