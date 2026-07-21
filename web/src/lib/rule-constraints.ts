import type { EditRule, WhenClause } from './rule-model'

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

export function routeDisabled(rule: EditRule, value: string): boolean {
  return value === 'passthrough' ? whenHasTTFT(rule) : value === 'wait' ? !whenHasTTFT(rule) : false
}

export function normalizeRule(rule: EditRule): void {
  if (rule.routeMode === 'single' && routeDisabled(rule, rule.routeSingle)) {
    rule.routeSingle = whenHasTTFT(rule) ? 'next_provider' : 'passthrough'
  }
}
