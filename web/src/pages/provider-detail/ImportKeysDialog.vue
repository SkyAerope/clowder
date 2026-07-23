<script setup lang="ts">
import { ref } from 'vue'
import { toast } from 'vue-sonner'
import { Save } from '@lucide/vue'
import { api } from '../../api'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { getErrorMessage } from './types'

const props = defineProps<{
  providerName: string
  afterSave: () => Promise<void>
}>()
const openState = ref(false)
const importing = ref(false)
const importKeys = ref('')

function open(): void {
  openState.value = true
}

async function importKeysToDatabase(): Promise<void> {
  importing.value = true
  try {
    const result = await api.post<{ added: number; skipped: number }>(`/api/admin/providers/${props.providerName}/keys`, {
      keys: importKeys.value,
    })
    openState.value = false
    importKeys.value = ''
    toast.success(`已导入 ${result.added} 个 key，跳过 ${result.skipped} 个`)
    await props.afterSave()
  } catch (error: unknown) {
    toast.error(`导入失败: ${getErrorMessage(error)}`)
  } finally {
    importing.value = false
  }
}

defineExpose({ open })
</script>

<template>
  <Dialog v-model:open="openState">
    <DialogContent class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>添加 Key</DialogTitle>
        <DialogDescription>每行输入一个 key，保存后直接写入数据库。</DialogDescription>
      </DialogHeader>
      <Textarea v-model="importKeys" :rows="10" class="max-h-[60vh] overflow-y-auto font-mono text-xs" placeholder="key-one\nkey-two" spellcheck="false" />
      <DialogFooter>
        <Button variant="outline" @click="openState = false">取消</Button>
        <Button :disabled="importing || !importKeys.trim()" @click="importKeysToDatabase">
          <Save class="size-4" /> 保存
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
