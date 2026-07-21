<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, type ErrorGroup } from '../api'
import TableSkeleton from '../components/TableSkeleton.vue'
import RuleVisualEditor from '../components/rules/RuleVisualEditor.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog'
import { RefreshCw, BookmarkPlus, Eye } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import {
  blankRule, normalizeRule, parseRuleFragment, serializeRuleFragment, type EditRule,
} from '@/lib/rule-editor'

const router = useRouter()
const groups = ref<ErrorGroup[]>([])
const loaded = ref(false)
const ruleFilter = ref('')
const dialog = ref<{ group: ErrorGroup; yaml: string; draft: EditRule[] } | null>(null)
const dialogOpen = ref(false)
const dialogTab = ref<'visual' | 'source'>('visual')
const parseError = ref('')
const saving = ref(false)

async function load() {
  const q = ruleFilter.value ? `?rule=${encodeURIComponent(ruleFilter.value)}` : ''
  try {
    groups.value = (await api.get<ErrorGroup[]>(`/api/admin/errors${q}`)) ?? []
  } finally {
    loaded.value = true
  }
}

function makeRule(g: ErrorGroup): EditRule {
  const rule = blankRule()
  rule.slug = `known-${g.provider}-${g.fingerprint.slice(0, 8)}`
  rule.name = `已知错误 (fingerprint ${g.fingerprint.slice(0, 8)})`
  rule.priority = 50
  rule.whenClauses.push({ field: 'provider', text: g.provider, regex: false })
  if (g.status_code) rule.whenClauses.push({ field: 'status_code', text: String(g.status_code), regex: false })
  const snippet = (g.sample_error || '').replace(/"/g, "'").slice(0, 60).trim()
  if (snippet) rule.whenClauses.push({ field: 'body', text: snippet, regex: false })
  rule.routeSingle = 'next_key'
  return rule
}

function openDialog(g: ErrorGroup) {
  const rule = makeRule(g)
  dialog.value = { group: g, yaml: serializeRuleFragment(rule), draft: [rule] }
  dialogTab.value = 'visual'
  parseError.value = ''
  dialogOpen.value = true
}

watch(() => dialog.value?.draft, (draft) => {
  if (!dialog.value || dialogTab.value !== 'visual' || !draft?.[0]) return
  normalizeRule(draft[0])
  dialog.value.yaml = serializeRuleFragment(draft[0])
}, { deep: true })

watch(dialogTab, (tab, previous) => {
  if (!dialog.value) return
  if (tab === 'source') {
    if (parseError.value) return
    const rule = dialog.value.draft[0]
    if (rule) dialog.value.yaml = serializeRuleFragment(rule)
    return
  }
  if (previous === 'source') {
    try {
      dialog.value.draft = [parseRuleFragment(dialog.value.yaml)]
      parseError.value = ''
    } catch (error: unknown) {
      parseError.value = `YAML 解析失败：${error instanceof Error ? error.message : String(error)}`
      dialogTab.value = 'source'
    }
  }
})

async function saveRule() {
  if (!dialog.value) return
  let fragment = dialog.value.yaml
  try {
    if (dialogTab.value === 'visual') {
      const rule = dialog.value.draft[0]
      if (!rule) return
      fragment = serializeRuleFragment(rule)
    } else {
      fragment = serializeRuleFragment(parseRuleFragment(fragment))
    }
    parseError.value = ''
  } catch (error: unknown) {
    parseError.value = `YAML 解析失败：${error instanceof Error ? error.message : String(error)}`
    return
  }
  saving.value = true
  try {
    const cur = await api.get<{ yaml: string }>('/api/admin/rules')
    let yaml = cur.yaml || 'rules:\n'
    if (!/^rules:/m.test(yaml)) yaml = 'rules:\n' + yaml
    yaml = yaml.trimEnd() + '\n\n' + fragment + '\n'
    await api.put('/api/admin/rules', { yaml })
    toast.success('规则已添加并生效')
    dialogOpen.value = false
  } catch (error: unknown) {
    toast.error(`保存失败: ${error instanceof Error ? error.message : String(error)}`)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between flex-wrap gap-2">
      <h1 class="text-xl font-semibold">错误聚合</h1>
      <div class="flex gap-2">
        <Input v-model="ruleFilter" class="w-52" placeholder="按命中规则过滤…" @keyup.enter="load" />
        <Button variant="outline" size="icon" @click="load"><RefreshCw class="size-4" /></Button>
      </div>
    </div>

    <div class="ring-foreground/10 bg-card overflow-x-auto rounded-xl ring-1">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>指纹</TableHead><TableHead>Provider</TableHead><TableHead>状态码</TableHead>
            <TableHead>模型</TableHead><TableHead>规则</TableHead><TableHead>次数</TableHead>
            <TableHead>最近出现</TableHead><TableHead>样例</TableHead><TableHead class="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableSkeleton v-if="!loaded" :cols="9" />
        <TableBody v-else>
          <TableRow v-for="g in groups" :key="g.fingerprint">
            <TableCell class="font-mono text-xs">{{ g.fingerprint.slice(0, 12) }}</TableCell>
            <TableCell>{{ g.provider }}</TableCell>
            <TableCell>{{ g.status_code ?? '—' }}</TableCell>
            <TableCell class="font-mono text-xs max-w-40 truncate" :title="g.models">{{ g.models }}</TableCell>
            <TableCell class="text-xs">{{ g.rules || '—' }}</TableCell>
            <TableCell class="font-semibold">{{ g.count }}</TableCell>
            <TableCell class="text-xs whitespace-nowrap">{{ new Date(g.last_seen + 'Z').toLocaleString() }}</TableCell>
            <TableCell class="text-xs text-muted-foreground max-w-64 truncate" :title="g.sample_error">{{ g.sample_error }}</TableCell>
            <TableCell>
              <div class="flex justify-end gap-1">
                <Tooltip>
                  <TooltipTrigger as-child><Button variant="ghost" size="icon-sm" aria-label="查看抓取记录" @click="router.push(`/captures?fingerprint=${g.fingerprint}`)"><Eye /></Button></TooltipTrigger>
                  <TooltipContent>查看抓取记录</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger as-child><Button variant="ghost" size="icon-sm" class="text-primary" aria-label="添加为规则" @click="openDialog(g)"><BookmarkPlus /></Button></TooltipTrigger>
                  <TooltipContent>添加为规则</TooltipContent>
                </Tooltip>
              </div>
            </TableCell>
          </TableRow>
          <TableEmpty v-if="!groups.length" :colspan="9">暂无错误记录</TableEmpty>
        </TableBody>
      </Table>
    </div>

    <!-- 规则生成对话框 -->
    <Dialog v-model:open="dialogOpen">
      <DialogContent class="max-h-[85vh] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>添加为规则</DialogTitle>
          <DialogDescription>将追加到 rules.yaml 末尾，可在此编辑后保存：</DialogDescription>
        </DialogHeader>
        <Tabs v-if="dialog" v-model="dialogTab" class="flex min-h-0 flex-col overflow-hidden">
          <TabsList><TabsTrigger value="visual">可视化</TabsTrigger><TabsTrigger value="source">源文件</TabsTrigger></TabsList>
          <Alert v-if="parseError" variant="destructive" class="mt-3"><AlertDescription>{{ parseError }}</AlertDescription></Alert>
          <TabsContent value="visual" class="min-h-0 flex-1 overflow-y-auto pr-1"><RuleVisualEditor v-model="dialog.draft" :allow-multiple="false" /></TabsContent>
          <TabsContent value="source" class="min-h-0 flex-1 overflow-y-auto"><Textarea v-model="dialog.yaml" :rows="18" class="font-mono text-xs leading-relaxed" spellcheck="false" /></TabsContent>
        </Tabs>
        <DialogFooter>
          <Button variant="outline" @click="dialogOpen = false">取消</Button>
          <Button :disabled="saving" @click="saveRule">保存并生效</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
