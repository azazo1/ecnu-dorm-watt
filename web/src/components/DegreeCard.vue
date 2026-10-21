<script setup lang="ts">
import { computed } from 'vue'
import type { UiError } from '@/utils/errors'
import { formatDateTime, formatNumber } from '@/utils/time'
import EmptyState from './EmptyState.vue'
import ErrorState from './ErrorState.vue'
import Skeleton from './Skeleton.vue'

const props = withDefaults(
  defineProps<{
    degree: number | null
    sampledAt: string | null
    timeZone: string
    nextPollAt?: string | null
    trend?: number | null
    loading?: boolean
    stale?: boolean
    error?: UiError | null
    emptyDescription?: string
  }>(),
  {
    nextPollAt: null,
    trend: null,
    loading: false,
    stale: false,
    error: null,
    emptyDescription: '还没有采集到电量, 可以点击刷新重试.',
  },
)

defineEmits<{ refresh: [] }>()

const trendText = computed(() => {
  if (props.trend === null) {
    return null
  }
  if (Math.abs(props.trend) < 0.005) {
    return '与上次持平'
  }
  const value = Math.abs(props.trend).toFixed(2)
  return props.trend > 0 ? `较上次回升 ${value} 度` : `较上次下降 ${value} 度`
})

const trendTone = computed(() => {
  if (props.trend === null || Math.abs(props.trend) < 0.005) {
    return 'muted'
  }
  return props.trend > 0 ? 'warn' : 'ok'
})
</script>

<template>
  <div class="degree-card card">
    <Skeleton v-if="loading && degree === null" variant="card" />
    <ErrorState v-else-if="error && degree === null" :error="error" @retry="$emit('refresh')" />
    <EmptyState
      v-else-if="degree === null"
      icon="battery"
      title="尚未采集到电量"
      :description="emptyDescription"
      action-label="立即刷新"
      @action="$emit('refresh')"
    />
    <div v-else class="degree-content">
      <div class="row-between">
        <span class="degree-label">当前剩余电量</span>
        <span v-if="loading" class="tag">刷新中</span>
      </div>
      <div class="degree-value tabular">
        <span class="degree-number">{{ formatNumber(degree) }}</span>
        <span class="degree-unit">度</span>
      </div>
      <div class="degree-meta">
        <span>采样于 {{ formatDateTime(sampledAt, timeZone) }}</span>
        <span v-if="nextPollAt">下次采集 {{ formatDateTime(nextPollAt, timeZone) }}</span>
      </div>
      <div class="degree-foot">
        <span v-if="trendText" class="tag" :data-trend="trendTone">{{ trendText }}</span>
        <span v-else class="muted small">暂无足够样本判断趋势</span>
      </div>
      <div v-if="stale" class="degree-stale" role="status">
        数据可能过期, 请检查采集状态或手动刷新.
      </div>
    </div>
  </div>
</template>

<style scoped>
.degree-card {
  min-height: 220px;
}

.degree-content {
  display: flex;
  height: 100%;
  flex-direction: column;
  gap: 8px;
}

.degree-label {
  color: var(--text-muted);
  font-size: 13px;
}

.degree-value {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin-top: 4px;
}

.degree-number {
  font-size: 40px;
  font-weight: 700;
  letter-spacing: -0.03em;
  line-height: 1.1;
}

.degree-unit {
  color: var(--text-muted);
  font-size: 16px;
}

.degree-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  color: var(--text-muted);
  font-size: 12px;
}

.degree-foot {
  margin-top: auto;
  padding-top: 10px;
}

.degree-foot .tag[data-trend="ok"] {
  background: var(--ok-soft);
  color: var(--ok);
}

.degree-foot .tag[data-trend="warn"] {
  background: var(--warn-soft);
  color: var(--warn);
}

.degree-stale {
  margin-top: 8px;
  padding: 7px 10px;
  border: 1px solid var(--warn);
  border-radius: var(--radius-sm);
  background: var(--warn-soft);
  color: var(--warn);
  font-size: 12px;
}
</style>
