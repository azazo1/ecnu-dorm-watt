<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ApiError, api } from '@/api/client'
import type { Room, RoomInfo, SessionInfo, SettingsUpdate } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import EmptyState from '@/components/EmptyState.vue'
import ErrorState from '@/components/ErrorState.vue'
import LoginForm from '@/components/LoginForm.vue'
import RoomCascader from '@/components/RoomCascader.vue'
import SettingsSection from '@/components/SettingsSection.vue'
import Skeleton from '@/components/Skeleton.vue'
import type { RoomSelection } from '@/components/room-selection'
import { useNotificationPermission } from '@/composables/notification-permission'
import { useToast } from '@/composables/useToast'
import { useNotificationPreferences } from '@/stores/preferences'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { useTheme, type ThemeMode } from '@/stores/theme'
import { toUiError, type UiError } from '@/utils/errors'
import { formatDateTime, isValidTimeZone } from '@/utils/time'

const route = useRoute()
const settings = useSettingsStore()
const status = useStatusStore()
const toast = useToast()
const theme = useTheme()
const themeMode = theme.mode
const localStatus = status.local
const remoteStatus = status.remote
const health = status.health
const { notifyLow, notifyRise, setNotifyLow, setNotifyRise } = useNotificationPreferences()
const {
  supported: notifySupported,
  permission: notificationPermission,
  refresh: refreshNotificationPermission,
  request: requestNotifyPermission,
} = useNotificationPermission()

const config = computed(() => settings.state.data)
const storeError = computed(() => settings.state.error)
const timezone = settings.timezone
const version = __APP_VERSION__

const serverBaseUrl = ref('')
const token = ref('')
const clearToken = ref(false)
const serverSaving = ref(false)
const serverError = ref<UiError | null>(null)

const timezoneInput = ref('')
const refreshInterval = ref('5s')
const thresholdInput = ref('')
const nativeNotify = ref(true)
const displaySaving = ref(false)
const displayError = ref<UiError | null>(null)

const session = ref<SessionInfo | null>(null)
const sessionLoading = ref(false)
const sessionError = ref<UiError | null>(null)
const sessionClearOpen = ref(false)
const sessionClearBusy = ref(false)
const sessionClearError = ref<UiError | null>(null)

const activeRoom = ref<Room | null>(null)
const roomInfo = ref<RoomInfo | null>(null)
const roomLoading = ref(false)
const roomError = ref<UiError | null>(null)
const roomBusy = ref(false)
const roomActivateError = ref<UiError | null>(null)
const roomClearOpen = ref(false)
const roomClearBusy = ref(false)
const roomClearError = ref<UiError | null>(null)

const shutdownOpen = ref(false)
const shutdownBusy = ref(false)
const shutdownError = ref<UiError | null>(null)
const shutdownRequested = ref(false)

let sessionController: AbortController | null = null
let roomController: AbortController | null = null

const timezoneOptions = [
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Asia/Singapore',
  'Europe/London',
  'Europe/Berlin',
  'America/New_York',
  'America/Los_Angeles',
  'UTC',
]

const refreshOptions = computed(() => {
  const values = ['3s', '5s', '10s', '30s']
  if (!values.includes(refreshInterval.value)) {
    values.push(refreshInterval.value)
  }
  return values
})

const baseUrlError = computed(() => {
  const value = serverBaseUrl.value.trim()
  if (!value) {
    return '请填写服务端 base URL.'
  }
  try {
    const parsed = new URL(value)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
      return '只允许 http 或 https 地址.'
    }
    if (parsed.search || parsed.hash) {
      return '地址不能包含 query 或 fragment.'
    }
    if (parsed.username || parsed.password) {
      return '地址不能包含用户名或密码.'
    }
  } catch {
    return 'URL 格式不正确.'
  }
  return ''
})

const serverDirty = computed(() => {
  if (!config.value) {
    return false
  }
  return (
    serverBaseUrl.value.trim() !== config.value.server.base_url ||
    token.value.length > 0 ||
    clearToken.value
  )
})

