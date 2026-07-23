<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { api } from '../api'
import Card from '../components/Card.vue'
import ConfigForm from './settings/ConfigForm.vue'
import ModelMappingsEditor from './settings/ModelMappingsEditor.vue'
import { defaultConfig, parseConfig, serializeConfig, type ConfigForm as ConfigFormModel } from './settings/config-codec'
import { parseModelMappings, serializeModelMappings, type ModelEntry, type ModelPattern } from './settings/model-mapping-codec'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { RotateCw, Save } from '@lucide/vue'
import { toast } from 'vue-sonner'

type ConfigResponse = { readonly config_yaml: string; readonly models_yaml: string }

const configYaml = ref('')
const modelsYaml = ref('')
const cfg = reactive<ConfigFormModel>(defaultConfig())
const modelEntries = ref<ModelEntry[]>([])
const modelPatterns = ref<ModelPattern[]>([])
const saving = ref(false)
const reloading = ref(false)
const loaded = ref(false)
const tab = ref<'visual' | 'source'>('visual')
const parseError = ref('')
let syncing = false

function errorMessage(error: unknown): string | undefined {
  if (typeof error !== 'object' || error === null || !('message' in error)) return undefined
  return String(error.message)
}

function refreshForms(): void {
  try {
    Object.assign(cfg, parseConfig(configYaml.value))
    const mappings = parseModelMappings(modelsYaml.value)
    modelEntries.value = mappings.entries
    modelPatterns.value = mappings.patterns
    parseError.value = ''
  } catch (error) {
    parseError.value = `YAML 解析失败，请在「源文件」修正后再切换：${errorMessage(error)}`
  }
}

function serializeForms(): void {
  syncing = true
  configYaml.value = serializeConfig(cfg)
  modelsYaml.value = serializeModelMappings({ entries: modelEntries.value, patterns: modelPatterns.value })
  syncing = false
}

watch([cfg, modelEntries, modelPatterns], () => {
  if (tab.value === 'visual' && !syncing) serializeForms()
}, { deep: true })

watch(tab, (currentTab) => {
  if (currentTab === 'visual') refreshForms()
})

async function load(): Promise<void> {
  try {
    const response = await api.get<ConfigResponse>('/api/admin/config')
    configYaml.value = response.config_yaml
    modelsYaml.value = response.models_yaml
    refreshForms()
  } finally {
    loaded.value = true
  }
}

async function save(): Promise<void> {
  if (tab.value === 'visual') serializeForms()
  saving.value = true
  try {
    await api.put('/api/admin/config', { config_yaml: configYaml.value, models_yaml: modelsYaml.value })
    toast.success('已保存并热重载（端口等冷字段需重启进程生效）')
  } catch (error) {
    toast.error(`保存失败: ${errorMessage(error)}`)
  } finally {
    saving.value = false
  }
}

async function reload(): Promise<void> {
  reloading.value = true
  try {
    await api.post('/api/admin/reload')
    toast.success('已重载')
    await load()
  } catch (error) {
    toast.error(`重载失败: ${errorMessage(error)}`)
  } finally {
    reloading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <h1 class="text-xl font-semibold">设置</h1>
      <div class="flex gap-2">
        <Button variant="outline" :disabled="reloading" @click="reload">
          <RotateCw class="size-4" :class="reloading && 'animate-spin'" /> 重载
        </Button>
        <Button :disabled="saving" @click="save">
          <Save class="size-4" /> 保存并生效
        </Button>
      </div>
    </div>

    <div v-if="!loaded" class="space-y-4">
      <Skeleton class="h-9 w-48" />
      <div class="grid grid-cols-1 xl:grid-cols-2 gap-4">
        <Skeleton class="h-64 w-full rounded-xl" />
        <Skeleton class="h-64 w-full rounded-xl" />
      </div>
    </div>

    <Tabs v-else v-model="tab">
      <TabsList>
        <TabsTrigger value="visual">可视化</TabsTrigger>
        <TabsTrigger value="source">源文件</TabsTrigger>
      </TabsList>

      <!-- 可视化编辑 -->
      <TabsContent value="visual" class="space-y-4">
        <Alert v-if="parseError" variant="destructive">
          <AlertDescription>{{ parseError }}</AlertDescription>
        </Alert>

        <ConfigForm v-model="cfg" />

        <!-- 模型映射 -->
        <ModelMappingsEditor v-model:entries="modelEntries" v-model:patterns="modelPatterns" />
      </TabsContent>

      <!-- 源文件编辑 -->
      <TabsContent value="source">
        <div class="grid grid-cols-1 xl:grid-cols-2 gap-4">
          <Card title="config.yaml">
            <Textarea v-model="configYaml" :rows="24" class="font-mono text-xs leading-relaxed" spellcheck="false" />
          </Card>
          <Card title="models.yaml（模型映射）">
            <Textarea v-model="modelsYaml" :rows="24" class="font-mono text-xs leading-relaxed" spellcheck="false" />
          </Card>
        </div>
      </TabsContent>
    </Tabs>
  </div>
</template>
