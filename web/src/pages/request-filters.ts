export interface RequestFilterDraft {
  from: string
  to: string
  provider: string
  model: string
  status: string
  error: string
  rule: string
  fingerprint: string
  captured: string
}

export const emptyRequestFilters: RequestFilterDraft = {
  from: '',
  to: '',
  provider: '',
  model: '',
  status: '',
  error: '',
  rule: '',
  fingerprint: '',
  captured: '',
}

export function defaultRequestFilters(now: Date = new Date()): RequestFilterDraft {
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  return {
    ...emptyRequestFilters,
    from: localDatetime(yesterday, '00:00'),
    to: localDatetime(now, '23:59'),
  }
}

function localDatetime(date: Date, time: string): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}T${time}`
}

function normalizeQueryValue(value: unknown): string {
  if (Array.isArray(value)) return value.length > 0 ? String(value[0]) : ''
  if (typeof value === 'string') return value
  return ''
}

function fromUTCISO(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  const d = new Date(trimmed)
  if (Number.isNaN(d.getTime())) return ''
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hours = String(d.getHours()).padStart(2, '0')
  const minutes = String(d.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day}T${hours}:${minutes}`
}

export function parseRequestFiltersFromQuery(
  query: Record<string, unknown>,
  now: Date = new Date(),
): RequestFilterDraft {
  const defaults = defaultRequestFilters(now)
  const result: RequestFilterDraft = { ...emptyRequestFilters }

  if ('from' in query) {
    const raw = normalizeQueryValue(query.from)
    result.from = raw ? fromUTCISO(raw) : ''
  } else {
    result.from = defaults.from
  }

  if ('to' in query) {
    const raw = normalizeQueryValue(query.to)
    result.to = raw ? fromUTCISO(raw) : ''
  } else {
    result.to = defaults.to
  }

  result.provider = normalizeQueryValue(query.provider)
  result.model = normalizeQueryValue(query.model)
  result.status = normalizeQueryValue(query.status)
  const error = normalizeQueryValue(query.error)
  result.error = error === 'true' || error === 'false' ? error : ''
  result.rule = normalizeQueryValue(query.rule)
  result.fingerprint = normalizeQueryValue(query.fingerprint)
  const captured = normalizeQueryValue(query.captured)
  result.captured = captured === 'true' || captured === 'false' ? captured : ''

  return result
}

export function buildRequestParams(filters: RequestFilterDraft, limit: number, offset: number): string {
  const params = new URLSearchParams()
  params.set('limit', String(limit))
  params.set('offset', String(offset))
  setIfPresent(params, 'from', toUTCISO(filters.from))
  setIfPresent(params, 'to', toUTCISO(filters.to))
  setIfPresent(params, 'provider', filters.provider)
  setIfPresent(params, 'model', filters.model)
  setIfPresent(params, 'status', filters.status)
  if (filters.error === 'true' || filters.error === 'false') params.set('error', filters.error)
  setIfPresent(params, 'rule', filters.rule)
  setIfPresent(params, 'fingerprint', filters.fingerprint)
  if (filters.captured === 'true' || filters.captured === 'false') params.set('captured', filters.captured)
  return params.toString()
}

function setIfPresent(params: URLSearchParams, key: string, value: string): void {
  const trimmed = value.trim()
  if (trimmed) params.set(key, trimmed)
}

export function statusFilterError(status: string): string | null {
  const trimmed = status.trim()
  if (!trimmed) return null
  if (!/^\d+$/.test(trimmed)) return '状态码需为整数'
  const code = Number(trimmed)
  if (code < 100 || code > 599) return '状态码需在 100–599 之间'
  return null
}

function toUTCISO(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  return new Date(trimmed).toISOString()
}
