<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError, type Capture, type RequestLogEntry } from '../api'
import TableSkeleton from '../components/TableSkeleton.vue'
import TraceTimeline from '../components/TraceTimeline.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { toast } from 'vue-sonner'
import {
  buildRequestParams, parseRequestFiltersFromQuery, statusFilterError, type RequestFilterDraft,
} from './request-filters'
import { RefreshCw, ChevronLeft, ChevronRight, GitBranch, Fingerprint, TriangleAlert, ListFilter, X, FileJson, ShieldCheck } from '@lucide/vue'

const route = useRoute()

const logs = ref<RequestLogEntry[]>([])
const loaded = ref(false)
const filtering = ref(false)
const offset = ref(0)
const limit = 50

const initial = parseRequestFiltersFromQuery(route.query)
const draft = ref<RequestFilterDraft>({ ...initial })
const applied = ref<RequestFilterDraft>({ ...initial })
const expandedIds = ref<Set<number>>(new Set())

async function load() {
  filtering.value = true
  try {
    const query = buildRequestParams(applied.value, limit, offset.value)
    logs.value = (await api.get<RequestLogEntry[]>(`/api/admin/requests?${query}`)) ?? []
  } catch (error) {
    if (error instanceof ApiError) {
      toast.error(`筛选失败：${error.message}`)
      return
    }
    throw error
  } finally {
    loaded.value = true
    filtering.value = false
    const ids = new Set(logs.value.map((l) => l.id))
    for (const id of expandedIds.value) {
      if (!ids.has(id)) expandedIds.value.delete(id)
    }
  }
}

function applyFilters() {
  const statusError = statusFilterError(draft.value.status)
  if (statusError) {
    toast.error(statusError)
    return
  }
  applied.value = { ...draft.value }
  offset.value = 0
  load()
}

function resetFilters() {
  draft.value = parseRequestFiltersFromQuery({})
}

function toggleExpand(id: number) {
  if (expandedIds.value.has(id)) expandedIds.value.delete(id)
  else expandedIds.value.add(id)
}

const captureFieldLabels: Record<string, string> = {
  request_headers: '请求头',
  request_body: '请求体',
  response_headers: '响应头',
  response_body: '响应体',
}

function hasCapture(l: RequestLogEntry): boolean {
  return l.capture != null && captureParts(l.capture).length > 0
}

function capturedLabel(cap: Capture | null): string {
  if (!cap) return ''
  return captureParts(cap).map((part) => part.label).join('、')
}

function captureParts(cap: Capture): { key: string; label: string; value: string }[] {
  const parts: { key: string; label: string; value: string }[] = []
  const add = (key: string, label: string, value?: string) => {
    if (value) parts.push({ key, label, value: pretty(value) })
  }
  add('request_headers', '请求头', cap.request_headers)
  add('request_body', '请求体', cap.request_body)
  add('response_headers', '响应头', cap.response_headers)
  add('response_body', '响应体', cap.response_body)
  return parts
}

