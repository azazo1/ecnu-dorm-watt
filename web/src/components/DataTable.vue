<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { RecordSample } from '@/api/types'
import type { UiError } from '@/utils/errors'
import { formatDateTime, formatNumber } from '@/utils/time'
import EmptyState from './EmptyState.vue'
import ErrorState from './ErrorState.vue'
import Skeleton from './Skeleton.vue'

const props = withDefaults(
  defineProps<{
    records: RecordSample[]
    timeZone: string
    loading?: boolean
    error?: UiError | null
    pageSize?: number
    compact?: boolean
    emptyTitle?: string
    emptyDescription?: string
  }>(),
  {
    loading: false,
    error: null,
    pageSize: 100,
    compact: false,
    emptyTitle: '没有记录',
    emptyDescription: '该时间范围没有采样记录, 可以调整时间范围后重试.',
  },
)

defineEmits<{ retry: [] }>()

const page = ref(1)
const pageSize = computed(() => Math.max(1, props.pageSize))
const pageCount = computed(() => Math.max(1, Math.ceil(props.records.length / pageSize.value)))

watch(
  () => props.records,
  () => {
    page.value = 1
  },
)

watch(pageCount, (count) => {
  if (page.value > count) {
    page.value = count
  }
})

const pageRecords = computed(() => {
  if (props.compact) {
    return props.records
  }
  const start = (page.value - 1) * pageSize.value
  return props.records.slice(start, start + pageSize.value)
})

const startIndex = computed(() => (props.compact ? 0 : (page.value - 1) * pageSize.value))

function deltaAt(index: number): number | null {
  if (index <= 0) {
    return null
  }
  const previous = props.records[index - 1]
  const current = props.records[index]
  if (!previous || !current) {
    return null
  }
  return current.degree - previous.degree
}

function deltaText(delta: number | null): string {
  if (delta === null) {
    return '-'
  }
  if (Math.abs(delta) < 0.005) {
    return '持平'
  }
  return delta > 0 ? `回升 ${formatNumber(delta)}` : `用电 ${formatNumber(Math.abs(delta))}`
}

function deltaTone(delta: number | null): string {
  if (delta === null || Math.abs(delta) < 0.005) {
    return 'muted'
  }
  return delta > 0 ? 'warn' : 'ok'
}
</script>

<template>
  <div class="data-table-component">
    <Skeleton v-if="loading" variant="table" :rows="5" />
    <ErrorState v-else-if="error" :error="error" compact @retry="$emit('retry')" />
    <EmptyState
      v-else-if="records.length === 0"
      icon="records"
      :title="emptyTitle"
      :description="emptyDescription"
      action-label="刷新"
      @action="$emit('retry')"
    />
    <template v-else>
      <div class="table-wrap">
        <table class="data-table responsive-table">
          <caption class="sr-only">电量采样记录</caption>
          <thead>
            <tr>
              <th scope="col">时间</th>
              <th scope="col" class="align-right">剩余电量</th>
              <th scope="col" class="align-right">变化</th>
              <th scope="col">标记</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(record, offset) in pageRecords" :key="`${startIndex + offset}-${record.time}`">
              <td data-label="时间" class="nowrap tabular">
                {{ formatDateTime(record.time, timeZone) }}
              </td>
              <td data-label="剩余电量" class="align-right tabular">
                {{ formatNumber(record.degree) }} 度
              </td>
              <td data-label="变化" class="align-right tabular">
                {{ deltaText(deltaAt(startIndex + offset)) }}
              </td>
              <td data-label="标记">
                <span
                  v-if="deltaAt(startIndex + offset) !== null"
                  class="tag"
                  :class="{
                    'tag-warn': deltaTone(deltaAt(startIndex + offset)) === 'warn',
                    'tag-ok': deltaTone(deltaAt(startIndex + offset)) === 'ok',
                  }"
                >
                  {{ deltaTone(deltaAt(startIndex + offset)) === 'warn' ? '充值或换表' : '正常采样' }}
                </span>
                <span v-else class="muted small">首条记录</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="!compact" class="pagination">
        <span class="muted small">共 {{ records.length }} 条, 每页 {{ pageSize }} 条</span>
        <div class="pagination-pages">
          <button
            type="button"
            class="btn btn-sm"
            :disabled="page <= 1"
            aria-label="上一页"
            @click="page -= 1"
          >
            上一页
          </button>
          <span class="small tabular">第 {{ page }} / {{ pageCount }} 页</span>
          <button
            type="button"
            class="btn btn-sm"
            :disabled="page >= pageCount"
            aria-label="下一页"
            @click="page += 1"
          >
            下一页
          </button>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.data-table-component {
  min-height: 120px;
}

.nowrap {
  white-space: nowrap;
}
</style>
