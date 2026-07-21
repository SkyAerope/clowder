<script setup lang="ts">
import Card from '../../components/Card.vue'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import type { ConfigForm } from './config-codec'

const cfg = defineModel<ConfigForm>({ required: true })
</script>

<template>
  <div class="grid grid-cols-1 xl:grid-cols-2 gap-4">
    <!-- 基础 -->
    <Card title="基础">
      <div class="grid grid-cols-2 gap-3">
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">监听地址 (bind_addr)</Label>
          <Input v-model="cfg.bind_addr" class="font-mono" />
        </div>
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">端口 (port)</Label>
          <Input v-model.number="cfg.port" type="number" />
        </div>
        <div class="col-span-2 space-y-1">
          <Label class="text-xs text-muted-foreground">API Key (api_key)</Label>
          <Input v-model="cfg.api_key" class="font-mono" />
        </div>
        <div class="col-span-2 space-y-1">
          <Label class="text-xs text-muted-foreground">Providers 目录 (providers_dir)</Label>
          <Input v-model="cfg.providers_dir" class="font-mono" />
        </div>
      </div>
    </Card>

    <!-- 面板 -->
    <Card title="面板 (dashboard)">
      <div class="grid grid-cols-2 gap-3">
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">公开访问 (public)</Label>
          <div class="flex h-8 items-center">
            <Switch v-model="cfg.dashboard.public" />
          </div>
        </div>
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">启用管理 (admin)</Label>
          <div class="flex h-8 items-center">
            <Switch v-model="cfg.dashboard.admin" />
          </div>
        </div>
        <div class="col-span-2 space-y-1">
          <Label class="text-xs text-muted-foreground">管理密钥 (admin_key)</Label>
          <Input v-model="cfg.dashboard.admin_key" class="font-mono" />
        </div>
      </div>
    </Card>

    <!-- 连接池 -->
    <Card title="连接池 (pool)">
      <div class="grid grid-cols-2 gap-3">
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">最小活跃 Key 数</Label>
          <Input v-model.number="cfg.pool.min_active_keys" type="number" />
        </div>
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">最大 Key 总数</Label>
          <Input v-model.number="cfg.pool.max_total_keys" type="number" />
        </div>
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">注册间隔(秒)</Label>
          <Input v-model.number="cfg.pool.register_interval" type="number" />
        </div>
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">注册退避基数(秒)</Label>
          <Input v-model.number="cfg.pool.register_backoff_base_seconds" type="number" />
        </div>
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">注册退避上限(秒，0=无)</Label>
          <Input v-model.number="cfg.pool.register_backoff_max_seconds" type="number" />
        </div>
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">自动注册</Label>
          <div class="flex h-8 items-center">
            <Switch v-model="cfg.pool.auto_register" />
          </div>
        </div>
      </div>
    </Card>

    <!-- 路由 -->
    <Card title="路由 (routing)">
      <div class="grid grid-cols-2 gap-3">
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">最大重试次数</Label>
          <Input v-model.number="cfg.routing.max_retries" type="number" />
        </div>
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">最大并发尝试</Label>
          <Input v-model.number="cfg.routing.max_parallel_attempts" type="number" />
        </div>
        <div class="col-span-2 space-y-1">
          <Label class="text-xs text-muted-foreground">上游流式 (upstream_streaming)</Label>
          <div class="flex h-8 items-center">
            <Switch v-model="cfg.routing.upstream_streaming" />
          </div>
        </div>
      </div>
    </Card>

    <!-- 退避 -->
    <Card title="退避 (backoff)">
      <div class="grid grid-cols-2 gap-3">
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">基数(秒)</Label>
          <Input v-model.number="cfg.backoff.base_seconds" type="number" />
        </div>
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">上限(秒，0=无)</Label>
          <Input v-model.number="cfg.backoff.max_seconds" type="number" />
        </div>
      </div>
    </Card>

    <!-- 抓包 -->
    <Card title="抓包 (capture)">
      <div class="grid grid-cols-2 gap-3">
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">最大记录数</Label>
          <Input v-model.number="cfg.capture.max_records" type="number" />
        </div>
        <div class="space-y-1">
          <Label class="text-xs text-muted-foreground">响应缓冲(KB)</Label>
          <Input v-model.number="cfg.capture.response_buffer_kb" type="number" />
        </div>
        <div class="col-span-2 space-y-1">
          <Label class="text-xs text-muted-foreground">脱敏 (redact)</Label>
          <div class="flex h-8 items-center">
            <Switch v-model="cfg.capture.redact" />
          </div>
        </div>
      </div>
    </Card>
  </div>
</template>
