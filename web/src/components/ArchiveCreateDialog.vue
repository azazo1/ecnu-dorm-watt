<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { UiError } from '@/utils/errors'
import { toZonedInput, zonedInputToRfc3339 } from '@/utils/time'
import BaseModal from './BaseModal.vue'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    initialFrom?: string | null
    initialTo?: string | null
    timeZone: string
    roomLabel?: string
    busy?: boolean
    error?: UiError | null
  }>(),
  {
    initialFrom: null,
    initialTo: null,
    roomLabel: '当前活动房间',
    busy: false,
    error: null,
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  submit: [payload: { from: string; to: string; name: string }]
}>()

const fromInput = ref('')
const toInput = ref('')
const name = ref('')
const localError = ref('')

const nameError = computed(() => {
  const value = name.value.trim()
  if (!value) {
    return ''
  }
  if (value.length > 64) {
    return '名称不能超过 64 个字符.'
  }
  if (value === '.' || value === '..' || value.startsWith('.')) {
    return '名称不能是 . 或 .. , 也不能以点开头.'
  }
  if (/[/\\\u0000-\u001f]/.test(value)) {
    return '名称不能包含斜杠或控制字符.'
  }
  return ''
})

const timeError = computed(() => {
  const from = zonedInputToRfc3339(fromInput.value, props.timeZone)
  const to = zonedInputToRfc3339(toInput.value, props.timeZone)
  if (!fromInput.value || !toInput.value) {
    return '请填写起止时间.'
  }
  if (!from || !to) {
    return '时间格式不正确.'
  }
  if (Date.parse(from) > Date.parse(to)) {
    return '起始时间不能晚于结束时间.'
  }
  return ''
})

const canSubmit = computed(
  () => !props.busy && !nameError.value && !timeError.value,
)

watch(
  () => props.modelValue,
  (open) => {
    if (!open) {
      return
    }
    const now = Date.now()
    fromInput.value = toZonedInput(props.initialFrom ?? now - 7 * 86400000, props.timeZone)
    toInput.value = toZonedInput(props.initialTo ?? now, props.timeZone)
    name.value = ''
    localError.value = ''
  },
)

function submit(): void {
  if (!canSubmit.value) {
    return
  }
  const from = zonedInputToRfc3339(fromInput.value, props.timeZone)
  const to = zonedInputToRfc3339(toInput.value, props.timeZone)
  if (!from || !to) {
    localError.value = '时间转换失败, 请检查时区设置.'
    return
  }
  emit('submit', {
    from,
    to,
    name: name.value.trim(),
  })
}
</script>

<template>
  <BaseModal
    :model-value="modelValue"
    title="新建归档"
    :busy="busy"
    size="md"
    :close-on-backdrop="!busy"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div class="archive-form">
      <div class="alert alert-warn">
        <div class="alert-body">
          <p class="alert-title">归档是破坏性操作</p>
          <p class="alert-text">
            归档会从当前记录中移除这些样本, 操作不可撤销. 归档后仍可在归档页下载 CSV.
          </p>
        </div>
      </div>

      <p class="small muted">归档房间: {{ roomLabel }}</p>

      <div class="form-grid">
        <div class="field">
          <label for="archive-from">起始时间</label>
          <input
            id="archive-from"
            v-model="fromInput"
            class="input"
            type="datetime-local"
            step="60"
            :disabled="busy"
          />
        </div>
        <div class="field">
          <label for="archive-to">结束时间</label>
          <input
            id="archive-to"
            v-model="toInput"
            class="input"
            type="datetime-local"
            step="60"
            :disabled="busy"
          />
        </div>
      </div>

      <div class="field">
        <label for="archive-name">归档名称</label>
        <input
          id="archive-name"
          v-model="name"
          class="input"
          type="text"
          maxlength="64"
          autocomplete="off"
          placeholder="可留空, 默认按日期生成"
          :disabled="busy"
        />
        <p v-if="nameError" class="field-error">{{ nameError }}</p>
        <p v-else class="field-hint">1 到 64 个字符, 不能包含斜杠, 不能是 . 或 .. .</p>
      </div>

      <p class="field-hint">时间按 {{ timeZone }} 解释. 起止两端都包含.</p>

      <p v-if="timeError" class="field-error">{{ timeError }}</p>
      <p v-if="localError" class="field-error">{{ localError }}</p>
      <div v-if="error" class="form-error" role="alert">
        <strong>{{ error.title }}</strong>
        <span>{{ error.detail }}</span>
        <span>{{ error.advice }}</span>
        <span class="small">错误码: <code>{{ error.code }}</code></span>
      </div>
    </div>

    <template #footer>
      <button
        type="button"
        class="btn"
        :disabled="busy"
        @click="emit('update:modelValue', false)"
      >
        取消
      </button>
      <button
        type="button"
        class="btn btn-danger"
        :disabled="!canSubmit"
        @click="submit"
      >
        {{ busy ? '归档中...' : '归档并移除' }}
      </button>
    </template>
  </BaseModal>
</template>

<style scoped>
.archive-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
</style>
