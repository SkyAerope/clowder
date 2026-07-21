import assert from 'node:assert/strict'
import test from 'node:test'
import { ref } from 'vue'
import { createProviderKeyOperations } from '../src/pages/provider-detail/useProviderKeys.ts'

function operationsFor(keys, post, events = []) {
  return createProviderKeyOperations({
    providerName: 'example',
    keys: ref(keys),
    load: async () => { events.push('load') },
    post,
    deleteRequest: async () => {},
    success: () => { events.push('success') },
    warning: () => { events.push('warning') },
    error: () => { events.push('error') },
  })
}

test('bulk checking with no keys still reports success and reloads', async () => {
  // Given
  const events = []
  const operations = operationsFor([], async (url) => {
    events.push(`post:${url}`)
    return { check_status: 'active', metadata: '', model_response: '', ttft_ms: 0 }
  }, events)

  // When
  await operations.checkAll()

  // Then
  assert.deepEqual(events, ['success', 'load'])
  assert.deepEqual(operations.keyState.value, {})
})
