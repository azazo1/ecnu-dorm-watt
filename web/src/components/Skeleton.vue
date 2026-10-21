<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    variant?: 'card' | 'table' | 'chart' | 'list' | 'text'
    rows?: number
    height?: string
  }>(),
  {
    variant: 'card',
    rows: 3,
    height: '280px',
  },
)

const rowCount = computed(() => Math.max(1, props.rows))
const chartStyle = computed(() => ({ height: props.height }))
</script>

<template>
  <div class="skeleton" role="status" aria-live="polite" aria-busy="true">
    <span class="sr-only">加载中</span>

    <template v-if="variant === 'chart'">
      <div class="skeleton-block skeleton-chart" :style="chartStyle">
        <div class="skeleton-chart-lines">
          <span v-for="index in 5" :key="index" />
        </div>
      </div>
    </template>

    <template v-else-if="variant === 'table'">
      <div class="skeleton-table">
        <div class="skeleton-row skeleton-head">
          <span v-for="index in 5" :key="index" />
        </div>
        <div v-for="row in rowCount" :key="row" class="skeleton-row">
          <span v-for="index in 5" :key="index" />
        </div>
      </div>
    </template>

    <template v-else-if="variant === 'list'">
      <div class="skeleton-list">
        <div v-for="row in rowCount" :key="row" class="skeleton-list-item">
          <span class="skeleton-block skeleton-avatar" />
          <div class="skeleton-list-text">
            <span class="skeleton-block skeleton-line skeleton-line-lg" />
            <span class="skeleton-block skeleton-line skeleton-line-sm" />
          </div>
        </div>
      </div>
    </template>

    <template v-else-if="variant === 'text'">
      <div class="skeleton-text">
        <span
          v-for="row in rowCount"
          :key="row"
          class="skeleton-block skeleton-line"
          :class="{ 'skeleton-line-lg': row === 1, 'skeleton-line-sm': row === rowCount && rowCount > 1 }"
        />
      </div>
    </template>

    <template v-else>
      <div class="skeleton-card">
        <span class="skeleton-block skeleton-line skeleton-line-sm" />
        <span class="skeleton-block skeleton-line skeleton-line-lg" />
        <span class="skeleton-block skeleton-line skeleton-line-sm" />
      </div>
    </template>
  </div>
</template>

<style scoped>
.skeleton {
  width: 100%;
}

.skeleton-block {
  display: block;
  border-radius: 6px;
  background: linear-gradient(
    90deg,
    var(--surface-soft) 20%,
    var(--surface-strong) 38%,
    var(--surface-soft) 58%
  );
  background-size: 220% 100%;
  animation: shimmer 1.4s ease-in-out infinite;
}

.skeleton-card {
  display: flex;
  min-height: 132px;
  flex-direction: column;
  justify-content: center;
  gap: 12px;
  padding: 20px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}

.skeleton-line {
  height: 12px;
}

.skeleton-line-lg {
  width: 58%;
  height: 28px;
}

.skeleton-line-sm {
  width: 36%;
  height: 10px;
}

.skeleton-chart {
  display: flex;
  align-items: flex-end;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}

.skeleton-chart-lines {
  display: flex;
  width: 100%;
  height: 82%;
  flex-direction: column;
  justify-content: space-between;
}

.skeleton-chart-lines span {
  height: 1px;
  background: var(--border);
}

.skeleton-table {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.skeleton-row {
  display: grid;
  grid-template-columns: 1.3fr 1fr 1fr 0.8fr 0.7fr;
  gap: 12px;
  padding: 9px 0;
}

.skeleton-row span {
  display: block;
  height: 12px;
  border-radius: 4px;
  background: var(--surface-strong);
  animation: pulse-soft 1.4s ease-in-out infinite;
}

.skeleton-head span {
  height: 9px;
  opacity: 0.7;
}

.skeleton-text {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.skeleton-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.skeleton-list-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 0;
}

.skeleton-avatar {
  width: 38px;
  height: 38px;
  border-radius: 50%;
  flex: 0 0 auto;
}

.skeleton-list-text {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 8px;
}
</style>
