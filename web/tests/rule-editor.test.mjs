import assert from 'node:assert/strict'
import test, { after } from 'node:test'
import { createServer } from 'vite'

const vite = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
const { parseRuleFragment, parseRules, serializeRules } = await vite.ssrLoadModule('/src/lib/rule-editor.ts')
after(() => vite.close())

test('parses an independently authored indented error-rule fragment', () => {
  // Given
  const source = `  - known-featherless-71db6124:
      name: 已知错误
      priority: 50
      when:
        provider: featherless
      route: next_key
`

  // When
  const parsed = parseRuleFragment(source)

  // Then
  assert.equal(parsed.slug, 'known-featherless-71db6124')
  assert.equal(parsed.name, '已知错误')
  assert.equal(parsed.whenClauses[0]?.text, 'featherless')
  assert.equal(parsed.routeSingle, 'next_key')
})

test('serializes rule name before slug using the new schema', () => {
  // Given
  const source = `rules:\n  - catch-all:\n      name: 兜底规则\n      when: { error: true }\n      route: passthrough\n`

  // When
  const serialized = serializeRules(parseRules(source).rules)

  // Then
  assert.match(serialized, /- catch-all:\n\s+name: 兜底规则/)
  assert.doesNotMatch(serialized, /slug:/)
  assert.doesNotMatch(serialized, /description:/)
})

test('preserves the boolean error trigger through a YAML round trip', () => {
  // Given
  const source = `rules:\n  - catch-all:\n      when: { error: true }\n      route: passthrough\n`

  // When
  const parsed = parseRules(source)
  const reparsed = parseRules(serializeRules(parsed.rules))

  // Then
  assert.deepEqual(parsed.rules[0]?.whenClauses, [{ field: 'error', text: '', regex: false }])
  assert.deepEqual(reparsed.rules[0]?.whenClauses, parsed.rules[0]?.whenClauses)
})

test('normalizes upstream trigger variants from YAML', () => {
  // Given
  const source = `rules:
  - normalized-triggers:
      when:
        status_code: 5xx
        transport: true
        body_regex: rate.*limit
        ttft_over_ms: 2500
        error: true
      route: next_provider
`

  // When
  const parsed = parseRules(source)
  const reparsed = parseRules(serializeRules(parsed.rules))

  // Then
  assert.deepEqual(reparsed.rules[0]?.whenClauses, [
    { field: 'status_code', text: '500-599', regex: false },
    { field: 'body', text: 'rate.*limit', regex: true },
    { field: 'transport', text: 'true', regex: false },
    { field: 'ttft_over_ms', text: '2500', regex: false },
    { field: 'error', text: '', regex: false },
  ])
})

test('normalizes action payload variants from YAML', () => {
  // Given
  const source = `rules:
  - normalized-actions:
      when: { error: true }
      do:
        - mark_key: rate_limited
        - check_key: { chain: true }
        - check_keys: {}
        - capture: { fields: [request_body, status_code] }
        - mark_provider_unhealthy: {}
      route: next_key
`

  // When
  const parsed = parseRules(source)
  const reparsed = parseRules(serializeRules(parsed.rules))

  // Then
  assert.deepEqual(reparsed.rules[0]?.dos, [
    { kind: 'mark_key', mark_status: 'rate_limited', chain: false, fields: [] },
    { kind: 'check_key', mark_status: 'invalid', chain: true, fields: [] },
    { kind: 'check_keys', mark_status: 'invalid', chain: false, fields: [] },
    { kind: 'capture', mark_status: 'invalid', chain: false, fields: ['request_body', 'status_code'] },
    { kind: 'mark_provider_unhealthy', mark_status: 'invalid', chain: false, fields: [] },
  ])
})

test('normalizes both race route encodings through a YAML round trip', () => {
  // Given
  const source = `rules:
  - direct-race:
      when: { error: true }
      route: [next_key, next_provider]
  - nested-race:
      when: { error: true }
      route: { race: [retry_same, next_key] }
`

  // When
  const parsed = parseRules(source)
  const reparsed = parseRules(serializeRules(parsed.rules))

  // Then
  assert.deepEqual(
    reparsed.rules.map(({ routeMode, routeRace }) => ({ routeMode, routeRace })),
    [
      { routeMode: 'race', routeRace: ['next_key', 'next_provider'] },
      { routeMode: 'race', routeRace: ['retry_same', 'next_key'] },
    ],
  )
})

test('ignores unknown document fields and non-list rules', () => {
  // Given
  const source = `schema_version: 7\nrules: not-a-list\nfuture_options: { mode: strict }\n`

  // When
  const parsed = parseRules(source)

  // Then
  assert.deepEqual(parsed.rules, [])
  assert.deepEqual([...parsed.disabledBuiltins], [])
})

test('rejects invalid YAML', () => {
  // Given
  const source = `rules:\n  - invalid: [unterminated\n`

  // When / Then
  assert.throws(() => parseRules(source))
})

test('preserves disabled builtin identifiers through a YAML round trip', () => {
  // Given
  const source = `disabled_builtins: [rate-limit, transport-error]\nrules: []\n`

  // When
  const parsed = parseRules(source)
  const reparsed = parseRules(serializeRules(parsed.rules, parsed.disabledBuiltins))

  // Then
  assert.deepEqual([...reparsed.disabledBuiltins], ['rate-limit', 'transport-error'])
})
