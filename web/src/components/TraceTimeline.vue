<script setup lang="ts">
import type { RequestLog } from '../api'

defineProps<{ trace: RequestLog[] }>()

function statusClass(code?: number) {
  if (!code) return 'text-destructive'
  if (code < 300) return 'text-success'
  if (code < 500) return 'text-warning'
  return 'text-destructive'
}
</script>

<template>
  <div class="space-y-2">
    <div v-for="(t, i) in trace" :key="t.id" class="flex gap-3">
      <div class="flex flex-col items-center">
        <div class="size-6 shrink-0 rounded-full flex items-center justify-center text-xs font-semibold"
          :class="t.status_code && t.status_code < 300 ? 'bg-success/15 text-success' : 'bg-destructive/15 text-destructive'">
          {{ t.attempt_no }}
        </div>
        <div v-if="i < trace.length - 1" class="w-px flex-1 bg-border mt-2" />
      </div>
      <div class="pb-3 text-sm space-y-0.5 min-w-0">
        <div>
          <span class="font-medium">{{ t.provider }}</span>
          <span class="text-muted-foreground"> / </span>
          <span class="font-mono text-xs">{{ t.model }}</span>
          <span class="ml-2" :class="statusClass(t.status_code)">{{ t.status_code ?? 'transport error' }}</span>
          <span v-if="t.key_id" class="text-xs ml-2"><span class="text-primary">key</span><span class="text-muted-foreground ml-1">#{{ t.key_id }}</span></span>
        </div>
        <div class="text-xs text-muted-foreground">
          <span class="text-primary">TTFT</span> {{ t.ttft_ms != null ? t.ttft_ms + 'ms' : '—' }}
          <span class="text-primary ml-2">耗时</span> {{ t.latency_ms != null ? t.latency_ms + 'ms' : '—' }}
          <template v-if="t.matched_slug"><span class="text-primary ml-2">命中规则</span> {{ t.matched_slug }}</template>
        </div>
        <div v-if="t.error_msg" class="text-destructive whitespace-pre-wrap break-words [overflow-wrap:anywhere] text-xs">{{ t.error_msg }}</div>
      </div>
    </div>
  </div>
</template>
