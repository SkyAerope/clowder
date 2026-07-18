import { ref } from 'vue'

export interface Session { admin: boolean; public: boolean }

export const session = ref<Session>({ admin: false, public: true })

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, (data as any).error || res.statusText)
  return data as T
}

export class ApiError extends Error {
  status: number
  constructor(status: number, msg: string) {
    super(msg)
    this.status = status
  }
}

export const api = {
  get: <T>(url: string) => request<T>('GET', url),
  post: <T>(url: string, body?: unknown) => request<T>('POST', url, body),
  put: <T>(url: string, body?: unknown) => request<T>('PUT', url, body),
  delete: <T>(url: string) => request<T>('DELETE', url),
}

export async function refreshSession() {
  try {
    session.value = await api.get<Session>('/api/session')
  } catch {
    /* ignore */
  }
}

let sessionPromise: Promise<void> | null = null

// ensureSession 保证 session 至少被拉取过一次（首次导航/守卫使用），并复用同一个 in-flight 请求。
export function ensureSession(): Promise<void> {
  if (!sessionPromise) sessionPromise = refreshSession()
  return sessionPromise
}

// ---- 类型 ----
export interface Provider {
  name: string
  display_name: string
  status: string
  active_keys: number
  total_keys: number
  avg_ttft_ms: number
  priority: number
  model_count: number
}

export interface ModelEdge {
  provider: string
  native: string
  ttft_ms: number
  has_ttft: boolean
  healthy: boolean
  best: boolean
}

export interface Candidate {
  provider: string
  native: string
  canonical: string
  priority: number
  ttft_ms: number
  has_ttft: boolean
}

export interface KeyRow {
  id: number
  provider: string
  api_key: string
  email: string
  password: string
  status: string
  rate_limit_count: number
  rate_limit_until?: string
  first_request_at?: string
  last_used_at?: string
  created_at: string
  metadata?: string
}

export interface RequestLog {
  id: number
  trace_id: string
  attempt_no: number
  key_id?: number
  provider: string
  model: string
  status_code?: number
  ttft_ms?: number
  latency_ms?: number
  matched_rule?: string
  error_msg?: string
  error_fingerprint?: string
  created_at: string
}

export interface ErrorGroup {
  fingerprint: string
  provider: string
  status_code?: number
  models: string
  rules: string
  count: number
  first_seen: string
  last_seen: string
  sample_error: string
}

export interface Capture {
  id: number
  request_id: number
  matched_rule: string
  fingerprint?: string
  provider: string
  request_headers?: string
  request_body?: string
  response_headers?: string
  response_body?: string
  redacted: boolean
  created_at: string
}

export interface HealthSnapshot {
  provider: string
  model: string
  status: string
  avg_ttft_ms: number
  error_count: number
}

export interface RuleView {
  name: string
  description: string
  priority: number
  source: number
  builtin: boolean
  enabled: boolean
  type: string
  detail?: RuleDetail
}

export interface RuleDetail {
  when: Record<string, unknown>
  if: { field: string; op: string; value: number }[]
  do: { kind: string; mark_status: string; chain: boolean; fields: string[] }[]
  route: { mode: 'single' | 'race'; single: string; race: string[] }
}

export interface RulePreset {
  rule: Record<string, unknown>
  yaml: string
  exists: boolean
}
