import { ref } from 'vue'
import { useToast } from './useToast'

export const notificationSupported =
  typeof window !== 'undefined' && 'Notification' in window

export const notificationPermission = ref<NotificationPermission>(
  notificationSupported ? Notification.permission : 'denied',
)

export function refreshNotificationPermission(): void {
  if (notificationSupported) {
    notificationPermission.value = Notification.permission
  }
}

export async function requestNotificationPermission(): Promise<NotificationPermission> {
  const toast = useToast()
  if (!notificationSupported) {
    notificationPermission.value = 'denied'
    return notificationPermission.value
  }
  if (Notification.permission !== 'default') {
    notificationPermission.value = Notification.permission
    return notificationPermission.value
  }
  try {
    notificationPermission.value = await Notification.requestPermission()
    if (notificationPermission.value === 'granted') {
      toast.success('浏览器通知已开启.')
    } else if (notificationPermission.value === 'denied') {
      toast.error('浏览器通知被拒绝, 可在浏览器站点设置中重新允许.')
    }
  } catch {
    notificationPermission.value = Notification.permission
    toast.error('浏览器通知授权失败, 请在浏览器设置中手动允许.')
  }
  return notificationPermission.value
}

export function useNotificationPermission() {
  return {
    supported: notificationSupported,
    permission: notificationPermission,
    refresh: refreshNotificationPermission,
    request: requestNotificationPermission,
  }
}
