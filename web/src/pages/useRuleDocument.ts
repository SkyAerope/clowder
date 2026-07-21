import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { toast } from 'vue-sonner'
import { api, type RulePreset, type RuleView } from '../api'
import {
  detailToEdit,
  normalizeRule,
  parseRules,
  rawToEdit,
  serializeRules,
  type EditRule,
} from '@/lib/rule-editor'

export function useRuleDocument() {
  const yamlText = ref('')
  const rules = ref<RuleView[]>([])
  const warnings = ref<{ rule: string; message: string }[]>([])
  const presets = ref<RulePreset[]>([])
  const saving = ref(false)
  const loaded = ref(false)
  const tab = ref<'visual' | 'source'>('visual')
  const draft = ref<EditRule[]>([])
  const parseError = ref('')
  const addedPresets = ref<Set<string>>(new Set())
  const disabledBuiltins = ref<Set<string>>(new Set())
  const savedYaml = ref('')
  const visualDirty = ref(false)
  const dirty = computed(() => visualDirty.value || yamlText.value !== savedYaml.value)

  let serializeTimer: ReturnType<typeof setTimeout> | undefined

  function serializeDraft() {
    if (serializeTimer) {
      clearTimeout(serializeTimer)
      serializeTimer = undefined
    }
    yamlText.value = serializeRules(draft.value, disabledBuiltins.value)
    visualDirty.value = false
  }

  function scheduleDraftSerialization() {
    visualDirty.value = true
    if (serializeTimer) clearTimeout(serializeTimer)
    serializeTimer = setTimeout(serializeDraft, 150)
  }

  function refreshDraft() {
    try {
      const parsed = parseRules(yamlText.value)
      draft.value = parsed.rules
      disabledBuiltins.value = parsed.disabledBuiltins
      parseError.value = ''
    } catch (error: unknown) {
      parseError.value = `YAML 解析失败，请在「源文件」修正后再切换：${error instanceof Error ? error.message : String(error)}`
    }
  }

  watch(draft, () => {
    for (const rule of draft.value) normalizeRule(rule)
    if (tab.value === 'visual') scheduleDraftSerialization()
  }, { deep: true })

  watch(tab, (value) => {
    if (value === 'visual') refreshDraft()
    else serializeDraft()
  })

  const builtinEdits = computed(() => rules.value.filter((rule) => rule.builtin).map(detailToEdit))
  const presetEdits = computed(() => presets.value.map((preset) => ({ preset, edit: rawToEdit(preset.rule) })))

  function toggleBuiltin(name: string, enabled: boolean) {
    if (enabled) disabledBuiltins.value.delete(name)
    else disabledBuiltins.value.add(name)
    serializeDraft()
  }

  async function load() {
    try {
      const response = await api.get<{ yaml: string; rules: RuleView[]; warnings: { rule: string; message: string }[] }>('/api/admin/rules')
      yamlText.value = response.yaml
      rules.value = response.rules ?? []
      warnings.value = response.warnings ?? []
      presets.value = (await api.get<RulePreset[]>('/api/admin/rules/presets')) ?? []
      addedPresets.value = new Set()
      refreshDraft()
      serializeDraft()
      await nextTick()
      if (serializeTimer) {
        clearTimeout(serializeTimer)
        serializeTimer = undefined
      }
      visualDirty.value = false
      savedYaml.value = yamlText.value
    } finally {
      loaded.value = true
    }
  }

  async function save() {
    if (tab.value === 'visual') serializeDraft()
    saving.value = true
    try {
      await api.put('/api/admin/rules', { yaml: yamlText.value })
      toast.success('已保存并生效')
      await load()
    } catch (error: unknown) {
      toast.error(`保存失败: ${error instanceof Error ? error.message : String(error)}`)
    } finally {
      saving.value = false
    }
  }

  function addPreset(preset: RulePreset) {
    if (tab.value === 'visual') serializeDraft()
    let text = yamlText.value
    if (!/^rules:/m.test(text)) text = `rules:\n${text}`
    const indented = preset.yaml.split('\n').map((line) => (line ? `  ${line}` : line)).join('\n')
    yamlText.value = `${text.trimEnd()}\n\n${indented}\n`
    if (tab.value === 'visual') refreshDraft()
    addedPresets.value.add(String(preset.rule.slug))
    toast.info(`已插入 ${preset.rule.name ?? preset.rule.slug}，请检查后保存`)
  }

  function onBeforeUnload(event: BeforeUnloadEvent) {
    if (visualDirty.value) serializeDraft()
    if (dirty.value) {
      event.preventDefault()
      event.returnValue = ''
    }
  }

  onMounted(() => {
    load()
    window.addEventListener('beforeunload', onBeforeUnload)
  })
  onUnmounted(() => {
    if (serializeTimer) clearTimeout(serializeTimer)
    window.removeEventListener('beforeunload', onBeforeUnload)
  })
  onBeforeRouteLeave(() => {
    if (visualDirty.value) serializeDraft()
    if (dirty.value) return window.confirm('有未保存的规则修改，确定离开吗？')
    return true
  })

  return {
    yamlText, rules, warnings, presets, saving, loaded, tab, draft, parseError,
    addedPresets, dirty, visualDirty, builtinEdits, presetEdits, disabledBuiltins,
    save, addPreset, toggleBuiltin,
  }
}
