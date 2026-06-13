<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import * as YAML from 'yaml'
import { api, type RuleView, type RulePreset } from '../api'
import Card from '../components/Card.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from '@/components/ui/dialog'
import {
  DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuCheckboxItem,
} from '@/components/ui/dropdown-menu'
import { Save, TriangleAlert, Plus, Trash2, ChevronDown, ChevronRight } from 'lucide-vue-next'

// ---------- 可视化编辑模型 ----------
type WhenType = 'upstream' | 'check' | 'schedule'
interface WhenClause { field: string; text: string; regex: boolean }
interface EditCond { field: string; op: string; value: number }
interface EditAction { kind: string; mark_status: string; chain: boolean; fields: string[] }
interface EditRule {
  _uid: number
  name: string
  description: string
  priority: number
  enabled: boolean
  whenType: WhenType
  whenClauses: WhenClause[]
  check_status: string
  cron: string
  scope_provider: string
  ifs: EditCond[]
  dos: EditAction[]
  routeMode: 'single' | 'race'
  routeSingle: string
  routeRace: string[]
}

interface Opt { v: string; label: string }
const WHEN_FIELDS: Opt[] = [
  { v: 'provider', label: 'Provider' },
  { v: 'model', label: 'Model' },
  { v: 'status_code', label: 'Status Code' },
  { v: 'body', label: 'Body' },
  { v: 'transport', label: '传输错误' },
  { v: 'ttft_over_ms', label: 'TTFT 超时(ms)' },
  { v: 'error', label: '任意错误' },
]
const ROUTES: Opt[] = [
  { v: 'next_key', label: '换下一个 Key' },
  { v: 'next_provider', label: '换下一个 Provider' },
  { v: 'retry_same', label: '重试当前' },
  { v: 'passthrough', label: '透传响应' },
  { v: 'wait', label: '继续等待' },
]
const RACE_ROUTES = ROUTES.filter((r) => r.v !== 'passthrough' && r.v !== 'wait')
// 一次 attempt 只有一种结局：TTFT 超时（上游未返回）/ 传输错误 / 收到响应 / 任意错误。
// 故 ttft_over_ms 与下列「已出结局」字段（以及另一个 ttft）全部互斥，AND 语义下永不同真。
const TTFT_FIELD = 'ttft_over_ms'
const TTFT_EXCLUSIVE = ['status_code', 'body', 'transport', 'error']
const TRANSPORTS: Opt[] = [
  { v: 'true', label: '任意传输错误' },
  { v: 'dns_failure', label: 'DNS 解析失败' },
  { v: 'conn_refused', label: '连接被拒' },
  { v: 'tls_error', label: 'TLS 错误' },
  { v: 'connect_timeout', label: '连接超时' },
  { v: 'stream_cut', label: '上游流式中断' },
  { v: 'malformed', label: '响应格式错误' },
]
const IF_FIELDS: Opt[] = [
  { v: 'attempt_no', label: '尝试次数' },
  { v: 'key_switches', label: 'Key 切换数' },
  { v: 'provider_switches', label: 'Provider 切换数' },
]
const IF_OPS: Opt[] = [
  { v: 'eq', label: '=' }, { v: 'ne', label: '≠' },
  { v: 'gt', label: '>' }, { v: 'gte', label: '≥' },
  { v: 'lt', label: '<' }, { v: 'lte', label: '≤' },
]
const ACTION_KINDS: Opt[] = [
  { v: 'mark_key', label: '标记 Key 状态' },
  { v: 'mark_model_unhealthy', label: '标记模型不健康' },
  { v: 'mark_provider_unhealthy', label: '标记 Provider 不健康' },
  { v: 'refresh_models', label: '刷新模型列表' },
  { v: 'check_key', label: '探活当前 Key' },
  { v: 'check_keys', label: '探活全部 Key' },
  { v: 'capture', label: '抓包' },
  { v: 'backoff_key', label: 'Key 退避' },
]
const MARK_STATUS: Opt[] = [
  { v: 'invalid', label: '失效 invalid' },
  { v: 'rate_limited', label: '限流 rate_limited' },
  { v: 'active', label: '可用 active' },
]
const CHECK_STATUS: Opt[] = [
  { v: 'active', label: '可用 active' },
  { v: 'invalid', label: '失效 invalid' },
  { v: 'rate_limited', label: '限流 rate_limited' },
  { v: 'error', label: '错误 error' },
]
const CAPTURE_FIELDS = ['request_headers', 'request_body', 'response_headers', 'response_body', 'status_code']
const CAPTURE_LABELS: Record<string, string> = {
  request_headers: '请求头',
  request_body: '请求体',
  response_headers: '响应头',
  response_body: '响应体',
  status_code: 'Status Code',
}
const TYPE_LABELS: Record<string, string> = { upstream: '上游', check: '探活', schedule: '定时' }

