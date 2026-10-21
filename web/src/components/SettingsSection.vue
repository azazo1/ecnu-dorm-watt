<script setup lang="ts">
import type { UiError } from '@/utils/errors'

withDefaults(
  defineProps<{
    id?: string
    title: string
    description?: string
    dirty?: boolean
    saving?: boolean
    error?: UiError | null
    showStatus?: boolean
  }>(),
  {
    id: undefined,
    description: '',
    dirty: false,
    saving: false,
    error: null,
    showStatus: true,
  },
)
</script>

<template>
  <section :id="id" class="settings-section card" :aria-busy="saving">
    <header class="settings-header">
      <div>
        <h2 class="card-title">{{ title }}</h2>
        <p v-if="description" class="card-subtitle">{{ description }}</p>
      </div>
      <span v-if="showStatus && saving" class="tag tag-accent">保存中</span>
      <span v-else-if="showStatus && dirty" class="tag tag-warn">有未保存的更改</span>
      <span v-else-if="showStatus" class="tag tag-ok">已保存</span>
    </header>

    <div class="settings-body">
      <slot />
    </div>

    <div v-if="error" class="form-error" role="alert">
      <strong>{{ error.title }}</strong>
      <span>{{ error.detail }}</span>
      <span class="muted">{{ error.advice }}</span>
      <span class="small">错误码: <code>{{ error.code }}</code></span>
    </div>

    <footer v-if="$slots.actions" class="settings-actions">
      <slot name="actions" />
    </footer>
  </section>
</template>

<style scoped>
.settings-section {
  scroll-margin-top: calc(var(--topbar-height) + 16px);
}

.settings-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}

.settings-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.settings-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}

.form-error {
  margin-top: 14px;
}
</style>
