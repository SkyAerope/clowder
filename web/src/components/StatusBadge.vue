<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ status: string }>()

const cls = computed(() => {
  switch (props.status) {
    case 'healthy':
    case 'active':
      return 'bg-success/15 text-success'
    case 'probing':
    case 'rate_limited':
    case 'idle':
      return 'bg-warning/20 text-warning'
    case 'unhealthy':
    case 'invalid':
      return 'bg-destructive/15 text-destructive'
    default:
      return 'bg-muted text-muted-foreground'
  }
})
</script>

<template>
  <span class="badge" :class="cls">{{ status }}</span>
</template>
