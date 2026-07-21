import * as YAML from 'yaml'

export type ManifestForm = {
  display_name: string
  description: string
  base_url: string
  format: string
  key_strategy: string
  default_check_model: string
  models: string[]
  register: string
  check: string
  register_timeout: number
  check_timeout: number
  priority: number
  weight: number
  hedge: boolean
}

export type ManifestDocument = Record<string, unknown>

export type ParsedManifest = {
  readonly raw: ManifestDocument
  readonly form: ManifestForm
}

function isRecord(value: unknown): value is ManifestDocument {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function recordValue(value: unknown): ManifestDocument {
  return isRecord(value) ? value : {}
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}

function numberValue(value: unknown, fallback: number): number {
  const number = Number(value)
  return Number.isFinite(number) ? number : fallback
}

function booleanValue(value: unknown, fallback = false): boolean {
  return typeof value === 'boolean' ? value : fallback
}

function setOrDelete(target: ManifestDocument, key: string, value: unknown, empty: boolean): void {
  if (empty) delete target[key]
  else target[key] = value
}

export function parseManifest(text: string): ParsedManifest {
  const raw = recordValue(YAML.parse(text))
  const scripts = recordValue(raw.scripts)
  const routing = recordValue(raw.routing)
  const models = Array.isArray(raw.models) ? raw.models.map(String) : []

  return {
    raw,
    form: {
      display_name: stringValue(raw.display_name),
      description: stringValue(raw.description),
      base_url: stringValue(raw.base_url),
      format: stringValue(raw.format, 'openai-compatible'),
      key_strategy: stringValue(raw.key_strategy, 'sticky'),
      default_check_model: stringValue(raw.default_check_model),
      models,
      register: stringValue(scripts.register),
      check: stringValue(scripts.check),
      register_timeout: numberValue(scripts.register_timeout, 300),
      check_timeout: numberValue(scripts.check_timeout, 60),
      priority: numberValue(routing.priority, 100),
      weight: numberValue(routing.weight, 1),
      hedge: booleanValue(routing.hedge),
    },
  }
}

export function serializeManifest(raw: ManifestDocument, form: ManifestForm): string {
  const document: ManifestDocument = { ...raw }
  setOrDelete(document, 'display_name', form.display_name, !form.display_name)
  setOrDelete(document, 'description', form.description, !form.description)
  document.base_url = form.base_url
  setOrDelete(document, 'format', form.format, !form.format)
  document.key_strategy = form.key_strategy
  setOrDelete(document, 'default_check_model', form.default_check_model, !form.default_check_model)
  setOrDelete(document, 'models', form.models, form.models.length === 0)

  const scripts: ManifestDocument = { ...recordValue(raw.scripts) }
  setOrDelete(scripts, 'register', form.register, !form.register)
  setOrDelete(scripts, 'check', form.check, !form.check)
  setOrDelete(scripts, 'register_timeout', form.register_timeout, form.register_timeout === 300)
  setOrDelete(scripts, 'check_timeout', form.check_timeout, form.check_timeout === 60)
  setOrDelete(document, 'scripts', scripts, Object.keys(scripts).length === 0)

  const routing: ManifestDocument = { ...recordValue(raw.routing) }
  routing.priority = form.priority
  routing.weight = form.weight
  setOrDelete(routing, 'hedge', true, !form.hedge)
  document.routing = routing

  return YAML.stringify(document, { lineWidth: 0 })
}
