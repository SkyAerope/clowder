<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api, type ErrorGroup } from '../api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow,
} from '@/components/ui/table'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog'
import { RefreshCw, BookmarkPlus, Camera } from 'lucide-vue-next'

const router = useRouter()
const groups = ref<ErrorGroup[]>([])
const ruleFilter = ref('')
const dialog = ref<{ group: ErrorGroup; yaml: string } | null>(null)
const dialogOpen = ref(false)
const msg = ref('')

async function load() {
  const q = ruleFilter.value ? `?rule=${encodeURIComponent(ruleFilter.value)}` : ''
  groups.value = (await api.get<ErrorGroup[]>(`/api/admin/errors${q}`)) ?? []
}

function makeRuleYAML(g: ErrorGroup): string {
  const name = `known-${g.provider}-${g.fingerprint.slice(0, 8)}`
  const lines = [
    `  - name: ${name}`,
    `    description: "已知错误 (fingerprint ${g.fingerprint.slice(0, 8)})"`,
    `    priority: 50`,
    `    when:`,
    `      upstream:`,
    `        provider: ${g.provider}`,
  ]
  if (g.status_code) lines.push(`        status_code: "${g.status_code}"`)
  // 从 sample_error 提取一段可匹配的特征文本
  const snippet = (g.sample_error || '').replace(/"/g, "'").slice(0, 60).trim()
  if (snippet) lines.push(`        body_contains: "${snippet}"`)
  lines.push(`    do: []  # 已知错误：不再抓取`)
  lines.push(`    route: next_key  # 按需修改: next_key / next_provider / passthrough / retry_same`)
  return lines.join('\n')
}

function openDialog(g: ErrorGroup) {
  dialog.value = { group: g, yaml: makeRuleYAML(g) }
  dialogOpen.value = true
}

async function saveRule() {
  if (!dialog.value) return
  msg.value = ''
  try {
    const cur = await api.get<{ yaml: string }>('/api/admin/rules')
    let yaml = cur.yaml || 'rules:\n'
    if (!/^rules:/m.test(yaml)) yaml = 'rules:\n' + yaml
    yaml = yaml.trimEnd() + '\n\n' + dialog.value.yaml + '\n'
    await api.put('/api/admin/rules', { yaml })
    msg.value = '规则已添加并生效'
    dialogOpen.value = false
  } catch (e: any) {
    msg.value = `保存失败: ${e.message}`
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

    <Alert v-if="msg" :variant="msg.includes('失败') ? 'destructive' : 'default'">
      <AlertDescription>{{ msg }}</AlertDescription>
    </Alert>

    <div class="ring-foreground/10 bg-card overflow-x-auto rounded-xl ring-1">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>指纹</TableHead><TableHead>Provider</TableHead><TableHead>状态码</TableHead>
            <TableHead>模型</TableHead><TableHead>规则</TableHead><TableHead>次数</TableHead>
            <TableHead>最近出现</TableHead><TableHead>样例</TableHead><TableHead class="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
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
                <Button variant="ghost" size="icon-sm" title="查看抓取记录"
                  @click="router.push(`/captures?fingerprint=${g.fingerprint}`)">
                  <Camera class="size-4" />
                </Button>
                <Button variant="ghost" size="icon-sm" class="text-primary" title="加为已知错误规则" @click="openDialog(g)">
                  <BookmarkPlus class="size-4" />
                </Button>
              </div>
            </TableCell>
          </TableRow>
          <TableEmpty v-if="!groups.length" :colspan="9">暂无错误记录</TableEmpty>
        </TableBody>
      </Table>
    </div>

    <!-- 规则生成对话框 -->
    <Dialog v-model:open="dialogOpen">
      <DialogContent class="max-w-2xl">
        <DialogHeader>
          <DialogTitle>添加为已知错误规则</DialogTitle>
          <DialogDescription>将追加到 rules.yaml 末尾，可在此编辑后保存：</DialogDescription>
        </DialogHeader>
        <Textarea v-if="dialog" v-model="dialog.yaml" :rows="12" class="font-mono text-xs leading-relaxed" spellcheck="false" />
        <DialogFooter>
          <Button variant="outline" @click="dialogOpen = false">取消</Button>
          <Button @click="saveRule">保存并生效</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
