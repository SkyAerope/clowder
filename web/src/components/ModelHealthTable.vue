<script setup lang="ts">
import { computed } from 'vue'
import type { HealthSnapshot } from '../api'
import StatusBadge from './StatusBadge.vue'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'

type ModelHealthStatus = 'healthy' | 'probing' | 'unhealthy' | 'disabled'

type ModelHealthRow = {
  readonly model: string
  readonly status: ModelHealthStatus
  readonly avgTTFTMs: number
  readonly errorCount: number
  readonly lastErrorAt?: string
  readonly lastSuccessAt?: string
  readonly lastRule?: string
}

const props = defineProps<{
  models: readonly string[]
  snapshots: readonly HealthSnapshot[]
}>()

const rows = computed<ModelHealthRow[]>(() => {
  const snapshotsByModel = new Map(
    props.snapshots
      .filter((snapshot) => snapshot.model !== '')
      .map((snapshot) => [snapshot.model, snapshot]),
  )

  return props.models.map((model) => {
    const snapshot = snapshotsByModel.get(model)
    return {
      model,
      status: snapshot?.status === 'healthy' || snapshot?.status === 'unhealthy' || snapshot?.status === 'probing' ? snapshot.status : 'probing',
      avgTTFTMs: snapshot?.avg_ttft_ms ?? 0,
      errorCount: snapshot?.error_count ?? 0,
      lastErrorAt: snapshot?.last_error_at,
      lastSuccessAt: snapshot?.last_success_at,
      lastRule: snapshot?.last_rule,
    }
  })
})

const summary = computed(() => {
  const counts: Record<ModelHealthStatus, number> = { healthy: 0, probing: 0, unhealthy: 0, disabled: 0 }
  for (const row of rows.value) counts[row.status]++
  return [
    counts.healthy > 0 ? `${counts.healthy} 健康` : '',
    counts.probing > 0 ? `${counts.probing} 待测试` : '',
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

function isNeverRequested(row: ModelHealthRow): boolean {
  return row.status === 'probing' && !row.lastErrorAt && !row.lastSuccessAt
}

function healthDetails(row: ModelHealthRow): string[] {
  if (row.status === 'healthy' || isNeverRequested(row)) return []
  return [
    row.lastRule ? `触发规则：${row.lastRule}` : '',
    row.lastErrorAt ? `失败时间：${formatTime(row.lastErrorAt)}` : '',
  ].filter(Boolean)
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
              <StatusBadge
                :status="row.status"
                :label="isNeverRequested(row) ? '从未请求' : undefined"
                :details="healthDetails(row)"
              />
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
