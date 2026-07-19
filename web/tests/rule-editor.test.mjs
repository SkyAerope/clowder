import assert from 'node:assert/strict'
import test from 'node:test'
import { blankRule, parseRuleFragment, serializeRuleFragment } from '../src/lib/rule-editor.ts'

test('parses the indented fragment produced for the error-rule dialog', () => {
  // Given
  const rule = blankRule()
  rule.name = 'known-featherless-71db6124'
  rule.description = '已知错误'
  rule.priority = 50
  rule.whenClauses.push({ field: 'provider', text: 'featherless', regex: false })
  rule.routeSingle = 'next_key'
  const source = serializeRuleFragment(rule)

  // When
  const parsed = parseRuleFragment(source)

  // Then
  assert.equal(parsed.name, rule.name)
  assert.equal(parsed.whenClauses[0]?.text, 'featherless')
  assert.equal(parsed.routeSingle, 'next_key')
})
