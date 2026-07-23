<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { Save } from '@lucide/vue'
import { toast } from 'vue-sonner'
import { api } from '../../api'
import Card from '../../components/Card.vue'
import ModelTagsInput from '../../components/ModelTagsInput.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { parseManifest, serializeManifest, type ManifestDocument, type ManifestForm } from './manifest-codec'
import { getErrorMessage } from './types'

const props = defineProps<{
  providerName: string
  afterSave: () => Promise<void>
}>()
const openState = ref(false)
const manifestYaml = ref('')
const tab = ref<'visual' | 'source'>('visual')
const parseError = ref('')
const saving = ref(false)
const form = reactive<ManifestForm>(parseManifest('').form)
let raw: ManifestDocument = {}
let syncing = false

function refreshForm(): void {
  try {
    const parsed = parseManifest(manifestYaml.value)
    raw = parsed.raw
    Object.assign(form, parsed.form)
    parseError.value = ''
  } catch (error: unknown) {
    parseError.value = `YAML 解析失败，请在「源文件」修正：${getErrorMessage(error)}`
  }
}

function serializeForm(): void {
  syncing = true
  manifestYaml.value = serializeManifest(raw, form)
  syncing = false
}

watch(form, () => {
  if (openState.value && tab.value === 'visual' && !syncing) serializeForm()
}, { deep: true })
watch(tab, (value) => {
  if (value === 'visual') refreshForm()
})

function open(yaml: string): void {
  manifestYaml.value = yaml
  tab.value = 'visual'
  refreshForm()
  openState.value = true
}

async function saveManifest(): Promise<void> {
  if (tab.value === 'visual') serializeForm()
  saving.value = true
  try {
    await api.put(`/api/admin/providers/${props.providerName}/manifest`, { yaml: manifestYaml.value })
    openState.value = false
    toast.success('manifest 已保存并生效')
    await props.afterSave()
  } catch (error: unknown) {
    toast.error(`保存失败: ${getErrorMessage(error)}`)
  } finally {
    saving.value = false
  }
}

defineExpose({ open })
</script>

<template>
  <Dialog v-model:open="openState">
    <DialogContent class="sm:max-w-2xl">
      <DialogHeader>
        <DialogTitle>修改 manifest</DialogTitle>
        <DialogDescription>编辑 providers/{{ providerName }}/manifest.yaml，保存后自动校验并热重载。</DialogDescription>
      </DialogHeader>
      <Tabs v-model="tab">
        <TabsList><TabsTrigger value="visual">可视化</TabsTrigger><TabsTrigger value="source">源文件</TabsTrigger></TabsList>
        <TabsContent value="visual" class="max-h-[60vh] space-y-3 overflow-y-auto overflow-x-hidden px-1 [scrollbar-gutter:stable]">
          <Alert v-if="parseError" variant="destructive"><AlertDescription>{{ parseError }}</AlertDescription></Alert>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div class="space-y-1"><Label class="text-xs text-muted-foreground">显示名 (display_name)</Label><Input v-model="form.display_name" /></div>
            <div class="space-y-1"><Label class="text-xs text-muted-foreground">格式 (format)</Label><Input v-model="form.format" class="font-mono" /></div>
            <div class="space-y-1 sm:col-span-2"><Label class="text-xs text-muted-foreground">描述 (description)</Label><Input v-model="form.description" /></div>
            <div class="space-y-1 sm:col-span-2"><Label class="text-xs text-muted-foreground">base_url</Label><Input v-model="form.base_url" class="font-mono" /></div>
            <div class="space-y-1 sm:col-span-2">
              <Label class="text-xs text-muted-foreground">Key 策略 (key_strategy)</Label>
              <Select v-model="form.key_strategy"><SelectTrigger class="w-full"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="sticky">sticky</SelectItem><SelectItem value="round-robin">round-robin</SelectItem></SelectContent></Select>
            </div>
            <div class="space-y-1 sm:col-span-2">
              <Label class="text-xs text-muted-foreground">默认测试模型 (default_check_model)<span v-if="form.check" class="text-destructive">· check 脚本存在时不生效</span></Label>
              <Input v-model="form.default_check_model" class="font-mono" :disabled="!!form.check" placeholder="留空时随机选用 healthy 模型" />
            </div>
          </div>
          <div class="flex flex-col gap-1"><Label class="text-xs text-muted-foreground">模型 (models)</Label><ModelTagsInput v-model="form.models" :provider="providerName" /></div>
          <Card title="脚本 (scripts)">
            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div class="space-y-1"><Label class="text-xs text-muted-foreground">register</Label><Input v-model="form.register" class="font-mono" placeholder="register.sh" /></div>
              <div class="space-y-1"><Label class="text-xs text-muted-foreground">check</Label><Input v-model="form.check" class="font-mono" placeholder="check.sh" /></div>
              <div class="space-y-1"><Label class="text-xs text-muted-foreground">register 超时(秒)</Label><Input v-model.number="form.register_timeout" type="number" /></div>
              <div class="space-y-1"><Label class="text-xs text-muted-foreground">check 超时(秒)</Label><Input v-model.number="form.check_timeout" type="number" /></div>
            </div>
          </Card>
          <Card title="路由 (routing)">
            <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div class="space-y-1"><Label class="text-xs text-muted-foreground">优先级</Label><Input v-model.number="form.priority" type="number" /></div>
              <div class="space-y-1"><Label class="text-xs text-muted-foreground">权重</Label><Input v-model.number="form.weight" type="number" /></div>
              <div class="space-y-1"><Label class="text-xs text-muted-foreground">hedge</Label><div class="flex h-8 items-center"><Switch v-model="form.hedge" /></div></div>
            </div>
          </Card>
          <p class="text-xs text-muted-foreground">内嵌 rules 等其它字段会原样保留，可在「源文件」查看。</p>
        </TabsContent>
        <TabsContent value="source"><Textarea v-model="manifestYaml" :rows="22" class="font-mono text-xs leading-relaxed max-h-[60vh]" spellcheck="false" /></TabsContent>
      </Tabs>
      <DialogFooter><Button variant="outline" @click="openState = false">取消</Button><Button :disabled="saving" @click="saveManifest"><Save class="size-4" /> 保存并生效</Button></DialogFooter>
    </DialogContent>
  </Dialog>
</template>
