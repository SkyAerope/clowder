import * as YAML from 'yaml'
import type { RuleView } from '../api'

export type WhenType = 'upstream' | 'check' | 'schedule'
export type RuleRouteMode = 'single' | 'race'

export interface WhenClause { field: string; text: string; regex: boolean }
export interface EditCond { field: string; op: string; value: number }
export interface EditAction { kind: string; mark_status: string; chain: boolean; fields: string[] }
export interface EditRule {
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
  routeMode: RuleRouteMode
  routeSingle: string
  routeRace: string[]
}

export interface Opt { v: string; label: string }
export const WHEN_FIELDS: Opt[] = [
  { v: 'provider', label: 'Provider' }, { v: 'model', label: 'Model' },
  { v: 'status_code', label: 'Status Code' }, { v: 'body', label: 'Body' },
  { v: 'transport', label: '传输错误' }, { v: 'ttft_over_ms', label: 'TTFT 超时(ms)' },
  { v: 'error', label: '任意错误' },
]
export const ROUTES: Opt[] = [
  { v: 'next_key', label: '换下一个 Key' }, { v: 'next_provider', label: '换下一个 Provider' },
  { v: 'retry_same', label: '重试当前' }, { v: 'passthrough', label: '透传响应' },
  { v: 'wait', label: '继续等待' },
]
export const RACE_ROUTES = ROUTES.filter((route) => route.v !== 'passthrough' && route.v !== 'wait')
export const TRANSPORTS: Opt[] = [
  { v: 'true', label: '任意传输错误' }, { v: 'dns_failure', label: 'DNS 解析失败' },
  { v: 'conn_refused', label: '连接被拒' }, { v: 'tls_error', label: 'TLS 错误' },
  { v: 'connect_timeout', label: '连接超时' }, { v: 'stream_cut', label: '上游流式中断' },
  { v: 'malformed', label: '响应格式错误' },
]
export const IF_FIELDS: Opt[] = [
  { v: 'attempt_no', label: '尝试次数' }, { v: 'key_switches', label: 'Key 切换数' },
  { v: 'provider_switches', label: 'Provider 切换数' },
]
export const IF_OPS: Opt[] = [
  { v: 'eq', label: '=' }, { v: 'ne', label: '≠' }, { v: 'gt', label: '>' },
  { v: 'gte', label: '≥' }, { v: 'lt', label: '<' }, { v: 'lte', label: '≤' },
]
export const ACTION_KINDS: Opt[] = [
  { v: 'mark_key', label: '标记 Key 状态' }, { v: 'mark_model_unhealthy', label: '标记模型不健康' },
  { v: 'mark_provider_unhealthy', label: '标记 Provider 不健康' }, { v: 'check_key', label: '探活当前 Key' },
  { v: 'check_keys', label: '探活全部 Key' }, { v: 'capture', label: '抓包' },
  { v: 'backoff_key', label: 'Key 退避' },
]
export const MARK_STATUS: Opt[] = [
  { v: 'invalid', label: '无效 invalid' }, { v: 'rate_limited', label: '限流 rate_limited' },
  { v: 'active', label: '可用 active' },
]
export const CHECK_STATUS: Opt[] = [
  { v: 'active', label: '可用 active' }, { v: 'invalid', label: '无效 invalid' },
  { v: 'rate_limited', label: '限流 rate_limited' }, { v: 'error', label: '错误 error' },
]
export const CAPTURE_FIELDS = ['request_headers', 'request_body', 'response_headers', 'response_body', 'status_code']
export const CAPTURE_LABELS: Record<string, string> = {
  request_headers: '请求头', request_body: '请求体', response_headers: '响应头',
  response_body: '响应体', status_code: 'Status Code',
}
export const TYPE_LABELS: Record<string, string> = { upstream: '上游', check: '探活', schedule: '定时' }

let uidSequence = 1

