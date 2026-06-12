<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import * as YAML from 'yaml'
import { api, type RuleView, type RulePreset } from '../api'
import Card from '../components/Card.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Separator } from '@/components/ui/separator'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import { Save, TriangleAlert, Plus, Trash2 } from 'lucide-vue-next'

// ---------- 可视化编辑模型 ----------
type WhenType = 'upstream' | 'check' | 'schedule'
interface EditCond { field: string; op: string; value: number }
interface EditAction { kind: string; mark_status: string; chain: boolean; fields: string[] }
interface EditRule {
  name: string
  description: string
  priority: number
  whenType: WhenType
  provider: string
  model: string
  status_code: string
  body_contains: string
  body_regex: string
  transport: string
  ttft_over_ms: string
  error: boolean
  check_status: string
  cron: string
  scope_provider: string
  ifs: EditCond[]
  dos: EditAction[]
  routeMode: 'single' | 'race'
  routeSingle: string
  routeRace: string[]
}

const ROUTES = ['next_key', 'next_provider', 'retry_same', 'passthrough']
const RACE_ROUTES = ['next_key', 'next_provider', 'retry_same']
const TRANSPORTS = ['true', 'dns_failure', 'conn_refused', 'tls_error', 'connect_timeout', 'stream_cut', 'malformed']
const IF_FIELDS = ['attempt_no', 'key_switches', 'provider_switches']
const IF_OPS = ['eq', 'ne', 'gt', 'gte', 'lt', 'lte']
const ACTION_KINDS = [
  'mark_key', 'mark_model_unhealthy', 'mark_provider_unhealthy',
  'refresh_models', 'check_key', 'check_keys', 'capture', 'backoff_key',
]
const CAPTURE_FIELDS = ['request_headers', 'request_body', 'response_headers', 'response_body', 'status_code']

const yamlText = ref('')
const rules = ref<RuleView[]>([])
const warnings = ref<{ rule: string; message: string }[]>([])
const presets = ref<RulePreset[]>([])
const msg = ref('')
const saving = ref(false)
const tab = ref<'visual' | 'source'>('visual')
const draft = ref<EditRule[]>([])
const parseError = ref('')

const sourceNames = ['manifest', '用户', '内置'] as const

function blankRule(): EditRule {
  return {
    name: '', description: '', priority: 100, whenType: 'upstream',
    provider: '', model: '', status_code: '', body_contains: '', body_regex: '',
    transport: 'none', ttft_over_ms: '', error: false,
    check_status: 'invalid', cron: '', scope_provider: '',
    ifs: [], dos: [], routeMode: 'single', routeSingle: 'passthrough', routeRace: [],
  }
}

// ---------- YAML <-> 可视化 ----------
function parseToDraft(text: string): EditRule[] {
  const doc = YAML.parse(text) || {}
  const raws: any[] = Array.isArray(doc.rules) ? doc.rules : []
  return raws.map((rr) => {
    const e = blankRule()
    e.name = rr.name ?? ''
    e.description = rr.description ?? ''
    e.priority = typeof rr.priority === 'number' ? rr.priority : 100
    const w = rr.when ?? {}
    if (w.schedule) {
      e.whenType = 'schedule'
      e.cron = w.schedule.cron ?? ''
    } else if (w.check) {
      e.whenType = 'check'
      e.check_status = w.check.status ?? 'invalid'
    } else {
      e.whenType = 'upstream'
      e.provider = w.provider ?? ''
      e.model = w.model ?? ''
      e.status_code = w.status_code != null ? String(w.status_code) : ''
      e.body_contains = w.body_contains ?? ''
      e.body_regex = w.body_regex ?? ''
      e.transport = w.transport === true ? 'true' : (w.transport || 'none')
      e.ttft_over_ms = w.ttft_over_ms != null ? String(w.ttft_over_ms) : ''
      e.error = w.error === true
    }
    if (w.scope?.provider) e.scope_provider = w.scope.provider
    // if
    const iff = rr.if ?? {}
    for (const field of Object.keys(iff)) {
      const ops = iff[field] ?? {}
      for (const op of Object.keys(ops)) {
        e.ifs.push({ field, op, value: Number(ops[op]) || 0 })
      }
    }
    // do
    const dos: any[] = Array.isArray(rr.do) ? rr.do : []
    for (const dm of dos) {
      const kind = Object.keys(dm)[0]
      if (!kind) continue
      const v = dm[kind]
      const a: EditAction = { kind, mark_status: 'invalid', chain: false, fields: [] }
      if (kind === 'mark_key') a.mark_status = typeof v === 'string' ? v : 'invalid'
      if ((kind === 'check_key' || kind === 'check_keys') && v && typeof v === 'object') a.chain = !!v.chain
      if (kind === 'capture' && v && Array.isArray(v.fields)) a.fields = v.fields
      e.dos.push(a)
    }
    // route
    const rt = rr.route
    if (Array.isArray(rt)) {
      e.routeMode = 'race'; e.routeRace = rt.map(String)
    } else if (rt && typeof rt === 'object' && Array.isArray(rt.race)) {
      e.routeMode = 'race'; e.routeRace = rt.race.map(String)
    } else if (typeof rt === 'string') {
      e.routeMode = 'single'; e.routeSingle = rt
    }
    return e
  })
}