function lbl(arr: Opt[], v: string): string {
  return arr.find((x) => x.v === v)?.label ?? v
}

const yamlText = ref('')
const rules = ref<RuleView[]>([])
const warnings = ref<{ rule: string; message: string }[]>([])
const presets = ref<RulePreset[]>([])
const msg = ref('')
const saving = ref(false)
const loaded = ref(false)
const tab = ref<'visual' | 'source'>('visual')
const draft = ref<EditRule[]>([])
const parseError = ref('')
const expanded = ref<Set<number>>(new Set())
const presetOpen = ref(false)
const presetExpanded = ref<Set<number>>(new Set())
const addedPresets = ref<Set<string>>(new Set())
const disabledBuiltins = ref<Set<string>>(new Set())
const savedYaml = ref('')
const dirty = computed(() => yamlText.value !== savedYaml.value)

let uidSeq = 1

function blankRule(): EditRule {
  return {
    _uid: uidSeq++,
    name: '', description: '', priority: 100, enabled: true, whenType: 'upstream',
    whenClauses: [], check_status: 'invalid', cron: '', scope_provider: '',
    ifs: [], dos: [], routeMode: 'single', routeSingle: 'passthrough', routeRace: [],
  }
}

function normStatus(s: string): string {
  switch (s) {
    case '1xx': return '100-199'
    case '2xx': return '200-299'
    case '3xx': return '300-399'
    case '4xx': return '400-499'
    case '5xx': return '500-599'
    default: return s
  }
}

// ---------- 原始规则对象 -> 可视化 ----------
function rawToEdit(rr: any): EditRule {
  const e = blankRule()
  e.name = rr.name ?? ''
  e.description = rr.description ?? ''
  e.priority = typeof rr.priority === 'number' ? rr.priority : 100
  e.enabled = rr.enabled !== false
  const w = rr.when ?? {}
  if (w.schedule) {
    e.whenType = 'schedule'
    e.cron = w.schedule.cron ?? ''
  } else if (w.check) {
    e.whenType = 'check'
    e.check_status = w.check.status ?? 'invalid'
  } else {
    e.whenType = 'upstream'
    if (w.provider) e.whenClauses.push({ field: 'provider', text: String(w.provider), regex: false })
    if (w.model) e.whenClauses.push({ field: 'model', text: String(w.model), regex: false })
    if (w.status_code != null && w.status_code !== '') e.whenClauses.push({ field: 'status_code', text: normStatus(String(w.status_code)), regex: false })
    if (w.body_contains) e.whenClauses.push({ field: 'body', text: String(w.body_contains), regex: false })
    if (w.body_regex) e.whenClauses.push({ field: 'body', text: String(w.body_regex), regex: true })
    if (w.transport === true) e.whenClauses.push({ field: 'transport', text: 'true', regex: false })
    else if (w.transport) e.whenClauses.push({ field: 'transport', text: String(w.transport), regex: false })
    if (w.ttft_over_ms != null) e.whenClauses.push({ field: 'ttft_over_ms', text: String(w.ttft_over_ms), regex: false })
    if (w.error === true) e.whenClauses.push({ field: 'error', text: '', regex: false })
  }
  if (w.scope?.provider) e.scope_provider = String(w.scope.provider)
  const iff = rr.if ?? {}
  for (const field of Object.keys(iff)) {
    const ops = iff[field] ?? {}
    for (const op of Object.keys(ops)) e.ifs.push({ field, op, value: Number(ops[op]) || 0 })
  }
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
  const rt = rr.route
  if (Array.isArray(rt)) { e.routeMode = 'race'; e.routeRace = rt.map(String) }
  else if (rt && typeof rt === 'object' && Array.isArray(rt.race)) { e.routeMode = 'race'; e.routeRace = rt.race.map(String) }
  else if (typeof rt === 'string') { e.routeMode = 'single'; e.routeSingle = rt }
  return e
}

// ---------- 后端 detail -> 可视化（内置规则只读用） ----------
function detailToEdit(v: RuleView): EditRule {
  const e = blankRule()
  e.name = v.name
  e.description = v.description
  e.priority = v.priority
  e.enabled = v.enabled
  e.whenType = (v.type as WhenType) || 'upstream'
  const w: any = v.detail?.when ?? {}
  if (e.whenType === 'schedule') {
    e.cron = String(w.cron ?? '')
    e.scope_provider = String(w.scope_provider ?? '')
  } else if (e.whenType === 'check') {
    e.check_status = String(w.check_status ?? 'invalid')
  } else {
    if (w.provider) e.whenClauses.push({ field: 'provider', text: String(w.provider), regex: false })
    if (w.model) e.whenClauses.push({ field: 'model', text: String(w.model), regex: false })
    if (w.status_code) e.whenClauses.push({ field: 'status_code', text: normStatus(String(w.status_code)), regex: false })
    if (w.body) e.whenClauses.push({ field: 'body', text: String(w.body), regex: !!w.body_regex })
    if (w.transport) e.whenClauses.push({ field: 'transport', text: String(w.transport), regex: false })
    if (w.ttft_over_ms) e.whenClauses.push({ field: 'ttft_over_ms', text: String(w.ttft_over_ms), regex: false })
    if (w.error) e.whenClauses.push({ field: 'error', text: '', regex: false })
  }
  e.ifs = (v.detail?.if ?? []).map((c) => ({ field: c.field, op: c.op, value: c.value }))
  e.dos = (v.detail?.do ?? []).map((a) => ({ kind: a.kind, mark_status: a.mark_status || 'invalid', chain: !!a.chain, fields: a.fields ?? [] }))
  e.routeMode = v.detail?.route?.mode ?? 'single'
  e.routeSingle = v.detail?.route?.single ?? 'passthrough'
  e.routeRace = v.detail?.route?.race ?? []
  return e
}

