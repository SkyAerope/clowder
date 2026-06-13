<script setup lang="ts">
import { ref } from 'vue'
import { toast } from 'vue-sonner'
import { Copy, Check } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const props = defineProps<{ value: string; label?: string }>()

const copied = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined

async function copy() {
  try {
    await navigator.clipboard.writeText(props.value)
    copied.value = true
    toast.success(`已复制${props.label ? ' ' + props.label : ''}`)
    clearTimeout(timer)
    timer = setTimeout(() => (copied.value = false), 1500)
  } catch {
    toast.error('复制失败')
  }
}
</script>

<template>
  <Button
    variant="ghost"
    size="icon-sm"
    class="shrink-0"
    :title="`复制${label ? ' ' + label : ''}`"
    @click="copy"
  >
    <Check v-if="copied" class="size-3.5 text-success" />
    <Copy v-else class="size-3.5" />
  </Button>
</template>
