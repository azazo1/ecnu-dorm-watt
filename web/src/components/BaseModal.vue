<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import AppIcon from './AppIcon.vue'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    title: string
    size?: 'sm' | 'md' | 'lg'
    busy?: boolean
    closeOnBackdrop?: boolean
    showClose?: boolean
  }>(),
  {
    size: 'md',
    busy: false,
    closeOnBackdrop: true,
    showClose: true,
  },
)

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const titleId = useId()
const panel = ref<HTMLElement | null>(null)
let previousOverflow = ''
let previousFocus: HTMLElement | null = null

function close(): void {
  if (props.busy) {
    return
  }
  emit('update:modelValue', false)
}

function handleKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    close()
  }
}

function lockBody(): void {
  previousOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  document.addEventListener('keydown', handleKeydown)
  previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  void nextTick(() => {
    panel.value?.focus()
  })
}

function unlockBody(): void {
  document.body.style.overflow = previousOverflow
  document.removeEventListener('keydown', handleKeydown)
  previousFocus?.focus()
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      lockBody()
    } else {
      unlockBody()
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (props.modelValue) {
    unlockBody()
  }
})
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="modelValue"
        class="modal-backdrop"
        @click.self="closeOnBackdrop && close()"
      >
        <section
          ref="panel"
          class="modal-panel"
          :class="`modal-${size}`"
          role="dialog"
          aria-modal="true"
          :aria-labelledby="titleId"
          tabindex="-1"
        >
          <header class="modal-header">
            <h2 :id="titleId" class="modal-title">{{ title }}</h2>
            <button
              v-if="showClose"
              type="button"
              class="btn btn-ghost btn-icon"
              aria-label="关闭"
              :disabled="busy"
              @click="close"
            >
              <AppIcon name="close" :size="18" />
            </button>
          </header>
          <div class="modal-body">
            <slot />
          </div>
          <footer v-if="$slots.footer" class="modal-footer">
            <slot name="footer" />
          </footer>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.modal-backdrop {
  position: fixed;
  z-index: 900;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  background: rgba(15, 23, 42, 0.58);
  backdrop-filter: blur(2px);
}

.modal-panel {
  width: 100%;
  max-height: calc(100vh - 40px);
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface);
  box-shadow: var(--shadow);
}

.modal-sm {
  max-width: 420px;
}

.modal-md {
  max-width: 560px;
}

.modal-lg {
  max-width: 760px;
}

.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
}

.modal-title {
  font-size: 17px;
}

.modal-body {
  padding: 20px;
}

.modal-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
  padding: 14px 20px;
  border-top: 1px solid var(--border);
  background: var(--surface-soft);
}

.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.16s ease;
}

.modal-enter-active .modal-panel,
.modal-leave-active .modal-panel {
  transition: transform 0.16s ease;
}

.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}

.modal-enter-from .modal-panel,
.modal-leave-to .modal-panel {
  transform: translateY(8px) scale(0.99);
}

@media (max-width: 640px) {
  .modal-backdrop {
    align-items: flex-end;
    padding: 0;
  }

  .modal-panel {
    max-height: calc(100vh - 24px);
    border-radius: 12px 12px 0 0;
  }

  .modal-header,
  .modal-body,
  .modal-footer {
    padding-right: 16px;
    padding-left: 16px;
  }

  .modal-footer {
    padding-bottom: calc(14px + env(safe-area-inset-bottom));
  }
}
</style>
