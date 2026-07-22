import type { EditAction, EditRule, WhenClause } from './rule-model'

export function whenHasTTFT(rule: EditRule): boolean {
  return rule.whenType === 'upstream' && rule.whenClauses.some((clause) => clause.field === 'ttft_over_ms')
}

export function whenFieldDisabled(rule: EditRule, clause: WhenClause, value: string): boolean {
  if (value === clause.field) return false
  const others = rule.whenClauses.filter((candidate) => candidate !== clause).map((candidate) => candidate.field)
  const exclusive = ['status_code', 'body', 'transport', 'error']
  return value === 'ttft_over_ms'
    ? others.some((field) => field === 'ttft_over_ms' || exclusive.includes(field))
    : exclusive.includes(value) && others.includes('ttft_over_ms')
}

function actionHasScheduleContext(rule: EditRule, kind: string): boolean {
  if (rule.whenType !== 'schedule') return true
  if (kind === 'check_keys') return true
  return kind === 'mark_provider_unhealthy' && rule.scope_provider.trim() !== ''
}

export function actionKindDisabled(rule: EditRule, action: EditAction, kind: string): boolean {
  if (!actionHasScheduleContext(rule, kind)) return true
  if (kind === action.kind) return false
  const others = rule.dos.filter((a) => a !== action).map((a) => a.kind)
  return others.includes(kind)
}

export function nextActionKind(rule: EditRule, kinds: readonly string[]): string | undefined {
  const used = new Set(rule.dos.map((a) => a.kind))
  return kinds.find((kind) => actionHasScheduleContext(rule, kind) && !used.has(kind))
}

export function hasUnusedActionKind(rule: EditRule, kinds: readonly string[]): boolean {
  return nextActionKind(rule, kinds) !== undefined
}

export function routeDisabled(rule: EditRule, value: string): boolean {
  return value === 'passthrough' ? whenHasTTFT(rule) : value === 'wait' ? !whenHasTTFT(rule) : false
}

export function retryConditionsDisabled(rule: EditRule): boolean {
  return rule.whenType === 'schedule'
}

export function routeControlsDisabled(rule: EditRule): boolean {
  return rule.whenType === 'schedule'
}

export function normalizeRule(rule: EditRule): void {
  if (rule.routeMode === 'single' && routeDisabled(rule, rule.routeSingle)) {
    rule.routeSingle = whenHasTTFT(rule) ? 'next_provider' : 'passthrough'
  }
}
