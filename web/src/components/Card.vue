<script setup lang="ts">
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

defineProps<{ title?: string }>()
</script>

<template>
  <Card>
    <CardHeader v-if="title">
      <CardTitle class="text-sm font-medium text-muted-foreground">{{ title }}</CardTitle>
    </CardHeader>
    <CardContent>
      <slot />
    </CardContent>
  </Card>
</template>
