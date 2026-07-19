<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { api, type RuleView, type RulePreset } from '../api'
import Card from '../components/Card.vue'
import RuleVisualEditor from '../components/rules/RuleVisualEditor.vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription,
} from '@/components/ui/dialog'
import { Save, TriangleAlert, Plus, ChevronDown, ChevronRight } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  TYPE_LABELS, actPhrase, detailToEdit, ifPhrase, normalizeRule, parseRules, rawToEdit,
  routePhrase, serializeRules, summarize, whenPhrase, type EditRule,
} from '@/lib/rule-editor'

const yamlText = ref('')
const rules = ref<RuleView[]>([])
const warnings = ref<{ rule: string; message: string }[]>([])
const presets = ref<RulePreset[]>([])
const saving = ref(false)
const loaded = ref(false)
const tab = ref<'visual' | 'source'>('visual')
const draft = ref<EditRule[]>([])
const parseError = ref('')
const expanded = ref<Set<number>>(new Set())
const presetOpen = ref(false)
const presetExpanded = ref<Set<number>>(new Set())
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

// 可视化编辑时合并连续变更，避免每次按键都全量序列化 YAML。
watch(draft, () => {
  for (const e of draft.value) normalizeRule(e)
  if (tab.value === 'visual') scheduleDraftSerialization()
}, { deep: true })

// 切换到可视化 -> 用最新 yamlText 重建 draft
watch(tab, (t) => {
  if (t === 'visual') refreshDraft()
  else serializeDraft()
})

const builtinEdits = computed(() => rules.value.filter((r) => r.builtin).map(detailToEdit))
const presetEdits = computed(() => presets.value.map((preset) => ({ preset, edit: rawToEdit(preset.rule) })))

function toggleOpen(uid: number) {
  if (expanded.value.has(uid)) expanded.value.delete(uid)
  else expanded.value.add(uid)
}
function togglePreset(uid: number) {
  if (presetExpanded.value.has(uid)) presetExpanded.value.delete(uid)
  else presetExpanded.value.add(uid)
}
function presetAdded(p: RulePreset): boolean {
  return p.exists || addedPresets.value.has(String(p.rule.name))
}

function toggleBuiltin(name: string, enabled: boolean) {
  if (enabled) disabledBuiltins.value.delete(name)
  else disabledBuiltins.value.add(name)
  serializeDraft()
}

