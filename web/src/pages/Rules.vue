<script setup lang="ts">
import { reactive, ref } from 'vue'
import Card from '../components/Card.vue'
import RuleVisualEditor from '../components/rules/RuleVisualEditor.vue'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Save, TriangleAlert, Plus } from '@lucide/vue'
import BuiltinRulesPanel from './BuiltinRulesPanel.vue'
import PresetRulesDialog from './PresetRulesDialog.vue'
import { useRuleDocument } from './useRuleDocument'

const presetOpen = ref(false)
const document = reactive(useRuleDocument())
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between gap-2">
      <h1 class="text-xl font-semibold">规则管理</h1>
      <div class="flex items-center gap-2">
        <Button variant="outline" @click="presetOpen = true"><Plus class="size-4" /> 添加预设规则</Button>
        <Button :disabled="document.saving || !document.dirty" @click="document.save"><Save class="size-4" /> 保存并生效</Button>
      </div>
    </div>

    <Alert v-if="document.warnings.length" variant="default" class="border-warning/40">
      <TriangleAlert class="size-4 text-warning" />
      <AlertTitle class="text-warning">校验警告</AlertTitle>
      <AlertDescription><ul class="text-xs text-muted-foreground list-disc pl-4 space-y-0.5"><li v-for="(warning, index) in document.warnings" :key="index"><span class="font-mono text-foreground">{{ warning.rule }}</span>: {{ warning.message }}</li></ul></AlertDescription>
    </Alert>

    <div v-if="!document.loaded" class="space-y-3"><Skeleton class="h-9 w-48" /><Skeleton class="h-40 w-full rounded-xl" /><Skeleton class="h-40 w-full rounded-xl" /></div>

    <Tabs v-else v-model="document.tab">
      <TabsList><TabsTrigger value="visual">可视化</TabsTrigger><TabsTrigger value="source">源文件</TabsTrigger></TabsList>
      <TabsContent value="visual" class="space-y-3">
        <Alert v-if="document.parseError" variant="destructive"><AlertDescription>{{ document.parseError }}</AlertDescription></Alert>
        <RuleVisualEditor v-model="document.draft" />
        <BuiltinRulesPanel :edits="document.builtinEdits" :disabled="document.disabledBuiltins" @toggle="document.toggleBuiltin" />
      </TabsContent>
      <TabsContent value="source"><Card title="rules.yaml"><Textarea v-model="document.yamlText" :rows="30" class="font-mono text-xs leading-relaxed" spellcheck="false" /></Card></TabsContent>
    </Tabs>

    <PresetRulesDialog v-model:open="presetOpen" :edits="document.presetEdits" :added="document.addedPresets" @add="document.addPreset" />
  </div>
</template>
