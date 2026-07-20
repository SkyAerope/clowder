<script setup lang="ts">
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

defineProps<{ title?: string }>()
</script>

<template>
  <Card>
    <CardHeader v-if="title" class="flex flex-row items-center justify-between gap-2">
      <CardTitle class="text-sm font-medium text-muted-foreground">{{ title }}</CardTitle>
      <slot name="action" />
    </CardHeader>
    <CardContent>
      <slot />
    </CardContent>
  </Card>
</template>
