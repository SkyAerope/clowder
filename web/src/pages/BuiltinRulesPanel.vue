<script setup lang="ts">
import { ref } from 'vue'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { ChevronDown, ChevronRight } from 'lucide-vue-next'
import { TYPE_LABELS, actPhrase, ifPhrase, routePhrase, summarize, whenPhrase, type EditRule } from '@/lib/rule-editor'

defineProps<{ edits: EditRule[]; disabled: Set<string> }>()
const emit = defineEmits<{ toggle: [name: string, enabled: boolean] }>()
const expanded = ref<Set<number>>(new Set())

function toggleOpen(uid: number) {
  if (expanded.value.has(uid)) expanded.value.delete(uid)
  else expanded.value.add(uid)
}
</script>

<template>
  <div v-if="edits.length" class="space-y-2 pt-2">
    <h2 class="text-sm font-medium text-muted-foreground">内置规则（只读）</h2>
    <div v-for="rule in edits" :key="rule._uid" class="border-border bg-muted/30 overflow-hidden rounded-xl border" :class="{ 'opacity-60': disabled.has(rule.slug) }">
      <div class="flex items-start gap-2 p-3">
        <button type="button" class="flex min-w-0 flex-1 cursor-pointer items-start gap-2 text-left" @click="toggleOpen(rule._uid)">
          <component :is="expanded.has(rule._uid) ? ChevronDown : ChevronRight" class="text-muted-foreground mt-0.5 size-4 shrink-0" />
          <div class="min-w-0 flex-1 space-y-1">
            <div class="flex flex-col gap-0.5"><span class="truncate font-medium">{{ rule.name || '内置规则' }}</span><span class="text-muted-foreground truncate font-mono text-xs">{{ rule.slug }}</span></div>
            <div class="flex flex-wrap items-center gap-1.5"><Badge variant="secondary" class="shrink-0">P{{ rule.priority }}</Badge><Badge variant="outline" class="shrink-0">{{ TYPE_LABELS[rule.whenType] }}</Badge><span class="text-muted-foreground text-xs">{{ summarize(rule) }}</span></div>
          </div>
        </button>
        <Switch :model-value="!disabled.has(rule.slug)" class="mt-1 shrink-0" title="启用 / 停用内置规则" @update:model-value="(value: boolean) => emit('toggle', rule.slug, value)" />
      </div>
      <div v-if="expanded.has(rule._uid)" class="space-y-1 border-t border-foreground/10 p-3 pl-9 text-xs text-muted-foreground">
        <div><span class="text-foreground">触发：</span>{{ whenPhrase(rule) }}</div>
        <div v-if="rule.ifs.length"><span class="text-foreground">条件：</span>{{ rule.ifs.map(ifPhrase).join('，') }}</div>
        <div v-if="rule.dos.length"><span class="text-foreground">动作：</span>{{ rule.dos.map(actPhrase).join('，') }}</div>
        <div><span class="text-foreground">路由：</span>{{ routePhrase(rule) }}</div>
      </div>
    </div>
  </div>
</template>