export function blankRule(): EditRule {
  return {
    _uid: uidSequence++, name: '', description: '', priority: 100, enabled: true,
    whenType: 'upstream', whenClauses: [], check_status: 'invalid', cron: '', scope_provider: '',
    ifs: [], dos: [], routeMode: 'single', routeSingle: 'passthrough', routeRace: [],
  }
}

function record(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return {}
  return Object.fromEntries(Object.entries(value))
}

function text(value: unknown, fallback = ''): string {
  return value == null ? fallback : String(value)
}

function normalizeStatus(value: string): string {
  return ({ '1xx': '100-199', '2xx': '200-299', '3xx': '300-399', '4xx': '400-499', '5xx': '500-599' } as Record<string, string>)[value] ?? value
}

export function rawToEdit(raw: unknown): EditRule {
  const source = record(raw)
  const rule = blankRule()
  rule.name = text(source.name)
  rule.description = text(source.description)
  rule.priority = typeof source.priority === 'number' ? source.priority : 100
  rule.enabled = source.enabled !== false
  const when = record(source.when)
  const schedule = record(when.schedule)
  const check = record(when.check)
  if (Object.keys(schedule).length) {
    rule.whenType = 'schedule'
    rule.cron = text(schedule.cron)
  } else if (Object.keys(check).length) {
    rule.whenType = 'check'
    rule.check_status = text(check.status, 'invalid')
  } else {
    const add = (field: string, value: unknown, regex = false) => {
      if (value != null && value !== '') rule.whenClauses.push({ field, text: text(value), regex })
    }
    add('provider', when.provider); add('model', when.model)
    if (when.status_code != null && when.status_code !== '') add('status_code', normalizeStatus(text(when.status_code)))
    add('body', when.body_contains); add('body', when.body_regex, true)
    if (when.transport === true) add('transport', 'true'); else add('transport', when.transport)
    add('ttft_over_ms', when.ttft_over_ms)
    if (when.error === true) rule.whenClauses.push({ field: 'error', text: '', regex: false })
  }
  const scope = record(when.scope)
  rule.scope_provider = text(scope.provider)
  const conditions = record(source.if)
  for (const [field, rawOps] of Object.entries(conditions)) {
    for (const [op, value] of Object.entries(record(rawOps))) rule.ifs.push({ field, op, value: Number(value) || 0 })
  }
  const actions = Array.isArray(source.do) ? source.do : []
  for (const rawAction of actions) {
    const actionRecord = record(rawAction)
    const entry = Object.entries(actionRecord)[0]
    if (!entry) continue
    const [kind, rawValue] = entry
    const value = record(rawValue)
    const action: EditAction = { kind, mark_status: 'invalid', chain: false, fields: [] }
    if (kind === 'mark_key') action.mark_status = typeof rawValue === 'string' ? rawValue : 'invalid'
    if (kind === 'check_key' || kind === 'check_keys') action.chain = value.chain === true
    if (kind === 'capture' && Array.isArray(value.fields)) action.fields = value.fields.map(String)
    rule.dos.push(action)
  }
  const route = source.route
  const routeRecord = record(route)
  if (Array.isArray(route)) { rule.routeMode = 'race'; rule.routeRace = route.map(String) }
  else if (Array.isArray(routeRecord.race)) { rule.routeMode = 'race'; rule.routeRace = routeRecord.race.map(String) }
  else if (typeof route === 'string') rule.routeSingle = route
  return rule
}

