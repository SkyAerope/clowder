<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import type { ModelEdge } from '../api'

const props = defineProps<{ models: Record<string, ModelEdge[]> }>()

const ROW_H = 40
const PAD = 24

interface Row {
  id: string
  canonical: string
  native: string
  provider: string
  best: boolean
  healthy: boolean
  ttft: string
  // 该 canonical 分组的首行/尾行索引，用于左列分组框
  groupStart: number
  groupSpan: number
}

function emphasized(r: Row) {
  return r.best || !r.canonical
}

const canonicals = computed(() =>
  Object.keys(props.models).sort((a, b) => {
    if (!a) return 1
    if (!b) return -1
    return a.localeCompare(b)
  }),
)

// 每条 canonical→provider 映射占一行：客户端名 → 实际模型 → 提供商。
// provider 不去重，按 canonical 分组连续排列。
const rows = computed<Row[]>(() => {
  const out: Row[] = []
  for (const c of canonicals.value) {
    const edges = [...(props.models[c] ?? [])].sort((a, b) =>
      (a.provider_display_name || a.provider).localeCompare(b.provider_display_name || b.provider),
    )
    const start = out.length
    for (const e of edges) {
      out.push({
        id: `${c}|${e.provider}|${e.native}`,
        canonical: c,
        native: e.native,
        provider: e.provider_display_name || e.provider,
        best: e.best,
        healthy: e.healthy,
        ttft: e.has_ttft ? `${e.ttft_ms}ms` : '',
        groupStart: start,
        groupSpan: edges.length,
      })
    }
  }
  return out
})

// 左列分组：同一 canonical 合并为一个框，纵向居中跨其所有行。
interface Group {
  canonical: string
  start: number
  span: number
  healthy: boolean
}
const groups = computed<Group[]>(() => {
  const seen = new Map<string, Group>()
  const out: Group[] = []
  rows.value.forEach((r) => {
    if (!r.canonical) return
    let g = seen.get(r.canonical)
    if (!g) {
      g = { canonical: r.canonical, start: r.groupStart, span: r.groupSpan, healthy: r.healthy }
      seen.set(r.canonical, g)
      out.push(g)
    } else if (r.healthy) {
      g.healthy = true
    }
  })
  return out
})

const height = computed(() => rows.value.length * ROW_H + PAD * 2)

const width = ref(960)
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

const boxW = 200
const sideW = boxW + 16
const midBoxW = boxW
const leftDotX = sideW
const midX = computed(() => width.value / 2)
const midLeftX = computed(() => midX.value - midBoxW / 2)
const midRightX = computed(() => midX.value + midBoxW / 2)
const rightDotX = computed(() => width.value - sideW)

const rowY = (i: number) => PAD + i * ROW_H + ROW_H / 2
const groupY = (g: Group) => PAD + g.start * ROW_H + (g.span * ROW_H) / 2
const rowGroupY = (r: Row) => PAD + r.groupStart * ROW_H + (r.groupSpan * ROW_H) / 2

function curve(x1: number, y1: number, x2: number, y2: number) {
  const mx = (x1 + x2) / 2
  return `M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`
}

function strokeOf(healthy: boolean, best: boolean) {
  if (!healthy) return 'var(--color-destructive)'
  return best ? 'var(--color-primary)' : 'var(--color-border)'
}

function strokeWidthOf(healthy: boolean, emphasized: boolean) {
  return healthy && emphasized ? 1.5 : 1
}

function lineWidthOf(healthy: boolean, emphasized: boolean) {
  return healthy && emphasized ? 2.5 : 1.5
}

function short(s: string, max = 26) {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s
}
</script>

