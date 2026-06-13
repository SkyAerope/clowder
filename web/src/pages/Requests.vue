<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, type RequestLog } from '../api'
import TableSkeleton from '../components/TableSkeleton.vue'
import { Button } from '@/components/ui/button'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from '@/components/ui/dialog'
import { RefreshCw, ChevronLeft, ChevronRight, GitBranch } from 'lucide-vue-next'

const logs = ref<RequestLog[]>([])
const loaded = ref(false)
const offset = ref(0)
const limit = 50
const trace = ref<RequestLog[] | null>(null)
const traceID = ref('')
const traceOpen = ref(false)

async function load() {
  try {
    logs.value = (await api.get<RequestLog[]>(`/api/admin/requests?limit=${limit}&offset=${offset.value}`)) ?? []
  } finally {
    loaded.value = true
  }
}

async function showTrace(id: string) {
  traceID.value = id
  trace.value = (await api.get<RequestLog[]>(`/api/admin/trace/${id}`)) ?? []
  traceOpen.value = true
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
        <Button variant="outline" size="icon" :disabled="offset === 0" @click="prev"><ChevronLeft class="size-4" /></Button>
        <Button variant="outline" size="icon" :disabled="logs.length < limit" @click="next"><ChevronRight class="size-4" /></Button>
        <Button variant="outline" size="icon" @click="load"><RefreshCw class="size-4" /></Button>
      </div>
    </div>

    <div class="ring-foreground/10 bg-card overflow-x-auto rounded-xl ring-1">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>时间</TableHead><TableHead>Trace</TableHead><TableHead>#</TableHead>
            <TableHead>Provider</TableHead><TableHead>模型</TableHead><TableHead>状态</TableHead>
            <TableHead>TTFT</TableHead><TableHead>耗时</TableHead><TableHead>命中规则</TableHead><TableHead>错误</TableHead>
          </TableRow>
        </TableHeader>
        <TableSkeleton v-if="!loaded" :cols="10" />
        <TableBody v-else>
          <TableRow v-for="l in logs" :key="l.id">
            <TableCell class="text-xs whitespace-nowrap">{{ new Date(l.created_at + 'Z').toLocaleString() }}</TableCell>
            <TableCell>
              <button class="font-mono text-xs text-primary hover:underline flex items-center gap-1 cursor-pointer"
                @click="showTrace(l.trace_id)">
                <GitBranch class="size-3" />{{ l.trace_id.slice(0, 8) }}
              </button>
            </TableCell>
            <TableCell>{{ l.attempt_no }}</TableCell>
            <TableCell>{{ l.provider }}</TableCell>
            <TableCell class="font-mono text-xs">{{ l.model }}</TableCell>
            <TableCell><span :class="statusClass(l.status_code)" class="font-medium">{{ l.status_code ?? 'ERR' }}</span></TableCell>
            <TableCell class="text-xs">{{ l.ttft_ms != null ? l.ttft_ms + 'ms' : '—' }}</TableCell>
            <TableCell class="text-xs">{{ l.latency_ms != null ? l.latency_ms + 'ms' : '—' }}</TableCell>
            <TableCell class="text-xs">{{ l.matched_rule || '—' }}</TableCell>
            <TableCell class="text-xs text-destructive max-w-56 truncate" :title="l.error_msg">{{ l.error_msg || '' }}</TableCell>
          </TableRow>
          <TableEmpty v-if="!logs.length" :colspan="10">暂无请求</TableEmpty>
        </TableBody>
      </Table>
    </div>

    <!-- Trace 链视图 -->
    <Dialog v-model:open="traceOpen">
      <DialogContent class="max-w-3xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Trace <span class="font-mono text-sm text-muted-foreground">{{ traceID }}</span></DialogTitle>
        </DialogHeader>
        <div v-if="trace" class="space-y-2">
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
      </DialogContent>
    </Dialog>
  </div>
</template>
