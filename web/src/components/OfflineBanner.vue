<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { HealthState } from '@/api/types'
import type { UiError } from '@/utils/errors'
import AppIcon from './AppIcon.vue'

const props = withDefaults(
  defineProps<{
    health: HealthState
    error?: UiError | null
    intervalMs?: number
    updatedAt?: number | null
    detail?: string | null
  }>(),
  {
    error: null,
    intervalMs: 5000,
    updatedAt: null,
    detail: null,
  },
)

defineEmits<{ retry: [] }>()

const remaining = ref(Math.ceil(props.intervalMs / 1000))
let timer: number | null = null

const visible = computed(
  () =>
    props.health === 'server_down' ||
    props.health === 'no_net' ||
    props.health === 'tls_error' ||
    props.health === 'unknown' ||
    props.error?.code === 'network_error' ||
    props.error?.code === 'upstream_unreachable',
)

const title = computed(() => {
  if (props.health === 'no_net') {
    return '网络已断开'
  }
  if (props.health === 'tls_error') {
    return 'TLS 校验失败'
  }
  if (props.error?.code === 'network_error') {
    return '无法连接本地守护进程'
  }
  if (props.error?.code === 'upstream_unreachable') {
    return '无法连接服务端'
  }
  if (props.health === 'unknown') {
    return '守护进程状态未知'
  }
  return '服务端不可达'
})

const description = computed(() => {
  if (props.health === 'no_net') {
    return '请检查本机网络连接, 恢复后页面会自动重试.'
  }
  if (props.health === 'tls_error') {
    return '请检查代理设置, 系统证书, 以及服务端证书链.'
  }
  if (props.error?.detail) {
    return props.error.detail
  }
  return props.detail || '页面会按设定间隔自动重试, 也可以手动刷新.'
})

function restartTimer(): void {
  if (timer !== null) {
    window.clearInterval(timer)
  }
  remaining.value = Math.ceil(props.intervalMs / 1000)
  timer = window.setInterval(() => {
    remaining.value = remaining.value <= 0
      ? Math.ceil(props.intervalMs / 1000)
      : remaining.value - 1
  }, 1000)
}

watch(
  [() => props.intervalMs, () => props.updatedAt],
  () => {
    if (visible.value) {
      restartTimer()
    }
  },
  { immediate: true },
)

watch(visible, (value) => {
  if (value) {
    restartTimer()
  } else if (timer !== null) {
    window.clearInterval(timer)
    timer = null
  }
})

onBeforeUnmount(() => {
  if (timer !== null) {
    window.clearInterval(timer)
  }
})
</script>

<template>
  <div v-if="visible" class="offline-banner" role="alert">
    <span class="offline-icon">
      <AppIcon name="alert" :size="20" />
    </span>
    <div class="offline-body">
      <strong>{{ title }}</strong>
      <p>{{ description }}</p>
      <p class="offline-meta">将在 {{ remaining }} 秒后自动重试</p>
    </div>
    <div class="offline-actions">
      <button type="button" class="btn btn-sm" @click="$emit('retry')">立即重试</button>
      <RouterLink class="btn btn-sm btn-ghost" to="/settings">连接设置</RouterLink>
    </div>
  </div>
</template>

<style scoped>
.offline-banner {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 24px;
  border-bottom: 1px solid var(--danger);
  background: var(--danger-soft);
  color: var(--text);
}

.offline-icon {
  color: var(--danger);
  flex: 0 0 auto;
  margin-top: 1px;
}

.offline-body {
  flex: 1;
  min-width: 0;
}

.offline-body p {
  margin-top: 1px;
  font-size: 12px;
  color: var(--text-muted);
  overflow-wrap: anywhere;
}

.offline-meta {
  font-variant-numeric: tabular-nums;
}

.offline-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

@media (max-width: 768px) {
  .offline-banner {
    align-items: flex-start;
    flex-wrap: wrap;
    padding: 10px 16px;
  }

  .offline-actions {
    width: 100%;
    padding-left: 30px;
  }
}
</style>
