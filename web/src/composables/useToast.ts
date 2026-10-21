import { reactive } from 'vue'

export type ToastKind = 'success' | 'error' | 'info'

export interface ToastItem {
  id: number
  kind: ToastKind
  message: string
}

const items = reactive<ToastItem[]>([])
const timers = new Map<number, number>()
let sequence = 0

export function removeToast(id: number): void {
  const index = items.findIndex((item) => item.id === id)
  if (index >= 0) {
    items.splice(index, 1)
  }
  const timer = timers.get(id)
  if (timer !== undefined) {
    window.clearTimeout(timer)
    timers.delete(id)
  }
}

export function pushToast(kind: ToastKind, message: string, timeout = 4500): number {
  const id = ++sequence
  items.push({ id, kind, message })
  const timer = window.setTimeout(() => removeToast(id), timeout)
  timers.set(id, timer)
  return id
}

export function useToast() {
  return {
    items,
    push: pushToast,
    success: (message: string) => pushToast('success', message),
    error: (message: string) => pushToast('error', message),
    info: (message: string) => pushToast('info', message),
    remove: removeToast,
  }
}