const tokenPrefix = computed(() => config.value?.server.token_prefix ?? null)
const requestTimeout = computed(() => config.value?.server.request_timeout ?? null)

const timezoneError = computed(() => {
  const value = timezoneInput.value.trim()
  if (!value) {
    return '请填写 IANA 时区名称.'
  }
  if (!isValidTimeZone(value)) {
    return '时区名称无效, 例如 Asia/Shanghai.'
  }
  return ''
})

const thresholdError = computed(() => {
  if (thresholdInput.value.trim() === '') {
    return '请填写低电量阈值.'
  }
  const value = Number(thresholdInput.value)
  if (!Number.isFinite(value)) {
    return '阈值必须是数字.'
  }
  if (value < 0 || value > 100000) {
    return '阈值必须在 0 到 100000 之间.'
  }
  return ''
})

const displayDirty = computed(() => {
  if (!config.value) {
    return false
  }
  return (
    timezoneInput.value.trim() !== config.value.ui.timezone ||
    refreshInterval.value !== config.value.ui.refresh_interval ||
    Number(thresholdInput.value) !== config.value.monitor.degree_threshold ||
    nativeNotify.value !== config.value.local.notify
  )
})

const serverSaveDisabled = computed(
  () => serverSaving.value || !serverDirty.value || Boolean(baseUrlError.value),
)
const displaySaveDisabled = computed(
  () =>
    displaySaving.value ||
    !displayDirty.value ||
    Boolean(timezoneError.value) ||
    Boolean(thresholdError.value),
)

const displayTimeZone = computed(() =>
  isValidTimeZone(timezoneInput.value.trim()) ? timezoneInput.value.trim() : timezone.value,
)
const timezonePreview = computed(() => formatDateTime(Date.now(), displayTimeZone.value))

const sessionStateLabel = computed(() => {
  const state = session.value?.state
  if (state === 'ok') {
    return '有效'
  }
  if (state === 'invalid') {
    return '已失效'
  }
  if (state === 'unknown') {
    return '待验证'
  }
  if (state === 'missing') {
    return '从未上传'
  }
  return '未知'
})
const sessionStateTone = computed(() => {
  const state = session.value?.state
  if (state === 'ok') {
    return 'tag-ok'
  }
  if (state === 'invalid') {
    return 'tag-danger'
  }
  if (state === 'unknown') {
    return 'tag-warn'
  }
  return ''
})

const roomPath = computed(() => {
  const info = roomInfo.value
  if (!info) {
    return ''
  }
  return [
    info.area?.area_name,
    info.district?.district_name,
    info.building?.building_name,
    info.floor?.floor_name ? `${info.floor.floor_name} 层` : null,
    info.room?.room_name,
  ]
    .filter(Boolean)
    .join(' / ')
})

const clientVersion = computed(() => localStatus.value?.version ?? '-')
const serverVersion = computed(() => remoteStatus.value?.version ?? '-')
const upstreamBase = computed(
  () => localStatus.value?.upstream_base ?? config.value?.server.base_url ?? '-',
)
const uptimeText = computed(() => formatUptime(localStatus.value?.uptime_seconds))
const sampleCount = computed(() => remoteStatus.value?.storage.samples ?? null)
const archiveCount = computed(() => remoteStatus.value?.storage.archives ?? null)
const healthLabel = computed(() => {
  const labels: Record<string, string> = {
    starting: '启动中',
    ok: '正常',
    no_room: '未选择房间',
    not_logged_in: '登录已失效',
    server_down: '服务端不可达',
    no_net: '网络已断开',
    tls_error: 'TLS 校验失败',
    upstream_error: '上游查询失败',
    unknown: '状态未知',
  }
  return labels[health.value] ?? health.value
})

const permissionLabel = computed(() => {
  if (!notifySupported) {
    return '当前浏览器不支持通知'
  }
  if (notificationPermission.value === 'granted') {
    return '已授权'
  }
  if (notificationPermission.value === 'denied') {
    return '已拒绝'
  }
  return '未请求'
})

