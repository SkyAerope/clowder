<script setup lang="ts">
import { ref } from 'vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ChevronDown, ChevronRight } from 'lucide-vue-next'
import { TYPE_LABELS, actPhrase, ifPhrase, routePhrase, summarize, whenPhrase, type EditRule } from '@/lib/rule-editor'
import type { RulePreset } from '../api'

type PresetEdit = { preset: RulePreset; edit: EditRule }
const props = defineProps<{ open: boolean; edits: PresetEdit[]; added: Set<string> }>()
const emit = defineEmits<{ 'update:open': [value: boolean]; add: [preset: RulePreset] }>()
const expanded = ref<Set<number>>(new Set())

function toggleOpen(uid: number) {
  if (expanded.value.has(uid)) expanded.value.delete(uid)
  else expanded.value.add(uid)
}

function isAdded(preset: RulePreset) {
  return preset.exists || props.added.has(String(preset.rule.slug))
}
</script>

<template>
  <Dialog :open="open" @update:open="emit('update:open', $event)">
    <DialogContent class="sm:max-w-lg">
      <DialogHeader><DialogTitle>预设规则</DialogTitle><DialogDescription>选择常用规则插入到编辑器，保存后生效。</DialogDescription></DialogHeader>
      <div class="max-h-[60vh] space-y-2 overflow-y-auto pr-1">
        <div v-for="item in edits" :key="item.edit._uid" class="border-border overflow-hidden rounded-xl border">
          <div class="flex items-start gap-2 p-3">
            <button type="button" class="min-w-0 flex-1 cursor-pointer space-y-1 text-left" @click="toggleOpen(item.edit._uid)">
              <div class="flex flex-col gap-0.5 pl-6"><component :is="expanded.has(item.edit._uid) ? ChevronDown : ChevronRight" class="text-muted-foreground size-4 shrink-0" /><span class="truncate font-medium">{{ item.edit.name || '未命名规则' }}</span><span class="text-muted-foreground truncate font-mono text-xs">{{ item.edit.slug }}</span></div>
              <div class="flex flex-wrap items-center gap-1.5 pl-6"><Badge variant="secondary" class="shrink-0">P{{ item.edit.priority }}</Badge><Badge variant="outline" class="shrink-0">{{ TYPE_LABELS[item.edit.whenType] }}</Badge><span class="text-muted-foreground text-xs">{{ summarize(item.edit) }}</span></div>
            </button>
            <Button v-if="isAdded(item.preset)" size="sm" variant="outline" disabled class="shrink-0">已添加</Button>
            <Button v-else size="sm" variant="outline" class="shrink-0" @click="emit('add', item.preset)">添加</Button>
          </div>
          <div v-if="expanded.has(item.edit._uid)" class="space-y-1 border-t border-foreground/10 p-3 pl-9 text-xs text-muted-foreground">
            <div><span class="text-foreground">触发：</span>{{ whenPhrase(item.edit) }}</div>
            <div v-if="item.edit.ifs.length"><span class="text-foreground">条件：</span>{{ item.edit.ifs.map(ifPhrase).join('，') }}</div>
            <div v-if="item.edit.dos.length"><span class="text-foreground">动作：</span>{{ item.edit.dos.map(actPhrase).join('，') }}</div>
            <div><span class="text-foreground">路由：</span>{{ routePhrase(item.edit) }}</div>
          </div>
        </div>
        <p v-if="!edits.length" class="py-4 text-center text-sm text-muted-foreground">暂无预设</p>
      </div>
    </DialogContent>
  </Dialog>
</template>
