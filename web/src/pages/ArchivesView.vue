<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { ArchiveMeta, RecordSample, Room } from '@/api/types'
import ArchiveCreateDialog from '@/components/ArchiveCreateDialog.vue'
import ArchiveDetailDialog from '@/components/ArchiveDetailDialog.vue'
import ArchiveList from '@/components/ArchiveList.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import AppIcon from '@/components/AppIcon.vue'
import { useToast } from '@/composables/useToast'
import { useSettingsStore } from '@/stores/settings'
import { useStatusStore } from '@/stores/status'
import { toUiError, type UiError } from '@/utils/errors'

const status = useStatusStore()
const settings = useSettingsStore()
const toast = useToast()
const timezone = settings.timezone

const rooms = ref<Room[]>([])
const roomsLoading = ref(false)
const roomsError = ref<UiError | null>(null)
const roomFilter = ref<number | 'all'>('all')

const archives = ref<ArchiveMeta[]>([])
const loading = ref(false)
const error = ref<UiError | null>(null)
const includeDeleted = ref(false)
const busyName = ref<string | null>(null)

const createOpen = ref(false)
const createBusy = ref(false)
const createError = ref<UiError | null>(null)

const deleteTarget = ref<ArchiveMeta | null>(null)
const deleteOpen = ref(false)
const deleteBusy = ref(false)
const deleteError = ref<UiError | null>(null)

const detailOpen = ref(false)
const detailArchive = ref<ArchiveMeta | null>(null)
const detailRecords = ref<RecordSample[]>([])
const detailLoading = ref(false)
const detailError = ref<UiError | null>(null)

let archivesController: AbortController | null = null
let archivesSequence = 0
let roomsController: AbortController | null = null
let detailController: AbortController | null = null
let detailSequence = 0
let pollTimer: number | null = null

const showRoom = computed(() => roomFilter.value === 'all')
const selectedRoomLabel = computed(() => {
  if (roomFilter.value !== 'all') {
    const room = rooms.value.find((item) => item.id === roomFilter.value)
    return room ? room.room_no : `房间 ${roomFilter.value}`
  }
  const active = status.activeRoom.value
  return active ? `${active.room_no} (活动房间)` : '未选择房间'
})
const archiveTargetRoom = computed<number | null>(() => {
  if (roomFilter.value !== 'all') {
    return roomFilter.value
  }
  return status.activeRoom.value?.id ?? null
})

async function loadRooms(): Promise<void> {
  roomsController?.abort()
  roomsController = new AbortController()
  roomsLoading.value = true
  roomsError.value = null
  try {
    const data = await api.getRooms(roomsController.signal)
    rooms.value = data.rooms
    if (roomFilter.value === 'all') {
      const active = data.rooms.find((room) => room.active)
      if (active) {
        roomFilter.value = active.id
      }
    } else if (!data.rooms.some((room) => room.id === roomFilter.value)) {
      roomFilter.value = 'all'
    }
  } catch (caught) {
    if (caught instanceof DOMException && caught.name === 'AbortError') {
      return
    }
    roomsError.value = toUiError(caught)
  } finally {
    roomsLoading.value = false
  }
}

async function loadArchives(): Promise<void> {
  archivesController?.abort()
  archivesController = new AbortController()
  const current = ++archivesSequence
  loading.value = true
  error.value = null
  try {
    const data = await api.getArchives(
      {
        room_id: roomFilter.value === 'all' ? 'all' : roomFilter.value,
        include_deleted: includeDeleted.value,
      },
      archivesController.signal,
    )
    if (current !== archivesSequence) {
      return
    }
    archives.value = data.archives
  } catch (caught) {
    if (caught instanceof DOMException && caught.name === 'AbortError') {
      return
    }
    if (current !== archivesSequence) {
      return
    }
    archives.value = []
    error.value = toUiError(caught)
  } finally {
    if (current === archivesSequence) {
      loading.value = false
    }
  }
}

function onRoomFilterChange(event: Event): void {
  const target = event.target as HTMLSelectElement
  roomFilter.value = target.value === 'all' ? 'all' : Number(target.value)
  void loadArchives()
}

function onIncludeDeletedChange(): void {
  void loadArchives()
}

async function openDetail(archive: ArchiveMeta): Promise<void> {
  detailController?.abort()
  detailController = new AbortController()
  const current = ++detailSequence
  detailArchive.value = archive
  detailRecords.value = []
  detailError.value = null
  detailLoading.value = true
  detailOpen.value = true
  try {
    const data = await api.getArchiveSamples(archive.name, archive.room_id, detailController.signal)
    if (current !== detailSequence) {
      return
    }
    detailArchive.value = data.archive
    detailRecords.value = data.records
  } catch (caught) {
    if (caught instanceof DOMException && caught.name === 'AbortError') {
      return
    }
    if (current !== detailSequence) {
      return
    }
    detailRecords.value = []
    detailError.value = toUiError(caught)
  } finally {
    if (current === detailSequence) {
      detailLoading.value = false
    }
  }
}

function retryDetail(): void {
  if (detailArchive.value) {
    void openDetail(detailArchive.value)
  }
}

watch(detailOpen, (open) => {
  if (!open) {
    detailController?.abort()
    detailSequence++
    detailLoading.value = false
  }
})

function openCreate(): void {
  if (archiveTargetRoom.value === null) {
    toast.error('请先选择活动房间, 再创建归档.')
    return
  }
  createError.value = null
  createOpen.value = true
}

