<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, recordsCsvUrl } from '@/api/client'
import type { ArchiveMeta, RecordSample, Room } from '@/api/types'
import ArchiveCreateDialog from '@/components/ArchiveCreateDialog.vue'
import DataTable from '@/components/DataTable.vue'
import RangePicker from '@/components/RangePicker.vue'
import Skeleton from '@/components/Skeleton.vue'
import StatsCard from '@/components/StatsCard.vue'
import AppIcon from '@/components/AppIcon.vue'
import { useToast } from '@/composables/useToast'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { toUiError, type UiError } from '@/utils/errors'
import {
  isRangePreset,
  presetRange,
  type RangeSelection,
} from '@/utils/range'
import { computeRangeStats } from '@/utils/stats'

const UsageChart = defineAsyncComponent({
  loader: () => import('@/components/UsageChart.vue'),
  loadingComponent: Skeleton,
  delay: 120,
})

type ViewMode = 'chart' | 'table'

const route = useRoute()
const router = useRouter()
const status = useStatusStore()
const settings = useSettingsStore()
const toast = useToast()

const timezone = settings.timezone
const currentDegree = computed(() => status.remote.value?.monitor.last_degree ?? null)

function queryString(value: unknown): string | null {
  if (typeof value === 'string' && value) {
    return value
  }
  if (Array.isArray(value) && typeof value[0] === 'string' && value[0]) {
    return value[0]
  }
  return null
}

function initialRange(): RangeSelection {
  const presetValue = queryString(route.query.preset)
  const from = queryString(route.query.from)
  const to = queryString(route.query.to)
  if (from || to) {
    return { preset: 'custom', from, to }
  }
  if (isRangePreset(presetValue) && presetValue !== 'custom') {
    return presetRange(presetValue)
  }
  return presetRange('7d')
}

function initialView(): ViewMode {
  return queryString(route.query.view) === 'table' ? 'table' : 'chart'
}

function initialRoom(): number | null {
  const value = queryString(route.query.room)
  if (!value) {
    return null
  }
  const parsed = Number(value)
  return Number.isInteger(parsed) ? parsed : null
}

const range = ref<RangeSelection>(initialRange())
const view = ref<ViewMode>(initialView())
const roomId = ref<number | null>(initialRoom())
const brushEnabled = ref(false)
const brushRange = ref<{ from: string; to: string } | null>(null)
const chartRef = ref<{ clearBrush: () => void } | null>(null)

const rooms = ref<Room[]>([])
const roomsLoading = ref(false)
const roomsError = ref<UiError | null>(null)

const records = ref<RecordSample[]>([])
const recordsLoading = ref(false)
const recordsError = ref<UiError | null>(null)
const visibleCount = ref(500)
const pageSize = 100

const archiveOpen = ref(false)
const archiveBusy = ref(false)
const archiveError = ref<UiError | null>(null)

let recordsController: AbortController | null = null
let recordsSequence = 0
let roomsController: AbortController | null = null

const stats = computed(() => {
  if (!brushRange.value) {
    return null
  }
  const from = Date.parse(brushRange.value.from)
  const to = Date.parse(brushRange.value.to)
  if (!Number.isFinite(from) || !Number.isFinite(to)) {
    return null
  }
  return computeRangeStats(records.value, Math.min(from, to), Math.max(from, to), currentDegree.value)
})

const tableRecords = computed(() => records.value.slice(-visibleCount.value))
const hasMore = computed(() => records.value.length > tableRecords.value.length)
const selectedRoomLabel = computed(() => {
  if (roomId.value !== null) {
    const match = rooms.value.find((room) => room.id === roomId.value)
    return match ? match.room_no : `房间 ${roomId.value}`
  }
  const active = status.activeRoom.value
  return active ? `${active.room_no} (活动房间)` : '当前活动房间'
})
const exportUrl = computed(() =>
  recordsCsvUrl({
    room_id: roomId.value,
    from: range.value.from,
    to: range.value.to,
    header: true,
  }),
)
const archiveRoomId = computed(() => roomId.value ?? status.activeRoom.value?.id ?? null)

