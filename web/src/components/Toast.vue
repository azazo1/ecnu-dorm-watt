<script setup lang="ts">
import { useToast } from '@/composables/useToast'
import AppIcon from './AppIcon.vue'

const toast = useToast()
</script>

<template>
  <div class="toast-host" aria-live="polite" aria-atomic="false">
    <TransitionGroup name="toast">
      <div
        v-for="item in toast.items"
        :key="item.id"
        class="toast"
        :data-kind="item.kind"
        role="status"
      >
        <span class="toast-icon">
          <AppIcon
            :name="item.kind === 'success' ? 'check' : item.kind === 'error' ? 'alert' : 'info'"
            :size="17"
          />
        </span>
        <span class="toast-message">{{ item.message }}</span>
        <button
          type="button"
          class="toast-close"
          aria-label="关闭提示"
          @click="toast.remove(item.id)"
        >
          <AppIcon name="close" :size="14" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>

<style scoped>
.toast-host {
  position: fixed;
  z-index: 1000;
  top: 72px;
  right: 20px;
  display: flex;
  width: min(360px, calc(100vw - 32px));
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
}

.toast {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 11px 12px;
  border: 1px solid var(--border);
  border-left: 3px solid var(--accent);
  border-radius: var(--radius-sm);
  background: var(--surface);
  box-shadow: var(--shadow);
  color: var(--text);
  pointer-events: auto;
}

.toast[data-kind="success"] {
  border-left-color: var(--ok);
}

.toast[data-kind="error"] {
  border-left-color: var(--danger);
}

.toast[data-kind="info"] {
  border-left-color: var(--accent);
}

.toast-icon {
  flex: 0 0 auto;
  margin-top: 1px;
  color: var(--accent);
}

.toast[data-kind="success"] .toast-icon {
  color: var(--ok);
}

.toast[data-kind="error"] .toast-icon {
  color: var(--danger);
}

.toast-message {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.toast-close {
  display: inline-flex;
  width: 22px;
  height: 22px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted);
}

.toast-close:hover {
  background: var(--surface-soft);
  color: var(--text);
}

.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}

.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}

@media (max-width: 768px) {
  .toast-host {
    top: 66px;
    right: 16px;
    left: 16px;
    width: auto;
  }
}
</style>
