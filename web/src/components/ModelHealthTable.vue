<script setup lang="ts">
import { computed } from 'vue'
import type { HealthSnapshot } from '../api'
import StatusBadge from './StatusBadge.vue'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Info } from 'lucide-vue-next'

type ModelHealthStatus = 'healthy' | 'probing' | 'unhealthy' | 'disabled'

type ModelHealthRow = {
  readonly model: string
  readonly status: ModelHealthStatus
  readonly reason?: string
  readonly avgTTFTMs: number
  readonly errorCount: number
  readonly lastErrorAt?: string
  readonly lastSuccessAt?: string
}

const props = defineProps<{
  models: readonly string[]
  snapshots: readonly HealthSnapshot[]
  providerEnabled: boolean
}>()

const rows = computed<ModelHealthRow[]>(() => {
  const providerHealth = props.snapshots.find((snapshot) => snapshot.model === '')
  const snapshotsByModel = new Map(
    props.snapshots
      .filter((snapshot) => snapshot.model !== '')
      .map((snapshot) => [snapshot.model, snapshot]),
  )

  return props.models.map((model) => {
    const snapshot = snapshotsByModel.get(model)
    if (!props.providerEnabled) {
      return { model, status: 'disabled', reason: '提供商已停用', avgTTFTMs: snapshot?.avg_ttft_ms ?? 0, errorCount: snapshot?.error_count ?? 0, lastErrorAt: snapshot?.last_error_at, lastSuccessAt: snapshot?.last_success_at }
    }
    if (providerHealth?.status === 'unhealthy') {
      return { model, status: 'unhealthy', reason: '提供商级健康检查失败', avgTTFTMs: snapshot?.avg_ttft_ms ?? 0, errorCount: snapshot?.error_count ?? 0, lastErrorAt: snapshot?.last_error_at, lastSuccessAt: snapshot?.last_success_at }
    }
    if (providerHealth?.status === 'probing') {
      return { model, status: 'probing', reason: '提供商正在探测恢复状态', avgTTFTMs: snapshot?.avg_ttft_ms ?? 0, errorCount: snapshot?.error_count ?? 0, lastErrorAt: snapshot?.last_error_at, lastSuccessAt: snapshot?.last_success_at }
    }
    return {
      model,
      status: snapshot?.status === 'healthy' || snapshot?.status === 'unhealthy' ? snapshot.status : 'probing',
      avgTTFTMs: snapshot?.avg_ttft_ms ?? 0,
      errorCount: snapshot?.error_count ?? 0,
      lastErrorAt: snapshot?.last_error_at,
      lastSuccessAt: snapshot?.last_success_at,
    }
  })
})

const summary = computed(() => {
  const counts: Record<ModelHealthStatus, number> = { healthy: 0, probing: 0, unhealthy: 0, disabled: 0 }
  for (const row of rows.value) counts[row.status]++
  return [
    counts.healthy > 0 ? `${counts.healthy} 健康` : '',
    counts.probing > 0 ? `${counts.probing} 探测中` : '',
    counts.unhealthy > 0 ? `${counts.unhealthy} 不健康` : '',
    counts.disabled > 0 ? `${counts.disabled} 禁用` : '',
  ].filter(Boolean).join(' · ')
})

function formatTime(value?: string): string {
  return value ? new Date(value).toLocaleString() : '—'
}

function latestActivityAt(row: ModelHealthRow): string | undefined {
  if (!row.lastErrorAt) return row.lastSuccessAt
  if (!row.lastSuccessAt) return row.lastErrorAt
  return Date.parse(row.lastErrorAt) > Date.parse(row.lastSuccessAt) ? row.lastErrorAt : row.lastSuccessAt
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <div v-if="summary" class="text-xs text-muted-foreground">{{ summary }}</div>
    <div class="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>模型</TableHead>
            <TableHead>健康状态</TableHead>
            <TableHead>平均 TTFT</TableHead>
            <TableHead>最近活动</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="row in rows" :key="row.model">
            <TableCell class="font-mono text-xs">{{ row.model }}</TableCell>
            <TableCell>
              <div class="flex items-center gap-1.5">
                <StatusBadge :status="row.status" :reason="row.reason" />
                <Tooltip v-if="row.errorCount > 0 || row.lastErrorAt || row.lastSuccessAt">
                  <TooltipTrigger as-child>
                    <button type="button" class="text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-sm" aria-label="查看健康详情">
                      <Info class="size-3.5" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent class="max-w-72 text-xs">
                    <div class="flex flex-col gap-1">
                      <p>累计错误：{{ row.errorCount }}</p>
                      <p>最近成功：{{ formatTime(row.lastSuccessAt) }}</p>
                      <p>最近失败：{{ formatTime(row.lastErrorAt) }}</p>
                    </div>
                  </TooltipContent>
                </Tooltip>
              </div>
            </TableCell>
            <TableCell class="tabular-nums">{{ row.avgTTFTMs > 0 ? `${row.avgTTFTMs}ms` : '—' }}</TableCell>
            <TableCell class="text-xs text-muted-foreground">
              {{ formatTime(latestActivityAt(row)) }}
            </TableCell>
          </TableRow>
          <TableEmpty v-if="!rows.length" :colspan="4">无模型</TableEmpty>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