function syncForm(): void {
  const value = config.value
  if (!value) {
    return
  }
  serverBaseUrl.value = value.server.base_url
  token.value = ''
  clearToken.value = false
  timezoneInput.value = value.ui.timezone
  refreshInterval.value = value.ui.refresh_interval
  thresholdInput.value = String(value.monitor.degree_threshold)
  nativeNotify.value = value.local.notify
}

watch(config, syncForm, { immediate: true })

async function saveServer(): Promise<void> {
  if (!config.value || serverSaveDisabled.value) {
    return
  }
  const payload: SettingsUpdate = { server: {} }
  if (serverBaseUrl.value.trim() !== config.value.server.base_url) {
    payload.server!.base_url = serverBaseUrl.value.trim()
  }
  if (token.value) {
    payload.server!.token = token.value
  }
  if (clearToken.value) {
    payload.server!.token = null
  }
  serverSaving.value = true
  serverError.value = null
  try {
    const updated = await api.updateSettings(payload)
    settings.setSettings(updated)
    token.value = ''
    clearToken.value = false
    if (updated.restart_required) {
      toast.info('连接设置已保存, 需要重启守护进程后生效.')
    } else {
      toast.success('服务端连接设置已保存, 正在重新检查连接.')
    }
    await Promise.all([settings.loadSettings(true), status.refreshStatus()])
  } catch (caught) {
    serverError.value = toUiError(caught)
  } finally {
    serverSaving.value = false
  }
}

function toggleClearToken(): void {
  clearToken.value = !clearToken.value
  if (clearToken.value) {
    token.value = ''
  }
}

async function saveDisplay(): Promise<void> {
  if (!config.value || displaySaveDisabled.value) {
    return
  }
  const payload: SettingsUpdate = {
    local: { notify: nativeNotify.value },
    monitor: { degree_threshold: Number(thresholdInput.value) },
    ui: {
      timezone: timezoneInput.value.trim(),
      refresh_interval: refreshInterval.value,
    },
  }
  displaySaving.value = true
  displayError.value = null
  try {
    const updated = await api.updateSettings(payload)
    settings.setSettings(updated)
    toast.success('显示与阈值设置已保存.')
    await Promise.all([settings.loadSettings(true), status.refreshStatus()])
  } catch (caught) {
    displayError.value = toUiError(caught)
  } finally {
    displaySaving.value = false
  }
}

async function loadSession(): Promise<void> {
  sessionController?.abort()
  sessionController = new AbortController()
  sessionLoading.value = true
  sessionError.value = null
  try {
    session.value = await api.getSession(sessionController.signal)
  } catch (caught) {
    if (caught instanceof DOMException && caught.name === 'AbortError') {
      return
    }
    sessionError.value = toUiError(caught)
    const fallback = remoteStatus.value?.session
    if (fallback) {
      session.value = fallback
    }
  } finally {
    sessionLoading.value = false
  }
}

async function onLoginSuccess(): Promise<void> {
  await Promise.all([loadSession(), status.refreshStatus()])
}

async function clearSession(): Promise<void> {
  if (sessionClearBusy.value) {
    return
  }
  sessionClearBusy.value = true
  sessionClearError.value = null
  try {
    await api.clearSession()
    toast.success('会话已清除.')
    sessionClearOpen.value = false
    await Promise.all([loadSession(), status.refreshStatus()])
  } catch (caught) {
    sessionClearError.value = toUiError(caught)
  } finally {
    sessionClearBusy.value = false
  }
}

async function loadActiveRoom(): Promise<void> {
  roomController?.abort()
  roomController = new AbortController()
  roomLoading.value = true
  roomError.value = null
  try {
    activeRoom.value = await api.getActiveRoom(roomController.signal)
    roomInfo.value = null
    if (activeRoom.value) {
      try {
        roomInfo.value = await api.getRoomInfo(activeRoom.value.id)
      } catch {
        roomInfo.value = null
      }
    }
  } catch (caught) {
    if (caught instanceof DOMException && caught.name === 'AbortError') {
      return
    }
    if (caught instanceof ApiError && caught.code === 'room_not_configured') {
      activeRoom.value = null
      roomInfo.value = null
      roomError.value = null
      return
    }
    roomError.value = toUiError(caught)
  } finally {
    roomLoading.value = false
  }
}

