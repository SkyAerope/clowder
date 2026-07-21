<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api, type Provider } from '../api'
import ProviderStatusBadge from '../components/ProviderStatusBadge.vue'
import TableSkeleton from '../components/TableSkeleton.vue'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import { RefreshCw, ChevronRight } from 'lucide-vue-next'

const router = useRouter()
const providers = ref<Provider[]>([])
const loading = ref(false)
const loaded = ref(false)
const msg = ref('')

async function load() {
  loading.value = true
  msg.value = ''
  try {
    const p = await api.get<Provider[]>('/api/providers')
    providers.value = p ? [...p].sort((a, b) => (a.display_name || a.name).localeCompare(b.display_name || b.name)) : []
  } catch (e: any) {
    msg.value = e.message
  } finally {
    loading.value = false
    loaded.value = true
  }
}

function open(p: Provider) {
  router.push(`/providers/${p.name}`)
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between flex-wrap gap-2">
      <h1 class="text-xl font-semibold">提供商管理</h1>
      <Button variant="outline" size="icon" :disabled="loading" @click="load">
        <RefreshCw class="size-4" :class="loading && 'animate-spin'" />
      </Button>
    </div>

    <Alert v-if="msg" variant="destructive">
      <AlertDescription>{{ msg }}</AlertDescription>
    </Alert>

    <div class="ring-foreground/10 bg-card overflow-x-auto rounded-xl ring-1">
      <Table class="min-w-[42rem]">
        <TableHeader>
          <TableRow>
            <TableHead>提供商</TableHead><TableHead>状态</TableHead><TableHead>可用 Key</TableHead>
            <TableHead>模型数</TableHead><TableHead>平均 TTFT</TableHead><TableHead>优先级</TableHead>
            <TableHead class="text-right">管理</TableHead>
          </TableRow>
        </TableHeader>
        <TableSkeleton v-if="!loaded" :cols="7" />
        <TableBody v-else>
          <TableRow v-for="p in providers" :key="p.name" class="cursor-pointer" @click="open(p)">
            <TableCell class="font-medium">{{ p.display_name || p.name }}</TableCell>
            <TableCell><ProviderStatusBadge :provider="p" /></TableCell>
            <TableCell>{{ p.active_keys }}/{{ p.total_keys }}</TableCell>
            <TableCell>{{ p.model_count }}</TableCell>
            <TableCell>{{ p.avg_ttft_ms > 0 ? p.avg_ttft_ms + 'ms' : '—' }}</TableCell>
            <TableCell>{{ p.priority }}</TableCell>
            <TableCell>
              <div class="flex justify-end">
                <Button variant="ghost" size="icon-sm" title="进入管理">
                  <ChevronRight class="size-4" />
                </Button>
              </div>
            </TableCell>
          </TableRow>
          <TableEmpty v-if="!providers.length" :colspan="7">
            暂无 provider，在 ~/.clowder/providers/ 下添加目录后 reload
          </TableEmpty>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
