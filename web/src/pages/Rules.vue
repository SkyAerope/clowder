<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, type RuleView, type RulePreset } from '../api'
import Card from '../components/Card.vue'
import { Save, TriangleAlert, Plus } from 'lucide-vue-next'

const yamlText = ref('')
const rules = ref<RuleView[]>([])
const warnings = ref<{ rule: string; message: string }[]>([])
const presets = ref<RulePreset[]>([])
const msg = ref('')
const saving = ref(false)

const sourceNames = ['manifest', '用户', '内置'] as const

async function load() {
  const r = await api.get<{ yaml: string; rules: RuleView[]; warnings: { rule: string; message: string }[] }>('/api/admin/rules')
  yamlText.value = r.yaml
  rules.value = r.rules ?? []
  warnings.value = r.warnings ?? []
  presets.value = (await api.get<RulePreset[]>('/api/admin/rules/presets')) ?? []
}

async function save() {
  saving.value = true
  msg.value = ''
  try {
    await api.put('/api/admin/rules', { yaml: yamlText.value })
    msg.value = '已保存并生效'
    await load()
  } catch (e: any) {
    msg.value = `保存失败: ${e.message}`
  } finally {
    saving.value = false
  }
}

function addPreset(p: RulePreset) {
  let y = yamlText.value
  if (!/^rules:/m.test(y)) y = 'rules:\n' + y
  // preset yaml 是顶层数组，缩进两格挂到 rules: 下
  const indented = p.yaml.split('\n').map(l => (l ? '  ' + l : l)).join('\n')
  yamlText.value = y.trimEnd() + '\n\n' + indented + '\n'
  msg.value = `已插入 ${p.rule.name}，请检查后保存`
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <h1 class="text-xl font-semibold">规则管理</h1>
      <button class="btn-primary" :disabled="saving" @click="save">
        <Save class="size-4" /> 保存并生效
      </button>
    </div>

    <p v-if="msg" class="text-sm" :class="msg.includes('失败') ? 'text-destructive' : 'text-success'">{{ msg }}</p>

    <div v-if="warnings.length" class="card p-4 space-y-1">
      <div class="flex items-center gap-2 text-warning font-medium text-sm">
        <TriangleAlert class="size-4" /> 校验警告
      </div>
      <ul class="text-xs text-muted-foreground list-disc pl-5 space-y-0.5">
        <li v-for="(w, i) in warnings" :key="i">
          <span class="font-mono text-primary">{{ w.rule }}</span>: {{ w.message }}
        </li>
      </ul>
    </div>

    <div class="grid grid-cols-1 xl:grid-cols-3 gap-4">
      <div class="xl:col-span-2">
        <Card title="rules.yaml">
          <textarea v-model="yamlText" rows="28" class="input font-mono text-xs leading-relaxed" spellcheck="false" />
        </Card>
      </div>

      <div class="space-y-4">
        <Card :title="`生效规则 (${rules.length})`">
          <div class="space-y-1.5 max-h-72 overflow-y-auto">
            <div v-for="r in rules" :key="r.name" class="flex items-center gap-2 text-xs">
              <span class="badge bg-muted text-muted-foreground shrink-0">{{ sourceNames[r.source] ?? r.source }}</span>
              <span class="font-mono truncate" :title="r.description">{{ r.name }}</span>
              <span class="ml-auto text-muted-foreground shrink-0">p{{ r.priority }} · s{{ r.specificity }}</span>
            </div>
          </div>
        </Card>

        <Card title="预设规则">
          <div class="space-y-2">
            <div v-for="p in presets" :key="String(p.rule.name)" class="flex items-center gap-2 text-sm">
              <span class="font-mono text-xs truncate" :title="String(p.rule.description ?? '')">{{ p.rule.name }}</span>
              <button v-if="!p.exists" class="btn-ghost p-1 ml-auto text-primary shrink-0" title="插入到编辑器"
                @click="addPreset(p)">
                <Plus class="size-4" />
              </button>
              <span v-else class="badge bg-success/15 text-success ml-auto shrink-0">已启用</span>
            </div>
          </div>
        </Card>
      </div>
    </div>
  </div>
</template>