async function activateRoom(selection: RoomSelection): Promise<void> {
  if (roomBusy.value) {
    return
  }
  roomBusy.value = true
  roomActivateError.value = null
  try {
    const room = await api.createRoom({
      room_no: selection.room_no,
      elcarea: selection.elcarea,
      elcbuis: selection.elcbuis,
      activate: false,
    })
    await api.setActiveRoom(room.id)
    toast.success(`活动房间已切换为 ${selection.building_name} ${selection.room_name}.`)
    await Promise.all([loadActiveRoom(), status.refreshStatus()])
  } catch (caught) {
    roomActivateError.value = toUiError(caught)
  } finally {
    roomBusy.value = false
  }
}

async function clearActiveRoom(): Promise<void> {
  if (roomClearBusy.value) {
    return
  }
  roomClearBusy.value = true
  roomClearError.value = null
  try {
    await api.clearActiveRoom()
    toast.success('活动房间已清除, 采集循环将暂停.')
    roomClearOpen.value = false
    activeRoom.value = null
    roomInfo.value = null
    await Promise.all([loadActiveRoom(), status.refreshStatus()])
  } catch (caught) {
    roomClearError.value = toUiError(caught)
  } finally {
    roomClearBusy.value = false
  }
}

function onThemeChange(event: Event): void {
  const target = event.target as HTMLSelectElement
  theme.setTheme(target.value as ThemeMode)
}

async function askNotificationPermission(): Promise<void> {
  await requestNotifyPermission()
}

function toggleNotifyLow(): void {
  setNotifyLow(!notifyLow.value)
}

function toggleNotifyRise(): void {
  setNotifyRise(!notifyRise.value)
}

function formatUptime(seconds: number | undefined): string {
  if (!seconds || seconds < 0) {
    return '-'
  }
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) {
    return `${days} 天 ${hours} 小时`
  }
  if (hours > 0) {
    return `${hours} 小时 ${minutes} 分钟`
  }
  return `${minutes} 分钟`
}

async function confirmShutdown(): Promise<void> {
  if (shutdownBusy.value) {
    return
  }
  shutdownBusy.value = true
  shutdownError.value = null
  try {
    await api.shutdown()
    shutdownRequested.value = true
    shutdownOpen.value = false
    toast.info('守护进程正在退出, 页面将在几秒后失去连接.')
  } catch (caught) {
    shutdownError.value = toUiError(caught)
  } finally {
    shutdownBusy.value = false
  }
}

