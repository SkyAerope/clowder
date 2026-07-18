<script setup lang="ts">
import { ref, reactive, watch, onMounted } from 'vue'
import * as YAML from 'yaml'
import { api } from '../api'
import Card from '../components/Card.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Save, RotateCw, Plus, Trash2 } from 'lucide-vue-next'

// ---------- 可视化表单模型 ----------
interface CfgForm {
  bind_addr: string
  port: number
  api_key: string
  providers_dir: string
  dashboard: { public: boolean; admin: boolean; admin_key: string }
  pool: {
    min_active_keys: number
    max_total_keys: number
    auto_register: boolean
    register_interval: number
    register_backoff_base_seconds: number
    register_backoff_max_seconds: number
  }
  routing: { max_retries: number; max_parallel_attempts: number; upstream_streaming: boolean }
  backoff: { base_seconds: number; max_seconds: number }
  capture: { redact: boolean; max_records: number; response_buffer_kb: number }
}

interface ModelEntry { canonical: string; aliases: string }
interface ModelPattern { match: string; canonical: string }

function defaultCfg(): CfgForm {
  return {
    bind_addr: '0.0.0.0',
    port: 12100,
    api_key: '',
    providers_dir: 'providers/',
    dashboard: { public: true, admin: true, admin_key: '' },
    pool: {
      min_active_keys: 5,
      max_total_keys: 20,
      auto_register: false,
      register_interval: 300,
      register_backoff_base_seconds: 60,
      register_backoff_max_seconds: 3600,
    },
    routing: { max_retries: 10, max_parallel_attempts: 2, upstream_streaming: true },
    backoff: { base_seconds: 5, max_seconds: 600 },
    capture: { redact: false, max_records: 1000, response_buffer_kb: 256 },
  }
}

const configYaml = ref('')
const modelsYaml = ref('')
const cfg = reactive<CfgForm>(defaultCfg())
const modelEntries = ref<ModelEntry[]>([])
const modelPatterns = ref<ModelPattern[]>([])

const msg = ref('')
const saving = ref(false)
const reloading = ref(false)
const loaded = ref(false)
const tab = ref<'visual' | 'source'>('visual')
const parseError = ref('')

let syncing = false // 防止表单<->源文件互相触发的递归

// ---------- 源文件 -> 表单 ----------
function num(v: any, dflt: number): number {
  const n = Number(v)
  return Number.isFinite(n) ? n : dflt
}

function parseConfig(text: string) {
  const d = defaultCfg()
  const doc = (YAML.parse(text) || {}) as any
  d.bind_addr = doc.bind_addr ?? d.bind_addr
  d.port = num(doc.port, d.port)
  d.api_key = doc.api_key ?? ''
  d.providers_dir = doc.providers_dir ?? d.providers_dir
  const dash = doc.dashboard ?? {}
  d.dashboard.public = dash.public ?? d.dashboard.public
  d.dashboard.admin = dash.admin ?? d.dashboard.admin
  d.dashboard.admin_key = dash.admin_key ?? ''
  const pool = doc.pool ?? {}
  d.pool.min_active_keys = num(pool.min_active_keys, d.pool.min_active_keys)
  d.pool.max_total_keys = num(pool.max_total_keys, d.pool.max_total_keys)
  d.pool.auto_register = pool.auto_register ?? d.pool.auto_register
  d.pool.register_interval = num(pool.register_interval, d.pool.register_interval)
  d.pool.register_backoff_base_seconds = num(pool.register_backoff_base_seconds, d.pool.register_backoff_base_seconds)
  d.pool.register_backoff_max_seconds = num(pool.register_backoff_max_seconds, d.pool.register_backoff_max_seconds)
  const rt = doc.routing ?? {}
  d.routing.max_retries = num(rt.max_retries, d.routing.max_retries)
  d.routing.max_parallel_attempts = num(rt.max_parallel_attempts, d.routing.max_parallel_attempts)
  d.routing.upstream_streaming = rt.upstream_streaming ?? d.routing.upstream_streaming
  const bo = doc.backoff ?? {}
  d.backoff.base_seconds = num(bo.base_seconds, d.backoff.base_seconds)
  d.backoff.max_seconds = num(bo.max_seconds, d.backoff.max_seconds)
  const cap = doc.capture ?? {}
  d.capture.redact = cap.redact ?? d.capture.redact
  d.capture.max_records = num(cap.max_records, d.capture.max_records)
  d.capture.response_buffer_kb = num(cap.response_buffer_kb, d.capture.response_buffer_kb)
  Object.assign(cfg, d)
}