function draftToObj(list: EditRule[]): any {
  return {
    rules: list.map((e) => {
      const r: any = { name: e.name }
      if (e.description) r.description = e.description
      if (e.priority !== 100) r.priority = e.priority
      const w: any = {}
      if (e.whenType === 'schedule') {
        w.schedule = { cron: e.cron }
        if (e.scope_provider) w.scope = { provider: e.scope_provider }
      } else if (e.whenType === 'check') {
        w.check = { status: e.check_status }
      } else {
        if (e.provider) w.provider = e.provider
        if (e.model) w.model = e.model
        if (e.status_code) w.status_code = e.status_code
        if (e.body_contains) w.body_contains = e.body_contains
        if (e.body_regex) w.body_regex = e.body_regex
        if (e.transport === 'true') w.transport = true
        else if (e.transport && e.transport !== 'none') w.transport = e.transport
        if (e.ttft_over_ms) w.ttft_over_ms = Number(e.ttft_over_ms)
        if (e.error) w.error = true
      }
      r.when = w
      if (e.ifs.length) {
        const iff: any = {}
        for (const c of e.ifs) {
          iff[c.field] = iff[c.field] || {}
          iff[c.field][c.op] = c.value
        }
        r.if = iff
      }
      if (e.dos.length) {
        r.do = e.dos.map((a) => {
          if (a.kind === 'mark_key') return { mark_key: a.mark_status }
          if (a.kind === 'check_key' || a.kind === 'check_keys') return { [a.kind]: { chain: a.chain } }
          if (a.kind === 'capture') return a.fields.length ? { capture: { fields: a.fields } } : { capture: {} }
          return { [a.kind]: {} }
        })
      }
      if (e.routeMode === 'race') r.route = { race: e.routeRace.filter(Boolean) }
      else r.route = e.routeSingle
      return r
    }),
  }
}

function serializeDraft() {
  yamlText.value = YAML.stringify(draftToObj(draft.value), { lineWidth: 0 })
}

function refreshDraft() {
  try {
    draft.value = parseToDraft(yamlText.value)
    parseError.value = ''
  } catch (e: any) {
    parseError.value = `YAML 解析失败，请在「源文件」修正后再切换：${e.message}`
  }
}

// 可视化编辑时，draft 变化 -> 同步回 yamlText
watch(draft, () => {
  if (tab.value === 'visual') serializeDraft()
}, { deep: true })

// 切换到可视化 -> 用最新 yamlText 重建 draft
watch(tab, (t) => {
  if (t === 'visual') refreshDraft()
})

// ---------- 编辑操作 ----------
function addRule() { draft.value.push(blankRule()) }
function removeRule(i: number) { draft.value.splice(i, 1) }
function addCond(e: EditRule) { e.ifs.push({ field: 'attempt_no', op: 'gte', value: 2 }) }
function removeCond(e: EditRule, i: number) { e.ifs.splice(i, 1) }
function addAction(e: EditRule) { e.dos.push({ kind: 'capture', mark_status: 'invalid', chain: false, fields: [] }) }
function removeAction(e: EditRule, i: number) { e.dos.splice(i, 1) }
function toggleField(a: EditAction, f: string) {
  const i = a.fields.indexOf(f)
  if (i >= 0) a.fields.splice(i, 1)
  else a.fields.push(f)
}
function addRace(e: EditRule) { e.routeRace.push('next_key') }
function removeRace(e: EditRule, i: number) { e.routeRace.splice(i, 1) }