async function submitCreate(payload: { from: string; to: string; name: string }): Promise<void> {
  const target = archiveTargetRoom.value
  if (target === null || createBusy.value) {
    return
  }
  createBusy.value = true
  createError.value = null
  try {
    const created = await api.createArchive({
      room_id: target,
      from: payload.from,
      to: payload.to,
      name: payload.name || undefined,
    })
    toast.success(`归档 ${created.name} 已创建, 共 ${created.records_num} 条样本.`)
    createOpen.value = false
    await loadArchives()
    await status.refreshStatus()
  } catch (caught) {
    createError.value = toUiError(caught)
  } finally {
    createBusy.value = false
  }
}

function askDelete(archive: ArchiveMeta): void {
  deleteTarget.value = archive
  deleteError.value = null
  deleteOpen.value = true
}

async function confirmDelete(): Promise<void> {
  const target = deleteTarget.value
  if (!target || deleteBusy.value) {
    return
  }
  deleteBusy.value = true
  deleteError.value = null
  busyName.value = target.name
  try {
    await api.deleteArchive(target.name, target.room_id)
    toast.success(`归档 ${target.name} 已删除. 数据仍保留在服务端, 可由管理员清理.`)
    deleteOpen.value = false
    deleteTarget.value = null
    await loadArchives()
    await status.refreshStatus()
  } catch (caught) {
    deleteError.value = toUiError(caught)
  } finally {
    deleteBusy.value = false
    busyName.value = null
  }
}

function handleVisibility(): void {
  if (!document.hidden) {
    void loadArchives()
  }
}

function handleOnline(): void {
  void loadArchives()
}

function startPolling(): void {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
  }
  pollTimer = window.setInterval(() => {
    if (!document.hidden) {
      void loadArchives()
    }
  }, 10000)
}

watch(
  () => status.manualRefreshCount.value,
  () => {
    void loadArchives()
  },
)

onMounted(async () => {
  await loadRooms()
  await loadArchives()
  document.addEventListener('visibilitychange', handleVisibility)
  window.addEventListener('online', handleOnline)
  startPolling()
})

onBeforeUnmount(() => {
  archivesController?.abort()
  roomsController?.abort()
  detailController?.abort()
  document.removeEventListener('visibilitychange', handleVisibility)
  window.removeEventListener('online', handleOnline)
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
  }
})
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div class="page-heading">
        <h1 class="page-title">归档</h1>
        <p class="page-subtitle">把指定时间范围的样本归档保存, 并可在线查看或下载 CSV.</p>
      </div>
      <div class="page-actions">
        <button
          type="button"
          class="btn btn-primary"
          :disabled="archiveTargetRoom === null"
          :title="archiveTargetRoom === null ? '请先选择活动房间' : '新建归档'"
          @click="openCreate"
        >
          <AppIcon name="plus" :size="16" />
          新建归档
        </button>
      </div>
    </header>

    <section class="card toolbar">
      <div class="row">
        <div class="field room-field">
          <label for="archive-room">房间</label>
          <select
            id="archive-room"
            class="select"
            :value="roomFilter === 'all' ? 'all' : String(roomFilter)"
            :disabled="roomsLoading"
            @change="onRoomFilterChange"
          >
            <option value="all">全部房间</option>
            <option v-for="room in rooms" :key="room.id" :value="room.id">
              {{ room.room_no }}{{ room.active ? ' (活动)' : '' }}
            </option>
          </select>
        </div>

        <label class="checkbox-field">
          <input v-model="includeDeleted" type="checkbox" @change="onIncludeDeletedChange" />
          包含已删除
        </label>

        <span class="muted small">每 10 秒自动刷新, 页面隐藏时暂停.</span>
      </div>

      <p v-if="roomsError" class="field-error">
        房间列表加载失败: {{ roomsError.detail }}
        <button type="button" class="btn btn-sm btn-ghost" @click="loadRooms">重试</button>
      </p>
    </section>

    <section class="card">
      <div class="card-header">
        <div>
          <h2 class="card-title">归档列表</h2>
          <p class="card-subtitle">{{ selectedRoomLabel }} / 共 {{ archives.length }} 条</p>
        </div>
        <span v-if="loading" class="tag">刷新中</span>
      </div>
      <ArchiveList
        :archives="archives"
        :time-zone="timezone"
        :loading="loading"
        :error="error"
        :busy-name="busyName"
        :show-room="showRoom"
        @retry="loadArchives"
        @create="openCreate"
        @view="openDetail"
        @delete="askDelete"
      />
    </section>

    <ArchiveCreateDialog
      v-model="createOpen"
      :time-zone="timezone"
      :room-label="selectedRoomLabel"
      :busy="createBusy"
      :error="createError"
      @submit="submitCreate"
    />

    <ArchiveDetailDialog
      v-model="detailOpen"
      :archive="detailArchive"
      :records="detailRecords"
      :time-zone="timezone"
      :loading="detailLoading"
      :error="detailError"
      @retry="retryDetail"
    />

    <ConfirmDialog
      v-model="deleteOpen"
      title="删除归档"
      :message="`确定要删除归档 ${deleteTarget?.name ?? ''} 吗? 这是软删除, 数据仍保留在服务端.`"
      confirm-label="删除归档"
      :require-text="deleteTarget?.name ?? ''"
      danger
      :busy="deleteBusy"
      @confirm="confirmDelete"
    >
      <div v-if="deleteError" class="form-error" role="alert">
        <strong>{{ deleteError.title }}</strong>
        <span>{{ deleteError.detail }}</span>
        <span>{{ deleteError.advice }}</span>
        <span class="small">错误码: <code>{{ deleteError.code }}</code></span>
      </div>
    </ConfirmDialog>
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.room-field {
  min-width: 260px;
}

.checkbox-field {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding-bottom: 8px;
  color: var(--text);
  font-size: 13px;
}

.checkbox-field input {
  width: 16px;
  height: 16px;
  accent-color: var(--accent);
}

@media (max-width: 640px) {
  .room-field {
    width: 100%;
    min-width: 0;
  }
}
</style>
