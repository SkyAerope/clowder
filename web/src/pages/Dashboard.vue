<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { api, session, type Provider, type ModelEdge, type Candidate } from '../api'
import Card from '../components/Card.vue'
import StatusBadge from '../components/StatusBadge.vue'
import RoutingGraph from '../components/RoutingGraph.vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { RefreshCw, Zap } from 'lucide-vue-next'

const providers = ref<Provider[]>([])
const models = ref<Record<string, ModelEdge[]>>({})
const auto = ref<Candidate[]>([])
const error = ref('')
const loading = ref(false)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [p, m, a] = await Promise.all([
      api.get<Provider[]>('/api/providers'),
      api.get<Record<string, ModelEdge[]>>('/api/models'),
      api.get<Candidate[]>('/api/auto'),
    ])
    providers.value = p ?? []
    models.value = m ?? {}
    auto.value = a ?? []
  } catch (e: any) {
    error.value = e.status === 401 ? '看板未公开，请先登录管理面板' : e.message
  } finally {
    loading.value = false
  }
}

let timer: ReturnType<typeof setInterval>
onMounted(() => {
  load()
  timer = setInterval(load, 10000)
})
onUnmounted(() => clearInterval(timer))
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-xl font-semibold">看板</h1>
      <Button variant="outline" size="sm" :disabled="loading" @click="load">
        <RefreshCw class="size-4" :class="loading && 'animate-spin'" /> 刷新
      </Button>
    </div>

    <Alert v-if="error" variant="destructive">
      <AlertDescription>{{ error }}</AlertDescription>
    </Alert>

    <!-- Provider 状态卡片 -->
    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
      <component
        :is="session.admin ? 'router-link' : 'div'"
        v-for="p in providers"
        :key="p.name"
        :to="session.admin ? `/providers/${p.name}` : undefined"
        class="ring-foreground/10 bg-card text-card-foreground flex flex-col gap-2 rounded-xl p-4 ring-1"
        :class="session.admin && 'cursor-pointer transition-shadow hover:ring-primary/50'"
      >
        <div class="flex items-center justify-between">
          <span class="font-medium">{{ p.display_name || p.name }}</span>
          <StatusBadge :status="p.status" />
        </div>
        <div class="grid grid-cols-3 gap-2 text-sm">
          <div>
            <div class="text-muted-foreground text-xs">可用 Key</div>
            <div class="font-semibold">{{ p.active_keys }}/{{ p.total_keys }}</div>
          </div>
          <div>
            <div class="text-muted-foreground text-xs">模型数</div>
            <div class="font-semibold">{{ p.model_count }}</div>
          </div>
          <div>
            <div class="text-muted-foreground text-xs">平均 TTFT</div>
            <div class="font-semibold">{{ p.avg_ttft_ms > 0 ? p.avg_ttft_ms + 'ms' : '—' }}</div>
          </div>
        </div>
      </component>
      <div v-if="!providers.length && !error" class="ring-foreground/10 bg-card text-card-foreground col-span-full rounded-xl p-6 text-center text-sm text-muted-foreground ring-1">
        暂无 provider，在 ~/.clowder/providers/ 下添加目录后 reload
      </div>
    </div>

    <!-- auto 路由 top-3 -->
    <Card title="auto 当前路由（Top 3）">
      <div v-if="auto.length" class="flex flex-wrap gap-3">
        <div
          v-for="(c, i) in auto"
          :key="c.provider + c.canonical"
          class="ring-foreground/10 flex items-center gap-2 rounded-lg px-3 py-2 text-sm ring-1"
          :class="i === 0 && 'bg-primary/5 ring-primary/60'"
        >
          <Zap class="size-4" :class="i === 0 ? 'text-primary' : 'text-muted-foreground'" />
          <span class="font-medium">{{ c.canonical }}</span>
          <span class="text-muted-foreground">via {{ c.provider }}</span>
          <Badge v-if="c.has_ttft" variant="secondary">{{ c.ttft_ms }}ms</Badge>
        </div>
      </div>
      <div v-else class="text-sm text-muted-foreground">暂无可路由模型</div>
    </Card>

    <!-- 模型路由图 -->
    <Card title="模型路由图">
      <RoutingGraph v-if="Object.keys(models).length" :models="models" />
      <div v-else class="text-sm text-muted-foreground">暂无模型映射数据</div>
    </Card>
  </div>
</template>