export function detailToEdit(view: RuleView): EditRule {
  const rule = blankRule()
  rule.name = view.name; rule.description = view.description; rule.priority = view.priority; rule.enabled = view.enabled
  rule.whenType = view.type === 'check' || view.type === 'schedule' ? view.type : 'upstream'
  const when = record(view.detail?.when)
  if (rule.whenType === 'schedule') { rule.cron = text(when.cron); rule.scope_provider = text(when.scope_provider) }
  else if (rule.whenType === 'check') rule.check_status = text(when.check_status, 'invalid')
  else {
    const add = (field: string, value: unknown, regex = false) => { if (value) rule.whenClauses.push({ field, text: text(value), regex }) }
    add('provider', when.provider); add('model', when.model); add('status_code', normalizeStatus(text(when.status_code)))
    add('body', when.body, when.body_regex === true); add('transport', when.transport); add('ttft_over_ms', when.ttft_over_ms)
    if (when.error) rule.whenClauses.push({ field: 'error', text: '', regex: false })
  }
  rule.ifs = (view.detail?.if ?? []).map((condition) => ({ field: condition.field, op: condition.op, value: condition.value }))
  rule.dos = (view.detail?.do ?? []).map((action) => ({ kind: action.kind, mark_status: action.mark_status || 'invalid', chain: !!action.chain, fields: action.fields ?? [] }))
  rule.routeMode = view.detail?.route?.mode ?? 'single'; rule.routeSingle = view.detail?.route?.single ?? 'passthrough'; rule.routeRace = view.detail?.route?.race ?? []
  return rule
}

export function draftToObject(list: EditRule[], disabledBuiltins: Iterable<string> = []): Record<string, unknown> {
  const rules = list.map(ruleToObject)
  const output: Record<string, unknown> = { rules }
  const disabled = [...disabledBuiltins]
  if (disabled.length) output.disabled_builtins = disabled
  return output
}

function ruleToObject(rule: EditRule): Record<string, unknown> {
    const output: Record<string, unknown> = { name: rule.name }
    if (rule.description) output.description = rule.description
    if (rule.priority !== 100) output.priority = rule.priority
    if (!rule.enabled) output.enabled = false
    const when: Record<string, unknown> = {}
    if (rule.whenType === 'schedule') { when.schedule = { cron: rule.cron }; if (rule.scope_provider) when.scope = { provider: rule.scope_provider } }
    else if (rule.whenType === 'check') when.check = { status: rule.check_status }
    else for (const clause of rule.whenClauses) {
      if (clause.field === 'body' && clause.text) when[clause.regex ? 'body_regex' : 'body_contains'] = clause.text
      else if (clause.field === 'transport' && clause.text) when.transport = clause.text === 'true' ? true : clause.text
      else if (clause.field === 'ttft_over_ms' && clause.text) when.ttft_over_ms = Number(clause.text)
      else if (clause.field === 'error') when.error = true
      else if (clause.text) when[clause.field] = clause.text
    }
    output.when = when
    if (rule.ifs.length) {
      const conditions: Record<string, Record<string, number>> = {}
      for (const condition of rule.ifs) {
        const operators = conditions[condition.field] ?? {}
        operators[condition.op] = condition.value
        conditions[condition.field] = operators
      }
      output.if = conditions
    }
    if (rule.dos.length) output.do = rule.dos.map((action) => {
      if (action.kind === 'mark_key') return { mark_key: action.mark_status }
      if (action.kind === 'check_key' || action.kind === 'check_keys') return { [action.kind]: { chain: action.chain } }
      if (action.kind === 'capture') return { capture: action.fields.length ? { fields: action.fields } : {} }
      return { [action.kind]: {} }
    })
    output.route = rule.routeMode === 'race' ? { race: rule.routeRace.filter(Boolean) } : rule.routeSingle
  return output
}

export function serializeRules(list: EditRule[], disabledBuiltins: Iterable<string> = []): string {
  return YAML.stringify(draftToObject(list, disabledBuiltins), { lineWidth: 0 })
}

export function serializeRuleFragment(rule: EditRule): string {
  const document = YAML.stringify([ruleToObject(rule)], { lineWidth: 0 }).trimEnd()
  return document.split('\n').map((line) => `  ${line}`).join('\n')
}

