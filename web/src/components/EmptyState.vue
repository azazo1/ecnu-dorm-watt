<script setup lang="ts">
import AppIcon from './AppIcon.vue'
import type { IconName } from './icons'

withDefaults(
  defineProps<{
    icon?: IconName
    title: string
    description?: string
    actionLabel?: string
  }>(),
  { icon: 'info' },
)

defineEmits<{ action: [] }>()
</script>

<template>
  <div class="empty-state">
    <span class="empty-icon">
      <AppIcon :name="icon" :size="28" />
    </span>
    <h3 class="empty-title">{{ title }}</h3>
    <p v-if="description" class="empty-description">{{ description }}</p>
    <slot />
    <button v-if="actionLabel" type="button" class="btn btn-primary" @click="$emit('action')">
      {{ actionLabel }}
    </button>
  </div>
</template>

<style scoped>
.empty-state {
  display: flex;
  min-height: 190px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 28px 20px;
  border: 1px dashed var(--border-strong);
  border-radius: var(--radius);
  text-align: center;
}

.empty-icon {
  display: inline-flex;
  width: 52px;
  height: 52px;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--surface-soft);
  color: var(--text-muted);
}

.empty-title {
  font-size: 16px;
}

.empty-description {
  max-width: 460px;
  color: var(--text-muted);
  font-size: 13px;
}
</style>
