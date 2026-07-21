import assert from 'node:assert/strict'
import test from 'node:test'
import { ref } from 'vue'
import { createProviderKeyOperations } from '../src/pages/provider-detail/useProviderKeys.ts'

function key(id) {
  return {
    id,
    provider: 'example',
    api_key: `key-${id}`,
    email: '',
    status: 'idle',
    rate_limit_count: 0,
  }
}

test('checks every key sequentially and reloads after each check plus once at the end', async () => {
  // Given
  const events = []
  const keys = ref([key(1), key(2)])
  const operations = createProviderKeyOperations({
    providerName: 'example',
    keys,
    load: async () => { events.push('load') },
    post: async (url) => {
      events.push(`post:${url}`)
      return { check_status: 'active', metadata: '', model_response: '', ttft_ms: 0 }
    },
    deleteRequest: async () => {},
    success: () => {},
    warning: () => {},
    error: () => {},
  })

  // When
  await operations.checkAll()

  // Then
  assert.deepEqual(events, [
    'post:/api/admin/keys/1/check',
    'load',
    'post:/api/admin/keys/2/check',
    'load',
    'load',
  ])
  assert.equal(operations.keyState.value[1]?.result, 'success')
  assert.equal(operations.keyState.value[2]?.result, 'success')
})

test('maps a non-active check response to the current error result without a toast', async () => {
  // Given
  const errors = []
  const operations = createProviderKeyOperations({
    providerName: 'example',
    keys: ref([key(7)]),
    load: async () => {},
    post: async () => ({
      check_status: 'invalid',
      metadata: 'expired',
      model_response: 'denied',
      ttft_ms: 42,
    }),
    deleteRequest: async () => {},
    success: () => {},
    warning: () => {},
    error: (message) => { errors.push(message) },
  })

  // When
  await operations.keyAction(key(7), 'check')

  // Then
  assert.deepEqual(operations.keyState.value[7], {
    loading: false,
    result: 'error',
    checkStatus: 'invalid',
    metadata: 'expired',
    modelResponse: 'denied',
    ttftMS: 42,
  })
  assert.deepEqual(errors, [])
})
