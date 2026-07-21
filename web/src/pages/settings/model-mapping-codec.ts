import * as YAML from 'yaml'

export type ModelEntry = { canonical: string; aliases: string }
export type ModelPattern = { match: string; canonical: string }
export type ModelMappings = { entries: ModelEntry[]; patterns: ModelPattern[] }

type UnknownRecord = Record<string, unknown>

function isRecord(value: unknown): value is UnknownRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function record(value: unknown): UnknownRecord {
  return isRecord(value) ? value : {}
}

export function parseModelMappings(text: string): ModelMappings {
  const document = record(YAML.parse(text))
  const entries: ModelEntry[] = []
  const patterns: ModelPattern[] = []

  for (const key of Object.keys(document)) {
    if (key === 'patterns') {
      const rawPatterns = Array.isArray(document.patterns) ? document.patterns : []
      for (const rawPattern of rawPatterns) {
        const pattern = record(rawPattern)
        patterns.push({
          match: String(pattern.match ?? ''),
          canonical: String(pattern.canonical ?? ''),
        })
      }
      continue
    }
    const aliases = Array.isArray(document[key]) ? document[key] : []
    entries.push({ canonical: key, aliases: aliases.map(String).join(', ') })
  }

  return { entries, patterns }
}

export function serializeModelMappings(mappings: ModelMappings): string {
  const document: UnknownRecord = {}
  for (const entry of mappings.entries) {
    const canonical = entry.canonical.trim()
    if (!canonical) continue
    document[canonical] = entry.aliases.split(/[,\n]/).map((alias) => alias.trim()).filter(Boolean)
  }
  const patterns = mappings.patterns
    .filter((pattern) => pattern.match.trim() || pattern.canonical.trim())
    .map((pattern) => ({ match: pattern.match.trim(), canonical: pattern.canonical.trim() }))
  if (patterns.length) document.patterns = patterns
  if (!Object.keys(document).length) return ''
  return YAML.stringify(document, { lineWidth: 0 })
}
