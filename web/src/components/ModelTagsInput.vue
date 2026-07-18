<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef } from 'vue'
import {
  ListboxContent,
  ListboxItem,
  ListboxRoot,
  ListboxVirtualizer,
  useFilter,
} from 'reka-ui'
import { Download } from 'lucide-vue-next'
import { toast } from 'vue-sonner'
import { api } from '../api'
import { Button } from '@/components/ui/button'
import { Popover, PopoverAnchor, PopoverContent } from '@/components/ui/popover'
import {
  TagsInput,
  TagsInputInput,
  TagsInputItem,
  TagsInputItemDelete,
  TagsInputItemText,
} from '@/components/ui/tags-input'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

const props = defineProps<{ provider: string }>()
const models = defineModel<string[]>({ required: true })

const root = useTemplateRef<HTMLDivElement>('root')
const discoveredModels = ref<string[]>([])
const query = ref('')
const open = ref(false)
const discovering = ref(false)
const autocompleteActive = ref(false)
const { contains } = useFilter({ sensitivity: 'base' })

const suggestions = computed(() => {
  const selected = new Set(models.value)
  const seen = new Set<string>()
  return discoveredModels.value.filter((model) => {
    if (selected.has(model) || seen.has(model)) return false
    seen.add(model)
    return !query.value || contains(model, query.value)
  })
})

function updateQuery(event: Event) {
  query.value = (event.target as HTMLInputElement).value
  if (autocompleteActive.value) open.value = true
}

async function resetQuery() {
  query.value = ''
  await nextTick()
  const input = root.value?.querySelector('input')
  if (!input) return
  input.value = ''
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.focus()
}

function openSuggestions() {
  if (autocompleteActive.value) open.value = true
}

function keepOpenForAnchor(event: Event) {
  const originalEvent = (event as CustomEvent<{ originalEvent?: Event }>).detail?.originalEvent
  const target = originalEvent?.target
  if (target instanceof Node && root.value?.contains(target)) {
    event.preventDefault()
  }
}

async function discoverModels() {
  discovering.value = true
  try {
    const result = await api.post<{ models: string[] }>(`/api/admin/providers/${props.provider}/discover-models`)
    discoveredModels.value = result.models ?? []
    autocompleteActive.value = true
    open.value = true
    await nextTick()
    root.value?.querySelector('input')?.focus()
  } catch (error: unknown) {
    const message = error instanceof Error ? error.message : String(error)
    toast.error(`拉取模型失败: ${message}`)
  } finally {
    discovering.value = false
  }
}
</script>

<template>
  <Popover v-model:open="open" :modal="false">
    <ListboxRoot v-model="models" multiple highlight-on-hover>
      <PopoverAnchor as-child>
        <div ref="root" class="w-full" @focusin="openSuggestions">
          <TagsInput
            v-slot="{ modelValue }"
            v-model="models"
            class="min-h-8 w-full rounded-lg px-2.5 py-1 shadow-none"
            :duplicate="false"
            @add-tag="resetQuery"
          >
            <TagsInputItem
              v-for="model in modelValue"
              :key="model.toString()"
              :value="model.toString()"
            >
              <TagsInputItemText class="font-mono" />
              <TagsInputItemDelete class="cursor-pointer" />
            </TagsInputItem>
            <TagsInputInput
              placeholder="输入模型名并按回车"
              @input="updateQuery"
              @keydown.down="autocompleteActive && (open = true)"
            />
            <Tooltip>
              <TooltipTrigger as-child>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  class="order-last ml-auto shrink-0"
                  :disabled="discovering"
                  aria-label="从提供商拉取模型"
                  @click="discoverModels"
                >
                  <Download :class="discovering && 'animate-pulse'" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>从提供商拉取模型</TooltipContent>
            </Tooltip>
          </TagsInput>
        </div>
      </PopoverAnchor>
      <PopoverContent
        align="start"
        class="w-[var(--reka-popover-trigger-width)] p-1"
        @open-auto-focus.prevent
        @pointer-down-outside="keepOpenForAnchor"
        @focus-outside="keepOpenForAnchor"
      >
        <ListboxContent class="max-h-64 overflow-y-auto">
          <ListboxVirtualizer
            v-slot="{ option }"
            :options="suggestions"
            :text-content="(model: string) => model"
            :estimate-size="32"
          >
            <ListboxItem
              :key="option"
              :value="option"
              class="data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground block h-8 w-full min-w-0 cursor-default truncate rounded-sm px-2 py-1.5 text-left font-mono text-sm leading-5 outline-none"
              :title="option"
              @select="resetQuery"
            >
              {{ option }}
            </ListboxItem>
          </ListboxVirtualizer>
          <p v-if="!suggestions.length" class="px-2 py-4 text-center text-sm text-muted-foreground">
            没有可添加的模型
          </p>
        </ListboxContent>
      </PopoverContent>
    </ListboxRoot>
  </Popover>
</template>