function parseModels(text: string) {
  const doc = (YAML.parse(text) || {}) as any
  const entries: ModelEntry[] = []
  const patterns: ModelPattern[] = []
  for (const key of Object.keys(doc)) {
    if (key === 'patterns') {
      const raws: any[] = Array.isArray(doc.patterns) ? doc.patterns : []
      for (const p of raws) patterns.push({ match: String(p?.match ?? ''), canonical: String(p?.canonical ?? '') })
      continue
    }
    const aliases: any[] = Array.isArray(doc[key]) ? doc[key] : []
    entries.push({ canonical: key, aliases: aliases.map(String).join(', ') })
  }
  modelEntries.value = entries
  modelPatterns.value = patterns
}

// ---------- 表单 -> 源文件 ----------
function serializeConfig(): string {
  const obj: any = {
    bind_addr: cfg.bind_addr,
    port: cfg.port,
    api_key: cfg.api_key,
    dashboard: {
      public: cfg.dashboard.public,
      admin: cfg.dashboard.admin,
      admin_key: cfg.dashboard.admin_key,
    },
    providers_dir: cfg.providers_dir,
    pool: {
      min_active_keys: cfg.pool.min_active_keys,
      max_total_keys: cfg.pool.max_total_keys,
      auto_register: cfg.pool.auto_register,
      register_interval: cfg.pool.register_interval,
      register_backoff_base_seconds: cfg.pool.register_backoff_base_seconds,
      register_backoff_max_seconds: cfg.pool.register_backoff_max_seconds,
    },
    routing: {
      max_retries: cfg.routing.max_retries,
      max_parallel_attempts: cfg.routing.max_parallel_attempts,
      upstream_streaming: cfg.routing.upstream_streaming,
    },
    backoff: {
      base_seconds: cfg.backoff.base_seconds,
      max_seconds: cfg.backoff.max_seconds,
    },
    capture: {
      redact: cfg.capture.redact,
      max_records: cfg.capture.max_records,
      response_buffer_kb: cfg.capture.response_buffer_kb,
    },
  }
  return YAML.stringify(obj, { lineWidth: 0 })
}

function splitAliases(s: string): string[] {
  return s.split(/[,\n]/).map((x) => x.trim()).filter(Boolean)
}

function serializeModels(): string {
  const obj: any = {}
  for (const e of modelEntries.value) {
    if (!e.canonical.trim()) continue
    obj[e.canonical.trim()] = splitAliases(e.aliases)
  }
  const pats = modelPatterns.value.filter((p) => p.match.trim() || p.canonical.trim())
  if (pats.length) {
    obj.patterns = pats.map((p) => ({ match: p.match.trim(), canonical: p.canonical.trim() }))
  }
  if (!Object.keys(obj).length) return ''
  return YAML.stringify(obj, { lineWidth: 0 })
}

// ---------- 同步 ----------
function refreshForms() {
  try {
    parseConfig(configYaml.value)
    parseModels(modelsYaml.value)
    parseError.value = ''
  } catch (e: any) {
    parseError.value = `YAML 解析失败，请在「源文件」修正后再切换：${e.message}`
  }
}

function serializeForms() {
  syncing = true
  configYaml.value = serializeConfig()
  modelsYaml.value = serializeModels()
  syncing = false
}