export function parseRules(source: string): { rules: EditRule[]; disabledBuiltins: Set<string> } {
  const document = record(YAML.parse(source))
  const rules = Array.isArray(document.rules) ? document.rules.map(rawToEdit) : []
  const disabled = Array.isArray(document.disabled_builtins) ? document.disabled_builtins.map(String) : []
  return { rules, disabledBuiltins: new Set(disabled) }
}

export function parseRuleFragment(source: string): EditRule {
  const parsed = YAML.parse(source)
  if (!Array.isArray(parsed) || parsed.length !== 1) throw new Error('源文件必须只包含一条 YAML 列表规则')
  return rawToEdit(parsed[0])
}

export function lbl(options: Opt[], value: string): string { return options.find((option) => option.v === value)?.label ?? value }
export function whenHasTTFT(rule: EditRule): boolean { return rule.whenType === 'upstream' && rule.whenClauses.some((clause) => clause.field === 'ttft_over_ms') }
export function whenFieldDisabled(rule: EditRule, clause: WhenClause, value: string): boolean {
  if (value === clause.field) return false
  const others = rule.whenClauses.filter((candidate) => candidate !== clause).map((candidate) => candidate.field)
  const exclusive = ['status_code', 'body', 'transport', 'error']
  return value === 'ttft_over_ms' ? others.some((field) => field === 'ttft_over_ms' || exclusive.includes(field)) : exclusive.includes(value) && others.includes('ttft_over_ms')
}
export function routeDisabled(rule: EditRule, value: string): boolean { return value === 'passthrough' ? whenHasTTFT(rule) : value === 'wait' ? !whenHasTTFT(rule) : false }
export function normalizeRule(rule: EditRule): void { if (rule.routeMode === 'single' && routeDisabled(rule, rule.routeSingle)) rule.routeSingle = whenHasTTFT(rule) ? 'next_provider' : 'passthrough' }

export function whenPhrase(rule: EditRule): string {
  if (rule.whenType === 'check') return `探活结果=${lbl(CHECK_STATUS, rule.check_status)}`
  if (rule.whenType === 'schedule') return rule.scope_provider ? `定时(${rule.cron}) @${rule.scope_provider}` : `定时(${rule.cron})`
  const phrases = rule.whenClauses.map((clause) => {
    if (clause.field === 'body') return clause.regex ? `body 匹配 /${clause.text}/` : `body 包含“${clause.text}”`
    if (clause.field === 'transport') return `传输错误=${lbl(TRANSPORTS, clause.text)}`
    if (clause.field === 'ttft_over_ms') return `TTFT>${clause.text}ms`
    if (clause.field === 'error') return '发生错误'
    return `${clause.field}=${clause.text}`
  })
  return phrases.length ? phrases.join(' 且 ') : '任意请求'
}
export function ifPhrase(condition: EditCond): string { return `${lbl(IF_FIELDS, condition.field)} ${lbl(IF_OPS, condition.op)} ${condition.value}` }
export function actPhrase(action: EditAction): string {
  if (action.kind === 'mark_key') return `标记 Key=${lbl(MARK_STATUS, action.mark_status)}`
  if (action.kind === 'check_key') return action.chain ? '探活当前 Key(链式)' : '探活当前 Key'
  if (action.kind === 'check_keys') return action.chain ? '探活全部 Key(链式)' : '探活全部 Key'
  return lbl(ACTION_KINDS, action.kind)
}
export function routePhrase(rule: EditRule): string { return rule.routeMode === 'race' ? `竞速(${rule.routeRace.filter(Boolean).map((route) => lbl(RACE_ROUTES, route)).join(' / ')})` : lbl(ROUTES, rule.routeSingle) }
export function summarize(rule: EditRule): string {
  const actions = [...rule.dos.map(actPhrase), routePhrase(rule)].filter(Boolean)
  const condition = rule.ifs.length ? `${whenPhrase(rule)} 且 ${rule.ifs.map(ifPhrase).join(' 且 ')}` : whenPhrase(rule)
  return `当 ${condition} 时${actions.length ? ` ${actions.join(' 并 ')}` : ''}`
}
