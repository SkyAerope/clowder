import assert from 'node:assert/strict'
import test from 'node:test'
import { createModelHealthOperations } from '../src/components/model-health-operations.ts'

function checkResponse(overrides = {}) {
  return {
    status: 'active',
    key_id: 7,
    model_response: 'ready',
    ttft_ms: 42,
    rule_name: '',
    status_code: 200,
    response_body: '{"ok":true}',
    ...overrides,
  }
}

function operationsFor(post, events = []) {
  return createModelHealthOperations({
    provider: 'example',
    post,
    checked: () => { events.push('checked') },
    success: (message) => { events.push(`success:${message}`) },
    error: (message) => { events.push(`error:${message}`) },
  })
}

test('posts a model check with a JSON-encoded model value', async () => {
  // Given
  const requests = []
  const operations = operationsFor(async (url, body) => {
    requests.push({ url, body })
    return checkResponse()
  })

  // When
  await operations.checkModel('vendor/model:preview')

  // Then
  assert.deepEqual(requests, [{
    url: '/api/admin/providers/example/check-model',
    body: { model: 'vendor/model:preview' },
  }])
})

test('maps an active model check response to success', async () => {
  // Given
  const operations = operationsFor(async () => checkResponse({ rule_name: 'returned-rule' }))

  // When
  await operations.checkModel('model-a')

  // Then
  assert.deepEqual(operations.checkStates.value['model-a'], {
    loading: false,
    result: 'success',
    status: 'active',
    keyID: 7,
    modelResponse: 'ready',
    ttftMS: 42,
    ruleName: 'returned-rule',
    statusCode: 200,
    responseBody: '{"ok":true}',
    applying: false,
    applied: false,
    errorMessage: '',
  })
})

test('maps a non-active model check response to error', async () => {
  // Given
  const operations = operationsFor(async () => checkResponse({ status: 'invalid' }))

  // When
  await operations.checkModel('model-b')

  // Then
  assert.equal(operations.checkStates.value['model-b']?.result, 'error')
  assert.equal(operations.checkStates.value['model-b']?.status, 'invalid')
})

test('maps a model check request failure to an error result', async () => {
  // Given
  const operations = operationsFor(async () => { throw new Error('network unavailable') })

  // When
  await operations.checkModel('model-c')

  // Then
  assert.equal(operations.checkStates.value['model-c']?.result, 'error')
  assert.equal(operations.checkStates.value['model-c']?.errorMessage, 'network unavailable')
})

test('does not apply a rule unless the model check returned rule_name', async () => {
  // Given
  const requests = []
  const operations = operationsFor(async (url, body) => {
    requests.push({ url, body })
    return checkResponse()
  })
  await operations.checkModel('model-d')
  requests.length = 0

  // When
  await operations.applyRule('model-d')

  // Then
  assert.deepEqual(requests, [])
})

test('applies the returned rule and completes before toast and checked callbacks', async () => {
  // Given
  const events = []
  let operations
  operations = createModelHealthOperations({
    provider: 'example',
    post: async (url, body) => {
      events.push({ url, body })
      return url.endsWith('/check-model')
        ? checkResponse({ rule_name: 'returned-rule', status_code: 429, response_body: 'limited' })
        : undefined
    },
    checked: () => { events.push(`checked:${operations.checkStates.value['model-e']?.applied}`) },
    success: () => { events.push(`success:${operations.checkStates.value['model-e']?.applied}`) },
    error: (message) => { events.push(`error:${message}`) },
  })
  await operations.checkModel('model-e')
  events.length = 0

  // When
  await operations.applyRule('model-e')

  // Then
  assert.deepEqual(events, [
    {
      url: '/api/admin/providers/example/apply-model-check-rule',
      body: {
        model: 'model-e',
        key_id: 7,
        rule_name: 'returned-rule',
        status_code: 429,
        response_body: 'limited',
      },
    },
    'success:true',
    'checked:true',
  ])
})
