import { type Ref, ref } from 'vue'

export type ModelCheckState = {
  readonly loading: boolean
  readonly result: 'success' | 'error' | null
  readonly status: string
  readonly keyID: number
  readonly modelResponse: string
  readonly ttftMS: number
  readonly ruleName: string
  readonly statusCode: number
  readonly responseBody: string
  readonly applying: boolean
  readonly applied: boolean
  readonly errorMessage: string
}

type ModelCheckResponse = {
  readonly status: string
  readonly key_id: number
  readonly model_response: string
  readonly ttft_ms: number
  readonly rule_name: string
  readonly status_code: number
  readonly response_body: string
}

type ModelHealthOperationsDependencies = {
  readonly provider: string
  readonly post: <Response>(url: string, body: unknown) => Promise<Response>
  readonly checked: () => void
  readonly success: (message: string) => void
  readonly error: (message: string) => void
}

type ModelHealthOperations = {
  readonly checkStates: Ref<Record<string, ModelCheckState>>
  readonly checkModel: (model: string) => Promise<void>
  readonly applyRule: (model: string) => Promise<void>
}

function emptyCheckState(): ModelCheckState {
  return {
    loading: false, result: null, status: '', keyID: 0, modelResponse: '', ttftMS: 0,
    ruleName: '', statusCode: 0, responseBody: '', applying: false, applied: false, errorMessage: '',
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '未知错误'
}

export function createModelHealthOperations(
  dependencies: ModelHealthOperationsDependencies,
): ModelHealthOperations {
  const checkStates = ref<Record<string, ModelCheckState>>({})

  async function checkModel(model: string): Promise<void> {
    checkStates.value[model] = { ...emptyCheckState(), loading: true }
    try {
      const response = await dependencies.post<ModelCheckResponse>(
        `/api/admin/providers/${dependencies.provider}/check-model`,
        { model },
      )
      checkStates.value[model] = {
        ...emptyCheckState(),
        result: response.status === 'active' ? 'success' : 'error',
        status: response.status,
        keyID: response.key_id,
        modelResponse: response.model_response,
        ttftMS: response.ttft_ms,
        ruleName: response.rule_name,
        statusCode: response.status_code,
        responseBody: response.response_body,
      }
      dependencies.checked()
    } catch (error) {
      checkStates.value[model] = { ...emptyCheckState(), result: 'error', errorMessage: errorMessage(error) }
    }
  }

  async function applyRule(model: string): Promise<void> {
    const state = checkStates.value[model]
    if (!state?.ruleName || state.applying || state.applied) return
    checkStates.value[model] = { ...state, applying: true }
    try {
      await dependencies.post(`/api/admin/providers/${dependencies.provider}/apply-model-check-rule`, {
        model, key_id: state.keyID, rule_name: state.ruleName,
        status_code: state.statusCode, response_body: state.responseBody,
      })
      checkStates.value[model] = { ...state, applying: false, applied: true }
      dependencies.success(`已应用规则 ${state.ruleName}`)
      dependencies.checked()
    } catch (error) {
      checkStates.value[model] = { ...state, applying: false }
      dependencies.error(`应用规则失败: ${errorMessage(error)}`)
    }
  }

  return { checkStates, checkModel, applyRule }
}