async function scrollToHash(): Promise<void> {
  if (!route.hash) {
    return
  }
  await nextTick()
  const target = document.querySelector(route.hash)
  target?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

watch(
  () => route.hash,
  () => {
    void scrollToHash()
  },
)

onMounted(async () => {
  refreshNotificationPermission()
  await settings.loadSettings(true)
  await Promise.all([loadSession(), loadActiveRoom()])
  await scrollToHash()
})

onBeforeUnmount(() => {
  sessionController?.abort()
  roomController?.abort()
})
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div class="page-heading">
        <h1 class="page-title">设置</h1>
        <p class="page-subtitle">管理服务端连接, 会话, 房间, 显示与通知.</p>
      </div>
      <div class="page-actions">
        <RouterLink class="btn" to="/login-help">登录帮助</RouterLink>
      </div>
    </header>

    <Skeleton v-if="settings.state.loading && !config" variant="list" :rows="4" />
    <ErrorState
      v-else-if="storeError && !config"
      :error="storeError"
      @retry="settings.loadSettings(true)"
    />
    <EmptyState
      v-else-if="!config"
      icon="settings"
      title="没有可用的设置"
      description="守护进程没有返回设置内容, 可以重试读取."
      action-label="重新读取"
      @action="settings.loadSettings(true)"
    />

    <template v-else>
      <div v-if="storeError" class="alert alert-warn" role="status">
        <div class="alert-body">
          <p class="alert-title">设置刷新失败</p>
          <p class="alert-text">{{ storeError.detail }}</p>
          <div class="alert-actions">
            <button type="button" class="btn btn-sm" @click="settings.loadSettings(true)">
              重试
            </button>
          </div>
        </div>
      </div>

      <SettingsSection
        id="server"
        title="服务端连接"
        description="保存后立即重建上游客户端并检查连接."
        :dirty="serverDirty"
        :saving="serverSaving"
        :error="serverError"
      >
        <div class="field">
          <label for="server-base-url">base URL</label>
          <input
            id="server-base-url"
            v-model="serverBaseUrl"
            class="input"
            type="url"
            inputmode="url"
            autocomplete="off"
            placeholder="https://example.com/dormwatt/api"
            :disabled="serverSaving"
          />
          <p v-if="baseUrlError" class="field-error">{{ baseUrlError }}</p>
          <p v-else class="field-hint">必须使用 http 或 https, 不能包含 query 或 fragment.</p>
        </div>

        <div class="field">
          <label for="server-token">服务端 token</label>
          <input
            id="server-token"
            v-model="token"
            class="input"
            type="password"
            autocomplete="new-password"
            spellcheck="false"
            placeholder="留空表示不修改"
            :disabled="serverSaving || clearToken"
          />
          <p class="field-hint">
            当前状态:
            <span v-if="config.server.token_set">
              已设置{{ tokenPrefix ? `, 前缀 ${tokenPrefix}` : '' }}
            </span>
            <span v-else>未设置</span>
            <span v-if="requestTimeout"> / 请求超时 {{ requestTimeout }}</span>
          </p>
          <div v-if="config.server.token_set" class="form-row">
            <button type="button" class="btn btn-sm" :disabled="serverSaving" @click="toggleClearToken">
              {{ clearToken ? '取消清除' : '清除 token' }}
            </button>
            <span v-if="clearToken" class="text-danger small">保存后将清除服务端 token.</span>
          </div>
        </div>

        <template #actions>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="serverSaveDisabled"
            @click="saveServer"
          >
            {{ serverSaving ? '保存中...' : '保存并检查连接' }}
          </button>
          <span class="field-hint">token 不会回显, 也不会写入 localStorage.</span>
        </template>
      </SettingsSection>

      <SettingsSection
        id="login"
        title="ECNU 登录"
        description="会话值只发送到本地守护进程, 由守护进程转发."
        :show-status="false"
      >
        <div class="session-summary">
          <span class="tag" :class="sessionStateTone">{{ sessionStateLabel }}</span>
          <span v-if="session?.updated_at" class="small muted">
            更新于 {{ formatDateTime(session.updated_at, displayTimeZone) }}
          </span>
          <span v-if="session?.last_checked_at" class="small muted">
            最近检查 {{ formatDateTime(session.last_checked_at, displayTimeZone) }}
          </span>
          <button
            type="button"
            class="btn btn-sm btn-ghost text-danger"
            :disabled="!session || session.state === 'missing'"
            @click="sessionClearOpen = true"
          >
            清除会话
          </button>
        </div>

        <Skeleton v-if="sessionLoading" variant="text" :rows="2" />
        <ErrorState
          v-else-if="sessionError"
          :error="sessionError"
          compact
          @retry="loadSession"
        />

        <LoginForm @success="onLoginSuccess" />
      </SettingsSection>

      <SettingsSection
        id="room"
        title="房间"
        description="当前活动房间决定采集目标. 切换房间不会删除旧房间数据."
        :show-status="false"
      >
        <Skeleton v-if="roomLoading && !activeRoom" variant="text" :rows="2" />
        <ErrorState v-else-if="roomError" :error="roomError" compact @retry="loadActiveRoom" />
        <div v-else-if="activeRoom" class="room-summary">
          <div class="row-between">
            <div class="stack-sm">
              <span class="small muted">当前活动房间</span>
              <strong class="room-number">{{ activeRoom.room_no }}</strong>
              <span v-if="roomPath" class="small muted">{{ roomPath }}</span>
            </div>
            <button type="button" class="btn btn-sm text-danger" @click="roomClearOpen = true">
              清除房间
            </button>
          </div>
          <dl class="room-meta">
            <div>
              <dt>房间 ID</dt>
              <dd class="tabular">{{ activeRoom.id }}</dd>
            </div>
            <div>
              <dt>样本数</dt>
              <dd class="tabular">{{ activeRoom.sample_count }}</dd>
            </div>
            <div>
              <dt>最近采样</dt>
              <dd class="tabular">
                {{ formatDateTime(activeRoom.last_sample_at, displayTimeZone) }}
              </dd>
            </div>
          </dl>
        </div>
        <EmptyState
          v-else
          icon="room"
          title="未选择活动房间"
          description="从下面的级联目录选择一个房间, 采集循环才会启动."
        />

        <div class="divider" />

        <RoomCascader :busy="roomBusy" @select="activateRoom" />

        <div v-if="roomActivateError" class="form-error" role="alert">
          <strong>{{ roomActivateError.title }}</strong>
          <span>{{ roomActivateError.detail }}</span>
          <span>{{ roomActivateError.advice }}</span>
          <span class="small">错误码: <code>{{ roomActivateError.code }}</code></span>
        </div>
      </SettingsSection>

      <SettingsSection
        id="display"
        title="显示与通知"
        description="时区与阈值会立即生效. 主题和通知开关只保存在本浏览器."
        :dirty="displayDirty"
        :saving="displaySaving"
        :error="displayError"
      >
        <div class="form-grid">
          <div class="field">
            <label for="display-timezone">时区</label>
            <input
              id="display-timezone"
              v-model="timezoneInput"
              class="input"
              type="text"
              list="timezone-options"
              autocomplete="off"
              spellcheck="false"
              :disabled="displaySaving"
            />
            <datalist id="timezone-options">
              <option v-for="zone in timezoneOptions" :key="zone" :value="zone" />
            </datalist>
            <p v-if="timezoneError" class="field-error">{{ timezoneError }}</p>
            <p v-else class="field-hint">当前时间: {{ timezonePreview }}</p>
          </div>

          <div class="field">
            <label for="display-refresh">刷新间隔</label>
            <select
              id="display-refresh"
              v-model="refreshInterval"
              class="select"
              :disabled="displaySaving"
            >
              <option v-for="value in refreshOptions" :key="value" :value="value">
                {{ value }}
              </option>
            </select>
            <p class="field-hint">页面轮询本地状态与自动重试的间隔.</p>
          </div>

          <div class="field">
            <label for="display-threshold">低电量阈值</label>
            <input
              id="display-threshold"
              v-model="thresholdInput"
              class="input"
              type="number"
              min="0"
              max="100000"
              step="0.01"
              inputmode="decimal"
              :disabled="displaySaving"
            />
            <p v-if="thresholdError" class="field-error">{{ thresholdError }}</p>
            <p v-else class="field-hint">单位度, 必须在 0 到 100000 之间.</p>
          </div>
        </div>

        <div class="divider" />

        <div class="setting-row">
          <div>
            <span class="setting-label">主题</span>
            <p class="field-hint">默认跟随系统, 也可以固定为浅色或深色.</p>
          </div>
          <select class="select setting-control" :value="themeMode" @change="onThemeChange">
            <option value="auto">跟随系统</option>
            <option value="light">浅色</option>
            <option value="dark">深色</option>
          </select>
        </div>

        <div class="setting-row">
          <div>
            <span class="setting-label">系统通知</span>
            <p class="field-hint">
              守护进程在状态异常或低电量时调用系统通知中心, 页面关闭后仍然生效.
            </p>
          </div>
          <button
            type="button"
            class="switch-row setting-control"
            role="switch"
            :aria-checked="nativeNotify"
            @click="nativeNotify = !nativeNotify"
          >
            <span class="switch-track" :data-on="nativeNotify"><span class="switch-thumb" /></span>
            <span class="field-hint">{{ nativeNotify ? '已开启' : '已关闭' }}</span>
          </button>
        </div>

        <div class="setting-row">
          <div>
            <span class="setting-label">浏览器通知</span>
            <p class="field-hint">
              状态: {{ permissionLabel }}. 低电量与回升通知会按 30 分钟节流.
            </p>
            <p v-if="!notifySupported" class="field-error">当前浏览器不支持 Notification API.</p>
            <p v-else-if="notificationPermission === 'denied'" class="field-error">
              通知已被拒绝, 请在浏览器站点设置中重新允许.
            </p>
          </div>
          <button
            v-if="notifySupported && notificationPermission !== 'granted'"
            type="button"
            class="btn setting-control"
            @click="askNotificationPermission"
          >
            开启浏览器通知
          </button>
        </div>

        <div class="notify-grid">
          <button
            type="button"
            class="switch-row"
            role="switch"
            :aria-checked="notifyLow"
            :disabled="notificationPermission !== 'granted'"
            @click="toggleNotifyLow"
          >
            <span class="switch-track" :data-on="notifyLow"><span class="switch-thumb" /></span>
            <span>
              <strong>低电量通知</strong>
              <span class="field-hint">当前电量低于阈值时提醒一次.</span>
            </span>
          </button>

          <button
            type="button"
            class="switch-row"
            role="switch"
            :aria-checked="notifyRise"
            :disabled="notificationPermission !== 'granted'"
            @click="toggleNotifyRise"
          >
            <span class="switch-track" :data-on="notifyRise"><span class="switch-thumb" /></span>
            <span>
              <strong>电量回升通知</strong>
              <span class="field-hint">检测到电量增加时提醒.</span>
            </span>
          </button>
        </div>

        <template #actions>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="displaySaveDisabled"
            @click="saveDisplay"
          >
            {{ displaySaving ? '保存中...' : '保存显示设置' }}
          </button>
          <span class="field-hint">通知开关与主题不需要保存到服务端.</span>
        </template>
      </SettingsSection>

      <SettingsSection
        id="about"
        title="关于"
        description="版本与运行状态来自本地守护进程."
        :show-status="false"
      >
        <dl class="about-grid">
          <div>
            <dt>Web 版本</dt>
            <dd class="tabular">{{ version }}</dd>
          </div>
          <div>
            <dt>客户端版本</dt>
            <dd class="tabular">{{ clientVersion }}</dd>
          </div>
          <div>
            <dt>服务端版本</dt>
            <dd class="tabular">{{ serverVersion }}</dd>
          </div>
          <div>
            <dt>客户端运行时长</dt>
            <dd class="tabular">{{ uptimeText }}</dd>
          </div>
          <div>
            <dt>当前健康状态</dt>
            <dd>{{ healthLabel }}</dd>
          </div>
          <div class="about-wide">
            <dt>上游地址</dt>
            <dd class="break-anywhere">{{ upstreamBase }}</dd>
          </div>
          <div>
            <dt>样本数</dt>
            <dd class="tabular">{{ sampleCount ?? '-' }}</dd>
          </div>
          <div>
            <dt>归档数</dt>
            <dd class="tabular">{{ archiveCount ?? '-' }}</dd>
          </div>
        </dl>

        <p class="field-hint">
          配置与日志路径请运行 <code>dormwatt client doctor</code> 查看.
          托盘状态由桌面守护进程管理, 当前 API 未暴露.
        </p>

        <div v-if="shutdownRequested" class="alert alert-warn" role="status">
          <div class="alert-body">
            <p class="alert-title">守护进程正在退出</p>
            <p class="alert-text">页面随后会失去连接. 需要时请重新启动 dormwatt client serve.</p>
          </div>
        </div>
        <div v-else class="form-actions">
          <button type="button" class="btn btn-danger" @click="shutdownOpen = true">
            <AppIcon name="trash" :size="16" />
            退出守护进程
          </button>
          <span class="field-hint">退出后采集和本地页面都会停止, 需要手动重启.</span>
        </div>
      </SettingsSection>
    </template>

    <ConfirmDialog
      v-model="sessionClearOpen"
      title="清除 ECNU 会话"
      message="清除后采集循环会暂停, 直到重新登录. 历史样本与归档不会删除."
      confirm-label="清除会话"
      danger
      :busy="sessionClearBusy"
      @confirm="clearSession"
    >
      <div v-if="sessionClearError" class="form-error" role="alert">
        <strong>{{ sessionClearError.title }}</strong>
        <span>{{ sessionClearError.detail }}</span>
        <span>{{ sessionClearError.advice }}</span>
        <span class="small">错误码: <code>{{ sessionClearError.code }}</code></span>
      </div>
    </ConfirmDialog>

    <ConfirmDialog
      v-model="roomClearOpen"
      title="清除活动房间"
      message="清除后采集循环会暂停, 历史数据仍保留. 之后可以重新选择房间."
      confirm-label="清除房间"
      danger
      :busy="roomClearBusy"
      @confirm="clearActiveRoom"
    >
      <div v-if="roomClearError" class="form-error" role="alert">
        <strong>{{ roomClearError.title }}</strong>
        <span>{{ roomClearError.detail }}</span>
        <span>{{ roomClearError.advice }}</span>
        <span class="small">错误码: <code>{{ roomClearError.code }}</code></span>
      </div>
    </ConfirmDialog>

    <ConfirmDialog
      v-model="shutdownOpen"
      title="退出守护进程"
      message="退出后本地 API 与采集循环会停止, 页面将无法继续刷新. 需要重新运行 dormwatt client serve 才能恢复."
      confirm-label="退出守护进程"
      require-text="退出"
      danger
      :busy="shutdownBusy"
      @confirm="confirmShutdown"
    >
      <div v-if="shutdownError" class="form-error" role="alert">
        <strong>{{ shutdownError.title }}</strong>
        <span>{{ shutdownError.detail }}</span>
        <span>{{ shutdownError.advice }}</span>
        <span class="small">错误码: <code>{{ shutdownError.code }}</code></span>
      </div>
    </ConfirmDialog>
  </div>
</template>

<style scoped>
.session-summary {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-soft);
}