// ---------- 可视化 -> YAML ----------
function draftToObj(list: EditRule[]): any {
  const obj: any = {
    rules: list.map((e) => {
      const r: any = { name: e.name }
      if (e.description) r.description = e.description
      if (e.priority !== 100) r.priority = e.priority
      if (!e.enabled) r.enabled = false
      const w: any = {}
      if (e.whenType === 'schedule') {
        w.schedule = { cron: e.cron }
        if (e.scope_provider) w.scope = { provider: e.scope_provider }
      } else if (e.whenType === 'check') {
        w.check = { status: e.check_status }
      } else {
        for (const c of e.whenClauses) {
          switch (c.field) {
            case 'provider': if (c.text) w.provider = c.text; break
            case 'model': if (c.text) w.model = c.text; break
            case 'status_code': if (c.text) w.status_code = c.text; break
            case 'body':
              if (c.text) { if (c.regex) w.body_regex = c.text; else w.body_contains = c.text }
              break
            case 'transport':
              if (c.text === 'true') w.transport = true
              else if (c.text) w.transport = c.text
              break
            case 'ttft_over_ms': if (c.text) w.ttft_over_ms = Number(c.text); break
            case 'error': w.error = true; break
          }
        }
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
  if (disabledBuiltins.value.size) obj.disabled_builtins = [...disabledBuiltins.value]
  return obj
}

function serializeDraft() {
  yamlText.value = YAML.stringify(draftToObj(draft.value), { lineWidth: 0 })
}

function refreshDraft() {
  try {
    const doc = YAML.parse(yamlText.value) || {}
    const raws: any[] = Array.isArray(doc.rules) ? doc.rules : []
    draft.value = raws.map(rawToEdit)
    const db: any[] = Array.isArray(doc.disabled_builtins) ? doc.disabled_builtins : []
    disabledBuiltins.value = new Set(db.map(String))
    parseError.value = ''
  } catch (e: any) {
    parseError.value = `YAML 解析失败，请在「源文件」修正后再切换：${e.message}`
  }
}

// ---------- 摘要文本 ----------
function clausePhrase(c: WhenClause): string {
  switch (c.field) {
    case 'provider': return `provider=${c.text}`
    case 'model': return `model=${c.text}`
    case 'status_code': return `status_code=${c.text}`
    case 'body': return c.regex ? `body 匹配 /${c.text}/` : `body 包含“${c.text}”`
    case 'transport': return `传输错误=${lbl(TRANSPORTS, c.text)}`
    case 'ttft_over_ms': return `TTFT>${c.text}ms`
    case 'error': return '发生错误'
    default: return c.field
  }
}

function whenPhrase(e: EditRule): string {
  if (e.whenType === 'check') return `探活结果=${lbl(CHECK_STATUS, e.check_status)}`
  if (e.whenType === 'schedule') return e.scope_provider ? `定时(${e.cron}) @${e.scope_provider}` : `定时(${e.cron})`
  const parts = e.whenClauses.map(clausePhrase).filter(Boolean)
  return parts.length ? parts.join(' 且 ') : '任意请求'
}

function ifPhrase(c: EditCond): string {
  return `${lbl(IF_FIELDS, c.field)} ${lbl(IF_OPS, c.op)} ${c.value}`
}

function actPhrase(a: EditAction): string {
  switch (a.kind) {
    case 'mark_key': return `标记 Key=${lbl(MARK_STATUS, a.mark_status)}`
    case 'mark_model_unhealthy': return '标记模型不健康'
    case 'mark_provider_unhealthy': return '标记 Provider 不健康'
    case 'refresh_models': return '刷新模型'
    case 'check_key': return a.chain ? '探活当前 Key(链式)' : '探活当前 Key'
    case 'check_keys': return a.chain ? '探活全部 Key(链式)' : '探活全部 Key'
    case 'capture': return '抓包'
    case 'backoff_key': return 'Key 退避'
    default: return a.kind
  }
}

function routePhrase(e: EditRule): string {
  if (e.routeMode === 'race') {
    const list = e.routeRace.filter(Boolean).map((r) => lbl(RACE_ROUTES, r))
    return list.length ? `竞速(${list.join(' / ')})` : ''
  }
  return e.routeSingle ? lbl(ROUTES, e.routeSingle) : ''
}

function summarize(e: EditRule): string {
  const acts = [...e.dos.map(actPhrase), routePhrase(e)].filter(Boolean)
  const cond = e.ifs.length ? `${whenPhrase(e)} 且 ${e.ifs.map(ifPhrase).join(' 且 ')}` : whenPhrase(e)
  return `当 ${cond} 时${acts.length ? ' ' + acts.join(' 并 ') : ''}`
}

// 可视化编辑时，draft 变化 -> 规范化约束后同步回 yamlText
watch(draft, () => {
  for (const e of draft.value) normalizeRule(e)
  if (tab.value === 'visual') serializeDraft()
}, { deep: true })

// 切换到可视化 -> 用最新 yamlText 重建 draft
watch(tab, (t) => {
  if (t === 'visual') refreshDraft()
})

const builtinEdits = computed(() => rules.value.filter((r) => r.builtin).map(detailToEdit))
const presetEdits = computed(() => presets.value.map((p) => ({ preset: p, edit: rawToEdit(p.rule as any) })))

function toggleOpen(uid: number) {
  if (expanded.value.has(uid)) expanded.value.delete(uid)
  else expanded.value.add(uid)
}
function togglePreset(uid: number) {
  if (presetExpanded.value.has(uid)) presetExpanded.value.delete(uid)
  else presetExpanded.value.add(uid)
}
function presetAdded(p: RulePreset): boolean {
  return p.exists || addedPresets.value.has(String(p.rule.name))
}

function toggleBuiltin(name: string, enabled: boolean) {
  if (enabled) disabledBuiltins.value.delete(name)
  else disabledBuiltins.value.add(name)
  serializeDraft()
}

// ---------- 编辑操作 ----------
function addRule() {
  const e = blankRule()
  draft.value.push(e)
  expanded.value.add(e._uid)
}
function removeRule(i: number) { draft.value.splice(i, 1) }
// ---------- 互斥 / 无意义动作约束 ----------
function whenHasTTFT(e: EditRule): boolean {
  return e.whenType === 'upstream' && e.whenClauses.some((c) => c.field === TTFT_FIELD)
}
// 某 when 字段是否与「其它」子句冲突：ttft 与 ttft / 已出结局字段互斥。
function fieldConflicts(optV: string, others: string[]): boolean {
  if (optV === TTFT_FIELD) return others.some((f) => f === TTFT_FIELD || TTFT_EXCLUSIVE.includes(f))
  if (TTFT_EXCLUSIVE.includes(optV)) return others.includes(TTFT_FIELD)
  return false
}
// 该选项在某子句的下拉中是否应禁用（自身当前值始终可选）。
function whenFieldDisabled(e: EditRule, clause: WhenClause, optV: string): boolean {
  if (optV === clause.field) return false
  return fieldConflicts(optV, e.whenClauses.filter((c) => c !== clause).map((c) => c.field))
}
// 路由单选项是否对当前 when 无意义（灰显禁用）：passthrough 与 wait 互补，由是否含 TTFT 决定。
function routeDisabled(e: EditRule, v: string): boolean {
  if (v === 'passthrough') return whenHasTTFT(e)
  if (v === 'wait') return !whenHasTTFT(e)
  return false
}
// 已选路由失效时回退到合法默认值。
function normalizeRule(e: EditRule) {
  if (e.routeMode === 'single' && routeDisabled(e, e.routeSingle)) {
    e.routeSingle = whenHasTTFT(e) ? 'next_provider' : 'passthrough'
  }
}
function addWhenClause(e: EditRule) {
  const used = new Set(e.whenClauses.map((c) => c.field))
  const others = e.whenClauses.map((c) => c.field)
  const next = WHEN_FIELDS.find((f) => !used.has(f.v) && !fieldConflicts(f.v, others)) ?? WHEN_FIELDS[0]
  e.whenClauses.push({ field: next.v, text: next.v === 'transport' ? 'true' : '', regex: false })
}
function removeWhenClause(e: EditRule, i: number) { e.whenClauses.splice(i, 1) }
function addCond(e: EditRule) { e.ifs.push({ field: 'attempt_no', op: 'gte', value: 2 }) }
function removeCond(e: EditRule, i: number) { e.ifs.splice(i, 1) }
function addAction(e: EditRule) { e.dos.push({ kind: 'capture', mark_status: 'invalid', chain: false, fields: [] }) }
function removeAction(e: EditRule, i: number) { e.dos.splice(i, 1) }
function toggleField(a: EditAction, f: string) {
  const i = a.fields.indexOf(f)
  if (i >= 0) a.fields.splice(i, 1)
  else a.fields.push(f)
}
function toggleRace(e: EditRule, v: string) {
  const i = e.routeRace.indexOf(v)
  if (i >= 0) e.routeRace.splice(i, 1)
  else e.routeRace.push(v)
}

// ---------- 加载/保存/预设 ----------
async function load() {
  try {
    const r = await api.get<{ yaml: string; rules: RuleView[]; warnings: { rule: string; message: string }[] }>('/api/admin/rules')
    yamlText.value = r.yaml
    rules.value = r.rules ?? []
    warnings.value = r.warnings ?? []
    presets.value = (await api.get<RulePreset[]>('/api/admin/rules/presets')) ?? []
    addedPresets.value = new Set()
    refreshDraft()
    serializeDraft()
    await nextTick()
    savedYaml.value = yamlText.value
  } finally {
    loaded.value = true
  }
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
  const indented = p.yaml.split('\n').map((l) => (l ? '  ' + l : l)).join('\n')
  yamlText.value = y.trimEnd() + '\n\n' + indented + '\n'
  if (tab.value === 'visual') refreshDraft()
  addedPresets.value.add(String(p.rule.name))
  msg.value = `已插入 ${p.rule.name}，请检查后保存`
}

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value) {
    e.preventDefault()
    e.returnValue = ''
  }
}

onMounted(() => {
  load()
  window.addEventListener('beforeunload', onBeforeUnload)
})
onUnmounted(() => window.removeEventListener('beforeunload', onBeforeUnload))
onBeforeRouteLeave(() => {
  if (dirty.value) return window.confirm('有未保存的规则修改，确定离开吗？')
  return true
})
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between gap-2">
      <h1 class="text-xl font-semibold">规则管理</h1>
      <div class="flex items-center gap-2">
        <Button variant="outline" @click="presetOpen = true">
          <Plus class="size-4" /> 添加预设规则
        </Button>
        <Button :disabled="saving || !dirty" @click="save">
          <Save class="size-4" /> 保存并生效
        </Button>
      </div>
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

    <div v-if="!loaded" class="space-y-3">
      <Skeleton class="h-9 w-48" />
      <Skeleton class="h-40 w-full rounded-xl" />
      <Skeleton class="h-40 w-full rounded-xl" />
    </div>

    <Tabs v-else v-model="tab">
      <TabsList>
        <TabsTrigger value="visual">可视化</TabsTrigger>
        <TabsTrigger value="source">源文件</TabsTrigger>
      </TabsList>

      <!-- 可视化编辑 -->
      <TabsContent value="visual" class="space-y-3">
        <Alert v-if="parseError" variant="destructive">
          <AlertDescription>{{ parseError }}</AlertDescription>
        </Alert>

        <!-- 可编辑规则卡片 -->
        <div v-for="(e, ri) in draft" :key="e._uid" class="border-border bg-card overflow-hidden rounded-xl border" :class="{ 'opacity-60': !e.enabled }">
          <div class="flex items-start gap-2 p-3">
            <button type="button" class="flex min-w-0 flex-1 items-start gap-2 text-left" @click="toggleOpen(e._uid)">
              <component :is="expanded.has(e._uid) ? ChevronDown : ChevronRight" class="text-muted-foreground mt-0.5 size-4 shrink-0" />
              <div class="min-w-0 flex-1 space-y-1">
                <div class="flex items-center gap-2">
                  <span class="truncate font-medium">{{ e.description || '未命名规则' }}</span>
                  <span class="text-muted-foreground ml-auto shrink-0 truncate font-mono text-xs">{{ e.name }}</span>
                </div>
                <div class="flex flex-wrap items-center gap-1.5">
                  <Badge variant="secondary" class="shrink-0">P{{ e.priority }}</Badge>
                  <Badge variant="outline" class="shrink-0">{{ TYPE_LABELS[e.whenType] }}</Badge>
                  <span class="text-muted-foreground text-xs">{{ summarize(e) }}</span>
                </div>
              </div>
            </button>
            <div class="mt-1 flex shrink-0 flex-col items-center gap-2">
              <Switch v-model="e.enabled" title="启用 / 停用此规则" />
              <Button v-if="expanded.has(e._uid)" variant="ghost" size="icon-sm" class="text-destructive" title="删除规则" @click="removeRule(ri)">
                <Trash2 class="size-4" />
              </Button>
            </div>
          </div>

          <!-- 展开编辑面板 -->
          <div v-if="expanded.has(e._uid)" class="space-y-3 border-t border-foreground/10 p-3">
            <Input v-model="e.name" placeholder="规则名（英文标识）" class="font-mono" />
            <Input v-model="e.description" placeholder="描述（显示为标题）" />

            <div class="flex flex-wrap items-end gap-3">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">优先级（越小越优先）</Label>
                <Input v-model.number="e.priority" type="number" class="w-28" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">触发类型 (when)</Label>
                <Select v-model="e.whenType">
                  <SelectTrigger class="w-40"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="upstream">上游结果</SelectItem>
                    <SelectItem value="check">探活结果</SelectItem>
                    <SelectItem value="schedule">定时</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <!-- upstream：条件列表 -->
            <div v-if="e.whenType === 'upstream'" class="space-y-2">
              <Label class="text-xs text-muted-foreground">触发条件 (when)</Label>
              <p v-if="!e.whenClauses.length" class="text-xs text-muted-foreground">未设条件 = 匹配任意请求</p>
              <div v-for="(c, ci) in e.whenClauses" :key="ci" class="flex flex-wrap items-center gap-2">
                <Select v-model="c.field">
                  <SelectTrigger class="w-40"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="f in WHEN_FIELDS" :key="f.v" :value="f.v" :disabled="whenFieldDisabled(e, c, f.v)">{{ f.label }}</SelectItem>
                  </SelectContent>
                </Select>
                <Select v-if="c.field === 'transport'" v-model="c.text">
                  <SelectTrigger class="w-44"><SelectValue placeholder="选择类别" /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="t in TRANSPORTS" :key="t.v" :value="t.v">{{ t.label }}</SelectItem>
                  </SelectContent>
                </Select>
                <span v-else-if="c.field === 'error'" class="text-xs text-muted-foreground">命中任意错误</span>
                <template v-else-if="c.field === 'body'">
                  <Input v-model="c.text" class="w-56" :class="c.regex ? 'font-mono' : ''" :placeholder="c.regex ? 'RE2 正则' : '包含文本'" />
                  <div class="flex items-center gap-1.5">
                    <Switch v-model="c.regex" />
                    <Label class="text-xs text-muted-foreground">正则</Label>
                  </div>
                </template>
                <Input v-else-if="c.field === 'status_code'" v-model="c.text" class="w-44" placeholder="403 或 400-499" />
                <Input v-else-if="c.field === 'ttft_over_ms'" v-model="c.text" type="number" class="w-44" placeholder="毫秒阈值" />
                <Input v-else v-model="c.text" class="w-56" placeholder="留空=任意" />
                <Button variant="ghost" size="icon-sm" class="text-destructive ml-auto shrink-0" @click="removeWhenClause(e, ci)">
                  <Trash2 class="size-4" />
                </Button>
              </div>
              <button type="button" class="text-muted-foreground hover:text-foreground hover:border-foreground/30 flex w-full items-center justify-center gap-1 rounded-lg border border-dashed border-foreground/20 py-1.5 text-xs" @click="addWhenClause(e)">
                <Plus class="size-3" /> 加条件
              </button>
            </div>
            <div v-else-if="e.whenType === 'check'" class="space-y-2">
              <Label class="text-xs text-muted-foreground">触发条件 (when)：探活状态命中</Label>
              <Select v-model="e.check_status">
                <SelectTrigger class="w-56"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="s in CHECK_STATUS" :key="s.v" :value="s.v">{{ s.label }}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <!-- schedule 字段 -->
            <div v-else class="space-y-2">
              <Label class="text-xs text-muted-foreground">触发条件 (when)：定时</Label>
              <div class="flex flex-wrap gap-3">
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">cron 表达式</Label>
                  <Input v-model="e.cron" class="w-48 font-mono" placeholder="0 */6 * * *" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">scope provider（可选）</Label>
                  <Input v-model="e.scope_provider" class="w-48" placeholder="限定 provider" />
                </div>
              </div>
            </div>

            <!-- if 条件 -->
            <div class="space-y-2">
              <Label class="text-xs text-muted-foreground">附加条件 (if)</Label>
              <div v-for="(c, ci) in e.ifs" :key="ci" class="flex items-center gap-2">
                <Select v-model="c.field">
                  <SelectTrigger class="w-44"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="f in IF_FIELDS" :key="f.v" :value="f.v">{{ f.label }}</SelectItem>
                  </SelectContent>
                </Select>
                <Select v-model="c.op">
                  <SelectTrigger class="w-24"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="o in IF_OPS" :key="o.v" :value="o.v">{{ o.label }}</SelectItem>
                  </SelectContent>
                </Select>
                <Input v-model.number="c.value" type="number" class="w-24" />
                <Button variant="ghost" size="icon-sm" class="text-destructive" @click="removeCond(e, ci)">
                  <Trash2 class="size-4" />
                </Button>
              </div>
              <button type="button" class="text-muted-foreground hover:text-foreground hover:border-foreground/30 flex w-full items-center justify-center gap-1 rounded-lg border border-dashed border-foreground/20 py-1.5 text-xs" @click="addCond(e)">
                <Plus class="size-3" /> 加条件
              </button>
            </div>

            <!-- do 动作 -->
            <div class="space-y-2">
              <Label class="text-xs text-muted-foreground">动作 (do)</Label>
              <div v-for="(a, ai) in e.dos" :key="ai" class="flex flex-wrap items-center gap-2">
                <Select v-model="a.kind">
                  <SelectTrigger class="w-48"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="k in ACTION_KINDS" :key="k.v" :value="k.v">{{ k.label }}</SelectItem>
                  </SelectContent>
                </Select>
                <Select v-if="a.kind === 'mark_key'" v-model="a.mark_status">
                  <SelectTrigger class="w-44"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="s in MARK_STATUS" :key="s.v" :value="s.v">{{ s.label }}</SelectItem>
                  </SelectContent>
                </Select>
                <div v-else-if="a.kind === 'check_key' || a.kind === 'check_keys'" class="flex items-center gap-1.5">
                  <Switch v-model="a.chain" />
                  <Label class="text-xs text-muted-foreground">链式（chain）：探活后按结果再次匹配</Label>
                </div>
                <DropdownMenu v-else-if="a.kind === 'capture'">
                  <DropdownMenuTrigger as-child>
                    <button type="button" class="border-input dark:bg-input/30 flex min-h-8 max-w-full flex-1 items-center gap-1 overflow-hidden rounded-lg border bg-transparent px-2.5 py-1 text-sm outline-none">
                      <span class="flex flex-1 flex-wrap items-center gap-1">
                        <Badge v-for="f in a.fields" :key="f" variant="secondary" class="h-5 shrink-0">{{ CAPTURE_LABELS[f] ?? f }}</Badge>
                        <span v-if="!a.fields.length" class="text-muted-foreground">全抓（点击多选）</span>
                      </span>
                      <ChevronDown class="text-muted-foreground size-4 shrink-0" />
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start">
                    <DropdownMenuCheckboxItem
                      v-for="f in CAPTURE_FIELDS" :key="f"
                      :model-value="a.fields.includes(f)"
                      @select="(ev: Event) => ev.preventDefault()"
                      @update:model-value="() => toggleField(a, f)"
                    >
                      {{ CAPTURE_LABELS[f] ?? f }}
                    </DropdownMenuCheckboxItem>
                  </DropdownMenuContent>
                </DropdownMenu>
                <Button variant="ghost" size="icon-sm" class="text-destructive ml-auto shrink-0" @click="removeAction(e, ai)">
                  <Trash2 class="size-4" />
                </Button>
              </div>
              <button type="button" class="text-muted-foreground hover:text-foreground hover:border-foreground/30 flex w-full items-center justify-center gap-1 rounded-lg border border-dashed border-foreground/20 py-1.5 text-xs" @click="addAction(e)">
                <Plus class="size-3" /> 加动作
              </button>
            </div>

            <!-- route 路由 -->
            <div class="space-y-2">
              <Label class="text-xs text-muted-foreground">路由 (route)</Label>
              <div class="flex items-start gap-2">
                <Select v-model="e.routeMode">
                  <SelectTrigger class="w-32"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="single">单一</SelectItem>
                    <SelectItem value="race">双发竞速</SelectItem>
                  </SelectContent>
                </Select>
                <Select v-if="e.routeMode === 'single'" v-model="e.routeSingle">
                  <SelectTrigger class="w-56"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="r in ROUTES" :key="r.v" :value="r.v" :disabled="routeDisabled(e, r.v)">{{ r.label }}</SelectItem>
                  </SelectContent>
                </Select>
                <DropdownMenu v-else>
                  <DropdownMenuTrigger as-child>
                    <button type="button" class="border-input dark:bg-input/30 flex min-h-8 max-w-full flex-1 items-center gap-1 overflow-hidden rounded-lg border bg-transparent px-2.5 py-1 text-sm outline-none">
                      <span class="flex flex-1 flex-wrap items-center gap-1">
                        <Badge v-for="r in e.routeRace" :key="r" variant="secondary" class="h-5 shrink-0">{{ lbl(RACE_ROUTES, r) }}</Badge>
                        <span v-if="!e.routeRace.length" class="text-muted-foreground">选择并发分支（点击多选）</span>
                      </span>
                      <ChevronDown class="text-muted-foreground size-4 shrink-0" />
                    </button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start">
                    <DropdownMenuCheckboxItem
                      v-for="opt in RACE_ROUTES" :key="opt.v"
                      :model-value="e.routeRace.includes(opt.v)"
                      @select="(ev: Event) => ev.preventDefault()"
                      @update:model-value="() => toggleRace(e, opt.v)"
                    >
                      {{ opt.label }}
                    </DropdownMenuCheckboxItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </div>
          </div>
        </div>

        <Button variant="outline" class="w-full" @click="addRule">
          <Plus class="size-4" /> 新增规则
        </Button>

        <!-- 内置规则（只读） -->
        <div v-if="builtinEdits.length" class="space-y-2 pt-2">
          <h2 class="text-sm font-medium text-muted-foreground">内置规则（只读）</h2>
          <div v-for="be in builtinEdits" :key="be._uid" class="border-border bg-muted/30 overflow-hidden rounded-xl border" :class="{ 'opacity-60': disabledBuiltins.has(be.name) }">
            <div class="flex items-start gap-2 p-3">
              <button type="button" class="flex min-w-0 flex-1 items-start gap-2 text-left" @click="toggleOpen(be._uid)">
                <component :is="expanded.has(be._uid) ? ChevronDown : ChevronRight" class="text-muted-foreground mt-0.5 size-4 shrink-0" />
                <div class="min-w-0 flex-1 space-y-1">
                  <div class="flex items-center gap-2">
                    <span class="truncate font-medium">{{ be.description || '内置规则' }}</span>
                    <span class="text-muted-foreground ml-auto shrink-0 truncate font-mono text-xs">{{ be.name }}</span>
                  </div>
                  <div class="flex flex-wrap items-center gap-1.5">
                    <Badge variant="secondary" class="shrink-0">P{{ be.priority }}</Badge>
                    <Badge variant="outline" class="shrink-0">{{ TYPE_LABELS[be.whenType] }}</Badge>
                    <span class="text-muted-foreground text-xs">{{ summarize(be) }}</span>
                  </div>
                </div>
              </button>
              <Switch :model-value="!disabledBuiltins.has(be.name)" class="mt-1 shrink-0" title="启用 / 停用内置规则" @update:model-value="(v: boolean) => toggleBuiltin(be.name, v)" />
            </div>
            <div v-if="expanded.has(be._uid)" class="space-y-1 border-t border-foreground/10 p-3 pl-9 text-xs text-muted-foreground">
              <div><span class="text-foreground">触发：</span>{{ whenPhrase(be) }}</div>
              <div v-if="be.ifs.length"><span class="text-foreground">条件：</span>{{ be.ifs.map(ifPhrase).join('，') }}</div>
              <div v-if="be.dos.length"><span class="text-foreground">动作：</span>{{ be.dos.map(actPhrase).join('，') }}</div>
              <div><span class="text-foreground">路由：</span>{{ routePhrase(be) }}</div>
            </div>
          </div>
        </div>
      </TabsContent>

      <!-- 源文件编辑 -->
      <TabsContent value="source">
        <Card title="rules.yaml">
          <Textarea v-model="yamlText" :rows="30" class="font-mono text-xs leading-relaxed" spellcheck="false" />
        </Card>
      </TabsContent>
    </Tabs>

    <!-- 预设规则对话框 -->
    <Dialog v-model:open="presetOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>预设规则</DialogTitle>
          <DialogDescription>选择常用规则插入到编辑器，保存后生效。</DialogDescription>
        </DialogHeader>
        <div class="max-h-[60vh] space-y-2 overflow-y-auto pr-1">
          <div v-for="pe in presetEdits" :key="pe.edit._uid" class="border-border overflow-hidden rounded-xl border">
            <div class="flex items-start gap-2 p-3">
              <button type="button" class="min-w-0 flex-1 space-y-1 text-left" @click="togglePreset(pe.edit._uid)">
                <div class="flex items-center gap-2">
                  <component :is="presetExpanded.has(pe.edit._uid) ? ChevronDown : ChevronRight" class="text-muted-foreground size-4 shrink-0" />
                  <span class="truncate font-medium">{{ pe.edit.description || pe.edit.name }}</span>
                </div>
                <div class="flex flex-wrap items-center gap-1.5 pl-6">
                  <Badge variant="secondary" class="shrink-0">P{{ pe.edit.priority }}</Badge>
                  <Badge variant="outline" class="shrink-0">{{ TYPE_LABELS[pe.edit.whenType] }}</Badge>
                  <span class="text-muted-foreground text-xs">{{ summarize(pe.edit) }}</span>
                </div>
              </button>
              <Button v-if="presetAdded(pe.preset)" size="sm" variant="outline" disabled class="shrink-0">已添加</Button>
              <Button v-else size="sm" variant="outline" class="shrink-0" @click="addPreset(pe.preset)">添加</Button>
            </div>
            <div v-if="presetExpanded.has(pe.edit._uid)" class="space-y-1 border-t border-foreground/10 p-3 pl-9 text-xs text-muted-foreground">
              <div><span class="text-foreground">触发：</span>{{ whenPhrase(pe.edit) }}</div>
              <div v-if="pe.edit.ifs.length"><span class="text-foreground">条件：</span>{{ pe.edit.ifs.map(ifPhrase).join('，') }}</div>
              <div v-if="pe.edit.dos.length"><span class="text-foreground">动作：</span>{{ pe.edit.dos.map(actPhrase).join('，') }}</div>
              <div><span class="text-foreground">路由：</span>{{ routePhrase(pe.edit) }}</div>
            </div>
          </div>
          <p v-if="!presetEdits.length" class="py-4 text-center text-sm text-muted-foreground">暂无预设</p>
        </div>
      </DialogContent>
    </Dialog>
  </div>
</template>
