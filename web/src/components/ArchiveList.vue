<script setup lang="ts">
import { archiveCsvUrl } from '@/api/client'
import type { ArchiveMeta } from '@/api/types'
import type { UiError } from '@/utils/errors'
import { formatDateTime } from '@/utils/time'
import AppIcon from './AppIcon.vue'
import EmptyState from './EmptyState.vue'
import ErrorState from './ErrorState.vue'
import Skeleton from './Skeleton.vue'

withDefaults(
  defineProps<{
    archives: ArchiveMeta[]
    timeZone: string
    loading?: boolean
    error?: UiError | null
    busyName?: string | null
    showRoom?: boolean
  }>(),
  {
    loading: false,
    error: null,
    busyName: null,
    showRoom: false,
  },
)

defineEmits<{
  retry: []
  create: []
  delete: [archive: ArchiveMeta]
}>()
</script>

<template>
  <div class="archive-list">
    <Skeleton v-if="loading" variant="table" :rows="4" />
    <ErrorState v-else-if="error" :error="error" @retry="$emit('retry')" />
    <EmptyState
      v-else-if="archives.length === 0"
      icon="archives"
      title="还没有归档"
      description="把一段时间范围的样本归档保存, 之后可以下载 CSV."
      action-label="新建归档"
      @action="$emit('create')"
    />
    <div v-else class="table-wrap">
      <table class="data-table responsive-table">
        <caption class="sr-only">归档列表</caption>
        <thead>
          <tr>
            <th scope="col">名称</th>
            <th v-if="showRoom" scope="col">房间</th>
            <th scope="col">起止时间</th>
            <th scope="col" class="align-right">样本数</th>
            <th scope="col">创建时间</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="archive in archives" :key="`${archive.room_id}-${archive.name}`">
            <td data-label="名称">
              <strong class="break-anywhere">{{ archive.name }}</strong>
            </td>
            <td v-if="showRoom" data-label="房间" class="tabular">{{ archive.room_id }}</td>
            <td data-label="起止时间" class="archive-range">
              <span class="tabular">{{ formatDateTime(archive.start_time, timeZone) }}</span>
              <span class="muted">至</span>
              <span class="tabular">{{ formatDateTime(archive.end_time, timeZone) }}</span>
            </td>
            <td data-label="样本数" class="align-right tabular">{{ archive.records_num }}</td>
            <td data-label="创建时间" class="nowrap tabular">
              {{ formatDateTime(archive.created_at, timeZone) }}
            </td>
            <td data-label="操作">
              <div class="archive-actions">
                <a
                  class="btn btn-sm"
                  :href="archiveCsvUrl(archive.name, archive.room_id)"
                  :download="`${archive.name}.csv`"
                >
                  <AppIcon name="download" :size="14" />
                  下载
                </a>
                <button
                  type="button"
                  class="btn btn-sm btn-ghost text-danger"
                  :disabled="busyName === archive.name"
                  @click="$emit('delete', archive)"
                >
                  <AppIcon name="trash" :size="14" />
                  {{ busyName === archive.name ? '删除中...' : '删除' }}
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.archive-range {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 210px;
  font-size: 12px;
}

.archive-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}

@media (max-width: 768px) {
  .archive-range {
    min-width: 0;
    align-items: flex-end;
  }

  .archive-actions {
    justify-content: flex-end;
  }
}
</style>
