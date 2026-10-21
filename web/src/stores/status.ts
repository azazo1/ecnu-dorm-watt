import { computed, reactive, ref } from 'vue'
import { api } from '@/api/client'
import type { HealthState, StatusResponse } from '@/api/types'
import { toUiError, type UiError } from '@/utils/errors'

interface StatusState {
  data: StatusResponse | null
  loading: boolean
  error: UiError | null
  updatedAt: number | null
}

const state = reactive<StatusState>({
  data: null,
  loading: false,
  error: null,
  updatedAt: null,
})

const manualRefreshCount = ref(0)
let controller: AbortController | null = null
let sequence = 0
let timer: number | null = null
let intervalMs = 5000
let started = false

function deriveHealth(): HealthState {
  if (!state.data) {
    return state.error ? 'server_down' : 'starting'
  }
  const local = state.data.local.health
  const remote = state.data.remote
  if (!remote) {
    return local
  }
  if (remote.active_room === null) {
    return 'no_room'
  }
  if (remote.session.state === 'missing' || remote.session.state === 'invalid') {
    return 'not_logged_in'
  }
  if (remote.monitor.last_error) {
    return 'upstream_error'
  }
  return local
}

const health = computed(deriveHealth)
const remote = computed(() => state.data?.remote ?? null)
const local = computed(() => state.data?.local ?? null)
const activeRoom = computed(() => state.data?.remote?.active_room ?? null)

export async function refreshStatus(): Promise<void> {
  controller?.abort()
  controller = new AbortController()
  const current = ++sequence
  state.loading = true
  try {
    const data = await api.getStatus(controller.signal)
    if (current !== sequence) {
      return
    }
    state.data = data
    state.error = null
    state.updatedAt = Date.now()
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

export async function manualRefresh(): Promise<void> {
  await refreshStatus()
  manualRefreshCount.value += 1
}

function schedule(): void {
  if (timer !== null) {
    window.clearInterval(timer)
  }
  timer = window.setInterval(() => {
    if (!document.hidden) {
      void refreshStatus()
    }
  }, intervalMs)
}

function handleVisibility(): void {
  if (!document.hidden) {
    void refreshStatus()
  }
}

function handleOnline(): void {
  void refreshStatus()
}

export function startStatusPolling(ms: number): void {
  intervalMs = Math.max(1000, ms)
  if (!started) {
    started = true
    document.addEventListener('visibilitychange', handleVisibility)
    window.addEventListener('online', handleOnline)
    void refreshStatus()
  }
  schedule()
}

export function setStatusInterval(ms: number): void {
  intervalMs = Math.max(1000, ms)
  if (started) {
    schedule()
  }
}

export function stopStatusPolling(): void {
  if (timer !== null) {
    window.clearInterval(timer)
    timer = null
  }
  if (started) {
    document.removeEventListener('visibilitychange', handleVisibility)
    window.removeEventListener('online', handleOnline)
    started = false
  }
}

export function useStatusStore() {
  return {
    state,
    health,
    remote,
    local,
    activeRoom,
    manualRefreshCount,
    manualRefresh,
    refreshStatus,
    startStatusPolling,
    setStatusInterval,
    stopStatusPolling,
  }
}
