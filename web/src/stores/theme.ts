import { computed, ref } from 'vue'

export type ThemeMode = 'auto' | 'light' | 'dark'
export type ResolvedTheme = 'light' | 'dark'

const STORAGE_KEY = 'dormwatt.theme'
const media = window.matchMedia('(prefers-color-scheme: dark)')

function readStoredMode(): ThemeMode {
  try {
    const value = localStorage.getItem(STORAGE_KEY)
    if (value === 'light' || value === 'dark' || value === 'auto') {
      return value
    }
  } catch {
    // 存储不可用时退回跟随系统.
  }
  return 'auto'
}

const mode = ref<ThemeMode>(readStoredMode())
const resolved = ref<ResolvedTheme>('light')

function resolve(modeValue: ThemeMode): ResolvedTheme {
  if (modeValue === 'auto') {
    return media.matches ? 'dark' : 'light'
  }
  return modeValue
}

function apply(): void {
  resolved.value = resolve(mode.value)
  const root = document.documentElement
  if (mode.value === 'auto') {
    root.removeAttribute('data-theme')
  } else {
    root.setAttribute('data-theme', mode.value)
  }
  root.style.colorScheme = resolved.value
}

function persist(): void {
  try {
    localStorage.setItem(STORAGE_KEY, mode.value)
  } catch {
    // 存储失败不影响当前主题.
  }
}

export function initTheme(): void {
  apply()
  media.addEventListener('change', () => {
    if (mode.value === 'auto') {
      apply()
    }
  })
}

export function setTheme(next: ThemeMode): void {
  mode.value = next
  persist()
  apply()
}

export function cycleTheme(): void {
  const order: ThemeMode[] = ['auto', 'light', 'dark']
  const index = order.indexOf(mode.value)
  setTheme(order[(index + 1) % order.length])
}

export function useTheme() {
  const label = computed(() => {
    if (mode.value === 'auto') {
      return '跟随系统'
    }
    return mode.value === 'light' ? '浅色' : '深色'
  })
  return {
    mode,
    resolved,
    label,
    setTheme,
    cycleTheme,
  }
}
