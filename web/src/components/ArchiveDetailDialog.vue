<script setup lang="ts">
import { computed } from 'vue'
import { archiveCsvUrl } from '@/api/client'
import type { ArchiveMeta, RecordSample } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import BaseModal from '@/components/BaseModal.vue'
import DataTable from '@/components/DataTable.vue'
import ErrorState from '@/components/ErrorState.vue'
import Skeleton from '@/components/Skeleton.vue'
import UsageChart from '@/components/UsageChart.vue'
import type { UiError } from '@/utils/errors'
import { formatDateTime, formatNumber } from '@/utils/time'

const props = defineProps<{
  modelValue: boolean
  archive: ArchiveMeta | null
  records: RecordSample[]
  timeZone: string
  loading?: boolean
  error?: UiError | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  retry: []
}>()

const open = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const title = computed(() =>
  props.archive ? `归档 ${props.archive.name}` : '归档详情',
)
</script>

<template>
  <BaseModal v-model="open" :title="title" size="lg">
    <template v-if="archive">
      <div class="archive-detail-meta">
        <div>
          <span class="meta-label">房间</span>
          <strong class="tabular">{{ archive.room_id }}</strong>
        </div>
        <div>
          <span class="meta-label">样本数</span>
          <strong class="tabular">{{ formatNumber(archive.records_num) }}</strong>
        </div>
        <div class="meta-range">
          <span class="meta-label">起止时间</span>
          <span class="tabular">{{ formatDateTime(archive.start_time, timeZone) }}</span>
          <span class="muted">至</span>
          <span class="tabular">{{ formatDateTime(archive.end_time, timeZone) }}</span>
        </div>
      </div>

      <ErrorState v-if="error" :error="error" @retry="$emit('retry')" />

      <template v-else>
        <div class="archive-detail-section">
          <h3 class="detail-heading">电量曲线与每日用量</h3>
          <UsageChart
            :records="records"
            :timezone="timeZone"
            :loading="loading"
            :error="error"
            :height="300"
            @retry="$emit('retry')"
          />
        </div>

        <div class="archive-detail-section">
          <h3 class="detail-heading">样本明细</h3>
          <Skeleton v-if="loading" variant="table" :rows="4" />
          <DataTable
            v-else
            :records="records"
            :time-zone="timeZone"
            :page-size="50"
            empty-title="归档中没有样本"
            empty-description="这个归档的样本为空."
            @retry="$emit('retry')"
          />
        </div>
      </template>
    </template>

    <template #footer>
      <a
        v-if="archive"
        class="btn btn-sm"
        :href="archiveCsvUrl(archive.name, archive.room_id)"
        :download="`${archive.name}.csv`"
      >
        <AppIcon name="download" :size="14" />
        下载 CSV
      </a>
      <button type="button" class="btn btn-sm btn-primary" @click="open = false">
        关闭
      </button>
    </template>
  </BaseModal>
</template>

<style scoped>
.archive-detail-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
  padding-bottom: 16px;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--border);
}

.archive-detail-meta > div {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 90px;
}

.meta-range {
  flex: 1 1 260px;
  display: grid;
  grid-template-columns: 1fr;
  gap: 2px !important;
}

.meta-range > .muted {
  font-size: 11px;
}

.meta-label {
  color: var(--text-muted);
  font-size: 11px;
}

.archive-detail-section + .archive-detail-section {
  margin-top: 20px;
}

.detail-heading {
  margin-bottom: 8px;
  font-size: 13px;
  font-weight: 600;
}

@media (max-width: 640px) {
  .archive-detail-meta {
    gap: 8px 16px;
  }
}
</style>
