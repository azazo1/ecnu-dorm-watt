<script setup lang="ts">
import { computed } from 'vue'
import type { RangeStats } from '@/utils/stats'
import { formatDateTime, formatDays, formatNumber } from '@/utils/time'
import AppIcon from './AppIcon.vue'
import Skeleton from './Skeleton.vue'

const props = withDefaults(
  defineProps<{
    stats: RangeStats | null
    timeZone: string
    loading?: boolean
  }>(),
  { loading: false },
)

defineEmits<{ archive: [] }>()

const hasSamples = computed(() => (props.stats?.samples ?? 0) > 0)

const statsItems = computed(() => {
  const value = props.stats
  if (!value) {
    return []
  }
  return [
    { label: '样本数', value: `${value.samples}` },
    { label: '用量合计', value: `${formatNumber(value.usage)} 度` },
    { label: '日均用量', value: value.avgPerDay === null ? '-' : `${formatNumber(value.avgPerDay)} 度` },
    { label: '预计可用', value: value.estimatedDays === null ? '-' : `${formatDays(value.estimatedDays)} 天` },
  ]
})
</script>

<template>
  <section class="stats-card card" aria-live="polite">
    <div class="card-header">
      <div>
        <h2 class="card-title">框选统计</h2>
        <p class="card-subtitle">在图表上框选 X 轴区间后显示</p>
      </div>
      <button
        type="button"
        class="btn btn-primary btn-sm"
        :disabled="!hasSamples"
        @click="$emit('archive')"
      >
        <AppIcon name="archives" :size="15" />
        归档此范围
      </button>
    </div>

    <Skeleton v-if="loading" variant="text" :rows="3" />

    <div v-else-if="!stats || !hasSamples" class="stats-placeholder">
      <AppIcon name="chart" :size="22" />
      <div>
        <p class="stats-placeholder-title">尚未选择统计范围</p>
        <p class="small muted">打开框选模式, 在电量曲线上横向拖动即可查看该区间的用量.</p>
      </div>
    </div>

    <template v-else>
      <div class="stats-range">
        <span>{{ formatDateTime(stats.from, timeZone) }}</span>
        <span class="muted">至</span>
        <span>{{ formatDateTime(stats.to, timeZone) }}</span>
      </div>
      <div class="stats-grid">
        <div v-for="item in statsItems" :key="item.label" class="stats-item">
          <span class="stats-label">{{ item.label }}</span>
          <span class="stats-value tabular">{{ item.value }}</span>
        </div>
      </div>
      <p v-if="stats.recharges > 0" class="stats-note">
        区间内识别到 {{ stats.recharges }} 次电量回升, 共 {{ formatNumber(stats.rechargeAmount) }} 度, 已排除在用量之外.
      </p>
    </template>
  </section>
</template>

<style scoped>
.stats-card {
  min-height: 190px;
}

.stats-placeholder {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 22px 16px;
  border: 1px dashed var(--border-strong);
  border-radius: var(--radius-sm);
  color: var(--text-muted);
}

.stats-placeholder-title {
  color: var(--text);
  font-size: 14px;
  font-weight: 600;
}

.stats-range {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 14px;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  background: var(--surface-soft);
  font-size: 13px;
  font-variant-numeric: tabular-nums;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.stats-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}

.stats-label {
  color: var(--text-muted);
  font-size: 12px;
}

.stats-value {
  font-size: 16px;
  font-weight: 650;
}

.stats-note {
  margin-top: 10px;
  color: var(--text-muted);
  font-size: 12px;
}

@media (max-width: 900px) {
  .stats-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 480px) {
  .stats-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
