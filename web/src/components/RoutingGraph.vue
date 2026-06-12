<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue'
import type { ModelEdge } from '../api'

const props = defineProps<{ models: Record<string, ModelEdge[]> }>()

const ROW_H = 44
const PAD = 24

const canonicals = computed(() => Object.keys(props.models).sort())
const providers = computed(() => {
  const set = new Set<string>()
  for (const edges of Object.values(props.models)) for (const e of edges) set.add(e.provider)
  return [...set].sort()
})

const height = computed(() => Math.max(canonicals.value.length, providers.value.length) * ROW_H + PAD * 2)

const width = ref(720)
const container = ref<HTMLElement>()
let ro: ResizeObserver | undefined
onMounted(() => {
  ro = new ResizeObserver(() => {
    if (container.value) width.value = container.value.clientWidth
  })
  if (container.value) {
    ro.observe(container.value)
    width.value = container.value.clientWidth
  }
})
onUnmounted(() => ro?.disconnect())

const leftX = 190
const rightX = computed(() => width.value - 190)

function yOf(idx: number) {
  return PAD + idx * ROW_H + ROW_H / 2
}

interface Edge {
  key: string
  path: string
  best: boolean
  healthy: boolean
  ttft: string
}

const edges = computed<Edge[]>(() => {
  const out: Edge[] = []
  canonicals.value.forEach((c, ci) => {
    for (const e of props.models[c] ?? []) {
      const pi = providers.value.indexOf(e.provider)
      if (pi < 0) continue
      const y1 = yOf(ci)
      const y2 = yOf(pi)
      const x1 = leftX
      const x2 = rightX.value
      const mx = (x1 + x2) / 2
      out.push({
        key: `${c}->${e.provider}`,
        path: `M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`,
        best: e.best,
        healthy: e.healthy,
        ttft: e.has_ttft ? `${e.ttft_ms}ms` : '',
      })
    }
  })
  return out
})

const hovered = ref('')
</script>

<template>
  <div ref="container" class="relative w-full overflow-x-auto">
    <svg :width="width" :height="height" class="block min-w-[560px]">
      <!-- 曲线 -->
      <path
        v-for="e in edges"
        :key="e.key"
        :d="e.path"
        fill="none"
        :stroke="!e.healthy ? 'var(--color-destructive)' : e.best ? 'var(--color-primary)' : 'var(--color-border)'"
        :stroke-width="e.best ? 2.5 : 1.5"
        :stroke-dasharray="e.healthy ? undefined : '4 4'"
        :opacity="hovered && hovered !== e.key ? 0.25 : 1"
        class="transition-opacity"
        @mouseenter="hovered = e.key"
        @mouseleave="hovered = ''"
      />
      <!-- 左列：canonical 模型 -->
      <g v-for="(c, i) in canonicals" :key="c">
        <rect :x="8" :y="yOf(i) - 14" :width="leftX - 16" height="28" rx="8"
          class="fill-[var(--color-muted)]" />
        <text :x="leftX - 16" :y="yOf(i) + 4" text-anchor="end" class="text-xs font-medium fill-[var(--color-foreground)]">
          {{ c }}
        </text>
        <circle :cx="leftX" :cy="yOf(i)" r="3.5" class="fill-[var(--color-primary)]" />
      </g>
      <!-- 右列：provider -->
      <g v-for="(p, i) in providers" :key="p">
        <rect :x="rightX + 8" :y="yOf(i) - 14" :width="leftX - 16" height="28" rx="8"
          class="fill-[var(--color-muted)]" />
        <text :x="rightX + 16" :y="yOf(i) + 4" class="text-xs font-medium fill-[var(--color-foreground)]">
          {{ p }}
        </text>
        <circle :cx="rightX" :cy="yOf(i)" r="3.5" class="fill-[var(--color-primary)]" />
      </g>
    </svg>
    <div class="flex gap-4 text-xs text-muted-foreground mt-1">
      <span class="flex items-center gap-1.5"><span class="inline-block w-5 h-0.5 bg-primary rounded" /> 最优路由</span>
      <span class="flex items-center gap-1.5"><span class="inline-block w-5 h-0.5 bg-border rounded" /> 备选</span>
      <span class="flex items-center gap-1.5"><span class="inline-block w-5 border-t-2 border-dashed border-destructive" /> 不健康</span>
    </div>
  </div>
</template>
