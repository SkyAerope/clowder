<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { api, type KeyRow, type Provider } from '../api'
import StatusBadge from '../components/StatusBadge.vue'
import { RefreshCw, Plus, Stethoscope, Ban, CircleCheck } from 'lucide-vue-next'

const keys = ref<KeyRow[]>([])
const providers = ref<Provider[]>([])
const filter = ref('')
const busy = ref<Record<number, boolean>>({})
const registering = ref(false)
const msg = ref('')

const filtered = computed(() =>
  filter.value ? keys.value.filter(k => k.provider === filter.value) : keys.value
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
  if (!filter.value) {
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
        <select v-model="filter" class="input w-44">
          <option value="">全部 provider</option>
          <option v-for="p in providers" :key="p.name" :value="p.name">{{ p.name }}</option>
        </select>
        <button class="btn-primary" :disabled="registering || !filter" @click="register">
          <Plus class="size-4" :class="registering && 'animate-pulse'" /> 注册新 Key
        </button>
        <button class="btn-outline" @click="load"><RefreshCw class="size-4" /></button>
      </div>
    </div>

    <p v-if="msg" class="text-sm" :class="msg.includes('失败') ? 'text-destructive' : 'text-success'">{{ msg }}</p>

    <div class="card overflow-x-auto">
      <table class="table-base">
        <thead>
          <tr>
            <th>ID</th><th>Provider</th><th>Key</th><th>Email</th><th>状态</th>
            <th>限流次数</th><th>限流至</th><th>最后使用</th><th>创建时间</th><th class="text-right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="k in filtered" :key="k.id" class="hover:bg-muted/50">
            <td class="text-muted-foreground">#{{ k.id }}</td>
            <td>{{ k.provider }}</td>
            <td class="font-mono text-xs">{{ k.api_key_masked }}</td>
            <td class="font-mono text-xs">{{ k.email_masked || '—' }}</td>
            <td><StatusBadge :status="k.status" /></td>
            <td>{{ k.rate_limit_count }}</td>
            <td class="text-xs">{{ fmtTime(k.rate_limit_until) }}</td>
            <td class="text-xs">{{ fmtTime(k.last_used_at) }}</td>
            <td class="text-xs">{{ fmtTime(k.created_at) }}</td>
            <td>
              <div class="flex justify-end gap-1">
                <button class="btn-ghost p-1.5" title="一键 check" :disabled="busy[k.id]" @click="action(k, 'check')">
                  <Stethoscope class="size-4" :class="busy[k.id] && 'animate-pulse'" />
                </button>
                <button v-if="k.status !== 'invalid'" class="btn-ghost p-1.5 text-destructive" title="禁用"
                  :disabled="busy[k.id]" @click="action(k, 'disable')">
                  <Ban class="size-4" />
                </button>
                <button v-else class="btn-ghost p-1.5 text-success" title="启用"
                  :disabled="busy[k.id]" @click="action(k, 'enable')">
                  <CircleCheck class="size-4" />
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="!filtered.length">
            <td colspan="10" class="text-center text-muted-foreground py-6">暂无 key</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
