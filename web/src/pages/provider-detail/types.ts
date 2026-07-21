import type { KeyRow } from '../../api'

export type ProviderManifest = Record<string, unknown> & {
  readonly display_name?: string
  readonly enabled?: boolean
  readonly base_url?: string
  readonly key_strategy?: string
  readonly priority?: number
  readonly weight?: number
  readonly has_register?: boolean
  readonly has_check?: boolean
  readonly default_check_model?: string
}

export type ProviderDetail = {
  readonly manifest: ProviderManifest
  readonly manifest_yaml: string
  readonly models: string[]
  readonly keys: KeyRow[]
  readonly last_register_at?: string
  readonly last_register_duration: number
  readonly last_register_error?: string
}

export type ProviderDetailResponse = Omit<ProviderDetail, 'models'> & {
  readonly models: string[] | null
}

export function formatProviderTime(value?: string): string {
  return value ? new Date(value).toLocaleString() : '—'
}

export function getErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
