import { computed, watch } from 'vue'
import { useSettingsStore } from '@/stores/settings'
import { readLastNotify, useNotificationPreferences, writeLastNotify } from '@/stores/preferences'
import { useStatusStore } from '@/stores/status'
import { useToast } from './useToast'
import { notificationPermission } from './notification-permission'

const THROTTLE_MS = 30 * 60 * 1000

export function useNotifications() {
  const status = useStatusStore()
  const settings = useSettingsStore()
  const toast = useToast()
  const { notifyLow, notifyRise } = useNotificationPreferences()

  const degree = computed(() => status.remote.value?.monitor.last_degree ?? null)
  const threshold = computed(() => settings.state.data?.monitor.degree_threshold ?? 0)
  let lastDegree: number | null = null

  function notify(kind: 'low' | 'rise', title: string, body: string, enabled: boolean): void {
    if (!enabled || notificationPermission.value !== 'granted') {
      return
    }
    const last = readLastNotify()
    if (last && Date.now() - last.at < THROTTLE_MS) {
      return
    }
    try {
      const notification = new Notification(title, {
        body,
        tag: `dormwatt-${kind}`,
      })
      notification.onerror = () => {
        toast.error('浏览器通知发送失败.')
      }
      writeLastNotify(kind)
    } catch {
      toast.error('浏览器通知发送失败, 请检查系统通知权限.')
    }
  }

  watch(
    [degree, threshold],
    ([current, limit]) => {
      if (current === null || !Number.isFinite(current)) {
        return
      }
      if (lastDegree === null) {
        lastDegree = current
        return
      }
      const previous = lastDegree
      lastDegree = current

      if (previous >= limit && current < limit) {
        notify(
          'low',
          '宿舍电量不足',
          `当前剩余 ${current.toFixed(2)} 度, 低于阈值 ${limit.toFixed(2)} 度.`,
          notifyLow.value,
        )
      }

      if (current > previous + 0.005) {
        notify(
          'rise',
          '宿舍电量增加',
          `电量从 ${previous.toFixed(2)} 度增加到 ${current.toFixed(2)} 度.`,
          notifyRise.value,
        )
      }
    },
    { flush: 'post' },
  )
}
