import { ref } from 'vue'

const LOW_KEY = 'dormwatt.notify.low'
const RISE_KEY = 'dormwatt.notify.rise'
const LAST_KEY = 'dormwatt.notify.last'

export type NotifyKind = 'low' | 'rise'

export interface LastNotify {
  at: number
  kind: NotifyKind
}

function readBoolean(key: string, fallback: boolean): boolean {
  try {
    const value = localStorage.getItem(key)
    if (value === '1') {
      return true
    }
    if (value === '0') {
      return false
    }
  } catch {
    // 存储不可用时使用默认值.
  }
  return fallback
}

function writeBoolean(key: string, value: boolean): void {
  try {
    localStorage.setItem(key, value ? '1' : '0')
  } catch {
    // 存储失败时开关只在当前页面生效.
  }
}

const notifyLow = ref(readBoolean(LOW_KEY, true))
const notifyRise = ref(readBoolean(RISE_KEY, true))

export function setNotifyLow(value: boolean): void {
  notifyLow.value = value
  writeBoolean(LOW_KEY, value)
}

export function setNotifyRise(value: boolean): void {
  notifyRise.value = value
  writeBoolean(RISE_KEY, value)
}

export function readLastNotify(): LastNotify | null {
  try {
    const raw = localStorage.getItem(LAST_KEY)
    if (!raw) {
      return null
    }
    const parsed = JSON.parse(raw) as Partial<LastNotify>
    if (typeof parsed.at === 'number' && (parsed.kind === 'low' || parsed.kind === 'rise')) {
      return { at: parsed.at, kind: parsed.kind }
    }
  } catch {
    // 忽略损坏的通知记录.
  }
  return null
}

export function writeLastNotify(kind: NotifyKind): void {
  try {
    localStorage.setItem(LAST_KEY, JSON.stringify({ at: Date.now(), kind }))
  } catch {
    // 忽略存储失败.
  }
}

export function useNotificationPreferences() {
  return {
    notifyLow,
    notifyRise,
    setNotifyLow,
    setNotifyRise,
  }
}
