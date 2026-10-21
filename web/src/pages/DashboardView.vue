<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { RecordSample } from '@/api/types'
import BaseModal from '@/components/BaseModal.vue'
import DataTable from '@/components/DataTable.vue'
import DegreeCard from '@/components/DegreeCard.vue'
import EmptyState from '@/components/EmptyState.vue'
import RoomCascader from '@/components/RoomCascader.vue'
import Skeleton from '@/components/Skeleton.vue'
import type { RoomSelection } from '@/components/room-selection'
import { useToast } from '@/composables/useToast'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { toUiError, type UiError } from '@/utils/errors'
import { presetRange } from '@/utils/range'
import { computeRangeStats, degreeChange } from '@/utils/stats'
import {
  formatDateTime,
  formatDays,
  formatNumber,
  parseTime,
  startOfDayMs,
} from '@/utils/time'

const UsageChart = defineAsyncComponent({
  loader: () => import('@/components/UsageChart.vue'),
  loadingComponent: Skeleton,
  delay: 120,
})

const status = useStatusStore()
const settings = useSettingsStore()
const toast = useToast()

const timezone = settings.timezone
const health = status.health
const remote = status.remote
const local = status.local
const activeRoom = status.activeRoom
const statusError = computed(() => status.state.error)

const records = ref<RecordSample[]>([])
const recordsLoading = ref(false)
const recordsError = ref<UiError | null>(null)
const freshDegree = ref<number | null>(null)
const freshSampledAt = ref<string | null>(null)
const freshLoading = ref(false)
const freshError = ref<UiError | null>(null)
const roomOpen = ref(false)
const roomBusy = ref(false)
const roomError = ref<UiError | null>(null)
const now = ref(Date.now())

let recordsController: AbortController | null = null
let recordsSequence = 0
let clockTimer: number | null = null

const degree = computed(
  () => freshDegree.value ?? remote.value?.monitor.last_degree ?? null,
)
const sampledAt = computed(
  () =>
    freshSampledAt.value ??
    remote.value?.monitor.last_success_at ??
    records.value[records.value.length - 1]?.time ??
    null,
)
const nextPollAt = computed(() => remote.value?.monitor.next_poll_at ?? null)
const pollIntervalSeconds = computed(
  () => remote.value?.monitor.poll_interval_seconds ?? 300,
)
const threshold = computed(() => settings.state.data?.monitor.degree_threshold ?? 0)
const lastMonitorError = computed(() => remote.value?.monitor.last_error ?? null)
const healthDetail = computed(() => local.value?.health_detail ?? null)
const lowBattery = computed(
  () => degree.value !== null && threshold.value > 0 && degree.value < threshold.value,
)
const stale = computed(() => {
  const time = parseTime(sampledAt.value)
  if (time === null) {
    return false
  }
  return now.value - time > pollIntervalSeconds.value * 2 * 1000
})
const trend = computed(() => degreeChange(records.value))

const todayRange = computed(() => {
  const from = startOfDayMs(now.value, timezone.value)
  return { from, to: now.value }
})
const todayStats = computed(() =>
  computeRangeStats(records.value, todayRange.value.from, todayRange.value.to, degree.value),
)
const weekRange = computed(() => {
  const range = presetRange('7d', now.value)
  return {
    from: range.from ? Date.parse(range.from) : now.value - 7 * 86400000,
    to: now.value,
  }
})
const weekStats = computed(() =>
  computeRangeStats(records.value, weekRange.value.from, weekRange.value.to, degree.value),
)
const todayUsage = computed(() => (todayStats.value.samples > 0 ? todayStats.value.usage : null))
const weekAverage = computed(() => weekStats.value.avgPerDay)
const estimatedDays = computed(() => weekStats.value.estimatedDays)

async function loadRecords(): Promise<void> {
  recordsController?.abort()
  recordsController = new AbortController()
  const current = ++recordsSequence
  recordsLoading.value = true
  recordsError.value = null
  const range = presetRange('7d', Date.now())
  try {
    const data = await api.getRecords(
      {
        from: range.from,
        to: range.to,
        limit: 200000,
      },
      recordsController.signal,
    )
    if (current !== recordsSequence) {
      return
    }
    records.value = data.records
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      return
    }
    if (current !== recordsSequence) {
      return
    }
    records.value = []
    recordsError.value = toUiError(error)
  } finally {
    if (current === recordsSequence) {
      recordsLoading.value = false
    }
  }
}

async function refreshDegree(): Promise<void> {
  if (freshLoading.value) {
    return
  }
  freshLoading.value = true
  freshError.value = null
  try {
    const data = await api.getDegree({ fresh: true })
    freshDegree.value = data.degree
    freshSampledAt.value = data.queried_at
    await status.refreshStatus()
    if (!status.state.error && status.remote.value?.monitor.last_degree !== null) {
      freshDegree.value = null
      freshSampledAt.value = null
    }
  } catch (error) {
    freshError.value = toUiError(error)
  } finally {
    freshLoading.value = false
  }
}