// 可视化编辑时，表单变化 -> 同步回源文件
watch([cfg, modelEntries, modelPatterns], () => {
  if (tab.value === 'visual' && !syncing) serializeForms()
}, { deep: true })

// 切换到可视化 -> 用最新源文件重建表单
watch(tab, (t) => {
  if (t === 'visual') refreshForms()
})

function addEntry() { modelEntries.value.push({ canonical: '', aliases: '' }) }
function removeEntry(i: number) { modelEntries.value.splice(i, 1) }
function addPattern() { modelPatterns.value.push({ match: '', canonical: '' }) }
function removePattern(i: number) { modelPatterns.value.splice(i, 1) }

// ---------- 加载/保存/重载 ----------
async function load() {
  try {
    const r = await api.get<{ config_yaml: string; models_yaml: string }>('/api/admin/config')
    configYaml.value = r.config_yaml
    modelsYaml.value = r.models_yaml
    refreshForms()
  } finally {
    loaded.value = true
  }
}

async function save() {
  if (tab.value === 'visual') serializeForms()
  saving.value = true
  msg.value = ''
  try {
    await api.put('/api/admin/config', { config_yaml: configYaml.value, models_yaml: modelsYaml.value })
    msg.value = '已保存并热重载（端口等冷字段需重启进程生效）'
  } catch (e: any) {
    msg.value = `保存失败: ${e.message}`
  } finally {
    saving.value = false
  }
}

