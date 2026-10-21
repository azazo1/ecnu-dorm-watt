<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { PRESET_LABELS, presetRange, type RangePreset, type RangeSelection } from '@/utils/range'
import { toZonedInput, zonedInputToRfc3339 } from '@/utils/time'

const props = defineProps<{
  modelValue: RangeSelection
  timeZone: string
  disabled?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: RangeSelection] }>()

const showCustom = ref(props.modelValue.preset === 'custom')
const customFrom = ref('')
const customTo = ref('')

watch(
  () => [props.modelValue.preset, props.modelValue.from, props.modelValue.to, props.timeZone],
  () => {
    showCustom.value = props.modelValue.preset === 'custom'
    if (props.modelValue.preset === 'custom') {
      customFrom.value = toZonedInput(props.modelValue.from, props.timeZone)
      customTo.value = toZonedInput(props.modelValue.to, props.timeZone)
    }
  },
  { immediate: true },
)

const customRange = computed(() => {
  const from = zonedInputToRfc3339(customFrom.value, props.timeZone)
  const to = zonedInputToRfc3339(customTo.value, props.timeZone)
  if (!customFrom.value || !customTo.value) {
    return { from, to, error: '请填写起始时间与结束时间.' }
  }
  if (!from) {
    return { from, to, error: '起始时间格式不正确.' }
  }
  if (!to) {
    return { from, to, error: '结束时间格式不正确.' }
  }
  if (Date.parse(from) > Date.parse(to)) {
    return { from, to, error: '起始时间不能晚于结束时间.' }
  }
  return { from, to, error: '' }
})

function choosePreset(preset: RangePreset): void {
  if (props.disabled) {
    return
  }
  if (preset === 'custom') {
    if (!showCustom.value) {
      const fallback = presetRange('7d')
      customFrom.value = toZonedInput(
        props.modelValue.from ?? fallback.from,
        props.timeZone,
      )
      customTo.value = toZonedInput(props.modelValue.to ?? fallback.to, props.timeZone)
    }
    showCustom.value = true
    return
  }
  showCustom.value = false
  emit('update:modelValue', presetRange(preset))
}

function applyCustom(): void {
  if (props.disabled || customRange.value.error) {
    return
  }
  emit('update:modelValue', {
    preset: 'custom',
    from: customRange.value.from,
    to: customRange.value.to,
  })
}
</script>

<template>
  <div class="range-picker">
    <div class="range-presets" role="group" aria-label="时间范围">
      <button
        v-for="preset in PRESET_LABELS"
        :key="preset.value"
        type="button"
        class="btn btn-sm range-button"
        :class="{ 'is-active': modelValue.preset === preset.value || (preset.value === 'custom' && showCustom) }"
        :aria-pressed="modelValue.preset === preset.value || (preset.value === 'custom' && showCustom)"
        :disabled="disabled"
        @click="choosePreset(preset.value)"
      >
        {{ preset.label }}
      </button>
    </div>

    <div v-if="showCustom" class="range-custom">
      <div class="field">
        <label for="range-from">起始时间</label>
        <input
          id="range-from"
          v-model="customFrom"
          class="input"
          type="datetime-local"
          step="60"
          :disabled="disabled"
        />
      </div>
      <div class="field">
        <label for="range-to">结束时间</label>
        <input
          id="range-to"
          v-model="customTo"
          class="input"
          type="datetime-local"
          step="60"
          :disabled="disabled"
        />
      </div>
      <button
        type="button"
        class="btn btn-primary range-apply"
        :disabled="disabled || Boolean(customRange.error)"
        @click="applyCustom"
      >
        应用
      </button>
      <p class="field-hint range-timezone">时区: {{ timeZone }}</p>
      <p v-if="customRange.error" class="field-error">{{ customRange.error }}</p>
    </div>
  </div>
</template>

<style scoped>
.range-picker {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.range-presets {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
  padding: 3px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-soft);
  width: fit-content;
}

.range-button {
  border-color: transparent;
  background: transparent;
}

.range-button.is-active {
  border-color: var(--accent);
  background: var(--accent);
  color: #ffffff;
}

.range-button.is-active:hover {
  background: var(--accent-hover);
  color: #ffffff;
}

.range-custom {
  display: flex;
  align-items: flex-end;
  gap: 10px;
  flex-wrap: wrap;
}

.range-custom .field {
  min-width: 200px;
}

.range-apply {
  margin-bottom: 0;
}

.range-timezone {
  padding-bottom: 9px;
}

@media (max-width: 640px) {
  .range-presets {
    width: 100%;
  }

  .range-button {
    flex: 1;
  }

  .range-custom .field {
    min-width: 100%;
  }

  .range-apply {
    width: 100%;
  }

  .range-timezone {
    padding-bottom: 0;
  }
}
</style>
