<script setup lang="ts">
import { computed } from 'vue'
import { Info } from 'lucide-vue-next'
import { Badge } from '@/components/ui/badge'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

const props = defineProps<{
  status: string
  label?: string
  reason?: string
  details?: readonly string[]
}>()

const tooltipLines = computed(() => props.details?.filter(Boolean) ?? (props.reason ? [props.reason] : []))

const labels: Record<string, string> = {
  active: '激活',
  idle: '空闲',
  rate_limited: '限流',
  invalid: '无效',
  healthy: '健康',
  probing: '探测中',
  unhealthy: '不健康',
  disabled: '禁用',
}

const cls = computed(() => {
  switch (props.status) {
    case 'healthy':
    case 'active':
      return 'border-transparent bg-success/15 text-success'
    case 'probing':
    case 'rate_limited':
    case 'idle':
      return 'border-transparent bg-warning/20 text-warning'
    case 'unhealthy':
    case 'invalid':
      return 'border-transparent bg-destructive/15 text-destructive'
    case 'disabled':
      return 'border-transparent bg-muted text-muted-foreground'
    default:
      return 'border-transparent bg-muted text-muted-foreground'
  }
})
</script>

<template>
  <Badge variant="outline" :class="cn(cls)">
    {{ label ?? labels[status] ?? status }}
    <Tooltip v-if="tooltipLines.length">
      <TooltipTrigger as-child>
        <span
          tabindex="0"
          aria-label="查看状态原因"
          class="relative inline-flex cursor-help before:absolute before:-inset-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-current/40"
        >
          <Info class="size-3" />
        </span>
      </TooltipTrigger>
      <TooltipContent class="pointer-events-none text-xs">
        <div class="flex flex-col gap-1">
          <p v-for="line in tooltipLines" :key="line">{{ line }}</p>
        </div>
      </TooltipContent>
    </Tooltip>
  </Badge>
</template>
