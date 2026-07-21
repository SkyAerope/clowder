import * as YAML from 'yaml'
import type { RuleView } from '../api'
import { blankRule, type EditAction, type EditRule } from './rule-model'

const STATUS_RANGES: Record<string, string> = {
  '1xx': '100-199',
  '2xx': '200-299',
  '3xx': '300-399',
  '4xx': '400-499',
  '5xx': '500-599',
}

function record(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return {}
  return Object.fromEntries(Object.entries(value))
}

function text(value: unknown, fallback = ''): string {
  return value == null ? fallback : String(value)
}

function normalizeStatus(value: string): string {
  return STATUS_RANGES[value] ?? value
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

export function draftToObject(list: EditRule[], disabledBuiltins: Iterable<string> = []): Record<string, unknown> {
  const rules = list.map(ruleToObject)
  const output: Record<string, unknown> = { rules }
  const disabled = [...disabledBuiltins]
  if (disabled.length) output.disabled_builtins = disabled
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
