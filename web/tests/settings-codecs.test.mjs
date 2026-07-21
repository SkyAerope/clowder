import assert from 'node:assert/strict'
import test from 'node:test'
import * as YAML from 'yaml'
import { parseConfig, serializeConfig } from '../src/pages/settings/config-codec.ts'
import { parseModelMappings, serializeModelMappings } from '../src/pages/settings/model-mapping-codec.ts'

test('applies current config defaults and numeric coercion when parsing YAML', () => {
  // Given
  const source = `port: "13000"
dashboard:
  public: false
pool:
  min_active_keys: not-a-number
routing:
  upstream_streaming: false
capture:
  max_records: 25
`

  // When
  const config = parseConfig(source)

  // Then
  assert.equal(config.port, 13000)
  assert.equal(config.dashboard.public, false)
  assert.equal(config.dashboard.admin, true)
  assert.equal(config.pool.min_active_keys, 5)
  assert.equal(config.routing.upstream_streaming, false)
  assert.equal(config.capture.max_records, 25)
})

test('drops unknown config fields through the current visual-form round trip', () => {
  // Given
  const source = `bind_addr: 127.0.0.1
future_top_level: keep-me
dashboard:
  public: true
  future_dashboard_option: keep-me-too
pool:
  future_pool_option: 42
`

  // When
  const serialized = YAML.parse(serializeConfig(parseConfig(source)))

  // Then
  assert.deepEqual(Object.keys(serialized).sort(), [
    'api_key',
    'backoff',
    'bind_addr',
    'capture',
    'dashboard',
    'pool',
    'port',
    'providers_dir',
    'routing',
  ])
  assert.deepEqual(Object.keys(serialized.dashboard).sort(), ['admin', 'admin_key', 'public'])
  assert.deepEqual(Object.keys(serialized.pool).sort(), [
    'auto_register',
    'max_total_keys',
    'min_active_keys',
    'register_backoff_base_seconds',
    'register_backoff_max_seconds',
    'register_interval',
  ])
})

test('parses model mappings with current treatment of unknown and malformed fields', () => {
  // Given
  // The editor intentionally preserves its current permissive mapping semantics;
  // malformed scalar entries become empty alias lists instead of being rejected.
  const source = `known-model: [native-a, 7]
future_scalar: keep-me
patterns:
  - match: ^known
    canonical: known-model
    future_pattern_option: keep-me-too
`

  // When
  const mappings = parseModelMappings(source)

  // Then
  assert.deepEqual(mappings.entries, [
    { canonical: 'known-model', aliases: 'native-a, 7' },
    { canonical: 'future_scalar', aliases: '' },
  ])
  assert.deepEqual(mappings.patterns, [{ match: '^known', canonical: 'known-model' }])
})

test('treats a non-list patterns field as no patterns', () => {
  // Given
  const source = `model-a: [native-a]
patterns: { match: ignored, canonical: ignored }
`

  // When
  const mappings = parseModelMappings(source)

  // Then
  assert.deepEqual(mappings.entries, [{ canonical: 'model-a', aliases: 'native-a' }])
  assert.deepEqual(mappings.patterns, [])
})

test('serializes model mappings with current trimming and omission semantics', () => {
  // Given
  const mappings = {
    entries: [
      { canonical: ' model-a ', aliases: ' native-a,\nnative-b, ,' },
      { canonical: ' ', aliases: 'ignored' },
    ],
    patterns: [
      { match: ' ^model ', canonical: ' model-a ' },
      { match: ' ', canonical: ' ' },
    ],
  }

  // When
  const reparsed = parseModelMappings(serializeModelMappings(mappings))

  // Then
  assert.deepEqual(reparsed.entries, [{ canonical: 'model-a', aliases: 'native-a, native-b' }])
  assert.deepEqual(reparsed.patterns, [{ match: '^model', canonical: 'model-a' }])
})
