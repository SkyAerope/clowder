<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../api'
import Card from '../components/Card.vue'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Save, RotateCw } from 'lucide-vue-next'

const configYaml = ref('')
const modelsYaml = ref('')
const msg = ref('')
const saving = ref(false)
const reloading = ref(false)

async function load() {
  const r = await api.get<{ config_yaml: string; models_yaml: string }>('/api/admin/config')
  configYaml.value = r.config_yaml
  modelsYaml.value = r.models_yaml
}

async function save() {
  saving.value = true
  msg.value = ''
  try {
    await api.put('/api/admin/config', { config_yaml: configYaml.value, models_yaml: modelsYaml.value })
    msg.value = '已保存并热重载（端口等冷字段需重启进程生效）'
  } catch (e: any) {
    msg.value = `保存失败: ${e.message}`
  } finally {
    saving.value = false
  }
}

async function reload() {
  reloading.value = true
  msg.value = ''
  try {
    await api.post('/api/admin/reload')
    msg.value = '已重载'
    await load()
  } catch (e: any) {
    msg.value = `重载失败: ${e.message}`
  } finally {
    reloading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <h1 class="text-xl font-semibold">设置</h1>
      <div class="flex gap-2">
        <Button variant="outline" size="sm" :disabled="reloading" @click="reload">
          <RotateCw class="size-4" :class="reloading && 'animate-spin'" /> 重载
        </Button>
        <Button size="sm" :disabled="saving" @click="save">
          <Save class="size-4" /> 保存并生效
        </Button>
      </div>
    </div>

    <Alert v-if="msg" :variant="msg.includes('失败') ? 'destructive' : 'default'">
      <AlertDescription>{{ msg }}</AlertDescription>
    </Alert>

    <div class="grid grid-cols-1 xl:grid-cols-2 gap-4">
      <Card title="config.yaml">
        <Textarea v-model="configYaml" :rows="24" class="font-mono text-xs leading-relaxed" spellcheck="false" />
      </Card>
      <Card title="models.yaml（模型映射）">
        <Textarea v-model="modelsYaml" :rows="24" class="font-mono text-xs leading-relaxed" spellcheck="false" />
      </Card>
    </div>
  </div>
</template>
