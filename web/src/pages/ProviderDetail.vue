<script setup lang="ts">
import { ref, reactive, watch, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import * as YAML from 'yaml'
import { api, type KeyRow } from '../api'
import Card from '../components/Card.vue'
import StatusBadge from '../components/StatusBadge.vue'
import CopyButton from '../components/CopyButton.vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { toast } from 'vue-sonner'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSub,
  DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { RefreshCw, Plus, Stethoscope, Check, X, ArrowLeft, FileCog, Save, MoreVertical, Trash2, CircleCheck, Ban, CircleDashed, SlidersHorizontal } from 'lucide-vue-next'
import { RouterLink } from 'vue-router'

const route = useRoute()
const name = route.params.name as string

interface Detail {
  manifest: Record<string, any>
  manifest_yaml: string
  models: string[]
  models_fetched_at?: string
  keys: KeyRow[]
  last_register_at?: string
  last_register_duration: number
  last_register_error?: string
}

const detail = ref<Detail | null>(null)
const registering = ref(false)
const importing = ref(false)
const importOpen = ref(false)
const importKeys = ref('')
const refreshing = ref(false)
const checking = ref(false)
const providerEnabledSaving = ref(false)
// { id: { loading, result: 'success'|'error'|null, checkStatus, metadata } }
const keyState = ref<Record<number, { loading: boolean; result: 'success' | 'error' | null; checkStatus: string; metadata: string }>>({})

function ensureKeyState(id: number) {
  if (!keyState.value[id]) {
    keyState.value[id] = { loading: false, result: null, checkStatus: '', metadata: '' }
  }
}

async function load() {
  detail.value = await api.get<Detail>(`/api/admin/providers/${name}`)
}

async function register() {
  registering.value = true
  try {
    const r = await api.post<{ registered: number; error?: string }>('/api/admin/register', { provider: name, count: 1 })
    if (r.error) {
      toast.error(`注册失败: ${r.error}`)
    } else {
      toast.success(`已注册 ${r.registered} 个 key`)
    }
    await load()
  } catch (e: any) {
    toast.error(`注册失败: ${e.message}`)
  } finally {
    registering.value = false
  }
}

async function importKeysToDatabase() {
  importing.value = true
  try {
    const result = await api.post<{ added: number; skipped: number }>(`/api/admin/providers/${name}/keys`, {
      keys: importKeys.value,
    })
    importOpen.value = false
    importKeys.value = ''
    toast.success(`已导入 ${result.added} 个 key，跳过 ${result.skipped} 个`)
    await load()
  } catch (e: any) {
    toast.error(`导入失败: ${e.message}`)
  } finally {
    importing.value = false
  }
}

async function refreshModels() {
  refreshing.value = true
  try {
    await api.post(`/api/admin/providers/${name}/refresh-models`)
    toast.success('模型已刷新')
    await load()
  } catch (e: any) {
    toast.error(`刷新失败: ${e.message}`)
  } finally {
    refreshing.value = false
  }
}

async function keyAction(k: KeyRow, act: 'disable' | 'enable' | 'check') {
  if (act === 'check') {
    ensureKeyState(k.id)
    keyState.value[k.id] = { loading: true, result: null, checkStatus: '', metadata: '' }
  }
  try {
    const resp = await api.post<{ check_status: string; metadata: string; status: string }>(`/api/admin/keys/${k.id}/${act}`)
    if (act === 'check') {
      keyState.value[k.id] = {
        loading: false,
        result: resp.check_status === 'active' ? 'success' : 'error',
        checkStatus: resp.check_status ?? '',
        metadata: resp.metadata ?? '',
      }
    } else {
      const labels: Record<string, string> = { disable: '已禁用', enable: '已启用' }
      toast.success(`key #${k.id} ${labels[act]}`)
    }
    await load()
  } catch (e: any) {
    if (act === 'check') {
      keyState.value[k.id] = { loading: false, result: 'error', checkStatus: '', metadata: '' }
    } else {
      toast.error(`key #${k.id} ${act} 失败: ${e.message}`)
    }
  }
}

const keyStatuses = [
  { value: 'active', label: '激活', icon: CircleCheck },
  { value: 'idle', label: '空闲', icon: CircleDashed },
  { value: 'invalid', label: '无效', icon: Ban },
]

async function setKeyStatus(k: KeyRow, status: string) {
  if (k.status === status) return
  try {
    await api.post(`/api/admin/keys/${k.id}/status`, { status })
    toast.success(`key #${k.id} 已设为${keyStatuses.find((s) => s.value === status)?.label ?? status}`)
    await load()
  } catch (e: any) {
    toast.error(`key #${k.id} 状态更新失败: ${e.message}`)
  }
}

async function deleteKey(k: KeyRow) {
  try {
    await api.delete(`/api/admin/keys/${k.id}`)
    toast.success(`key #${k.id} 已删除`)
    await load()
  } catch (e: any) {
    toast.error(`key #${k.id} 删除失败: ${e.message}`)
  }
}

async function checkAll() {
  checking.value = true
  let ok = 0, fail = 0
  for (const k of detail.value?.keys ?? []) {
    try {
      const resp = await api.post<{ check_status: string }>(`/api/admin/keys/${k.id}/check`)
      if (resp.check_status === 'active') { ok++ } else { fail++ }
    } catch {
      fail++
    }
  }
  if (fail > 0) {
    toast.warning(`check 完成：成功 ${ok}，失败 ${fail}`)
  } else {
    toast.success(`check 完成：全部 ${ok} 个通过`)
  }
  checking.value = false
  await load()
}

function fmtTime(s?: string) {
  return s ? new Date(s).toLocaleString() : '—'
}

async function setProviderEnabled(enabled: boolean) {
  if (!detail.value || providerEnabledSaving.value) return
  providerEnabledSaving.value = true
  try {
    const manifest = (YAML.parse(detail.value.manifest_yaml) || {}) as Record<string, unknown>
    manifest.enabled = enabled
    const yaml = YAML.stringify(manifest, { lineWidth: 0 })
    await api.put(`/api/admin/providers/${name}/manifest`, { yaml })
    toast.success(enabled ? '提供商已启用' : '提供商已停用')
    await load()
  } catch (e: any) {
    toast.error(`更新失败: ${e.message}`)
  } finally {
    providerEnabledSaving.value = false
  }
}

// ---------- manifest 编辑 ----------
interface MForm {
  display_name: string
  description: string
  base_url: string
  format: string
  key_strategy: string
  default_check_model: string
  whitelist: string
  blacklist: string
  register: string
  check: string
  register_timeout: number
  check_timeout: number
  priority: number
  weight: number
  hedge: boolean
}

const manifestOpen = ref(false)
const manifestYaml = ref('')
const mTab = ref<'visual' | 'source'>('visual')
const mParseError = ref('')
const mSaving = ref(false)
let mRaw: Record<string, any> = {} // 保留未覆盖字段（如内嵌 rules）
let mSyncing = false

const mForm = reactive<MForm>({
  display_name: '', description: '', base_url: '', format: 'openai-compatible',
  key_strategy: 'sticky', default_check_model: '', whitelist: '', blacklist: '',
  register: '', check: '', register_timeout: 300, check_timeout: 60,
  priority: 100, weight: 1, hedge: false,
})

function listText(v: any): string {
  return Array.isArray(v) ? v.map(String).join('\n') : ''
}
function splitList(s: string): string[] {
  return s.split(/[,\n]/).map((x) => x.trim()).filter(Boolean)
}
function mNum(v: any, dflt: number): number {
  const n = Number(v)
  return Number.isFinite(n) ? n : dflt
}

function parseManifest(text: string) {
  const doc = (YAML.parse(text) || {}) as Record<string, any>
  mRaw = doc
  const sc = doc.scripts ?? {}
  const rt = doc.routing ?? {}
  mForm.display_name = doc.display_name ?? ''
  mForm.description = doc.description ?? ''
  mForm.base_url = doc.base_url ?? ''
  mForm.format = doc.format ?? 'openai-compatible'
  mForm.key_strategy = doc.key_strategy ?? 'sticky'
  mForm.default_check_model = doc.default_check_model ?? ''
  mForm.whitelist = listText(doc.model_whitelist)
  mForm.blacklist = listText(doc.model_blacklist)
  mForm.register = sc.register ?? ''
  mForm.check = sc.check ?? ''
  mForm.register_timeout = mNum(sc.register_timeout, 300)
  mForm.check_timeout = mNum(sc.check_timeout, 60)
  mForm.priority = mNum(rt.priority, 100)
  mForm.weight = mNum(rt.weight, 1)
  mForm.hedge = rt.hedge ?? false
}

function serializeManifest(): string {
  const obj: Record<string, any> = { ...mRaw }
  const setOrDel = (k: string, v: any, empty: boolean) => {
    if (empty) delete obj[k]
    else obj[k] = v
  }
  setOrDel('display_name', mForm.display_name, !mForm.display_name)
  setOrDel('description', mForm.description, !mForm.description)
  obj.base_url = mForm.base_url
  setOrDel('format', mForm.format, !mForm.format)
  obj.key_strategy = mForm.key_strategy
  setOrDel('default_check_model', mForm.default_check_model, !mForm.default_check_model)
  setOrDel('model_whitelist', splitList(mForm.whitelist), !mForm.whitelist.trim())
  setOrDel('model_blacklist', splitList(mForm.blacklist), !mForm.blacklist.trim())
  const sc: Record<string, any> = { ...(mRaw.scripts ?? {}) }
  const setSc = (k: string, v: any, empty: boolean) => { if (empty) delete sc[k]; else sc[k] = v }
  setSc('register', mForm.register, !mForm.register)
  setSc('check', mForm.check, !mForm.check)
  setSc('register_timeout', mForm.register_timeout, mForm.register_timeout === 300)
  setSc('check_timeout', mForm.check_timeout, mForm.check_timeout === 60)
  setOrDel('scripts', sc, !Object.keys(sc).length)
  const rt: Record<string, any> = { ...(mRaw.routing ?? {}) }
  rt.priority = mForm.priority
  rt.weight = mForm.weight
  if (mForm.hedge) rt.hedge = true
  else delete rt.hedge
  obj.routing = rt
  return YAML.stringify(obj, { lineWidth: 0 })
}

function refreshMForm() {
  try {
    parseManifest(manifestYaml.value)
    mParseError.value = ''
  } catch (e: any) {
    mParseError.value = `YAML 解析失败，请在「源文件」修正：${e.message}`
  }
}
function serializeMForm() {
  mSyncing = true
  manifestYaml.value = serializeManifest()
  mSyncing = false
}

watch(mForm, () => {
  if (manifestOpen.value && mTab.value === 'visual' && !mSyncing) serializeMForm()
}, { deep: true })
watch(mTab, (t) => {
  if (t === 'visual') refreshMForm()
})

function openManifest() {
  manifestYaml.value = detail.value?.manifest_yaml ?? ''
  mTab.value = 'visual'
  refreshMForm()
  manifestOpen.value = true
}

async function saveManifest() {
  if (mTab.value === 'visual') serializeMForm()
  mSaving.value = true
  try {
    await api.put(`/api/admin/providers/${name}/manifest`, { yaml: manifestYaml.value })
    manifestOpen.value = false
    toast.success('manifest 已保存并生效')
    await load()
  } catch (e: any) {
    toast.error(`保存失败: ${e.message}`)
  } finally {
    mSaving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="detail" class="space-y-4">
    <div class="flex items-center justify-between flex-wrap gap-2">
      <div class="flex items-center gap-2">
        <Button as-child variant="ghost" size="icon-sm" title="返回提供商列表">
          <RouterLink to="/providers"><ArrowLeft class="size-4" /></RouterLink>
        </Button>
        <h1 class="text-xl font-semibold">{{ detail.manifest.display_name || name }}</h1>
        <Switch
          :model-value="detail.manifest.enabled"
          :disabled="providerEnabledSaving"
          aria-label="启用提供商"
          title="启用 / 停用提供商"
          @update:model-value="setProviderEnabled"
        />
      </div>
      <div class="flex gap-2">
        <Button v-if="detail.manifest.has_check" variant="outline" size="sm" :disabled="checking" @click="checkAll">
          <Stethoscope class="size-4" :class="checking && 'animate-pulse'" /> 全部 check
        </Button>
        <Button variant="outline" size="sm" :disabled="refreshing" @click="refreshModels">
          <RefreshCw class="size-4" :class="refreshing && 'animate-spin'" /> 刷新模型
        </Button>
        <Button variant="outline" size="sm" @click="openManifest">
          <FileCog class="size-4" /> 修改 manifest
        </Button>
        <Button variant="outline" size="sm" @click="importOpen = true">
          <Plus class="size-4" /> 添加 Key
        </Button>
        <Button v-if="detail.manifest.has_register" size="sm" :disabled="registering" @click="register">
          <Plus class="size-4" :class="registering && 'animate-pulse'" /> 注册新 Key
        </Button>
      </div>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <Card title="Manifest">
        <dl class="grid grid-cols-2 gap-y-2 text-sm">
          <dt class="text-muted-foreground">base_url</dt><dd class="font-mono text-xs break-all">{{ detail.manifest.base_url }}</dd>
          <dt class="text-muted-foreground">key_strategy</dt><dd>{{ detail.manifest.key_strategy }}</dd>
          <dt class="text-muted-foreground">priority / weight</dt><dd>{{ detail.manifest.priority }} / {{ detail.manifest.weight }}</dd>
          <dt class="text-muted-foreground">注册脚本</dt><dd>{{ detail.manifest.has_register ? '有' : '无' }}</dd>
          <dt class="text-muted-foreground">check 脚本</dt><dd>{{ detail.manifest.has_check ? '有' : '无' }}</dd>
          <template v-if="!detail.manifest.has_check">
            <dt class="text-muted-foreground">默认测试模型</dt>
            <dd class="font-mono text-xs break-all">{{ detail.manifest.default_check_model || '（未设置，check 时随机选 healthy 模型）' }}</dd>
          </template>
        </dl>
      </Card>

      <Card title="注册状态">
        <dl v-if="detail.manifest.has_register" class="grid grid-cols-2 gap-y-2 text-sm">
          <dt class="text-muted-foreground">上次注册</dt><dd>{{ fmtTime(detail.last_register_at) }}</dd>
          <dt class="text-muted-foreground">耗时</dt><dd>{{ detail.last_register_duration > 0 ? detail.last_register_duration + 'ms' : '—' }}</dd>
          <dt class="text-muted-foreground">上次错误</dt>
          <dd :class="detail.last_register_error && 'text-destructive'">{{ detail.last_register_error || '无' }}</dd>
        </dl>
        <p v-else class="text-sm text-muted-foreground">无注册脚本</p>
      </Card>
    </div>

    <Card :title="`模型 (${detail.models?.length ?? 0})，拉取于 ${fmtTime(detail.models_fetched_at)}`">
      <div class="flex flex-wrap gap-1.5">
        <Badge v-for="m in detail.models" :key="m" variant="secondary" class="font-mono">{{ m }}</Badge>
        <span v-if="!detail.models?.length" class="text-sm text-muted-foreground">无模型</span>
      </div>
    </Card>

    <Card :title="`Keys (${detail.keys?.length ?? 0})`">
      <div class="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead><TableHead>Key</TableHead><TableHead>Email</TableHead>
              <TableHead>状态</TableHead><TableHead>限流次数</TableHead><TableHead>最后使用</TableHead>
              <TableHead class="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="k in detail.keys" :key="k.id">
              <TableCell class="text-muted-foreground">#{{ k.id }}</TableCell>
              <TableCell>
                <div class="flex items-center gap-1">
                  <span class="font-mono text-xs truncate max-w-48" :title="k.api_key">{{ k.api_key }}</span>
                  <CopyButton :value="k.api_key" label="Key" />
                </div>
              </TableCell>
              <TableCell>
                <div v-if="k.email" class="flex items-center gap-1">
                  <span class="font-mono text-xs truncate max-w-48" :title="k.email">{{ k.email }}</span>
                  <CopyButton :value="k.email" label="Email" />
                </div>
                <span v-else class="text-muted-foreground">—</span>
              </TableCell>
              <TableCell><StatusBadge :status="k.status" /></TableCell>
              <TableCell>{{ k.rate_limit_count }}</TableCell>
              <TableCell class="text-xs">{{ fmtTime(k.last_used_at) }}</TableCell>
              <TableCell>
                <div class="flex justify-end gap-1">
                  <Tooltip>
                    <TooltipTrigger as-child>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        class="cursor-pointer"
                        :disabled="keyState[k.id]?.loading"
                        @click="keyAction(k, 'check')"
                      >
                        <span
                          class="inline-flex size-4 transition-all duration-300"
                          :class="keyState[k.id]?.result ? 'scale-100' : 'scale-100'"
                        >
                          <Stethoscope
                            v-if="!keyState[k.id]?.result && !keyState[k.id]?.loading"
                            class="size-4"
                          />
                          <Stethoscope
                            v-else-if="keyState[k.id]?.loading"
                            class="size-4 animate-pulse"
                          />
                          <Check
                            v-else-if="keyState[k.id]?.result === 'success'"
                            class="size-4 text-success animate-scale-in-center"
                          />
                          <X
                            v-else-if="keyState[k.id]?.result === 'error'"
                            class="size-4 text-destructive animate-scale-in-center"
                          />
                        </span>
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent
                      v-if="keyState[k.id]?.result === 'success'"
                      class="text-xs max-w-64"
                    >
                      <div class="space-y-1">
                        <p>检查通过：{{ keyState[k.id]?.checkStatus }}</p>
                        <p v-if="keyState[k.id]?.metadata" class="break-all">metadata: <code class="font-mono text-2xs">{{ keyState[k.id]?.metadata }}</code></p>
                        <p>点击再次检查</p>
                      </div>
                    </TooltipContent>
                    <TooltipContent
                      v-else-if="keyState[k.id]?.result === 'error'"
                      class="text-xs max-w-64"
                    >
                      <div class="space-y-1">
                         <p>状态异常：{{ keyState[k.id]?.checkStatus }}</p>
                        <p v-if="keyState[k.id]?.metadata" class="break-all">metadata: <code class="font-mono text-2xs">{{ keyState[k.id]?.metadata }}</code></p>
                        <p>点击再次检查</p>
                      </div>
                    </TooltipContent>
                    <TooltipContent v-else class="text-xs">
                      <span>检查</span>
                    </TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger as-child>
                      <span class="inline-flex">
                        <DropdownMenu :modal="false">
                          <DropdownMenuTrigger as-child>
                            <Button variant="ghost" size="icon-sm" class="cursor-pointer" :disabled="keyState[k.id]?.loading">
                              <MoreVertical class="size-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuSub>
                              <DropdownMenuSubTrigger class="cursor-pointer">
                                <SlidersHorizontal />
                                更改状态
                              </DropdownMenuSubTrigger>
                              <DropdownMenuSubContent>
                                <DropdownMenuItem
                                  v-for="status in keyStatuses"
                                  :key="status.value"
                                  :disabled="k.status === status.value"
                                  class="cursor-pointer"
                                  @select="setKeyStatus(k, status.value)"
                                >
                                  <component :is="status.icon" />
                                  {{ status.label }}
                                  <Check v-if="k.status === status.value" class="ml-auto" />
                                </DropdownMenuItem>
                              </DropdownMenuSubContent>
                            </DropdownMenuSub>
                            <DropdownMenuItem variant="destructive" class="cursor-pointer" @select="deleteKey(k)">
                              <Trash2 />
                              删除
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </span>
                    </TooltipTrigger>
                    <TooltipContent class="text-xs">更多操作</TooltipContent>
                  </Tooltip>
                </div>
              </TableCell>
            </TableRow>
            <TableEmpty v-if="!detail.keys?.length" :colspan="7">暂无 key</TableEmpty>
          </TableBody>
        </Table>
      </div>
    </Card>

    <Dialog v-model:open="importOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>添加 Key</DialogTitle>
          <DialogDescription>每行输入一个 key，保存后直接写入数据库。</DialogDescription>
        </DialogHeader>
        <Textarea v-model="importKeys" :rows="10" class="max-h-[60vh] overflow-y-auto font-mono text-xs" placeholder="key-one\nkey-two" spellcheck="false" />
        <DialogFooter>
          <Button variant="outline" @click="importOpen = false">取消</Button>
          <Button :disabled="importing || !importKeys.trim()" @click="importKeysToDatabase">
            <Save class="size-4" /> 保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- manifest 编辑器 -->
    <Dialog v-model:open="manifestOpen">
      <DialogContent class="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>修改 manifest</DialogTitle>
          <DialogDescription>编辑 providers/{{ name }}/manifest.yaml，保存后自动校验并热重载。</DialogDescription>
        </DialogHeader>

        <Tabs v-model="mTab">
          <TabsList>
            <TabsTrigger value="visual">可视化</TabsTrigger>
            <TabsTrigger value="source">源文件</TabsTrigger>
          </TabsList>

          <TabsContent value="visual" class="max-h-[60vh] space-y-3 overflow-y-auto overflow-x-hidden px-1 [scrollbar-gutter:stable]">
            <Alert v-if="mParseError" variant="destructive">
              <AlertDescription>{{ mParseError }}</AlertDescription>
            </Alert>

            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">显示名 (display_name)</Label>
                <Input v-model="mForm.display_name" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">格式 (format)</Label>
                <Input v-model="mForm.format" class="font-mono" />
              </div>
              <div class="col-span-2 space-y-1">
                <Label class="text-xs text-muted-foreground">描述 (description)</Label>
                <Input v-model="mForm.description" />
              </div>
              <div class="col-span-2 space-y-1">
                <Label class="text-xs text-muted-foreground">base_url</Label>
                <Input v-model="mForm.base_url" class="font-mono" />
              </div>
              <div class="col-span-2 space-y-1">
                <Label class="text-xs text-muted-foreground">Key 策略 (key_strategy)</Label>
                <Select v-model="mForm.key_strategy">
                  <SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="sticky">sticky</SelectItem>
                    <SelectItem value="round-robin">round-robin</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="col-span-2 space-y-1">
                <Label class="text-xs text-muted-foreground">
                  默认测试模型 (default_check_model)
                  <span v-if="mForm.check" class="text-destructive">· check 脚本存在时不生效</span>
                </Label>
                <Input
                  v-model="mForm.default_check_model"
                  class="font-mono"
                  :disabled="!!mForm.check"
                  placeholder="留空时 check 随机选 healthy 模型"
                />
              </div>
            </div>

            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">模型白名单（每行一个）</Label>
                <Textarea v-model="mForm.whitelist" :rows="4" class="font-mono text-xs" spellcheck="false" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">模型黑名单（每行一个）</Label>
                <Textarea v-model="mForm.blacklist" :rows="4" class="font-mono text-xs" spellcheck="false" />
              </div>
            </div>

            <Card title="脚本 (scripts)">
              <div class="grid grid-cols-2 gap-3">
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">register</Label>
                  <Input v-model="mForm.register" class="font-mono" placeholder="register.sh" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">check</Label>
                  <Input v-model="mForm.check" class="font-mono" placeholder="check.sh" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">register 超时(秒)</Label>
                  <Input v-model.number="mForm.register_timeout" type="number" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">check 超时(秒)</Label>
                  <Input v-model.number="mForm.check_timeout" type="number" />
                </div>
              </div>
            </Card>

            <Card title="路由 (routing)">
              <div class="grid grid-cols-3 gap-3">
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">优先级</Label>
                  <Input v-model.number="mForm.priority" type="number" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">权重</Label>
                  <Input v-model.number="mForm.weight" type="number" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">hedge</Label>
                  <div class="flex h-8 items-center">
                    <Switch v-model="mForm.hedge" />
                  </div>
                </div>
              </div>
            </Card>

            <p class="text-xs text-muted-foreground">内嵌 rules 等其它字段会原样保留，可在「源文件」查看。</p>
          </TabsContent>

          <TabsContent value="source">
            <Textarea v-model="manifestYaml" :rows="22" class="font-mono text-xs leading-relaxed max-h-[60vh]" spellcheck="false" />
          </TabsContent>
        </Tabs>

        <DialogFooter>
          <Button variant="outline" @click="manifestOpen = false">取消</Button>
          <Button :disabled="mSaving" @click="saveManifest">
            <Save class="size-4" /> 保存并生效
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>

  <div v-else class="space-y-4">
    <div class="flex items-center gap-2">
      <Skeleton class="size-8 rounded-md" />
      <Skeleton class="h-7 w-48" />
    </div>
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <Skeleton class="h-56 w-full rounded-xl" />
      <Skeleton class="h-56 w-full rounded-xl" />
    </div>
    <Skeleton class="h-64 w-full rounded-xl" />
  </div>
</template>
