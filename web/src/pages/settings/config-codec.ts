import * as YAML from 'yaml'

export type ConfigForm = {
  bind_addr: string
  port: number
  api_key: string
  providers_dir: string
  dashboard: { public: boolean; admin: boolean; admin_key: string }
  pool: {
    min_active_keys: number
    max_total_keys: number
    auto_register: boolean
    register_interval: number
    register_backoff_base_seconds: number
    register_backoff_max_seconds: number
  }
  routing: { max_retries: number; max_parallel_attempts: number; upstream_streaming: boolean }
  backoff: { base_seconds: number; max_seconds: number }
  capture: { redact: boolean; max_records: number; response_buffer_kb: number }
}

type UnknownRecord = Record<string, unknown>

function isRecord(value: unknown): value is UnknownRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function record(value: unknown): UnknownRecord {
  return isRecord(value) ? value : {}
}

function number(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

export function defaultConfig(): ConfigForm {
  return {
    bind_addr: '0.0.0.0',
    port: 12100,
    api_key: '',
    providers_dir: 'providers/',
    dashboard: { public: true, admin: true, admin_key: '' },
    pool: {
      min_active_keys: 5,
      max_total_keys: 20,
      auto_register: false,
      register_interval: 300,
      register_backoff_base_seconds: 60,
      register_backoff_max_seconds: 3600,
    },
    routing: { max_retries: 10, max_parallel_attempts: 2, upstream_streaming: true },
    backoff: { base_seconds: 5, max_seconds: 600 },
    capture: { redact: false, max_records: 1000, response_buffer_kb: 256 },
  }
}

export function parseConfig(text: string): ConfigForm {
  const config = defaultConfig()
  const document = record(YAML.parse(text))
  const dashboard = record(document.dashboard)
  const pool = record(document.pool)
  const routing = record(document.routing)
  const backoff = record(document.backoff)
  const capture = record(document.capture)

  config.bind_addr = typeof document.bind_addr === 'string' ? document.bind_addr : config.bind_addr
  config.port = number(document.port, config.port)
  config.api_key = typeof document.api_key === 'string' ? document.api_key : ''
  config.providers_dir = typeof document.providers_dir === 'string' ? document.providers_dir : config.providers_dir
  config.dashboard.public = typeof dashboard.public === 'boolean' ? dashboard.public : config.dashboard.public
  config.dashboard.admin = typeof dashboard.admin === 'boolean' ? dashboard.admin : config.dashboard.admin
  config.dashboard.admin_key = typeof dashboard.admin_key === 'string' ? dashboard.admin_key : ''
  config.pool.min_active_keys = number(pool.min_active_keys, config.pool.min_active_keys)
  config.pool.max_total_keys = number(pool.max_total_keys, config.pool.max_total_keys)
  config.pool.auto_register = typeof pool.auto_register === 'boolean' ? pool.auto_register : config.pool.auto_register
  config.pool.register_interval = number(pool.register_interval, config.pool.register_interval)
  config.pool.register_backoff_base_seconds = number(pool.register_backoff_base_seconds, config.pool.register_backoff_base_seconds)
  config.pool.register_backoff_max_seconds = number(pool.register_backoff_max_seconds, config.pool.register_backoff_max_seconds)
  config.routing.max_retries = number(routing.max_retries, config.routing.max_retries)
  config.routing.max_parallel_attempts = number(routing.max_parallel_attempts, config.routing.max_parallel_attempts)
  config.routing.upstream_streaming = typeof routing.upstream_streaming === 'boolean' ? routing.upstream_streaming : config.routing.upstream_streaming
  config.backoff.base_seconds = number(backoff.base_seconds, config.backoff.base_seconds)
  config.backoff.max_seconds = number(backoff.max_seconds, config.backoff.max_seconds)
  config.capture.redact = typeof capture.redact === 'boolean' ? capture.redact : config.capture.redact
  config.capture.max_records = number(capture.max_records, config.capture.max_records)
  config.capture.response_buffer_kb = number(capture.response_buffer_kb, config.capture.response_buffer_kb)
  return config
}

export function serializeConfig(config: ConfigForm): string {
  return YAML.stringify({
    bind_addr: config.bind_addr,
    port: config.port,
    api_key: config.api_key,
    dashboard: {
      public: config.dashboard.public,
      admin: config.dashboard.admin,
      admin_key: config.dashboard.admin_key,
    },
    providers_dir: config.providers_dir,
    pool: {
      min_active_keys: config.pool.min_active_keys,
      max_total_keys: config.pool.max_total_keys,
      auto_register: config.pool.auto_register,
      register_interval: config.pool.register_interval,
      register_backoff_base_seconds: config.pool.register_backoff_base_seconds,
      register_backoff_max_seconds: config.pool.register_backoff_max_seconds,
    },
    routing: {
      max_retries: config.routing.max_retries,
      max_parallel_attempts: config.routing.max_parallel_attempts,
      upstream_streaming: config.routing.upstream_streaming,
    },
    backoff: {
      base_seconds: config.backoff.base_seconds,
      max_seconds: config.backoff.max_seconds,
    },
    capture: {
      redact: config.capture.redact,
      max_records: config.capture.max_records,
      response_buffer_kb: config.capture.response_buffer_kb,
    },
  }, { lineWidth: 0 })
}
