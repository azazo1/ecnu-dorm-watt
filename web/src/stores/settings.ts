import { computed, reactive } from 'vue'
import { api } from '@/api/client'
import type { SettingsResponse } from '@/api/types'
import { parseDurationMs } from '@/utils/duration'
import { toUiError, type UiError } from '@/utils/errors'
import { browserTimeZone } from '@/utils/time'

interface SettingsState {
  data: SettingsResponse | null
  loading: boolean
  loaded: boolean
  error: UiError | null
}

const state = reactive<SettingsState>({
  data: null,
  loading: false,
  loaded: false,
  error: null,
})

let controller: AbortController | null = null
let sequence = 0

export async function loadSettings(force = false): Promise<void> {
  if (state.loaded && !force) {
    return
  }
  controller?.abort()
  controller = new AbortController()
  const current = ++sequence
  state.loading = true
  state.error = null
  try {
    const data = await api.getSettings(controller.signal)
    if (current !== sequence) {
      return
    }
    state.data = data
    state.loaded = true
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      return
    }
    if (current !== sequence) {
      return
    }
    state.error = toUiError(error)
  } finally {
    if (current === sequence) {
      state.loading = false
    }
  }
}

export function setSettings(data: SettingsResponse): void {
  state.data = data
  state.loaded = true
  state.error = null
}

const timezone = computed(() => state.data?.ui.timezone || browserTimeZone())
const refreshIntervalMs = computed(
  () => parseDurationMs(state.data?.ui.refresh_interval) ?? 5000,
)

export function useSettingsStore() {
  return {
    state,
    timezone,
    refreshIntervalMs,
    loadSettings,
    setSettings,
  }
}
