<script setup lang="ts">
import { computed } from 'vue'
import { Button } from '@/components/ui/button'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Check, Stethoscope, X } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { api } from '../api'
import type { HealthSnapshot } from '../api'
import { createModelHealthOperations } from './model-health-operations'
import StatusBadge from './StatusBadge.vue'

type ModelHealthStatus = 'healthy' | 'probing' | 'unhealthy' | 'disabled'

type ModelHealthRow = {
  readonly model: string
  readonly status: ModelHealthStatus
  readonly avgTTFTMs: number
  readonly errorCount: number
  readonly lastErrorAt?: string
  readonly lastSuccessAt?: string
  readonly lastSlug?: string
}

const props = defineProps<{
  provider: string
  models: readonly string[]
  snapshots: readonly HealthSnapshot[]
}>()

const emit = defineEmits<{
  checked: []
}>()

const { checkStates, checkModel, applyRule } = createModelHealthOperations({
  provider: props.provider,
  post: api.post,
  checked: () => emit('checked'),
  success: toast.success,
  error: toast.error,
})

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
      lastSlug: snapshot?.last_slug,
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
    row.lastSlug ? `触发规则：${row.lastSlug}` : '',
    row.lastErrorAt ? `失败时间：${formatTime(row.lastErrorAt)}` : '',
  ].filter(Boolean)
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <div v-if="summary" class="text-xs text-muted-foreground">{{ summary }}</div>
    <div class="overflow-x-auto">
      <Table class="min-w-[38rem]">
        <TableHeader>
          <TableRow>
            <TableHead>模型</TableHead>
            <TableHead>健康状态</TableHead>
            <TableHead>平均 TTFT</TableHead>
            <TableHead>最近活动</TableHead>
            <TableHead class="text-right">操作</TableHead>
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
            <TableCell>
              <div class="flex justify-end">
                <Tooltip>
                  <TooltipTrigger as-child>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      :disabled="checkStates[row.model]?.loading"
                      @click="checkModel(row.model)"
                    >
                      <Stethoscope v-if="checkStates[row.model]?.loading" class="animate-pulse" />
                      <Check v-else-if="checkStates[row.model]?.result === 'success'" class="text-success" />
                      <X v-else-if="checkStates[row.model]?.result === 'error'" class="text-destructive" />
                      <Stethoscope v-else />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent class="w-max max-w-[min(32rem,calc(100vw-2rem))] text-xs">
                    <template v-if="checkStates[row.model]?.result">
                      <div class="flex flex-col gap-1">
                        <p v-if="checkStates[row.model]?.status">检查结果：{{ checkStates[row.model]?.status }}</p>
                        <p v-if="checkStates[row.model]?.keyID">使用 Key：#{{ checkStates[row.model]?.keyID }}</p>
                        <p v-if="checkStates[row.model]?.modelResponse">模型回复：{{ checkStates[row.model]?.modelResponse }}</p>
                        <p v-if="checkStates[row.model]?.ttftMS">TTFT：{{ checkStates[row.model]?.ttftMS }} ms</p>
                        <p v-if="checkStates[row.model]?.errorMessage">{{ checkStates[row.model]?.errorMessage }}</p>
                        <p v-if="checkStates[row.model]?.ruleSlug" class="break-words">
                          命中规则：{{ checkStates[row.model]?.ruleSlug }}
                          <button
                            type="button"
                            class="cursor-pointer whitespace-nowrap font-medium underline underline-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                            :disabled="checkStates[row.model]?.applying || checkStates[row.model]?.applied"
                            @click="applyRule(row.model)"
                          >
                            {{ checkStates[row.model]?.applied ? '[已应用]' : '[应用该规则]' }}
                          </button>
                        </p>
                        <p>点击再次检查</p>
                      </div>
                    </template>
                    <span v-else>检查模型</span>
                  </TooltipContent>
                </Tooltip>
              </div>
            </TableCell>
          </TableRow>
          <TableEmpty v-if="!rows.length" :colspan="5">无模型</TableEmpty>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
