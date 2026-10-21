<script setup lang="ts">
import { computed } from 'vue'
import type { HealthState } from '@/api/types'

const props = withDefaults(
  defineProps<{
    health: HealthState
    detail?: string | null
  }>(),
  { detail: null },
)

const labels: Record<HealthState, string> = {
  starting: '启动中',
  ok: '正常',
  no_room: '未选择房间',
  not_logged_in: '登录已失效',
  server_down: '服务端不可达',
  no_net: '网络已断开',
  tls_error: 'TLS 校验失败',
  upstream_error: '上游查询失败',
  unknown: '状态未知',
}

const tones: Record<HealthState, 'ok' | 'warn' | 'danger' | 'muted'> = {
  starting: 'muted',
  ok: 'ok',
  no_room: 'muted',
  not_logged_in: 'warn',
  server_down: 'danger',
  no_net: 'danger',
  tls_error: 'danger',
  upstream_error: 'warn',
  unknown: 'muted',
}

const label = computed(() => labels[props.health] ?? '状态未知')
const tone = computed(() => tones[props.health] ?? 'muted')
</script>

<template>
  <span
    class="status-badge"
    :data-tone="tone"
    :title="detail ?? label"
    role="status"
    aria-live="polite"
  >
    <span class="status-dot" :class="{ 'is-pulsing': health === 'starting' }" />
    <span>{{ label }}</span>
  </span>
</template>

<style scoped>
.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-height: 28px;
  padding: 3px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--muted);
}

.status-badge[data-tone="ok"] {
  border-color: var(--ok);
  color: var(--ok);
}

.status-badge[data-tone="ok"] .status-dot {
  background: var(--ok);
}

.status-badge[data-tone="warn"] {
  border-color: var(--warn);
  color: var(--warn);
}

.status-badge[data-tone="warn"] .status-dot {
  background: var(--warn);
}

.status-badge[data-tone="danger"] {
  border-color: var(--danger);
  color: var(--danger);
}

.status-badge[data-tone="danger"] .status-dot {
  background: var(--danger);
}

.status-badge[data-tone="muted"] {
  color: var(--text-muted);
}

.status-dot.is-pulsing {
  animation: pulse-soft 1.2s ease-in-out infinite;
}
</style>
