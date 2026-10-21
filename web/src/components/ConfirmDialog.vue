<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import BaseModal from './BaseModal.vue'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    title?: string
    message: string
    confirmLabel?: string
    cancelLabel?: string
    danger?: boolean
    busy?: boolean
    requireText?: string
  }>(),
  {
    title: '请确认',
    confirmLabel: '确认',
    cancelLabel: '取消',
    danger: false,
    busy: false,
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  confirm: []
  cancel: []
}>()

const typed = ref('')
const input = ref<HTMLInputElement | null>(null)

const canConfirm = computed(() => {
  if (!props.requireText) {
    return true
  }
  return typed.value === props.requireText
})

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      typed.value = ''
      window.setTimeout(() => input.value?.focus(), 40)
    }
  },
)

function cancel(): void {
  if (props.busy) {
    return
  }
  emit('update:modelValue', false)
  emit('cancel')
}

function confirm(): void {
  if (!canConfirm.value || props.busy) {
    return
  }
  emit('confirm')
}

function closeFromModal(value: boolean): void {
  emit('update:modelValue', value)
}

async function focusInput(): Promise<void> {
  await nextTick()
  input.value?.focus()
}
</script>

<template>
  <BaseModal
    :model-value="modelValue"
    :title="title"
    size="sm"
    :busy="busy"
    :close-on-backdrop="!busy"
    @update:model-value="closeFromModal"
  >
    <div class="confirm-body">
      <p class="confirm-message">{{ message }}</p>
      <slot />
      <div v-if="requireText" class="field">
        <label for="confirm-text">
          请输入 <code>{{ requireText }}</code> 以确认
        </label>
        <input
          id="confirm-text"
          ref="input"
          v-model="typed"
          class="input"
          type="text"
          autocomplete="off"
          :disabled="busy"
          @focus="focusInput"
          @keydown.enter.prevent="confirm"
        />
      </div>
    </div>
    <template #footer>
      <button type="button" class="btn" :disabled="busy" @click="cancel">
        {{ cancelLabel }}
      </button>
      <button
        type="button"
        class="btn"
        :class="danger ? 'btn-danger' : 'btn-primary'"
        :disabled="!canConfirm || busy"
        @click="confirm"
      >
        {{ busy ? '处理中...' : confirmLabel }}
      </button>
    </template>
  </BaseModal>
</template>

<style scoped>
.confirm-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.confirm-message {
  color: var(--text);
  font-size: 14px;
  line-height: 1.7;
  overflow-wrap: anywhere;
}
</style>
