<script setup lang="ts">
import { ref } from 'vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuCheckboxItem } from '@/components/ui/dropdown-menu'
import { Plus, Trash2, ChevronDown, ChevronRight } from 'lucide-vue-next'
import {
  ACTION_KINDS, CAPTURE_FIELDS, CAPTURE_LABELS, CHECK_STATUS, IF_FIELDS, IF_OPS, MARK_STATUS,
  RACE_ROUTES, ROUTES, TRANSPORTS, TYPE_LABELS, WHEN_FIELDS, blankRule, lbl, routeDisabled,
  summarize, whenFieldDisabled, type EditAction, type EditRule,
} from '@/lib/rule-editor'

const props = withDefaults(defineProps<{ allowMultiple?: boolean }>(), { allowMultiple: true })
const model = defineModel<EditRule[]>({ required: true })
const initialRule = model.value[0]
const expanded = ref<Set<number>>(new Set(!props.allowMultiple && initialRule ? [initialRule._uid] : []))

function toggleOpen(uid: number): void {
  if (expanded.value.has(uid)) expanded.value.delete(uid); else expanded.value.add(uid)
}
function addRule(): void { const rule = blankRule(); model.value.push(rule); expanded.value.add(rule._uid) }
function addWhenClause(rule: EditRule): void {
  const used = new Set(rule.whenClauses.map((clause) => clause.field))
  const next = WHEN_FIELDS.find((option) => !used.has(option.v) && !whenFieldDisabled(rule, { field: '', text: '', regex: false }, option.v)) ?? WHEN_FIELDS[0]
  rule.whenClauses.push({ field: next.v, text: next.v === 'transport' ? 'true' : '', regex: false })
}
function addAction(rule: EditRule): void { rule.dos.push({ kind: 'capture', mark_status: 'invalid', chain: false, fields: [] }) }
function toggleField(action: EditAction, field: string): void { const index = action.fields.indexOf(field); if (index >= 0) action.fields.splice(index, 1); else action.fields.push(field) }
function toggleRace(rule: EditRule, route: string): void { const index = rule.routeRace.indexOf(route); if (index >= 0) rule.routeRace.splice(index, 1); else rule.routeRace.push(route) }
</script>

