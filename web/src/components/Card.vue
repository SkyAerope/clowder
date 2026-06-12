<script setup lang="ts">
defineProps<{ title?: string }>()
</script>

<template>
  <div class="card p-4">
    <h3 v-if="title" class="text-sm font-medium text-muted-foreground mb-3">{{ title }}</h3>
    <slot />
  </div>
</template>