<template>
  <div ref="container" class="relative w-full overflow-x-auto overflow-y-hidden">
    <svg :width="width" :height="height" class="block min-w-[680px]">
      <!-- 连线：有 canonical 时连接左列；所有模型均连接 native → provider。 -->
      <template v-for="(r, i) in rows" :key="r.id">
        <path
          v-if="r.canonical"
          :d="curve(leftDotX, rowGroupY(r), midLeftX, rowY(i))"
          fill="none"
          :stroke="strokeOf(r.healthy, r.best)"
          :stroke-width="lineWidthOf(r.healthy, r.best)"
          :stroke-dasharray="r.healthy ? undefined : '4 4'"
        />
        <path
          :d="curve(midRightX, rowY(i), rightDotX, rowY(i))"
          fill="none"
          :stroke="strokeOf(r.healthy, emphasized(r))"
          :stroke-width="lineWidthOf(r.healthy, emphasized(r))"
          :stroke-dasharray="r.healthy ? undefined : '4 4'"
        />
      </template>

      <!-- 左列：面向客户端的名称（同名合并、纵向跨行） -->
      <g v-for="g in groups" :key="'g-' + g.canonical">
        <rect
          :x="8"
          :y="groupY(g) - 14"
          :width="sideW - 16"
          height="28"
          rx="8"
          fill="var(--color-card)"
          :stroke="strokeOf(g.healthy, false)"
          stroke-width="1"
        />
        <text
          :x="sideW - 16"
          :y="groupY(g) + 4"
          text-anchor="end"
          class="text-xs font-medium fill-[var(--color-foreground)]"
        >
          {{ short(g.canonical, 22) }}
          <title>{{ g.canonical }}</title>
        </text>
        <circle
          :cx="leftDotX"
          :cy="groupY(g)"
          r="3.5"
          :fill="g.healthy ? 'black' : 'var(--color-destructive)'"
        />
      </g>

      <!-- 中列：模型实际名称（每行一个） -->
      <g v-for="(r, i) in rows" :key="'m-' + r.id">
        <rect
          :x="midLeftX"
          :y="rowY(i) - 14"
          :width="midBoxW"
          height="28"
          rx="8"
          fill="var(--color-card)"
          :stroke="strokeOf(r.healthy, emphasized(r))"
          :stroke-width="strokeWidthOf(r.healthy, emphasized(r))"
        />
        <text
          :x="midX"
          :y="rowY(i) + 4"
          text-anchor="middle"
          class="text-xs font-medium fill-[var(--color-foreground)]"
        >
          {{ short(r.native) }}
          <title>{{ r.native }}</title>
        </text>
      </g>

      <!-- 右列：提供商（每行一个，不去重） -->
      <g v-for="(r, i) in rows" :key="'p-' + r.id">
        <rect
          :x="rightDotX + 8"
          :y="rowY(i) - 14"
          :width="sideW - 16"
          height="28"
          rx="8"
          fill="var(--color-card)"
          :stroke="strokeOf(r.healthy, emphasized(r))"
          :stroke-width="strokeWidthOf(r.healthy, emphasized(r))"
        />
        <foreignObject
          :x="rightDotX + 16"
          :y="rowY(i) - 14"
          :width="sideW - 32"
          height="28"
        >
          <div
            class="flex h-full min-w-0 items-center text-xs font-medium text-foreground"
            :title="r.provider + (r.ttft ? ' · ' + r.ttft : '')"
	          >
	            <span class="min-w-0 flex-1 truncate">{{ r.provider }}</span>
	            <span v-if="r.ttft" class="ml-2 shrink-0 text-right text-muted-foreground">{{ r.ttft }}</span>
	          </div>
        </foreignObject>
        <circle :cx="rightDotX" :cy="rowY(i)" r="3.5" :fill="strokeOf(r.healthy, emphasized(r))" />
      </g>
    </svg>
    <div class="flex flex-wrap items-center gap-4 text-xs text-muted-foreground mt-2">
      <span class="flex items-center gap-1.5"><span class="inline-block h-0.5 w-5 rounded bg-primary" /> 最优路由</span>
      <span class="flex items-center gap-1.5"><span class="inline-block h-0.5 w-5 rounded bg-border" /> 备选</span>
      <span class="flex items-center gap-1.5"><span class="inline-block w-5 border-t-2 border-dashed border-destructive" /> 不健康</span>
      <span class="ml-auto">客户端名称（如有）→ 实际模型 → 提供商</span>
    </div>
  </div>
</template>