<template>
  <div class="flex flex-col gap-3">
    <div v-for="(rule, ruleIndex) in model" :key="rule._uid" class="border-border bg-card overflow-hidden rounded-xl border" :class="{ 'opacity-60': !rule.enabled }">
      <div class="flex items-start gap-2 p-3">
        <button type="button" class="flex min-w-0 flex-1 cursor-pointer items-start gap-2 text-left" @click="toggleOpen(rule._uid)">
          <component :is="expanded.has(rule._uid) ? ChevronDown : ChevronRight" class="text-muted-foreground mt-0.5 size-4 shrink-0" />
          <div class="min-w-0 flex-1 space-y-1">
            <div class="flex flex-col gap-0.5"><span class="truncate font-medium">{{ rule.name || '未命名规则' }}</span><span class="text-muted-foreground truncate font-mono text-xs">{{ rule.slug }}</span></div>
            <div class="flex flex-wrap items-center gap-1.5"><Badge variant="secondary">P{{ rule.priority }}</Badge><Badge variant="outline">{{ TYPE_LABELS[rule.whenType] }}</Badge><span class="text-muted-foreground text-xs">{{ summarize(rule) }}</span></div>
          </div>
        </button>
        <div class="mt-1 flex shrink-0 flex-col items-center gap-2">
          <Switch v-model="rule.enabled" title="启用 / 停用此规则" />
          <Button v-if="allowMultiple && expanded.has(rule._uid)" variant="ghost" size="icon-sm" class="text-destructive hover:text-destructive" title="删除规则" @click="model.splice(ruleIndex, 1)"><Trash2 /></Button>
        </div>
      </div>
      <div v-if="expanded.has(rule._uid)" class="space-y-3 border-t border-foreground/10 p-3">
        <Input v-model="rule.name" placeholder="名称（显示为标题）" />
        <Input v-model="rule.slug" placeholder="Slug（英文标识）" />
        <div class="flex flex-wrap items-end gap-3">
          <div class="space-y-1"><Label class="text-xs text-muted-foreground">优先级（越小越优先）</Label><Input v-model.number="rule.priority" type="number" class="w-28" /></div>
          <div class="space-y-1"><Label class="text-xs text-muted-foreground">触发类型 (when)</Label><Select v-model="rule.whenType"><SelectTrigger class="w-48"><SelectValue /></SelectTrigger><SelectContent :body-lock="false"><SelectItem value="upstream">上游结果</SelectItem><SelectItem value="check">探活结果</SelectItem><SelectItem value="schedule">定时</SelectItem></SelectContent></Select></div>
        </div>
        <div v-if="rule.whenType === 'upstream'" class="space-y-2">
          <Label class="text-xs text-muted-foreground">触发条件 (when)</Label>
          <p v-if="!rule.whenClauses.length" class="text-xs text-muted-foreground">未设条件 = 匹配任意请求</p>
          <div v-for="(clause, clauseIndex) in rule.whenClauses" :key="clauseIndex" class="flex flex-wrap items-center gap-2">
            <Select v-model="clause.field"><SelectTrigger class="w-48"><SelectValue /></SelectTrigger><SelectContent :body-lock="false"><SelectItem v-for="field in WHEN_FIELDS" :key="field.v" :value="field.v" :disabled="whenFieldDisabled(rule, clause, field.v)">{{ field.label }}</SelectItem></SelectContent></Select>
            <div class="flex min-w-0 flex-1 items-center gap-2">
              <Select v-if="clause.field === 'transport'" v-model="clause.text"><SelectTrigger class="w-56"><SelectValue placeholder="选择类别" /></SelectTrigger><SelectContent :body-lock="false"><SelectItem v-for="transport in TRANSPORTS" :key="transport.v" :value="transport.v">{{ transport.label }}</SelectItem></SelectContent></Select>
              <span v-else-if="clause.field === 'error'" class="text-xs text-muted-foreground">命中任意错误</span>
              <template v-else-if="clause.field === 'body'"><Input v-model="clause.text" class="field-sizing-content min-w-56 w-auto max-w-full" :class="clause.regex ? 'font-mono' : ''" :placeholder="clause.regex ? 'RE2 正则' : '包含文本'" /><div class="flex shrink-0 items-center gap-1.5"><Switch v-model="clause.regex" /><Label class="text-xs text-muted-foreground">正则</Label></div></template>
              <Input v-else-if="clause.field === 'status_code'" v-model="clause.text" class="field-sizing-content min-w-56 w-auto max-w-full" placeholder="403 或 400-499" />
              <Input v-else-if="clause.field === 'ttft_over_ms'" v-model="clause.text" type="number" class="field-sizing-content min-w-56 w-auto max-w-full" placeholder="毫秒阈值" />
              <Input v-else v-model="clause.text" class="field-sizing-content min-w-56 w-auto max-w-full" placeholder="留空=任意" />
            </div>
            <Button variant="ghost" size="icon-sm" class="text-destructive ml-auto shrink-0 hover:bg-transparent hover:text-destructive" @click="rule.whenClauses.splice(clauseIndex, 1)"><Trash2 /></Button>
          </div>
          <button type="button" class="text-muted-foreground hover:text-foreground hover:border-foreground/30 flex w-full cursor-pointer items-center justify-center gap-1 rounded-lg border border-dashed border-foreground/20 py-1.5 text-xs" @click="addWhenClause(rule)"><Plus class="size-3" /> 加条件</button>
        </div>
        <div v-else-if="rule.whenType === 'check'" class="space-y-2"><Label class="text-xs text-muted-foreground">触发条件 (when)：探活状态命中</Label><Select v-model="rule.check_status"><SelectTrigger class="w-56"><SelectValue /></SelectTrigger><SelectContent :body-lock="false"><SelectItem v-for="status in CHECK_STATUS" :key="status.v" :value="status.v">{{ status.label }}</SelectItem></SelectContent></Select></div>
        <div v-else class="space-y-2"><Label class="text-xs text-muted-foreground">触发条件 (when)：定时</Label><div class="flex flex-wrap gap-3"><div class="space-y-1"><Label class="text-xs text-muted-foreground">cron 表达式</Label><Input v-model="rule.cron" class="w-48 font-mono" placeholder="0 */6 * * *" /></div><div class="space-y-1"><Label class="text-xs text-muted-foreground">scope provider（可选）</Label><Input v-model="rule.scope_provider" class="w-48" placeholder="限定 provider" /></div></div></div>
        <div class="space-y-2">
          <Label class="text-xs text-muted-foreground">附加条件 (if)</Label>
          <div v-for="(condition, conditionIndex) in rule.ifs" :key="conditionIndex" class="flex items-center gap-2"><Select v-model="condition.field"><SelectTrigger class="w-48"><SelectValue /></SelectTrigger><SelectContent :body-lock="false"><SelectItem v-for="field in IF_FIELDS" :key="field.v" :value="field.v">{{ field.label }}</SelectItem></SelectContent></Select><Select v-model="condition.op"><SelectTrigger class="w-24"><SelectValue /></SelectTrigger><SelectContent :body-lock="false"><SelectItem v-for="operator in IF_OPS" :key="operator.v" :value="operator.v">{{ operator.label }}</SelectItem></SelectContent></Select><Input v-model.number="condition.value" type="number" class="w-24" /><Button variant="ghost" size="icon-sm" class="text-destructive ml-auto" @click="rule.ifs.splice(conditionIndex, 1)"><Trash2 /></Button></div>
          <button type="button" class="text-muted-foreground hover:text-foreground hover:border-foreground/30 flex w-full cursor-pointer items-center justify-center gap-1 rounded-lg border border-dashed border-foreground/20 py-1.5 text-xs" @click="rule.ifs.push({ field: 'attempt_no', op: 'gte', value: 2 })"><Plus class="size-3" /> 加条件</button>
        </div>
        <div class="space-y-2">
          <Label class="text-xs text-muted-foreground">动作 (do)</Label>
          <div v-for="(action, actionIndex) in rule.dos" :key="actionIndex" class="flex flex-wrap items-center gap-2"><Select v-model="action.kind"><SelectTrigger class="w-48"><SelectValue /></SelectTrigger><SelectContent :body-lock="false"><SelectItem v-for="kind in ACTION_KINDS" :key="kind.v" :value="kind.v">{{ kind.label }}</SelectItem></SelectContent></Select><Select v-if="action.kind === 'mark_key'" v-model="action.mark_status"><SelectTrigger class="w-56"><SelectValue /></SelectTrigger><SelectContent :body-lock="false"><SelectItem v-for="status in MARK_STATUS" :key="status.v" :value="status.v">{{ status.label }}</SelectItem></SelectContent></Select><div v-else-if="action.kind === 'check_key' || action.kind === 'check_keys'" class="flex items-center gap-1.5"><Switch v-model="action.chain" /><Label class="text-xs text-muted-foreground">链式（chain）</Label></div><DropdownMenu v-else-if="action.kind === 'capture'"><DropdownMenuTrigger as-child><button type="button" class="border-input flex min-h-8 min-w-56 items-center gap-1 rounded-lg border px-2.5 py-1 text-sm"><span class="flex flex-1 flex-wrap gap-1"><Badge v-for="field in action.fields" :key="field" variant="secondary">{{ CAPTURE_LABELS[field] ?? field }}</Badge><span v-if="!action.fields.length" class="text-muted-foreground">全抓（点击多选）</span></span><ChevronDown /></button></DropdownMenuTrigger><DropdownMenuContent align="start"><DropdownMenuCheckboxItem v-for="field in CAPTURE_FIELDS" :key="field" :model-value="action.fields.includes(field)" @select="(event: Event) => event.preventDefault()" @update:model-value="() => toggleField(action, field)">{{ CAPTURE_LABELS[field] ?? field }}</DropdownMenuCheckboxItem></DropdownMenuContent></DropdownMenu><Button variant="ghost" size="icon-sm" class="text-destructive ml-auto" @click="rule.dos.splice(actionIndex, 1)"><Trash2 /></Button></div>
          <button type="button" class="text-muted-foreground hover:text-foreground hover:border-foreground/30 flex w-full cursor-pointer items-center justify-center gap-1 rounded-lg border border-dashed border-foreground/20 py-1.5 text-xs" @click="addAction(rule)"><Plus class="size-3" /> 加动作</button>
        </div>
        <div class="space-y-2"><Label class="text-xs text-muted-foreground">路由 (route)</Label><div class="flex items-start gap-2"><Select v-model="rule.routeMode"><SelectTrigger class="w-48"><SelectValue /></SelectTrigger><SelectContent :body-lock="false"><SelectItem value="single">单一</SelectItem><SelectItem value="race">双发竞速</SelectItem></SelectContent></Select><Select v-if="rule.routeMode === 'single'" v-model="rule.routeSingle"><SelectTrigger class="w-56"><SelectValue /></SelectTrigger><SelectContent :body-lock="false"><SelectItem v-for="route in ROUTES" :key="route.v" :value="route.v" :disabled="routeDisabled(rule, route.v)">{{ route.label }}</SelectItem></SelectContent></Select><DropdownMenu v-else><DropdownMenuTrigger as-child><button type="button" class="border-input flex min-h-8 flex-1 items-center gap-1 rounded-lg border px-2.5 py-1 text-sm"><span class="flex flex-1 flex-wrap gap-1"><Badge v-for="route in rule.routeRace" :key="route" variant="secondary">{{ lbl(RACE_ROUTES, route) }}</Badge><span v-if="!rule.routeRace.length" class="text-muted-foreground">选择并发分支（点击多选）</span></span><ChevronDown /></button></DropdownMenuTrigger><DropdownMenuContent align="start"><DropdownMenuCheckboxItem v-for="route in RACE_ROUTES" :key="route.v" :model-value="rule.routeRace.includes(route.v)" @select="(event: Event) => event.preventDefault()" @update:model-value="() => toggleRace(rule, route.v)">{{ route.label }}</DropdownMenuCheckboxItem></DropdownMenuContent></DropdownMenu></div></div>
      </div>
    </div>
    <Button v-if="props.allowMultiple" variant="outline" class="w-full" @click="addRule"><Plus /> 新增规则</Button>
  </div>
</template>
