<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api, type ErrorGroup } from '../api'
import { RefreshCw, BookmarkPlus, Camera } from 'lucide-vue-next'

const router = useRouter()
const groups = ref<ErrorGroup[]>([])
const ruleFilter = ref('')
const dialog = ref<{ group: ErrorGroup; yaml: string } | null>(null)
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
    dialog.value = null
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
        <input v-model="ruleFilter" class="input w-52" placeholder="按命中规则过滤…" @keyup.enter="load" />
        <button class="btn-outline" @click="load"><RefreshCw class="size-4" /></button>
      </div>
    </div>

    <p v-if="msg" class="text-sm" :class="msg.includes('失败') ? 'text-destructive' : 'text-success'">{{ msg }}</p>

    <div class="card overflow-x-auto">
      <table class="table-base">
        <thead>
          <tr>
            <th>指纹</th><th>Provider</th><th>状态码</th><th>模型</th><th>规则</th>
            <th>次数</th><th>最近出现</th><th>样例</th><th class="text-right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="g in groups" :key="g.fingerprint" class="hover:bg-muted/50">
            <td class="font-mono text-xs">{{ g.fingerprint.slice(0, 12) }}</td>
            <td>{{ g.provider }}</td>
            <td>{{ g.status_code ?? '—' }}</td>
            <td class="font-mono text-xs max-w-40 truncate" :title="g.models">{{ g.models }}</td>
            <td class="text-xs">{{ g.rules || '—' }}</td>
            <td class="font-semibold">{{ g.count }}</td>
            <td class="text-xs whitespace-nowrap">{{ new Date(g.last_seen + 'Z').toLocaleString() }}</td>
            <td class="text-xs text-muted-foreground max-w-64 truncate" :title="g.sample_error">{{ g.sample_error }}</td>
            <td>
              <div class="flex justify-end gap-1">
                <button class="btn-ghost p-1.5" title="查看抓取记录"
                  @click="router.push(`/captures?fingerprint=${g.fingerprint}`)">
                  <Camera class="size-4" />
                </button>
                <button class="btn-ghost p-1.5 text-primary" title="加为已知错误规则" @click="openDialog(g)">
                  <BookmarkPlus class="size-4" />
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="!groups.length"><td colspan="9" class="text-center text-muted-foreground py-6">暂无错误记录</td></tr>
        </tbody>
      </table>
    </div>

    <!-- 规则生成对话框 -->
    <div v-if="dialog" class="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-6" @click.self="dialog = null">
      <div class="card p-5 max-w-2xl w-full space-y-3">
        <h2 class="font-semibold">添加为已知错误规则</h2>
        <p class="text-sm text-muted-foreground">将追加到 rules.yaml 末尾，可在此编辑后保存：</p>
        <textarea v-model="dialog.yaml" rows="12" class="input font-mono text-xs leading-relaxed" spellcheck="false" />
        <div class="flex justify-end gap-2">
          <button class="btn-outline" @click="dialog = null">取消</button>
          <button class="btn-primary" @click="saveRule">保存并生效</button>
        </div>
      </div>
    </div>
  </div>
</template>
