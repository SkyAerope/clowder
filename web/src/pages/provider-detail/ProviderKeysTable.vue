<script setup lang="ts">
import { Ban, Check, CircleCheck, CircleDashed, MoreVertical, SlidersHorizontal, Stethoscope, Trash2, X } from 'lucide-vue-next'
import type { KeyRow } from '../../api'
import Card from '../../components/Card.vue'
import CopyButton from '../../components/CopyButton.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Table, TableBody, TableCell, TableEmpty, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { KeyCheckState } from './useProviderKeys'
import { formatProviderTime } from './types'

defineProps<{
  keys: readonly KeyRow[]
  keyState: Readonly<Record<number, KeyCheckState>>
  checking: boolean
}>()
const emit = defineEmits<{
  checkAll: []
  check: [key: KeyRow]
  status: [key: KeyRow, status: string]
  delete: [key: KeyRow]
}>()

const keyStatuses = [
  { value: 'active', label: '激活', icon: CircleCheck },
  { value: 'idle', label: '空闲', icon: CircleDashed },
  { value: 'invalid', label: '无效', icon: Ban },
] as const
</script>

<template>
  <Card :title="`Keys (${keys.length})`">
    <template #action>
      <Button variant="outline" size="sm" :disabled="checking" @click="emit('checkAll')">
        <Stethoscope class="size-4" :class="checking && 'animate-pulse'" /> 全部检查
      </Button>
    </template>
    <div class="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead><TableHead>Key</TableHead><TableHead>Email</TableHead>
            <TableHead>Metadata</TableHead><TableHead>状态</TableHead><TableHead>最后使用</TableHead>
            <TableHead class="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="key in keys" :key="key.id">
            <TableCell class="text-muted-foreground">#{{ key.id }}</TableCell>
            <TableCell>
              <div class="flex items-center gap-1">
                <span class="font-mono text-xs truncate max-w-48" :title="key.api_key">{{ key.api_key }}</span>
                <CopyButton :value="key.api_key" label="Key" />
              </div>
            </TableCell>
            <TableCell>
              <div v-if="key.email" class="flex items-center gap-1">
                <span class="font-mono text-xs truncate max-w-48" :title="key.email">{{ key.email }}</span>
                <CopyButton :value="key.email" label="Email" />
              </div>
              <span v-else class="text-muted-foreground">—</span>
            </TableCell>
            <TableCell><span class="block max-w-64 truncate font-mono text-xs" :title="key.metadata">{{ key.metadata || '—' }}</span></TableCell>
            <TableCell>
              <StatusBadge :status="key.status" :reason="key.status === 'rate_limited' ? `限流 ${key.rate_limit_count} 次，恢复于 ${formatProviderTime(key.rate_limit_until)}` : undefined" />
            </TableCell>
            <TableCell class="text-xs">{{ formatProviderTime(key.last_used_at) }}</TableCell>
            <TableCell>
              <div class="flex justify-end gap-1">
                <Tooltip>
                  <TooltipTrigger as-child>
                    <Button variant="ghost" size="icon-sm" class="cursor-pointer" :disabled="keyState[key.id]?.loading" @click="emit('check', key)">
                      <span class="inline-flex size-4 transition-all duration-300" :class="keyState[key.id]?.result ? 'scale-100' : 'scale-100'">
                        <Stethoscope v-if="!keyState[key.id]?.result && !keyState[key.id]?.loading" class="size-4" />
                        <Stethoscope v-else-if="keyState[key.id]?.loading" class="size-4 animate-pulse" />
                        <Check v-else-if="keyState[key.id]?.result === 'success'" class="size-4 text-success animate-scale-in-center" />
                        <X v-else-if="keyState[key.id]?.result === 'error'" class="size-4 text-destructive animate-scale-in-center" />
                      </span>
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent v-if="keyState[key.id]?.result === 'success'" class="text-xs max-w-64">
                    <div class="space-y-1">
                      <p>检查通过：{{ keyState[key.id]?.checkStatus }}</p>
                      <p v-if="keyState[key.id]?.modelResponse">模型回复：{{ keyState[key.id]?.modelResponse }}</p>
                      <p v-if="keyState[key.id]?.ttftMS">TTFT：{{ keyState[key.id]?.ttftMS }} ms</p>
                      <p v-if="keyState[key.id]?.metadata" class="break-all">metadata: <code class="font-mono text-2xs">{{ keyState[key.id]?.metadata }}</code></p>
                      <p>点击再次检查</p>
                    </div>
                  </TooltipContent>
                  <TooltipContent v-else-if="keyState[key.id]?.result === 'error'" class="text-xs max-w-64">
                    <div class="space-y-1">
                      <p>状态异常：{{ keyState[key.id]?.checkStatus }}</p>
                      <p v-if="keyState[key.id]?.modelResponse">模型回复：{{ keyState[key.id]?.modelResponse }}</p>
                      <p v-if="keyState[key.id]?.ttftMS">TTFT：{{ keyState[key.id]?.ttftMS }} ms</p>
                      <p v-if="keyState[key.id]?.metadata" class="break-all">metadata: <code class="font-mono text-2xs">{{ keyState[key.id]?.metadata }}</code></p>
                      <p>点击再次检查</p>
                    </div>
                  </TooltipContent>
                  <TooltipContent v-else class="text-xs"><span>检查</span></TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger as-child>
                    <span class="inline-flex">
                      <DropdownMenu :modal="false">
                        <DropdownMenuTrigger as-child>
                          <Button variant="ghost" size="icon-sm" class="cursor-pointer" :disabled="keyState[key.id]?.loading"><MoreVertical class="size-4" /></Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuSub>
                            <DropdownMenuSubTrigger class="cursor-pointer"><SlidersHorizontal />更改状态</DropdownMenuSubTrigger>
                            <DropdownMenuSubContent>
                              <DropdownMenuItem v-for="status in keyStatuses" :key="status.value" :disabled="key.status === status.value" class="cursor-pointer" @select="emit('status', key, status.value)">
                                <component :is="status.icon" />{{ status.label }}<Check v-if="key.status === status.value" class="ml-auto" />
                              </DropdownMenuItem>
                            </DropdownMenuSubContent>
                          </DropdownMenuSub>
                          <DropdownMenuItem variant="destructive" class="cursor-pointer" @select="emit('delete', key)"><Trash2 />删除</DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </span>
                  </TooltipTrigger>
                  <TooltipContent class="text-xs">更多操作</TooltipContent>
                </Tooltip>
              </div>
            </TableCell>
          </TableRow>
          <TableEmpty v-if="!keys.length" :colspan="7">暂无 key</TableEmpty>
        </TableBody>
      </Table>
    </div>
  </Card>
</template>
