<script setup lang="ts">
import type { Provider } from '../api'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{ provider: Provider }>()

function formatTime(value: string): string {
  return new Date(value).toLocaleString()
}

function isNeverRequested(): boolean {
  return props.provider.status === 'probing'
    && !props.provider.last_error_at
    && !props.provider.last_success_at
}

function details(): string[] {
  if (props.provider.status === 'healthy' || props.provider.status === 'disabled' || isNeverRequested()) return []
  return [
    props.provider.status_reason ?? '',
  props.provider.last_slug ? `触发规则：${props.provider.last_slug}` : '',
    props.provider.last_error_at ? `失败时间：${formatTime(props.provider.last_error_at)}` : '',
  ].filter(Boolean)
}
</script>

<template>
  <StatusBadge
    :status="provider.status"
    :label="isNeverRequested() ? '从未请求' : undefined"
    :details="details()"
  />
</template>
