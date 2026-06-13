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
import { RefreshCw, Plus, Stethoscope, Ban, CircleCheck, ArrowLeft, FileCog, Save } from 'lucide-vue-next'
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
const msg = ref('')
const registering = ref(false)
const refreshing = ref(false)
const checking = ref(false)
const busy = ref<Record<number, boolean>>({})

async function load() {
  detail.value = await api.get<Detail>(`/api/admin/providers/${name}`)
}

async function register() {
  registering.value = true
  msg.value = ''
  try {
    const r = await api.post<{ registered: number; error?: string }>('/api/admin/register', { provider: name, count: 1 })
    msg.value = r.error ? `注册失败: ${r.error}` : `已注册 ${r.registered} 个 key`
    await load()
  } catch (e: any) {
    msg.value = `注册失败: ${e.message}`
  } finally {
    registering.value = false
  }
}

async function refreshModels() {
  refreshing.value = true
  msg.value = ''
  try {
    await api.post(`/api/admin/providers/${name}/refresh-models`)
    msg.value = '模型已刷新'
    await load()
  } catch (e: any) {
    msg.value = `刷新失败: ${e.message}`
  } finally {
    refreshing.value = false
  }
}

async function keyAction(k: KeyRow, act: 'disable' | 'enable' | 'check') {
  busy.value[k.id] = true
  msg.value = ''
  try {
    await api.post(`/api/admin/keys/${k.id}/${act}`)
    await load()
  } catch (e: any) {
    msg.value = `key #${k.id} ${act} 失败: ${e.message}`
  } finally {
    busy.value[k.id] = false
  }
}

async function checkAll() {
  checking.value = true
  msg.value = ''
  let ok = 0, fail = 0
  for (const k of detail.value?.keys ?? []) {
    try {
      await api.post(`/api/admin/keys/${k.id}/check`)
      ok++
    } catch {
      fail++
    }
  }
  msg.value = `check 完成：成功 ${ok}，失败 ${fail}`
  checking.value = false
  await load()
}

function fmtTime(s?: string) {
  return s ? new Date(s).toLocaleString() : '—'
}

// ---------- manifest 编辑 ----------
interface MForm {
  display_name: string
  description: string
  base_url: string
  enabled: boolean
  format: string
  key_source: string
  key_strategy: string
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
  display_name: '', description: '', base_url: '', enabled: true, format: 'openai-compatible',
  key_source: 'static', key_strategy: 'sequential', whitelist: '', blacklist: '',
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
  mForm.enabled = doc.enabled ?? true
  mForm.format = doc.format ?? 'openai-compatible'
  mForm.key_source = doc.key_source ?? 'static'
  mForm.key_strategy = doc.key_strategy ?? 'sequential'
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
  obj.enabled = mForm.enabled
  setOrDel('format', mForm.format, !mForm.format)
  obj.key_source = mForm.key_source
  obj.key_strategy = mForm.key_strategy
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
  msg.value = ''
  try {
    await api.put(`/api/admin/providers/${name}/manifest`, { yaml: manifestYaml.value })
    manifestOpen.value = false
    msg.value = 'manifest 已保存并生效'
    await load()
  } catch (e: any) {
    msg.value = `保存失败: ${e.message}`
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
        <Button v-if="detail.manifest.key_source === 'script'" size="sm" :disabled="registering" @click="register">
          <Plus class="size-4" :class="registering && 'animate-pulse'" /> 注册新 Key
        </Button>
      </div>
    </div>

    <Alert v-if="msg" :variant="msg.includes('失败') ? 'destructive' : 'default'">
      <AlertDescription>{{ msg }}</AlertDescription>
    </Alert>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <Card title="Manifest">
        <dl class="grid grid-cols-2 gap-y-2 text-sm">
          <dt class="text-muted-foreground">base_url</dt><dd class="font-mono text-xs break-all">{{ detail.manifest.base_url }}</dd>
          <dt class="text-muted-foreground">key_source</dt><dd>{{ detail.manifest.key_source }}</dd>
          <dt class="text-muted-foreground">key_strategy</dt><dd>{{ detail.manifest.key_strategy }}</dd>
          <dt class="text-muted-foreground">priority / weight</dt><dd>{{ detail.manifest.priority }} / {{ detail.manifest.weight }}</dd>
          <dt class="text-muted-foreground">enabled</dt><dd>{{ detail.manifest.enabled }}</dd>
          <dt class="text-muted-foreground">check 脚本</dt><dd>{{ detail.manifest.has_check ? '有' : '无' }}</dd>
        </dl>
      </Card>

      <Card title="注册状态">
        <dl class="grid grid-cols-2 gap-y-2 text-sm">
          <dt class="text-muted-foreground">上次注册</dt><dd>{{ fmtTime(detail.last_register_at) }}</dd>
          <dt class="text-muted-foreground">耗时</dt><dd>{{ detail.last_register_duration > 0 ? detail.last_register_duration + 'ms' : '—' }}</dd>
          <dt class="text-muted-foreground">上次错误</dt>
          <dd :class="detail.last_register_error && 'text-destructive'">{{ detail.last_register_error || '无' }}</dd>
        </dl>
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
                  <Button variant="ghost" size="icon-sm" title="一键 check" :disabled="busy[k.id]" @click="keyAction(k, 'check')">
                    <Stethoscope class="size-4" :class="busy[k.id] && 'animate-pulse'" />
                  </Button>
                  <Button v-if="k.status !== 'invalid'" variant="ghost" size="icon-sm" class="text-destructive" title="禁用"
                    :disabled="busy[k.id]" @click="keyAction(k, 'disable')">
                    <Ban class="size-4" />
                  </Button>
                  <Button v-else variant="ghost" size="icon-sm" class="text-success" title="启用"
                    :disabled="busy[k.id]" @click="keyAction(k, 'enable')">
                    <CircleCheck class="size-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
            <TableEmpty v-if="!detail.keys?.length" :colspan="7">暂无 key</TableEmpty>
          </TableBody>
        </Table>
      </div>
    </Card>

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

          <TabsContent value="visual" class="max-h-[60vh] space-y-3 overflow-y-auto pr-1">
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
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">Key 来源 (key_source)</Label>
                <Select v-model="mForm.key_source">
                  <SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="static">static</SelectItem>
                    <SelectItem value="script">script</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">Key 策略 (key_strategy)</Label>
                <Select v-model="mForm.key_strategy">
                  <SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="sequential">sequential</SelectItem>
                    <SelectItem value="round-robin">round-robin</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="col-span-2 flex items-center justify-between">
                <Label class="text-xs text-muted-foreground">启用 (enabled)</Label>
                <Switch v-model="mForm.enabled" />
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

            <div class="space-y-2 rounded-lg border border-dashed border-foreground/20 p-3">
              <Label class="text-xs text-muted-foreground">脚本 (scripts)</Label>
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
            </div>

            <div class="space-y-2 rounded-lg border border-dashed border-foreground/20 p-3">
              <Label class="text-xs text-muted-foreground">路由 (routing)</Label>
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
            </div>

            <p class="text-xs text-muted-foreground">内嵌 rules 等其它字段会原样保留，可在「源文件」查看。</p>
          </TabsContent>

          <TabsContent value="source">
            <Textarea v-model="manifestYaml" :rows="22" class="font-mono text-xs leading-relaxed" spellcheck="false" />
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