function pretty(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
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

    <form class="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-6" @submit.prevent="applyFilters">
      <Input v-model="draft.from" type="datetime-local" placeholder="开始时间" aria-label="开始时间" />
      <Input v-model="draft.to" type="datetime-local" placeholder="结束时间" aria-label="结束时间" />
      <Input v-model="draft.provider" placeholder="提供商" aria-label="提供商" />
      <Input v-model="draft.model" placeholder="模型" aria-label="模型" />
      <Input v-model="draft.status" type="number" inputmode="numeric" placeholder="状态码" aria-label="状态码" />
      <Input v-model="draft.rule" placeholder="规则" aria-label="规则" />
      <Input v-model="draft.fingerprint" placeholder="错误指纹" aria-label="错误指纹" />
      <Select v-model="draft.error">
        <SelectTrigger class="w-full" aria-label="是否错误">
          <SelectValue placeholder="是否错误" />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectItem value="all">全部</SelectItem>
            <SelectItem value="true">仅错误</SelectItem>
            <SelectItem value="false">仅成功</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>
      <Select v-model="draft.captured">
        <SelectTrigger class="w-full" aria-label="有无抓包">
          <SelectValue placeholder="有无抓包" />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectItem value="all">全部</SelectItem>
            <SelectItem value="true">已抓包</SelectItem>
            <SelectItem value="false">未抓包</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>
      <div class="col-span-2 flex items-center justify-end gap-2 md:col-span-3">
        <Button type="button" variant="outline" @click="resetFilters"><X data-icon="inline-start" />重置</Button>
        <Button type="submit" :disabled="filtering"><ListFilter data-icon="inline-start" />筛选</Button>
      </div>
    </form>

    <div class="ring-foreground/10 bg-card overflow-x-auto rounded-xl ring-1">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead class="w-10"><span class="sr-only">展开</span></TableHead>
            <TableHead>时间</TableHead><TableHead>Trace</TableHead><TableHead>#</TableHead>
            <TableHead>Provider</TableHead><TableHead>模型</TableHead><TableHead>状态</TableHead>
            <TableHead>TTFT</TableHead><TableHead>耗时</TableHead><TableHead>命中规则</TableHead><TableHead>错误</TableHead>
          </TableRow>
        </TableHeader>
        <TableSkeleton v-if="!loaded" :cols="11" />
        <TableBody v-else>
          <template v-for="l in logs" :key="l.id">
            <TableRow>
              <TableCell class="w-10">
                <button
                  class="flex size-6 cursor-pointer items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                  :aria-expanded="expandedIds.has(l.id)"
                  aria-label="展开详情"
                  @click="toggleExpand(l.id)"
                >
                  <ChevronRight class="size-4 transition-transform duration-200" :class="expandedIds.has(l.id) && 'rotate-90'" />
                </button>
              </TableCell>
              <TableCell class="text-xs whitespace-nowrap">{{ new Date(l.created_at + 'Z').toLocaleString() }}</TableCell>
              <TableCell>
                <span class="flex items-center gap-1 font-mono text-xs">
                  <GitBranch class="size-3 text-muted-foreground" aria-hidden="true" />{{ l.trace_id.slice(0, 8) }}
                </span>
              </TableCell>
              <TableCell>{{ l.attempt_no }}</TableCell>
              <TableCell>{{ l.provider }}</TableCell>
              <TableCell class="font-mono text-xs">{{ l.model }}</TableCell>
              <TableCell><span :class="statusClass(l.status_code)" class="font-medium">{{ l.status_code ?? 'ERR' }}</span></TableCell>
              <TableCell class="text-xs">{{ l.ttft_ms != null ? l.ttft_ms + 'ms' : '—' }}</TableCell>
              <TableCell class="text-xs">{{ l.latency_ms != null ? l.latency_ms + 'ms' : '—' }}</TableCell>
              <TableCell class="text-xs">{{ l.matched_slug || '—' }}</TableCell>
              <TableCell class="text-xs max-w-56">
                <Tooltip v-if="l.error_fingerprint || hasCapture(l)">
                  <TooltipTrigger as-child>
                    <span class="flex w-fit cursor-help items-center gap-1.5 text-foreground" tabindex="0">
                      <FileJson v-if="hasCapture(l)" class="size-4 shrink-0 text-primary" aria-label="已抓包" />
                      <template v-if="l.error_fingerprint">
                        <Fingerprint class="size-4 shrink-0" aria-hidden="true" />
                        <span>{{ l.error_fingerprint.slice(0, 8) }}</span>
                      </template>
                    </span>
                  </TooltipTrigger>
                  <TooltipContent class="max-w-96">
                    <div class="flex flex-col gap-1">
                      <div v-if="l.error_fingerprint" class="flex items-start gap-2">
                        <Fingerprint class="size-4 shrink-0" aria-label="完整指纹" />
                        <span class="font-mono">{{ l.error_fingerprint }}</span>
                      </div>
                      <div v-if="l.error_msg" class="flex items-start gap-2 break-all">
                        <TriangleAlert class="size-4 shrink-0" aria-label="错误详情" />
                        {{ l.error_msg }}
                      </div>
                      <div v-if="hasCapture(l)" class="flex items-start gap-2">
                        <FileJson class="size-4 shrink-0" aria-label="已抓包" />
                        <span>已抓包：{{ capturedLabel(l.capture) }}</span>
                      </div>
                    </div>
                  </TooltipContent>
                </Tooltip>
              </TableCell>
            </TableRow>
            <TableRow v-if="expandedIds.has(l.id)" class="hover:bg-transparent">
              <TableCell :colspan="11" class="p-0">
                <div class="grid grid-cols-1 gap-4 border-t border-border bg-muted/30 px-4 py-3" :class="hasCapture(l) && 'lg:grid-cols-2'">
                  <div class="min-w-0">
                    <div class="mb-2 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                      <GitBranch class="size-3.5" />重试链<span class="font-mono">{{ l.trace_id }}</span>
                    </div>
                    <TraceTimeline :trace="l.trace" />
                  </div>
                  <div v-if="l.capture && hasCapture(l)" class="min-w-0">
                    <div class="mb-2 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                      <FileJson class="size-3.5" /> 抓包
                    </div>
                    <div class="flex flex-col gap-3">
                      <div v-if="l.capture.redacted" class="flex items-center gap-1 text-xs text-muted-foreground">
                        <ShieldCheck class="size-3.5 text-success" /> 已脱敏
                      </div>
                      <div v-for="part in captureParts(l.capture)" :key="part.key">
                        <div class="mb-1 text-xs font-medium">{{ part.label }}</div>
                        <pre class="bg-background max-h-48 overflow-x-auto rounded-md border border-border p-2.5 font-mono text-[11px] leading-relaxed">{{ part.value }}</pre>
                      </div>
                    </div>
                  </div>
                </div>
              </TableCell>
            </TableRow>
          </template>
          <TableEmpty v-if="!logs.length" :colspan="11">暂无请求</TableEmpty>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
