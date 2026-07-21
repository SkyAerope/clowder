import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildRequestParams,
  defaultRequestFilters,
  emptyRequestFilters,
  parseRequestFiltersFromQuery,
  statusFilterError,
} from '../src/pages/request-filters.ts'

process.env.TZ = 'UTC'

test('omits empty filters and keeps pagination', () => {
  // Given / When
  const params = new URLSearchParams(buildRequestParams(emptyRequestFilters, 50, 0))

  // Then
  assert.deepEqual([...params.keys()].sort(), ['limit', 'offset'])
  assert.equal(params.get('limit'), '50')
  assert.equal(params.get('offset'), '0')
})

test('encodes every filter and converts local datetime to UTC', () => {
  // Given
  const draft = {
    from: '2026-07-20T12:00',
    to: '2026-07-20T13:30',
    provider: 'alpha',
    model: 'm1',
    status: '429',
    error: 'true',
    rule: 'retry',
    fingerprint: 'fp-rate-%_',
    captured: 'false',
  }

  // When
  const qs = buildRequestParams(draft, 25, 50)
  const params = new URLSearchParams(qs)

  // Then
  assert.equal(params.get('from'), '2026-07-20T12:00:00.000Z')
  assert.equal(params.get('to'), '2026-07-20T13:30:00.000Z')
  assert.equal(params.get('provider'), 'alpha')
  assert.equal(params.get('model'), 'm1')
  assert.equal(params.get('status'), '429')
  assert.equal(params.get('error'), 'true')
  assert.equal(params.get('rule'), 'retry')
  assert.equal(params.get('fingerprint'), 'fp-rate-%_')
  assert.equal(params.get('captured'), 'false')
  assert.equal(params.get('limit'), '25')
  assert.equal(params.get('offset'), '50')
  assert.ok(qs.includes('fingerprint=fp-rate-%25_'))
})

test('includes error=false but omits the all state', () => {
  // Given / When
  const withFalse = new URLSearchParams(
    buildRequestParams({ ...emptyRequestFilters, error: 'false' }, 50, 0),
  )
  const withAll = new URLSearchParams(
    buildRequestParams({ ...emptyRequestFilters, error: 'all' }, 50, 0),
  )

  // Then
  assert.equal(withFalse.get('error'), 'false')
  assert.equal(withAll.get('error'), null)
})

test('includes captured=true but omits the all state', () => {
  // Given / When
  const withTrue = new URLSearchParams(
    buildRequestParams({ ...emptyRequestFilters, captured: 'true' }, 50, 0),
  )
  const withAll = new URLSearchParams(
    buildRequestParams({ ...emptyRequestFilters, captured: 'all' }, 50, 0),
  )

  // Then
  assert.equal(withTrue.get('captured'), 'true')
  assert.equal(withAll.get('captured'), null)
})

test('treats whitespace-only text fields as empty', () => {
  // Given / When
  const params = new URLSearchParams(
    buildRequestParams({ ...emptyRequestFilters, provider: '   ', model: '\t' }, 50, 0),
  )

  // Then
  assert.equal(params.get('provider'), null)
  assert.equal(params.get('model'), null)
})

test('accepts empty and in-range integer status codes', () => {
  // Given / When / Then
  assert.equal(statusFilterError(''), null)
  assert.equal(statusFilterError('   '), null)
  assert.equal(statusFilterError('100'), null)
  assert.equal(statusFilterError('429'), null)
  assert.equal(statusFilterError('599'), null)
})

test('rejects out-of-range or non-integer status codes', () => {
  // Given / When / Then
  assert.ok(statusFilterError('99'))
  assert.ok(statusFilterError('600'))
  assert.ok(statusFilterError('abc'))
  assert.ok(statusFilterError('12.5'))
  assert.ok(statusFilterError('2e2'))
})

test('defaults the time range to yesterday 00:00 through today 23:59', () => {
  // Given
  const now = new Date('2026-07-21T15:30:00')

  // When
  const defaults = defaultRequestFilters(now)

  // Then
  assert.equal(defaults.from, '2026-07-20T00:00')
  assert.equal(defaults.to, '2026-07-21T23:59')
  assert.equal(defaults.provider, '')
  assert.equal(defaults.error, '')
})

test('rolls the default start date back across a month boundary', () => {
  // Given
  const now = new Date('2026-07-01T08:00:00')

  // When
  const defaults = defaultRequestFilters(now)

  // Then
  assert.equal(defaults.from, '2026-06-30T00:00')
  assert.equal(defaults.to, '2026-07-01T23:59')
})

test('initializes request filters from URL and preserves explicit empty dates', () => {
  // Given
  const now = new Date('2026-07-21T15:30:00')

  // When
  const filters = parseRequestFiltersFromQuery({
    fingerprint: 'fp-rate',
    provider: ['alpha', 'ignored'],
    from: '',
    to: '',
    error: 'true',
  }, now)

  // Then
  assert.equal(filters.fingerprint, 'fp-rate')
  assert.equal(filters.provider, 'alpha')
  assert.equal(filters.from, '')
  assert.equal(filters.to, '')
  assert.equal(filters.error, 'true')
})

test('keeps default dates when URL omits them and normalizes invalid booleans', () => {
  // Given
  const now = new Date('2026-07-21T15:30:00')

  // When
  const filters = parseRequestFiltersFromQuery({ error: 'invalid', captured: ['false'] }, now)

  // Then
  assert.equal(filters.from, '2026-07-20T00:00')
  assert.equal(filters.to, '2026-07-21T23:59')
  assert.equal(filters.error, '')
  assert.equal(filters.captured, 'false')
})
