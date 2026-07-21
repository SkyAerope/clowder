export type WhenType = 'upstream' | 'check' | 'schedule'
export type RuleRouteMode = 'single' | 'race'

export interface WhenClause { field: string; text: string; regex: boolean }
export interface EditCond { field: string; op: string; value: number }
export interface EditAction { kind: string; mark_status: string; chain: boolean; fields: string[] }
export interface EditRule {
  _uid: number
  slug: string
  name: string
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

let uidSequence = 1

export function blankRule(): EditRule {
  return {
    _uid: uidSequence++, slug: '', name: '', priority: 100, enabled: true,
    whenType: 'upstream', whenClauses: [], check_status: 'invalid', cron: '', scope_provider: '',
    ifs: [], dos: [], routeMode: 'single', routeSingle: 'passthrough', routeRace: [],
  }
}
