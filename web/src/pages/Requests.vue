<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, type RequestLog } from '../api'
import { RefreshCw, ChevronLeft, ChevronRight, GitBranch } from 'lucide-vue-next'

const logs = ref<RequestLog[]>([])
const offset = ref(0)
const limit = 50
const trace = ref<RequestLog[] | null>(null)
const traceID = ref('')

async function load() {
  logs.value = (await api.get<RequestLog[]>(`/api/admin/requests?limit=${limit}&offset=${offset.value}`)) ?? []
}

async function showTrace(id: string) {
  traceID.value = id
  trace.value = (await api.get<RequestLog[]>(`/api/admin/trace/${id}`)) ?? []
}

function statusClass(code?: number) {
  if (!code) return 'text-destructive'
  if (code < 300) return 'text-success'
  if (code < 500) return 'text-warning'
  return 'text-destructive'
}

function prev() { offset.value = Math.max(0, offset.value - limit); load() }
function next() { offset.value += limit; load() }

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <h1 class="text-xl font-semibold">请求日志</h1>
      <div class="flex gap-2">
        <button class="btn-outline" :disabled="offset === 0" @click="prev"><ChevronLeft class="size-4" /></button>
        <button class="btn-outline" :disabled="logs.length < limit" @click="next"><ChevronRight class="size-4" /></button>
        <button class="btn-outline" @click="load"><RefreshCw class="size-4" /></button>
      </div>
    </div>

    <div class="card overflow-x-auto">
      <table class="table-base">
        <thead>
          <tr>
            <th>时间</th><th>Trace</th><th>#</th><th>Provider</th><th>模型</th>
            <th>状态</th><th>TTFT</th><th>耗时</th><th>命中规则</th><th>错误</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="l in logs" :key="l.id" class="hover:bg-muted/50">
            <td class="text-xs whitespace-nowrap">{{ new Date(l.created_at + 'Z').toLocaleString() }}</td>
            <td>
              <button class="font-mono text-xs text-primary hover:underline flex items-center gap-1"
                @click="showTrace(l.trace_id)">
                <GitBranch class="size-3" />{{ l.trace_id.slice(0, 8) }}
              </button>
            </td>
            <td>{{ l.attempt_no }}</td>
            <td>{{ l.provider }}</td>
            <td class="font-mono text-xs">{{ l.model }}</td>
            <td><span :class="statusClass(l.status_code)" class="font-medium">{{ l.status_code ?? 'ERR' }}</span></td>
            <td class="text-xs">{{ l.ttft_ms != null ? l.ttft_ms + 'ms' : '—' }}</td>
            <td class="text-xs">{{ l.latency_ms != null ? l.latency_ms + 'ms' : '—' }}</td>
            <td class="text-xs">{{ l.matched_rule || '—' }}</td>
            <td class="text-xs text-destructive max-w-56 truncate" :title="l.error_msg">{{ l.error_msg || '' }}</td>
          </tr>
          <tr v-if="!logs.length"><td colspan="10" class="text-center text-muted-foreground py-6">暂无请求</td></tr>
        </tbody>
      </table>
    </div>

    <!-- Trace 链视图 -->
    <div v-if="trace" class="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-6" @click.self="trace = null">
      <div class="card p-5 max-w-3xl w-full max-h-[80vh] overflow-y-auto space-y-3">
        <div class="flex items-center justify-between">
          <h2 class="font-semibold">Trace <span class="font-mono text-sm text-muted-foreground">{{ traceID }}</span></h2>
          <button class="btn-ghost" @click="trace = null">关闭</button>
        </div>
        <div class="space-y-2">
          <div v-for="(t, i) in trace" :key="t.id" class="flex gap-3">
            <div class="flex flex-col items-center">
              <div class="size-6 rounded-full flex items-center justify-center text-xs font-semibold"
                :class="t.status_code && t.status_code < 300 ? 'bg-success/15 text-success' : 'bg-destructive/15 text-destructive'">
                {{ t.attempt_no }}
              </div>
              <div v-if="i < trace.length - 1" class="w-px flex-1 bg-border my-1" />
            </div>
            <div class="pb-3 text-sm space-y-0.5 min-w-0">
              <div>
                <span class="font-medium">{{ t.provider }}</span>
                <span class="text-muted-foreground"> / </span>
                <span class="font-mono text-xs">{{ t.model }}</span>
                <span class="ml-2" :class="statusClass(t.status_code)">{{ t.status_code ?? 'transport error' }}</span>
                <span v-if="t.key_id" class="text-xs text-muted-foreground ml-2">key #{{ t.key_id }}</span>
              </div>
              <div class="text-xs text-muted-foreground">
                TTFT {{ t.ttft_ms != null ? t.ttft_ms + 'ms' : '—' }} · 耗时 {{ t.latency_ms != null ? t.latency_ms + 'ms' : '—' }}
                <template v-if="t.matched_rule"> · 规则 <span class="text-primary">{{ t.matched_rule }}</span></template>
              </div>
              <div v-if="t.error_msg" class="text-xs text-destructive break-all">{{ t.error_msg }}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
