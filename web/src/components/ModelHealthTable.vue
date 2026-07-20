<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '../api'
import type { HealthSnapshot } from '../api'
import StatusBadge from './StatusBadge.vue'
import { Button } from '@/components/ui/button'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Check, Stethoscope, X } from 'lucide-vue-next'
import { toast } from 'vue-sonner'

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
  provider: string
  models: readonly string[]
  snapshots: readonly HealthSnapshot[]
}>()

const emit = defineEmits<{
  checked: []
}>()

type ModelCheckResponse = {
  readonly status: string
  readonly key_id: number
  readonly model_response: string
  readonly ttft_ms: number
  readonly rule_name: string
  readonly status_code: number
  readonly response_body: string
}

type ModelCheckState = {
  readonly loading: boolean
  readonly result: 'success' | 'error' | null
  readonly status: string
  readonly keyID: number
  readonly modelResponse: string
  readonly ttftMS: number
  readonly ruleName: string
  readonly statusCode: number
  readonly responseBody: string
  readonly applying: boolean
  readonly applied: boolean
  readonly errorMessage: string
}

const checkStates = ref<Record<string, ModelCheckState>>({})

function emptyCheckState(): ModelCheckState {
  return {
    loading: false, result: null, status: '', keyID: 0, modelResponse: '', ttftMS: 0,
    ruleName: '', statusCode: 0, responseBody: '', applying: false, applied: false, errorMessage: '',
  }
}

async function checkModel(model: string): Promise<void> {
  checkStates.value[model] = { ...emptyCheckState(), loading: true }
  try {
    const response = await api.post<ModelCheckResponse>(`/api/admin/providers/${props.provider}/check-model`, { model })
    checkStates.value[model] = {
      ...emptyCheckState(),
      result: response.status === 'active' ? 'success' : 'error',
      status: response.status,
      keyID: response.key_id,
      modelResponse: response.model_response,
      ttftMS: response.ttft_ms,
      ruleName: response.rule_name,
      statusCode: response.status_code,
      responseBody: response.response_body,
    }
    emit('checked')
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : '未知错误'
    checkStates.value[model] = { ...emptyCheckState(), result: 'error', errorMessage }
  }
}

async function applyRule(model: string): Promise<void> {
  const state = checkStates.value[model]
  if (!state?.ruleName || state.applying || state.applied) return
  checkStates.value[model] = { ...state, applying: true }
  try {
    await api.post(`/api/admin/providers/${props.provider}/apply-model-check-rule`, {
      model, key_id: state.keyID, rule_name: state.ruleName,
      status_code: state.statusCode, response_body: state.responseBody,
    })
    checkStates.value[model] = { ...state, applying: false, applied: true }
    toast.success(`已应用规则 ${state.ruleName}`)
    emit('checked')
  } catch (error) {
    checkStates.value[model] = { ...state, applying: false }
    toast.error(`应用规则失败: ${error instanceof Error ? error.message : '未知错误'}`)
  }
}

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
                        <p v-if="checkStates[row.model]?.ruleName" class="break-words">
                          命中规则：{{ checkStates[row.model]?.ruleName }}
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
