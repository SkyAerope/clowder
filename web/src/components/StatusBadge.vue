<script setup lang="ts">
import { computed } from 'vue'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

const props = defineProps<{ status: string }>()

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
    default:
      return 'border-transparent bg-muted text-muted-foreground'
  }
})
</script>

<template>
  <Badge variant="outline" :class="cn(cls)">{{ status }}</Badge>
</template>
