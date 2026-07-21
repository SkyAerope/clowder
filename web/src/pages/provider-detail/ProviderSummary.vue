<script setup lang="ts">
import Card from '../../components/Card.vue'
import type { ProviderDetail } from './types'
import { formatProviderTime } from './types'

defineProps<{ detail: ProviderDetail }>()
</script>

<template>
  <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
    <Card title="Manifest">
      <dl class="grid grid-cols-2 gap-y-2 text-sm">
        <dt class="text-muted-foreground">base_url</dt><dd class="font-mono text-xs break-all">{{ detail.manifest.base_url }}</dd>
        <dt class="text-muted-foreground">key_strategy</dt><dd>{{ detail.manifest.key_strategy }}</dd>
        <dt class="text-muted-foreground">priority / weight</dt><dd>{{ detail.manifest.priority }} / {{ detail.manifest.weight }}</dd>
        <dt class="text-muted-foreground">注册脚本</dt><dd>{{ detail.manifest.has_register ? '有' : '无' }}</dd>
        <dt class="text-muted-foreground">check 脚本</dt><dd>{{ detail.manifest.has_check ? '有' : '无' }}</dd>
        <template v-if="!detail.manifest.has_check">
          <dt class="text-muted-foreground">默认测试模型</dt>
          <dd class="font-mono text-xs break-all">{{ detail.manifest.default_check_model || '（未设置）' }}</dd>
        </template>
      </dl>
    </Card>

    <Card title="注册状态">
      <dl v-if="detail.manifest.has_register" class="grid grid-cols-2 gap-y-2 text-sm">
        <dt class="text-muted-foreground">上次注册</dt><dd>{{ formatProviderTime(detail.last_register_at) }}</dd>
        <dt class="text-muted-foreground">耗时</dt><dd>{{ detail.last_register_duration > 0 ? detail.last_register_duration + 'ms' : '—' }}</dd>
        <dt class="text-muted-foreground">上次错误</dt>
        <dd :class="detail.last_register_error && 'text-destructive'">{{ detail.last_register_error || '无' }}</dd>
      </dl>
      <p v-else class="text-sm text-muted-foreground">无注册脚本</p>
    </Card>
  </div>
</template>
