import assert from 'node:assert/strict'
import test from 'node:test'
import { parseManifest, serializeManifest } from '../src/pages/provider-detail/manifest-codec.ts'

test('preserves unknown manifest fields through a visual-form round trip', () => {
  // Given
  const source = `name: example
enabled: true
base_url: https://example.test/v1
scripts:
  check: check.sh
  future_script_option: keep-me
routing:
  priority: 20
  future_routing_option: keep-me-too
rules:
  - name: custom-rule
    when: { status_code: 418 }
future_top_level: { mode: strict }
`

  // When
  const parsed = parseManifest(source)
  parsed.form.display_name = 'Example Provider'
  const reparsed = parseManifest(serializeManifest(parsed.raw, parsed.form))

  // Then
  assert.deepEqual(reparsed.raw.rules, [{ name: 'custom-rule', when: { status_code: 418 } }])
  assert.deepEqual(reparsed.raw.future_top_level, { mode: 'strict' })
  assert.equal(reparsed.raw.scripts.future_script_option, 'keep-me')
  assert.equal(reparsed.raw.routing.future_routing_option, 'keep-me-too')
  assert.equal(reparsed.form.display_name, 'Example Provider')
})

test('applies current visual-form defaults and omits default script values', () => {
  // Given
  const source = 'base_url: https://example.test/v1\n'

  // When
  const parsed = parseManifest(source)
  const reparsed = parseManifest(serializeManifest(parsed.raw, parsed.form))

  // Then
  assert.deepEqual(reparsed.form, {
    display_name: '',
    description: '',
    base_url: 'https://example.test/v1',
    format: 'openai-compatible',
    key_strategy: 'sticky',
    default_check_model: '',
    models: [],
    register: '',
    check: '',
    register_timeout: 300,
    check_timeout: 60,
    priority: 100,
    weight: 1,
    hedge: false,
  })
  assert.equal(Object.hasOwn(reparsed.raw, 'scripts'), false)
  assert.deepEqual(reparsed.raw.routing, { priority: 100, weight: 1 })
})
