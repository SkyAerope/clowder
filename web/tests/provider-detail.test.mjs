import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const providerDetailPath = new URL('../src/pages/ProviderDetail.vue', import.meta.url)

test('renders bulk check as an unconditional Keys card action', async () => {
  const source = await readFile(providerDetailPath, 'utf8')
  const keysCard = source.match(/<Card :title="`Keys[\s\S]*?<\/Card>/)?.[0]

  assert.ok(keysCard, 'Keys card should exist')
  assert.match(keysCard, /<template #action>/)
  assert.match(keysCard, /@click="checkAll"/)
  assert.doesNotMatch(keysCard, /has_check/)

  const pageHeader = source.match(/<div class="flex items-center justify-between flex-wrap gap-2">[\s\S]*?<\/div>\s*<div class="grid/)?.[0]
  assert.ok(pageHeader, 'provider page header should exist')
  assert.doesNotMatch(pageHeader, /@click="checkAll"/)
})

test('renders key metadata and bulk check reuses each row check action', async () => {
  const source = await readFile(providerDetailPath, 'utf8')
  const keysCard = source.match(/<Card :title="`Keys[\s\S]*?<\/Card>/)?.[0]
  const checkAll = source.match(/async function checkAll\(\) \{[\s\S]*?\n\}/)?.[0]

  assert.ok(keysCard, 'Keys card should exist')
  assert.match(keysCard, /<TableHead>Metadata<\/TableHead>/)
  assert.match(keysCard, /\{\{ k\.metadata \|\| '—' \}\}/)
  assert.match(keysCard, /:colspan="7"/)

  assert.ok(checkAll, 'checkAll should exist')
  assert.match(checkAll, /await keyAction\(k, 'check'\)/)
  assert.doesNotMatch(checkAll, /api\.post/)
})