async function reload() {
  reloading.value = true
  msg.value = ''
  try {
    await api.post('/api/admin/reload')
    msg.value = '已重载'
    await load()
  } catch (e: any) {
    msg.value = `重载失败: ${e.message}`
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

    <Alert v-if="msg" :variant="msg.includes('失败') ? 'destructive' : 'default'">
      <AlertDescription>{{ msg }}</AlertDescription>
    </Alert>

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

        <div class="grid grid-cols-1 xl:grid-cols-2 gap-4">
          <!-- 基础 -->
          <Card title="基础">
            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">监听地址 (bind_addr)</Label>
                <Input v-model="cfg.bind_addr" class="font-mono" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">端口 (port)</Label>
                <Input v-model.number="cfg.port" type="number" />
              </div>
              <div class="col-span-2 space-y-1">
                <Label class="text-xs text-muted-foreground">API Key (api_key)</Label>
                <Input v-model="cfg.api_key" class="font-mono" />
              </div>
              <div class="col-span-2 space-y-1">
                <Label class="text-xs text-muted-foreground">Providers 目录 (providers_dir)</Label>
                <Input v-model="cfg.providers_dir" class="font-mono" />
              </div>
            </div>
          </Card>

          <!-- 面板 -->
          <Card title="面板 (dashboard)">
            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">公开访问 (public)</Label>
                <div class="flex h-8 items-center">
                  <Switch v-model="cfg.dashboard.public" />
                </div>
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">启用管理 (admin)</Label>
                <div class="flex h-8 items-center">
                  <Switch v-model="cfg.dashboard.admin" />
                </div>
              </div>
              <div class="col-span-2 space-y-1">
                <Label class="text-xs text-muted-foreground">管理密钥 (admin_key)</Label>
                <Input v-model="cfg.dashboard.admin_key" class="font-mono" />
              </div>
            </div>
          </Card>

          <!-- 连接池 -->
          <Card title="连接池 (pool)">
            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">最小活跃 Key 数</Label>
                <Input v-model.number="cfg.pool.min_active_keys" type="number" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">最大 Key 总数</Label>
                <Input v-model.number="cfg.pool.max_total_keys" type="number" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">注册间隔(秒)</Label>
                <Input v-model.number="cfg.pool.register_interval" type="number" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">注册退避基数(秒)</Label>
                <Input v-model.number="cfg.pool.register_backoff_base_seconds" type="number" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">注册退避上限(秒，0=无)</Label>
                <Input v-model.number="cfg.pool.register_backoff_max_seconds" type="number" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">自动注册</Label>
                <div class="flex h-8 items-center">
                  <Switch v-model="cfg.pool.auto_register" />
                </div>
              </div>
            </div>
          </Card>

          <!-- 路由 -->
          <Card title="路由 (routing)">
            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">最大重试次数</Label>
                <Input v-model.number="cfg.routing.max_retries" type="number" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">最大并发尝试</Label>
                <Input v-model.number="cfg.routing.max_parallel_attempts" type="number" />
              </div>
              <div class="col-span-2 space-y-1">
                <Label class="text-xs text-muted-foreground">上游流式 (upstream_streaming)</Label>
                <div class="flex h-8 items-center">
                  <Switch v-model="cfg.routing.upstream_streaming" />
                </div>
              </div>
            </div>
          </Card>

          <!-- 退避 -->
          <Card title="退避 (backoff)">
            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">基数(秒)</Label>
                <Input v-model.number="cfg.backoff.base_seconds" type="number" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">上限(秒，0=无)</Label>
                <Input v-model.number="cfg.backoff.max_seconds" type="number" />
              </div>
            </div>
          </Card>

          <!-- 抓包 -->
          <Card title="抓包 (capture)">
            <div class="grid grid-cols-2 gap-3">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">最大记录数</Label>
                <Input v-model.number="cfg.capture.max_records" type="number" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">响应缓冲(KB)</Label>
                <Input v-model.number="cfg.capture.response_buffer_kb" type="number" />
              </div>
              <div class="col-span-2 space-y-1">
                <Label class="text-xs text-muted-foreground">脱敏 (redact)</Label>
                <div class="flex h-8 items-center">
                  <Switch v-model="cfg.capture.redact" />
                </div>
              </div>
            </div>
          </Card>
        </div>

        <!-- 模型映射 -->
        <Card title="models.yaml（模型映射）">
          <div class="space-y-4">
            <div class="space-y-2">
              <div class="flex items-center justify-between">
                <Label class="text-xs text-muted-foreground">客户端请求 ← 上游（逗号分隔）</Label>
                <Button variant="ghost" size="xs" @click="addEntry"><Plus class="size-3" /> 加映射</Button>
              </div>
              <p v-if="!modelEntries.length" class="text-xs text-muted-foreground">暂无映射</p>
              <div v-for="(e, i) in modelEntries" :key="i" class="flex flex-wrap items-center gap-2">
                <Input v-model="e.canonical" class="w-52 font-mono" placeholder="canonical name" />
                <span class="text-muted-foreground">←</span>
                <Input v-model="e.aliases" class="min-w-0 flex-1 font-mono" placeholder="alias-1, alias-2" />
                <Button variant="ghost" size="icon-sm" class="text-destructive shrink-0" @click="removeEntry(i)">
                  <Trash2 class="size-4" />
                </Button>
              </div>
            </div>

            <div class="space-y-2 border-t border-foreground/10 pt-3">
              <div class="flex items-center justify-between">
                <Label class="text-xs text-muted-foreground">正则映射 (patterns)</Label>
                <Button variant="ghost" size="xs" @click="addPattern"><Plus class="size-3" /> 加正则</Button>
              </div>
              <p v-if="!modelPatterns.length" class="text-xs text-muted-foreground">暂无正则规则</p>
              <div v-for="(p, i) in modelPatterns" :key="i" class="flex flex-wrap items-center gap-2">
                <Input v-model="p.canonical" class="w-52 font-mono" placeholder="canonical name" />
                <span class="text-muted-foreground">←</span>
                <Input v-model="p.match" class="min-w-0 flex-1 font-mono" placeholder="RE2 正则 (match)" />
                <Button variant="ghost" size="icon-sm" class="text-destructive shrink-0" @click="removePattern(i)">
                  <Trash2 class="size-4" />
                </Button>
              </div>
            </div>
          </div>
        </Card>
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
