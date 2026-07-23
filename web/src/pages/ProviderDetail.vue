<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import * as YAML from 'yaml'
import { ArrowLeft, FileCog, Plus } from '@lucide/vue'
import { toast } from 'vue-sonner'
import { api, type HealthSnapshot } from '../api'
import Card from '../components/Card.vue'
import ModelHealthTable from '../components/ModelHealthTable.vue'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import ImportKeysDialog from './provider-detail/ImportKeysDialog.vue'
import ManifestEditorDialog from './provider-detail/ManifestEditorDialog.vue'
import ProviderKeysTable from './provider-detail/ProviderKeysTable.vue'
import ProviderSummary from './provider-detail/ProviderSummary.vue'
import type { ProviderDetail, ProviderDetailResponse } from './provider-detail/types'
import { getErrorMessage } from './provider-detail/types'
import { createProviderKeyOperations } from './provider-detail/useProviderKeys'

const route = useRoute()
const name = String(route.params.name)
const detail = ref<ProviderDetail | null>(null)
const healthSnapshots = ref<HealthSnapshot[]>([])
const registering = ref(false)
const providerEnabledSaving = ref(false)
const importDialog = ref<InstanceType<typeof ImportKeysDialog> | null>(null)
const manifestDialog = ref<InstanceType<typeof ManifestEditorDialog> | null>(null)
const keys = computed(() => detail.value?.keys ?? [])

async function load(): Promise<void> {
  const [providerDetail, snapshots] = await Promise.all([
    api.get<ProviderDetailResponse>(`/api/admin/providers/${name}`),
    api.get<HealthSnapshot[]>('/api/health'),
  ])
  detail.value = { ...providerDetail, models: providerDetail.models ?? [] }
  healthSnapshots.value = snapshots.filter((snapshot) => snapshot.provider === name)
}

const keyOperations = createProviderKeyOperations({
  providerName: name,
  keys,
  load,
  post: api.post,
  deleteRequest: api.delete,
  success: toast.success,
  warning: toast.warning,
  error: toast.error,
})

async function register(): Promise<void> {
  registering.value = true
  try {
    const result = await api.post<{ registered: number; error?: string }>('/api/admin/register', { provider: name, count: 1 })
    if (result.error) toast.error(`注册失败: ${result.error}`)
    else toast.success(`已注册 ${result.registered} 个 key`)
    await load()
  } catch (error: unknown) {
    toast.error(`注册失败: ${getErrorMessage(error)}`)
  } finally {
    registering.value = false
  }
}

async function setProviderEnabled(enabled: boolean): Promise<void> {
  if (!detail.value || providerEnabledSaving.value) return
  providerEnabledSaving.value = true
  try {
    const parsed = YAML.parse(detail.value.manifest_yaml)
    const manifest: Record<string, unknown> = typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed) ? parsed : {}
    manifest.enabled = enabled
    const yaml = YAML.stringify(manifest, { lineWidth: 0 })
    await api.put(`/api/admin/providers/${name}/manifest`, { yaml })
    toast.success(enabled ? '提供商已启用' : '提供商已停用')
    await load()
  } catch (error: unknown) {
    toast.error(`更新失败: ${getErrorMessage(error)}`)
  } finally {
    providerEnabledSaving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="detail" class="space-y-4">
    <div class="flex items-center justify-between flex-wrap gap-2">
      <div class="flex items-center gap-2">
        <Button as-child variant="ghost" size="icon-sm" title="返回提供商列表"><RouterLink to="/providers"><ArrowLeft class="size-4" /></RouterLink></Button>
        <h1 class="text-xl font-semibold">{{ detail.manifest.display_name || name }}</h1>
        <Switch :model-value="detail.manifest.enabled" :disabled="providerEnabledSaving" aria-label="启用提供商" title="启用 / 停用提供商" @update:model-value="setProviderEnabled" />
      </div>
      <div class="flex gap-2">
        <Button variant="outline" size="sm" @click="manifestDialog?.open(detail.manifest_yaml)"><FileCog class="size-4" /> 修改 manifest</Button>
        <Button variant="outline" size="sm" @click="importDialog?.open()"><Plus class="size-4" /> 添加 Key</Button>
        <Button v-if="detail.manifest.has_register" size="sm" :disabled="registering" @click="register"><Plus class="size-4" :class="registering && 'animate-pulse'" /> 注册新 Key</Button>
      </div>
    </div>

    <ProviderSummary :detail="detail" />
    <Card :title="`模型 (${detail.models?.length ?? 0})`"><ModelHealthTable :provider="name" :models="detail.models" :snapshots="healthSnapshots" @checked="load" /></Card>
    <ProviderKeysTable
      :keys="detail.keys"
      :key-state="keyOperations.keyState.value"
      :checking="keyOperations.checking.value"
      @check-all="keyOperations.checkAll"
      @check="keyOperations.keyAction($event, 'check')"
      @status="keyOperations.setKeyStatus"
      @delete="keyOperations.deleteKey"
    />
    <ImportKeysDialog ref="importDialog" :provider-name="name" :after-save="load" />
    <ManifestEditorDialog ref="manifestDialog" :provider-name="name" :after-save="load" />
  </div>

  <div v-else class="space-y-4">
    <div class="flex items-center gap-2"><Skeleton class="size-8 rounded-md" /><Skeleton class="h-7 w-48" /></div>
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4"><Skeleton class="h-56 w-full rounded-xl" /><Skeleton class="h-56 w-full rounded-xl" /></div>
    <Skeleton class="h-64 w-full rounded-xl" />
  </div>
</template>
