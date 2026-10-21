<script setup lang="ts">
import type { UiError } from '@/utils/errors'
import AppIcon from './AppIcon.vue'

withDefaults(
  defineProps<{
    error: UiError | null
    compact?: boolean
  }>(),
  { compact: false },
)

defineEmits<{ retry: [] }>()
</script>

<template>
  <div v-if="error" class="error-state" :class="{ compact }" role="alert">
    <span class="error-icon">
      <AppIcon name="alert" :size="compact ? 20 : 26" />
    </span>
    <div class="error-body">
      <h3 class="error-title">{{ error.title }}</h3>
      <p class="error-detail">{{ error.detail }}</p>
      <p class="error-advice">建议: {{ error.advice }}</p>
      <p class="error-meta">
        错误码: <code>{{ error.code }}</code>
        <span v-if="error.status">HTTP {{ error.status }}</span>
        <span v-if="error.requestId">请求 ID: {{ error.requestId }}</span>
      </p>
      <div class="error-actions">
        <button type="button" class="btn btn-sm" @click="$emit('retry')">重试</button>
        <slot />
      </div>
    </div>
  </div>
</template>

<style scoped>
.error-state {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 18px;
  border: 1px solid var(--danger);
  border-radius: var(--radius);
  background: var(--danger-soft);
}

.error-state.compact {
  padding: 12px;
}

.error-icon {
  flex: 0 0 auto;
  color: var(--danger);
}

.error-body {
  min-width: 0;
}

.error-title {
  font-size: 15px;
}

.error-detail,
.error-advice {
  margin-top: 3px;
  color: var(--text);
  font-size: 13px;
  overflow-wrap: anywhere;
}

.error-advice {
  color: var(--text-muted);
}

.error-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 12px;
  margin-top: 7px;
  color: var(--text-muted);
  font-size: 12px;
}

.error-meta code {
  color: var(--danger);
}

.error-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 10px;
}
</style>