async function refreshAll(): Promise<void> {
  await Promise.all([loadRecords(), refreshDegree()])
}

async function activateRoom(selection: RoomSelection): Promise<void> {
  roomBusy.value = true
  roomError.value = null
  try {
    const room = await api.createRoom({
      room_no: selection.room_no,
      elcarea: selection.elcarea,
      elcbuis: selection.elcbuis,
      activate: false,
    })
    await api.setActiveRoom(room.id)
    toast.success(`活动房间已切换为 ${selection.building_name} ${selection.room_name}.`)
    roomOpen.value = false
    freshDegree.value = null
    freshSampledAt.value = null
    await status.refreshStatus()
    await loadRecords()
  } catch (error) {
    roomError.value = toUiError(error)
  } finally {
    roomBusy.value = false
  }
}

function openRoomPicker(): void {
  roomError.value = null
  roomOpen.value = true
}

watch(
  () => status.manualRefreshCount.value,
  () => {
    void refreshAll()
  },
)

onMounted(() => {
  void loadRecords()
  clockTimer = window.setInterval(() => {
    now.value = Date.now()
  }, 30000)
})

onBeforeUnmount(() => {
  recordsController?.abort()
  if (clockTimer !== null) {
    window.clearInterval(clockTimer)
  }
})
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div class="page-heading">
        <h1 class="page-title">仪表盘</h1>
        <p class="page-subtitle">当前宿舍电量, 健康状态与最近 7 天趋势.</p>
      </div>
      <div class="page-actions">
        <button type="button" class="btn" @click="openRoomPicker">选择房间</button>
        <RouterLink class="btn btn-ghost" to="/login-help">登录帮助</RouterLink>
        <button type="button" class="btn btn-primary" :disabled="freshLoading" @click="refreshAll">
          {{ freshLoading ? '刷新中...' : '立即刷新' }}
        </button>
      </div>
    </header>

    <div class="grid-2">
      <DegreeCard
        :degree="degree"
        :sampled-at="sampledAt"
        :time-zone="timezone"
        :next-poll-at="nextPollAt"
        :trend="trend"
        :loading="status.state.loading || freshLoading"
        :stale="stale"
        :error="degree === null ? (freshError ?? statusError) : null"
        @refresh="refreshAll"
      />

      <section class="card summary-card">
        <div class="card-header">
          <div>
            <h2 class="card-title">用量概览</h2>
            <p class="card-subtitle">按最近 7 天样本估算</p>
          </div>
          <span v-if="recordsLoading" class="tag">计算中</span>
        </div>
        <Skeleton v-if="recordsLoading && records.length === 0" variant="text" :rows="3" />
        <EmptyState
          v-else-if="records.length === 0"
          icon="records"
          title="等待第一次采样"
          description="采集到样本后, 这里会显示今日用量与预计可用天数."
          action-label="刷新"
          @action="refreshAll"
        />
        <div v-else class="summary-grid">
          <div class="summary-item">
            <span class="summary-label">今日用量</span>
            <span class="summary-value tabular">
              {{ todayUsage === null ? '-' : `${formatNumber(todayUsage)} 度` }}
            </span>
          </div>
          <div class="summary-item">
            <span class="summary-label">7 天日均</span>
            <span class="summary-value tabular">
              {{ weekAverage === null ? '-' : `${formatNumber(weekAverage)} 度` }}
            </span>
          </div>
          <div class="summary-item">
            <span class="summary-label">预计可用</span>
            <span class="summary-value tabular">
              {{ estimatedDays === null ? '-' : `${formatDays(estimatedDays)} 天` }}
            </span>
          </div>
          <div class="summary-item">
            <span class="summary-label">样本数</span>
            <span class="summary-value tabular">{{ records.length }}</span>
          </div>
        </div>
        <p v-if="todayUsage === null && records.length > 0" class="field-hint summary-note">
          今日还没有足够的样本计算用量.
        </p>
      </section>
    </div>

    <div v-if="health === 'no_room'" class="alert alert-info" role="status">
      <div class="alert-body">
        <p class="alert-title">尚未选择房间</p>
        <p class="alert-text">选择房间后, 守护进程才会开始采集电量.</p>
        <div class="alert-actions">
          <button type="button" class="btn btn-sm btn-primary" @click="openRoomPicker">
            选择房间
          </button>
        </div>
      </div>
    </div>

    <div v-else-if="health === 'not_logged_in'" class="alert alert-warn" role="status">
      <div class="alert-body">
        <p class="alert-title">ECNU 登录已失效</p>
        <p class="alert-text">
          在终端运行 <code>dormwatt client login</code> 完成学校 SSO,
          或在设置页手动填写会话信息.
        </p>
        <div class="alert-actions">
          <RouterLink class="btn btn-sm btn-primary" to="/login-help">如何登录</RouterLink>
          <RouterLink class="btn btn-sm" to="/settings#login">填写会话</RouterLink>
        </div>
      </div>
    </div>

    <div
      v-else-if="health === 'server_down' || health === 'no_net' || health === 'tls_error'"
      class="alert alert-danger"
      role="status"
    >
      <div class="alert-body">
        <p class="alert-title">
          {{ health === 'no_net' ? '网络已断开' : health === 'tls_error' ? 'TLS 校验失败' : '无法连接服务端' }}
        </p>
        <p class="alert-text">{{ healthDetail || '请检查服务端地址, 网络与代理配置.' }}</p>
        <div class="alert-actions">
          <button type="button" class="btn btn-sm" @click="status.manualRefresh">立即重试</button>
          <RouterLink class="btn btn-sm btn-ghost" to="/settings">连接设置</RouterLink>
        </div>
      </div>
    </div>

    <div v-else-if="health === 'upstream_error'" class="alert alert-warn" role="status">
      <div class="alert-body">
        <p class="alert-title">上游查询失败</p>
        <p class="alert-text">
          {{ lastMonitorError?.message || healthDetail || '学校接口返回异常, 稍后会自动重试.' }}
        </p>
        <div class="alert-actions">
          <button type="button" class="btn btn-sm" @click="status.manualRefresh">立即重试</button>
          <RouterLink class="btn btn-sm btn-ghost" to="/settings">查看设置</RouterLink>
        </div>
      </div>
    </div>

    <div v-else-if="health === 'unknown'" class="alert alert-warn" role="status">
      <div class="alert-body">
        <p class="alert-title">状态未知</p>
        <p class="alert-text">{{ healthDetail || '守护进程尚未就绪, 或本地配置不完整.' }}</p>
        <div class="alert-actions">
          <RouterLink class="btn btn-sm" to="/settings">检查设置</RouterLink>
        </div>
      </div>
    </div>

    <div v-if="lowBattery" class="alert alert-warn" role="status">
      <div class="alert-body">
        <p class="alert-title">电量低于阈值</p>
        <p class="alert-text">
          当前 {{ formatNumber(degree) }} 度, 低于阈值 {{ formatNumber(threshold) }} 度.
          请及时充值.
        </p>
      </div>
    </div>

    <div v-if="freshError && degree !== null" class="form-error" role="alert">
      <strong>{{ freshError.title }}</strong>
      <span>{{ freshError.detail }}</span>
      <span>{{ freshError.advice }}</span>
      <span class="small">错误码: <code>{{ freshError.code }}</code></span>
    </div>

    <section class="card">
      <div class="card-header">
        <div>
          <h2 class="card-title">最近 7 天剩余电量</h2>
          <p class="card-subtitle">
            最近采样 {{ formatDateTime(sampledAt, timezone) }}
            <span v-if="activeRoom"> / 房间 {{ activeRoom.room_no }}</span>
          </p>
        </div>
        <RouterLink class="btn btn-sm" to="/records">查看记录</RouterLink>
      </div>
      <UsageChart
        :records="records"
        :timezone="timezone"
        :loading="recordsLoading"
        :error="recordsError"
        @retry="loadRecords"
      />
    </section>

    <section class="card">
      <div class="card-header">
        <div>
          <h2 class="card-title">最近记录</h2>
          <p class="card-subtitle">最新 5 条采样</p>
        </div>
        <RouterLink class="btn btn-sm" to="/records">查看全部</RouterLink>
      </div>
      <DataTable
        :records="records.slice(-5)"
        :time-zone="timezone"
        :loading="recordsLoading"
        :error="recordsError"
        compact
        empty-title="等待第一次采样"
        empty-description="采集循环还没有写入样本."
        @retry="loadRecords"
      />
    </section>

    <BaseModal
      v-model="roomOpen"
      title="选择活动房间"
      size="lg"
      :busy="roomBusy"
      :close-on-backdrop="!roomBusy"
    >
      <div class="stack">
        <p class="field-hint">
          房间目录来自学校接口. 选择后会把该房间设为活动采集目标, 旧房间历史数据会保留.
        </p>
        <RoomCascader :busy="roomBusy" @select="activateRoom" />
        <div v-if="roomError" class="form-error" role="alert">
          <strong>{{ roomError.title }}</strong>
          <span>{{ roomError.detail }}</span>
          <span>{{ roomError.advice }}</span>
          <span class="small">错误码: <code>{{ roomError.code }}</code></span>
        </div>
      </div>
    </BaseModal>
  </div>
</template>

<style scoped>
.summary-card {
  min-height: 220px;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.summary-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}

.summary-label {
  color: var(--text-muted);
  font-size: 12px;
}

.summary-value {
  font-size: 18px;
  font-weight: 650;
}

.summary-note {
  margin-top: 10px;
}
</style>
