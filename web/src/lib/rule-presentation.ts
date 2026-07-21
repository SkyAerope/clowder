import type { EditAction, EditCond, EditRule } from './rule-model'

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
export const CAPTURE_FIELDS = ['request_headers', 'request_body', 'response_headers', 'response_body']
export const CAPTURE_LABELS: Record<string, string> = {
  request_headers: '请求头', request_body: '请求体', response_headers: '响应头',
  response_body: '响应体',
}
export const TYPE_LABELS: Record<string, string> = { upstream: '上游', check: '探活', schedule: '定时' }

export function lbl(options: Opt[], value: string): string {
  return options.find((option) => option.v === value)?.label ?? value
}

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

export function ifPhrase(condition: EditCond): string {
  return `${lbl(IF_FIELDS, condition.field)} ${lbl(IF_OPS, condition.op)} ${condition.value}`
}

export function actPhrase(action: EditAction): string {
  if (action.kind === 'mark_key') return `标记 Key=${lbl(MARK_STATUS, action.mark_status)}`
  if (action.kind === 'check_key') return action.chain ? '探活当前 Key(链式)' : '探活当前 Key'
  if (action.kind === 'check_keys') return action.chain ? '探活全部 Key(链式)' : '探活全部 Key'
  return lbl(ACTION_KINDS, action.kind)
}

export function routePhrase(rule: EditRule): string {
  return rule.routeMode === 'race'
    ? `竞速(${rule.routeRace.filter(Boolean).map((route) => lbl(RACE_ROUTES, route)).join(' / ')})`
    : lbl(ROUTES, rule.routeSingle)
}

export function summarize(rule: EditRule): string {
  const actions = [...rule.dos.map(actPhrase), routePhrase(rule)].filter(Boolean)
  const condition = rule.ifs.length
    ? `${whenPhrase(rule)} 且 ${rule.ifs.map(ifPhrase).join(' 且 ')}`
    : whenPhrase(rule)
  return `当 ${condition} 时${actions.length ? ` ${actions.join(' 并 ')}` : ''}`
}