// ---------- 加载/保存/预设 ----------
async function load() {
  try {
    const r = await api.get<{ yaml: string; rules: RuleView[]; warnings: { rule: string; message: string }[] }>('/api/admin/rules')
    yamlText.value = r.yaml
    rules.value = r.rules ?? []
    warnings.value = r.warnings ?? []
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

function addPreset(p: RulePreset) {
  if (tab.value === 'visual') serializeDraft()
  let y = yamlText.value
  if (!/^rules:/m.test(y)) y = 'rules:\n' + y
  // preset yaml 是顶层数组，缩进两格挂到 rules: 下
  const indented = p.yaml.split('\n').map((l) => (l ? '  ' + l : l)).join('\n')
  yamlText.value = y.trimEnd() + '\n\n' + indented + '\n'
  if (tab.value === 'visual') refreshDraft()
  addedPresets.value.add(String(p.rule.name))
  toast.info(`已插入 ${p.rule.name}，请检查后保存`)
}

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (visualDirty.value) serializeDraft()
  if (dirty.value) {
    e.preventDefault()
    e.returnValue = ''
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
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between gap-2">
      <h1 class="text-xl font-semibold">规则管理</h1>
      <div class="flex items-center gap-2">
        <Button variant="outline" @click="presetOpen = true">
          <Plus class="size-4" /> 添加预设规则
        </Button>
        <Button :disabled="saving || !dirty" @click="save">
          <Save class="size-4" /> 保存并生效
        </Button>
      </div>
    </div>

    <Alert v-if="warnings.length" variant="default" class="border-warning/40">
      <TriangleAlert class="size-4 text-warning" />
      <AlertTitle class="text-warning">校验警告</AlertTitle>
      <AlertDescription>
        <ul class="text-xs text-muted-foreground list-disc pl-4 space-y-0.5">
          <li v-for="(w, i) in warnings" :key="i">
            <span class="font-mono text-foreground">{{ w.rule }}</span>: {{ w.message }}
          </li>
        </ul>
      </AlertDescription>
    </Alert>

    <div v-if="!loaded" class="space-y-3">
      <Skeleton class="h-9 w-48" />
      <Skeleton class="h-40 w-full rounded-xl" />
      <Skeleton class="h-40 w-full rounded-xl" />
    </div>

    <Tabs v-else v-model="tab">
      <TabsList>
        <TabsTrigger value="visual">可视化</TabsTrigger>
        <TabsTrigger value="source">源文件</TabsTrigger>
      </TabsList>

      <!-- 可视化编辑 -->
      <TabsContent value="visual" class="space-y-3">
        <Alert v-if="parseError" variant="destructive">
          <AlertDescription>{{ parseError }}</AlertDescription>
        </Alert>

        <RuleVisualEditor v-model="draft" />

        <!-- 内置规则（只读） -->
        <div v-if="builtinEdits.length" class="space-y-2 pt-2">
          <h2 class="text-sm font-medium text-muted-foreground">内置规则（只读）</h2>
          <div v-for="be in builtinEdits" :key="be._uid" class="border-border bg-muted/30 overflow-hidden rounded-xl border" :class="{ 'opacity-60': disabledBuiltins.has(be.name) }">
            <div class="flex items-start gap-2 p-3">
              <button type="button" class="flex min-w-0 flex-1 cursor-pointer items-start gap-2 text-left" @click="toggleOpen(be._uid)">
                <component :is="expanded.has(be._uid) ? ChevronDown : ChevronRight" class="text-muted-foreground mt-0.5 size-4 shrink-0" />
                <div class="min-w-0 flex-1 space-y-1">
                  <div class="flex items-center gap-2">
                    <span class="truncate font-medium">{{ be.description || '内置规则' }}</span>
                    <span class="text-muted-foreground ml-auto shrink-0 truncate font-mono text-xs">{{ be.name }}</span>
                  </div>
                  <div class="flex flex-wrap items-center gap-1.5">
                    <Badge variant="secondary" class="shrink-0">P{{ be.priority }}</Badge>
                    <Badge variant="outline" class="shrink-0">{{ TYPE_LABELS[be.whenType] }}</Badge>
                    <span class="text-muted-foreground text-xs">{{ summarize(be) }}</span>
                  </div>
                </div>
              </button>
              <Switch :model-value="!disabledBuiltins.has(be.name)" class="mt-1 shrink-0" title="启用 / 停用内置规则" @update:model-value="(v: boolean) => toggleBuiltin(be.name, v)" />
            </div>
            <div v-if="expanded.has(be._uid)" class="space-y-1 border-t border-foreground/10 p-3 pl-9 text-xs text-muted-foreground">
              <div><span class="text-foreground">触发：</span>{{ whenPhrase(be) }}</div>
              <div v-if="be.ifs.length"><span class="text-foreground">条件：</span>{{ be.ifs.map(ifPhrase).join('，') }}</div>
              <div v-if="be.dos.length"><span class="text-foreground">动作：</span>{{ be.dos.map(actPhrase).join('，') }}</div>
              <div><span class="text-foreground">路由：</span>{{ routePhrase(be) }}</div>
            </div>
          </div>
        </div>
      </TabsContent>

      <!-- 源文件编辑 -->
      <TabsContent value="source">
        <Card title="rules.yaml">
          <Textarea v-model="yamlText" :rows="30" class="font-mono text-xs leading-relaxed" spellcheck="false" />
        </Card>
      </TabsContent>
    </Tabs>

    <!-- 预设规则对话框 -->
    <Dialog v-model:open="presetOpen">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>预设规则</DialogTitle>
          <DialogDescription>选择常用规则插入到编辑器，保存后生效。</DialogDescription>
        </DialogHeader>
        <div class="max-h-[60vh] space-y-2 overflow-y-auto pr-1">
          <div v-for="pe in presetEdits" :key="pe.edit._uid" class="border-border overflow-hidden rounded-xl border">
            <div class="flex items-start gap-2 p-3">
              <button type="button" class="min-w-0 flex-1 cursor-pointer space-y-1 text-left" @click="togglePreset(pe.edit._uid)">
                <div class="flex items-center gap-2">
                  <component :is="presetExpanded.has(pe.edit._uid) ? ChevronDown : ChevronRight" class="text-muted-foreground size-4 shrink-0" />
                  <span class="truncate font-medium">{{ pe.edit.description || pe.edit.name }}</span>
                </div>
                <div class="flex flex-wrap items-center gap-1.5 pl-6">
                  <Badge variant="secondary" class="shrink-0">P{{ pe.edit.priority }}</Badge>
                  <Badge variant="outline" class="shrink-0">{{ TYPE_LABELS[pe.edit.whenType] }}</Badge>
                  <span class="text-muted-foreground text-xs">{{ summarize(pe.edit) }}</span>
                </div>
              </button>
              <Button v-if="presetAdded(pe.preset)" size="sm" variant="outline" disabled class="shrink-0">已添加</Button>
              <Button v-else size="sm" variant="outline" class="shrink-0" @click="addPreset(pe.preset)">添加</Button>
            </div>
            <div v-if="presetExpanded.has(pe.edit._uid)" class="space-y-1 border-t border-foreground/10 p-3 pl-9 text-xs text-muted-foreground">
              <div><span class="text-foreground">触发：</span>{{ whenPhrase(pe.edit) }}</div>
              <div v-if="pe.edit.ifs.length"><span class="text-foreground">条件：</span>{{ pe.edit.ifs.map(ifPhrase).join('，') }}</div>
              <div v-if="pe.edit.dos.length"><span class="text-foreground">动作：</span>{{ pe.edit.dos.map(actPhrase).join('，') }}</div>
              <div><span class="text-foreground">路由：</span>{{ routePhrase(pe.edit) }}</div>
            </div>
          </div>
          <p v-if="!presetEdits.length" class="py-4 text-center text-sm text-muted-foreground">暂无预设</p>
        </div>
      </DialogContent>
    </Dialog>
  </div>
</template>