function syncUrl(): void {
  const query: Record<string, string> = {
    preset: range.value.preset,
    view: view.value,
  }
  if (range.value.from) {
    query.from = range.value.from
  }
  if (range.value.to) {
    query.to = range.value.to
  }
  if (roomId.value !== null) {
    query.room = String(roomId.value)
  }
  void router.replace({ query })
}

async function loadRooms(): Promise<void> {
  roomsController?.abort()
  roomsController = new AbortController()
  roomsLoading.value = true
  roomsError.value = null
  try {
    const data = await api.getRooms(roomsController.signal)
    rooms.value = data.rooms
    if (roomId.value !== null && !rooms.value.some((room) => room.id === roomId.value)) {
      roomId.value = null
    }
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      return
    }
    roomsError.value = toUiError(error)
  } finally {
    roomsLoading.value = false
  }
}

async function loadRecords(): Promise<void> {
  recordsController?.abort()
  recordsController = new AbortController()
  const current = ++recordsSequence
  recordsLoading.value = true
  recordsError.value = null
  brushRange.value = null
  try {
    const data = await api.getRecords(
      {
        room_id: roomId.value,
        from: range.value.from,
        to: range.value.to,
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

function applyRange(next: RangeSelection): void {
  range.value = next
  visibleCount.value = 500
  brushRange.value = null
  chartRef.value?.clearBrush()
  syncUrl()
  void loadRecords()
}

function onRoomChange(event: Event): void {
  const target = event.target as HTMLSelectElement
  roomId.value = target.value ? Number(target.value) : null
  visibleCount.value = 500
  brushRange.value = null
  chartRef.value?.clearBrush()
  syncUrl()
  void loadRecords()
}

function setView(next: ViewMode): void {
  view.value = next
  syncUrl()
}

function toggleBrush(): void {
  brushEnabled.value = !brushEnabled.value
  if (!brushEnabled.value && brushRange.value) {
    brushRange.value = null
    chartRef.value?.clearBrush()
  }
}

function onSelection(selection: { from: string; to: string } | null): void {
  brushRange.value = selection
}

function loadMore(): void {
  visibleCount.value += 500
}

function openArchive(): void {
  if (!stats.value?.from || !stats.value.to) {
    return
  }
  archiveError.value = null
  archiveOpen.value = true
}

async function submitArchive(payload: { from: string; to: string; name: string }): Promise<void> {
  if (archiveBusy.value) {
    return
  }
  const targetRoom = archiveRoomId.value
  if (targetRoom === null) {
    archiveError.value = {
      code: 'room_not_configured',
      title: '未选择房间',
      detail: '当前没有活动房间, 无法创建归档.',
      advice: '先在仪表盘或设置页选择一个活动房间.',
      status: 0,
      requestId: null,
    }
    return
  }
  archiveBusy.value = true
  archiveError.value = null
  try {
    const created: ArchiveMeta = await api.createArchive({
      room_id: targetRoom,
      from: payload.from,
      to: payload.to,
      name: payload.name || undefined,
    })
    toast.success(`归档 ${created.name} 已创建, 共 ${created.records_num} 条样本.`)
    archiveOpen.value = false
    brushRange.value = null
    chartRef.value?.clearBrush()
    await loadRecords()
    await status.refreshStatus()
  } catch (error) {
    archiveError.value = toUiError(error)
  } finally {
    archiveBusy.value = false
  }
}

watch(
  () => status.manualRefreshCount.value,
  () => {
    void loadRecords()
  },
)

onMounted(() => {
  syncUrl()
  void loadRooms()
  void loadRecords()
})

onBeforeUnmount(() => {
  recordsController?.abort()
  roomsController?.abort()
})
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div class="page-heading">
        <h1 class="page-title">记录</h1>
        <p class="page-subtitle">按时间范围查看剩余电量, 每日用量与采样明细.</p>
      </div>
      <div class="page-actions">
        <a class="btn" :href="exportUrl" download="records.csv">
          <AppIcon name="download" :size="16" />
          导出 CSV
        </a>
      </div>
    </header>

    <section class="card toolbar">
      <RangePicker
        :model-value="range"
        :time-zone="timezone"
        :disabled="recordsLoading"
        @update:model-value="applyRange"
      />

      <div class="toolbar-row">
        <div class="field room-field">
          <label for="records-room">房间</label>
          <select
            id="records-room"
            class="select"
            :value="roomId ?? ''"
            :disabled="roomsLoading || recordsLoading"
            @change="onRoomChange"
          >
            <option value="">当前活动房间</option>
            <option v-for="room in rooms" :key="room.id" :value="room.id">
              {{ room.room_no }}{{ room.active ? ' (活动)' : '' }}
            </option>
          </select>
          <p v-if="roomsError" class="field-error">
            {{ roomsError.title }}
            <button type="button" class="btn btn-sm btn-ghost" @click="loadRooms">重试</button>
          </p>
        </div>

        <div class="view-toggle" role="group" aria-label="视图切换">
          <button
            type="button"
            class="btn btn-sm"
            :class="{ 'btn-primary': view === 'chart' }"
            :aria-pressed="view === 'chart'"
            @click="setView('chart')"
          >
            <AppIcon name="chart" :size="15" />
            图表
          </button>
          <button
            type="button"
            class="btn btn-sm"
            :class="{ 'btn-primary': view === 'table' }"
            :aria-pressed="view === 'table'"
            @click="setView('table')"
          >
            <AppIcon name="table" :size="15" />
            表格
          </button>
        </div>
      </div>
    </section>

    <template v-if="view === 'chart'">
      <section class="card">
        <div class="card-header">
          <div>
            <h2 class="card-title">电量曲线与每日用量</h2>
            <p class="card-subtitle">{{ selectedRoomLabel }} / {{ records.length }} 条记录</p>
          </div>
          <button
            type="button"
            class="btn btn-sm"
            :class="{ 'btn-primary': brushEnabled }"
            :aria-pressed="brushEnabled"
            @click="toggleBrush"
          >
            {{ brushEnabled ? '退出框选' : '框选统计' }}
          </button>
        </div>
        <UsageChart
          ref="chartRef"
          :records="records"
          :timezone="timezone"
          :loading="recordsLoading"
          :error="recordsError"
          :brush-enabled="brushEnabled"
          @retry="loadRecords"
          @selection="onSelection"
        />
      </section>

      <StatsCard
        :stats="stats"
        :time-zone="timezone"
        @archive="openArchive"
      />
    </template>

    <template v-else>
      <section class="card">
        <div class="card-header">
          <div>
            <h2 class="card-title">采样记录</h2>
            <p class="card-subtitle">
              {{ selectedRoomLabel }} / 已加载 {{ records.length }} 条
              <span v-if="records.length > 0">, 当前显示最近 {{ tableRecords.length }} 条</span>
            </p>
          </div>
          <button
            type="button"
            class="btn btn-sm"
            :disabled="!hasMore || recordsLoading"
            @click="loadMore"
          >
            加载更多
          </button>
        </div>
        <DataTable
          :records="tableRecords"
          :time-zone="timezone"
          :loading="recordsLoading"
          :error="recordsError"
          :page-size="pageSize"
          empty-title="该时间范围没有记录"
          empty-description="调整时间范围, 或等待采集循环写入新样本."
          @retry="loadRecords"
        />
        <p v-if="records.length >= 200000" class="field-hint table-warning">
          已达到单次查询上限 200000 条, 建议收窄时间范围, 或直接导出 CSV.
        </p>
      </section>
    </template>

    <ArchiveCreateDialog
      v-model="archiveOpen"
      :initial-from="stats?.from ?? null"
      :initial-to="stats?.to ?? null"
      :time-zone="timezone"
      :room-label="selectedRoomLabel"
      :busy="archiveBusy"
      :error="archiveError"
      @submit="submitArchive"
    />
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.toolbar-row {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.room-field {
  min-width: 260px;
}

.view-toggle {
  display: flex;
  gap: 4px;
  padding: 3px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-soft);
}

.table-warning {
  margin-top: 12px;
}

@media (max-width: 640px) {
  .room-field {
    width: 100%;
    min-width: 0;
  }

  .view-toggle {
    width: 100%;
  }

  .view-toggle .btn {
    flex: 1;
  }
}
</style>
