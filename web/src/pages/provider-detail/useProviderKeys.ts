import { type Ref, ref } from 'vue'
import type { KeyRow } from '../../api'

export type KeyCheckState = {
  readonly loading: boolean
  readonly result: 'success' | 'error' | null
  readonly checkStatus: string
  readonly metadata: string
  readonly modelResponse: string
  readonly ttftMS: number
}

type CheckResponse = {
  readonly check_status: string
  readonly metadata: string
  readonly model_response: string
  readonly ttft_ms: number
}

type ProviderKeyDependencies = {
  readonly providerName: string
  readonly keys: Ref<readonly KeyRow[]>
  readonly load: () => Promise<void>
  readonly post: <T>(url: string, body?: unknown) => Promise<T>
  readonly deleteRequest: (url: string) => Promise<unknown>
  readonly success: (message: string) => void
  readonly warning: (message: string) => void
  readonly error: (message: string) => void
}

export const keyStatuses = [
  { value: 'active', label: '激活' },
  { value: 'idle', label: '空闲' },
  { value: 'invalid', label: '无效' },
] as const

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

const emptyCheckState = (): KeyCheckState => ({
  loading: false,
  result: null,
  checkStatus: '',
  metadata: '',
  modelResponse: '',
  ttftMS: 0,
})

export function createProviderKeyOperations(dependencies: ProviderKeyDependencies) {
  const keyState = ref<Record<number, KeyCheckState>>({})
  const checking = ref(false)

  async function keyAction(key: KeyRow, action: 'disable' | 'enable' | 'check'): Promise<void> {
    if (action === 'check') keyState.value[key.id] = { ...emptyCheckState(), loading: true }
    try {
      const response = await dependencies.post<CheckResponse>(`/api/admin/keys/${key.id}/${action}`)
      if (action === 'check') {
        keyState.value[key.id] = {
          loading: false,
          result: response.check_status === 'active' ? 'success' : 'error',
          checkStatus: response.check_status ?? '',
          metadata: response.metadata ?? '',
          modelResponse: response.model_response ?? '',
          ttftMS: response.ttft_ms ?? 0,
        }
      } else {
        const label = action === 'disable' ? '已禁用' : '已启用'
        dependencies.success(`key #${key.id} ${label}`)
      }
      await dependencies.load()
    } catch (error: unknown) {
      if (action === 'check') keyState.value[key.id] = { ...emptyCheckState(), result: 'error' }
      else dependencies.error(`key #${key.id} ${action} 失败: ${errorMessage(error)}`)
    }
  }

  async function setKeyStatus(key: KeyRow, status: string): Promise<void> {
    if (key.status === status) return
    try {
      await dependencies.post(`/api/admin/keys/${key.id}/status`, { status })
      const label = keyStatuses.find((item) => item.value === status)?.label ?? status
      dependencies.success(`key #${key.id} 已设为${label}`)
      await dependencies.load()
    } catch (error: unknown) {
      dependencies.error(`key #${key.id} 状态更新失败: ${errorMessage(error)}`)
    }
  }

  async function deleteKey(key: KeyRow): Promise<void> {
    try {
      await dependencies.deleteRequest(`/api/admin/keys/${key.id}`)
      dependencies.success(`key #${key.id} 已删除`)
      await dependencies.load()
    } catch (error: unknown) {
      dependencies.error(`key #${key.id} 删除失败: ${errorMessage(error)}`)
    }
  }

  async function checkAll(): Promise<void> {
    checking.value = true
    let succeeded = 0
    let failed = 0
    for (const key of dependencies.keys.value) {
      await keyAction(key, 'check')
      if (keyState.value[key.id]?.result === 'success') succeeded++
      else failed++
    }
    if (failed > 0) dependencies.warning(`check 完成：成功 ${succeeded}，失败 ${failed}`)
    else dependencies.success(`check 完成：全部 ${succeeded} 个通过`)
    checking.value = false
    await dependencies.load()
  }

  return { keyState, checking, keyAction, setKeyStatus, deleteKey, checkAll }
}