.room-summary {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-soft);
}

.room-number {
  font-size: 16px;
  overflow-wrap: anywhere;
}

.room-meta {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin: 0;
}

.room-meta div,
.about-grid div {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.room-meta dt,
.about-grid dt {
  color: var(--text-muted);
  font-size: 12px;
}

.room-meta dd,
.about-grid dd {
  margin: 0;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.setting-label {
  font-size: 14px;
  font-weight: 600;
}

.setting-control {
  min-width: 180px;
}

.notify-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.switch-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text);
  text-align: left;
}

.switch-row:disabled {
  opacity: 0.6;
}

.switch-row > span:last-child {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.switch-track {
  position: relative;
  display: inline-block;
  width: 36px;
  height: 20px;
  flex: 0 0 auto;
  margin-top: 2px;
  border-radius: 999px;
  background: var(--surface-strong);
  transition: background-color 0.16s ease;
}

.switch-track[data-on="true"] {
  background: var(--accent);
}

.switch-thumb {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: #ffffff;
  transition: transform 0.16s ease;
}

.switch-track[data-on="true"] .switch-thumb {
  transform: translateX(16px);
}

.about-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin: 0;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-soft);
}

.about-wide {
  grid-column: span 2;
}

@media (max-width: 900px) {
  .about-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .about-wide {
    grid-column: span 2;
  }

  .room-meta {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .notify-grid,
  .about-grid,
  .room-meta {
    grid-template-columns: minmax(0, 1fr);
  }

  .about-wide {
    grid-column: auto;
  }

  .setting-control {
    width: 100%;
  }
}
</style>
