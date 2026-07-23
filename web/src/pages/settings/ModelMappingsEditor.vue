<script setup lang="ts">
import Card from '../../components/Card.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Plus, Trash2 } from '@lucide/vue'
import type { ModelEntry, ModelPattern } from './model-mapping-codec'

const entries = defineModel<ModelEntry[]>('entries', { required: true })
const patterns = defineModel<ModelPattern[]>('patterns', { required: true })

function addEntry(): void { entries.value.push({ canonical: '', aliases: '' }) }
function removeEntry(index: number): void { entries.value.splice(index, 1) }
function addPattern(): void { patterns.value.push({ match: '', canonical: '' }) }
function removePattern(index: number): void { patterns.value.splice(index, 1) }
</script>

<template>
  <Card title="models.yaml（模型映射）">
    <div class="space-y-4">
      <div class="space-y-2">
        <div class="flex items-center justify-between">
          <Label class="text-xs text-muted-foreground">客户端请求 ← 上游（逗号分隔）</Label>
          <Button variant="ghost" size="xs" @click="addEntry"><Plus class="size-3" /> 加映射</Button>
        </div>
        <p v-if="!entries.length" class="text-xs text-muted-foreground">暂无映射</p>
        <div v-for="(entry, index) in entries" :key="index" class="flex flex-col items-stretch gap-2 sm:flex-row sm:items-center">
          <Input v-model="entry.canonical" class="w-full font-mono sm:w-52" placeholder="canonical name" />
          <span class="text-muted-foreground">←</span>
          <Input v-model="entry.aliases" class="min-w-0 flex-1 font-mono" placeholder="alias-1, alias-2" />
          <Button variant="ghost" size="icon-sm" class="text-destructive shrink-0" @click="removeEntry(index)">
            <Trash2 class="size-4" />
          </Button>
        </div>
      </div>

      <div class="space-y-2 border-t border-foreground/10 pt-3">
        <div class="flex items-center justify-between">
          <Label class="text-xs text-muted-foreground">正则映射 (patterns)</Label>
          <Button variant="ghost" size="xs" @click="addPattern"><Plus class="size-3" /> 加正则</Button>
        </div>
        <p v-if="!patterns.length" class="text-xs text-muted-foreground">暂无正则规则</p>
        <div v-for="(pattern, index) in patterns" :key="index" class="flex flex-col items-stretch gap-2 sm:flex-row sm:items-center">
          <Input v-model="pattern.canonical" class="w-full font-mono sm:w-52" placeholder="canonical name" />
          <span class="text-muted-foreground">←</span>
          <Input v-model="pattern.match" class="min-w-0 flex-1 font-mono" placeholder="RE2 正则 (match)" />
          <Button variant="ghost" size="icon-sm" class="text-destructive shrink-0" @click="removePattern(index)">
            <Trash2 class="size-4" />
          </Button>
        </div>
      </div>
    </div>
  </Card>
</template>