// ---------- 加载/保存/预设 ----------
async function load() {
  const r = await api.get<{ yaml: string; rules: RuleView[]; warnings: { rule: string; message: string }[] }>('/api/admin/rules')
  yamlText.value = r.yaml
  rules.value = r.rules ?? []
  warnings.value = r.warnings ?? []
  presets.value = (await api.get<RulePreset[]>('/api/admin/rules/presets')) ?? []
  refreshDraft()
}

async function save() {
  if (tab.value === 'visual') serializeDraft()
  saving.value = true
  msg.value = ''
  try {
    await api.put('/api/admin/rules', { yaml: yamlText.value })
    msg.value = '已保存并生效'
    await load()
  } catch (e: any) {
    msg.value = `保存失败: ${e.message}`
  } finally {
    saving.value = false
  }
}

function addPreset(p: RulePreset) {
  let y = yamlText.value
  if (!/^rules:/m.test(y)) y = 'rules:\n' + y
  // preset yaml 是顶层数组，缩进两格挂到 rules: 下
  const indented = p.yaml.split('\n').map(l => (l ? '  ' + l : l)).join('\n')
  yamlText.value = y.trimEnd() + '\n\n' + indented + '\n'
  if (tab.value === 'visual') refreshDraft()
  msg.value = `已插入 ${p.rule.name}，请检查后保存`
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <h1 class="text-xl font-semibold">规则管理</h1>
      <Button :disabled="saving" @click="save">
        <Save class="size-4" /> 保存并生效
      </Button>
    </div>

    <Alert v-if="msg" :variant="msg.includes('失败') ? 'destructive' : 'default'">
      <AlertDescription>{{ msg }}</AlertDescription>
    </Alert>

    <Alert v-if="warnings.length" variant="default" class="border-warning/40">
      <TriangleAlert class="size-4 text-warning" />
      <AlertTitle class="text-warning">校验警告</AlertTitle>
      <AlertDescription>
        <ul class="text-xs text-muted-foreground list-disc pl-4 space-y-0.5">
          <li v-for="(w, i) in warnings" :key="i">
            <span class="font-mono text-foreground">{{ w.rule }}</span>: {{ w.message }}
          </li>
        </ul>
      </AlertDescription>
    </Alert>

    <div class="grid grid-cols-1 xl:grid-cols-3 gap-4">
      <div class="xl:col-span-2">
        <Tabs v-model="tab">
          <TabsList>
            <TabsTrigger value="visual">可视化</TabsTrigger>
            <TabsTrigger value="source">源文件</TabsTrigger>
          </TabsList>

          <!-- 可视化编辑 -->
          <TabsContent value="visual" class="space-y-3">
            <Alert v-if="parseError" variant="destructive">
              <AlertDescription>{{ parseError }}</AlertDescription>
            </Alert>

            <Card v-for="(e, ri) in draft" :key="ri" class="p-4 space-y-3">
              <div class="flex items-center gap-2">
                <Input v-model="e.name" placeholder="规则名" class="font-medium" />
                <Button variant="ghost" size="icon-sm" class="text-destructive shrink-0" title="删除规则" @click="removeRule(ri)">
                  <Trash2 class="size-4" />
                </Button>
              </div>
              <Input v-model="e.description" placeholder="描述（可选）" />

              <div class="grid grid-cols-2 gap-3">
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">优先级（越小越优先）</Label>
                  <Input v-model.number="e.priority" type="number" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">触发类型 (when)</Label>
                  <Select v-model="e.whenType">
                    <SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="upstream">upstream 上游结果</SelectItem>
                      <SelectItem value="check">check 探活</SelectItem>
                      <SelectItem value="schedule">schedule 定时</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>

              <!-- upstream 字段 -->
              <div v-if="e.whenType === 'upstream'" class="grid grid-cols-2 gap-3 rounded-none border border-dashed p-3">
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">provider</Label>
                  <Input v-model="e.provider" placeholder="留空=任意" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">model</Label>
                  <Input v-model="e.model" placeholder="留空=任意" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">status_code</Label>
                  <Input v-model="e.status_code" placeholder="403 / 4xx / 5xx" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">ttft_over_ms</Label>
                  <Input v-model="e.ttft_over_ms" type="number" placeholder="慢响应阈值" />
                </div>
                <div class="space-y-1 col-span-2">
                  <Label class="text-xs text-muted-foreground">body_contains</Label>
                  <Input v-model="e.body_contains" placeholder="响应体包含文本" />
                </div>
                <div class="space-y-1 col-span-2">
                  <Label class="text-xs text-muted-foreground">body_regex</Label>
                  <Input v-model="e.body_regex" class="font-mono" placeholder="RE2 正则" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">transport 传输错误</Label>
                  <Select v-model="e.transport">
                    <SelectTrigger class="w-full"><SelectValue placeholder="不设" /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">不设</SelectItem>
                      <SelectItem v-for="t in TRANSPORTS" :key="t" :value="t">{{ t }}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div class="flex items-center gap-2 pt-5">
                  <Switch v-model="e.error" />
                  <Label class="text-xs text-muted-foreground">任意错误 (error)</Label>
                </div>
              </div>

              <!-- check 字段 -->
              <div v-else-if="e.whenType === 'check'" class="rounded-none border border-dashed p-3 space-y-1">
                <Label class="text-xs text-muted-foreground">探活状态命中</Label>
                <Select v-model="e.check_status">
                  <SelectTrigger class="w-full"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="active">active</SelectItem>
                    <SelectItem value="invalid">invalid</SelectItem>
                    <SelectItem value="rate_limited">rate_limited</SelectItem>
                    <SelectItem value="error">error</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <!-- schedule 字段 -->
              <div v-else class="grid grid-cols-2 gap-3 rounded-none border border-dashed p-3">
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">cron 表达式</Label>
                  <Input v-model="e.cron" class="font-mono" placeholder="0 */6 * * *" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">scope provider（可选）</Label>
                  <Input v-model="e.scope_provider" placeholder="限定 provider" />
                </div>
              </div>

              <!-- if 条件 -->
              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <Label class="text-xs text-muted-foreground">条件 (if)</Label>
                  <Button variant="ghost" size="xs" @click="addCond(e)"><Plus class="size-3" /> 加条件</Button>
                </div>
                <div v-for="(c, ci) in e.ifs" :key="ci" class="flex items-center gap-2">
                  <Select v-model="c.field">
                    <SelectTrigger class="w-44"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem v-for="f in IF_FIELDS" :key="f" :value="f">{{ f }}</SelectItem>
                    </SelectContent>
                  </Select>
                  <Select v-model="c.op">
                    <SelectTrigger class="w-24"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem v-for="o in IF_OPS" :key="o" :value="o">{{ o }}</SelectItem>
                    </SelectContent>
                  </Select>
                  <Input v-model.number="c.value" type="number" class="w-24" />
                  <Button variant="ghost" size="icon-sm" class="text-destructive" @click="removeCond(e, ci)">
                    <Trash2 class="size-4" />
                  </Button>
                </div>
              </div>

              <!-- do 动作 -->
              <div class="space-y-2">
                <div class="flex items-center justify-between">
                  <Label class="text-xs text-muted-foreground">动作 (do)</Label>
                  <Button variant="ghost" size="xs" @click="addAction(e)"><Plus class="size-3" /> 加动作</Button>
                </div>
                <div v-for="(a, ai) in e.dos" :key="ai" class="rounded-none border p-2 space-y-2">
                  <div class="flex items-center gap-2">
                    <Select v-model="a.kind">
                      <SelectTrigger class="w-56"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem v-for="k in ACTION_KINDS" :key="k" :value="k">{{ k }}</SelectItem>
                      </SelectContent>
                    </Select>
                    <Button variant="ghost" size="icon-sm" class="text-destructive ml-auto" @click="removeAction(e, ai)">
                      <Trash2 class="size-4" />
                    </Button>
                  </div>
                  <Select v-if="a.kind === 'mark_key'" v-model="a.mark_status">
                    <SelectTrigger class="w-56"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="invalid">invalid</SelectItem>
                      <SelectItem value="rate_limited">rate_limited</SelectItem>
                      <SelectItem value="active">active</SelectItem>
                    </SelectContent>
                  </Select>
                  <div v-else-if="a.kind === 'check_key' || a.kind === 'check_keys'" class="flex items-center gap-2">
                    <Switch v-model="a.chain" />
                    <Label class="text-xs text-muted-foreground">链式（chain）：探活后按结果再次匹配</Label>
                  </div>
                  <div v-else-if="a.kind === 'capture'" class="flex flex-wrap gap-1.5">
                    <Badge v-for="f in CAPTURE_FIELDS" :key="f"
                      :variant="a.fields.includes(f) ? 'default' : 'outline'"
                      class="cursor-pointer" @click="toggleField(a, f)">{{ f }}</Badge>
                    <span class="text-xs text-muted-foreground self-center">（不选=全抓）</span>
                  </div>
                </div>
              </div>

              <!-- route 路由 -->
              <div class="space-y-2">
                <div class="flex items-center gap-3">
                  <Label class="text-xs text-muted-foreground">路由 (route)</Label>
                  <Select v-model="e.routeMode">
                    <SelectTrigger class="w-32"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="single">单一</SelectItem>
                      <SelectItem value="race">竞速 race</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <Select v-if="e.routeMode === 'single'" v-model="e.routeSingle">
                  <SelectTrigger class="w-56"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="r in ROUTES" :key="r" :value="r">{{ r }}</SelectItem>
                  </SelectContent>
                </Select>
                <div v-else class="space-y-2">
                  <div v-for="(r, rci) in e.routeRace" :key="rci" class="flex items-center gap-2">
                    <Select v-model="e.routeRace[rci]">
                      <SelectTrigger class="w-56"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        <SelectItem v-for="opt in RACE_ROUTES" :key="opt" :value="opt">{{ opt }}</SelectItem>
                      </SelectContent>
                    </Select>
                    <Button variant="ghost" size="icon-sm" class="text-destructive" @click="removeRace(e, rci)">
                      <Trash2 class="size-4" />
                    </Button>
                  </div>
                  <Button variant="outline" size="xs" @click="addRace(e)"><Plus class="size-3" /> 加并发分支</Button>
                </div>
              </div>
            </Card>

            <Button variant="outline" class="w-full" @click="addRule">
              <Plus class="size-4" /> 新增规则
            </Button>
          </TabsContent>

          <!-- 源文件编辑 -->
          <TabsContent value="source">
            <Card title="rules.yaml">
              <Textarea v-model="yamlText" :rows="30" class="font-mono text-xs leading-relaxed" spellcheck="false" />
            </Card>
          </TabsContent>
        </Tabs>
      </div>

      <div class="space-y-4">
        <Card :title="`生效规则 (${rules.length})`">
          <div class="space-y-1.5 max-h-72 overflow-y-auto">
            <div v-for="r in rules" :key="r.name" class="flex items-center gap-2 text-xs">
              <Badge variant="secondary" class="shrink-0">{{ sourceNames[r.source] ?? r.source }}</Badge>
              <span class="font-mono truncate" :title="r.description">{{ r.name }}</span>
              <span class="ml-auto text-muted-foreground shrink-0">p{{ r.priority }}</span>
            </div>
          </div>
        </Card>

        <Card title="预设规则">
          <div class="space-y-2">
            <div v-for="p in presets" :key="String(p.rule.name)" class="flex items-center gap-2 text-sm">
              <span class="font-mono text-xs truncate" :title="String(p.rule.description ?? '')">{{ p.rule.name }}</span>
              <Button v-if="!p.exists" variant="ghost" size="icon-sm" class="ml-auto text-primary shrink-0" title="插入到编辑器"
                @click="addPreset(p)">
                <Plus class="size-4" />
              </Button>
              <Badge v-else variant="outline" class="ml-auto shrink-0 border-transparent bg-success/15 text-success">已启用</Badge>
            </div>
          </div>
        </Card>
      </div>
    </div>
  </div>
</template>
