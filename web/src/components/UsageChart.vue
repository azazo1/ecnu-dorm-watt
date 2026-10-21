<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { BarChart, LineChart } from 'echarts/charts'
import {
  BrushComponent,
  DataZoomComponent,
  GridComponent,
  LegendComponent,
  MarkPointComponent,
  TooltipComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { EChartsCoreOption } from 'echarts/core'
import type { RecordSample } from '@/api/types'
import { useTheme } from '@/stores/theme'
import type { UiError } from '@/utils/errors'
import { dailyUsage, rechargePoints } from '@/utils/stats'
import { formatNumber, formatShortDateTime, formatTime, parseTime } from '@/utils/time'
import EmptyState from './EmptyState.vue'
import ErrorState from './ErrorState.vue'
import Skeleton from './Skeleton.vue'

echarts.use([
  LineChart,
  BarChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  DataZoomComponent,
  BrushComponent,
  MarkPointComponent,
  CanvasRenderer,
])

const props = withDefaults(
  defineProps<{
    records: RecordSample[]
    timezone: string
    loading?: boolean
    error?: UiError | null
    brushEnabled?: boolean
    height?: number
  }>(),
  {
    loading: false,
    error: null,
    brushEnabled: false,
    height: 340,
  },
)

const emit = defineEmits<{
  retry: []
  selection: [range: { from: string; to: string } | null]
}>()

interface ChartColors {
  text: string
  muted: string
  border: string
  surface: string
  accent: string
  accentSoft: string
  warn: string
}

interface BrushArea {
  coordRange?: Array<number | string>
}

interface TooltipParam {
  axisValue?: number | string
  dataIndex?: number
  seriesType?: string
}

const chartEl = ref<HTMLDivElement | null>(null)
const { resolved } = useTheme()
let chart: ReturnType<typeof echarts.init> | null = null
let resizeObserver: ResizeObserver | null = null
let lastSelection = ''

const chartStyle = computed(() => ({ '--chart-height': `${props.height}px` }))
const hasData = computed(
  () => !props.loading && !props.error && props.records.length > 0,
)

const dayData = computed(() => dailyUsage(props.records, props.timezone))
const recharges = computed(() => rechargePoints(props.records))

const rangeSpan = computed(() => {
  if (props.records.length < 2) {
    return 24 * 3600 * 1000
  }
  const first = parseTime(props.records[0]?.time)
  const last = parseTime(props.records[props.records.length - 1]?.time)
  if (first === null || last === null) {
    return 24 * 3600 * 1000
  }
  return Math.max(last - first, 0)
})

function readColors(): ChartColors {
  const styles = getComputedStyle(document.documentElement)
  const read = (name: string, fallback: string): string =>
    styles.getPropertyValue(name).trim() || fallback
  return {
    text: read('--text', '#111827'),
    muted: read('--text-muted', '#6b7280'),
    border: read('--border', '#e5e7eb'),
    surface: read('--surface', '#ffffff'),
    accent: read('--accent', '#2563eb'),
    accentSoft: read('--accent-soft', '#dbeafe'),
    warn: read('--warn', '#b45309'),
  }
}

function formatAxisLabel(value: number): string {
  if (rangeSpan.value > 86400000) {
    return formatShortDateTime(value, props.timezone)
  }
  return formatTime(value, props.timezone).slice(0, 5)
}

function tooltipFormatter(params: unknown): string {
  const list = (Array.isArray(params) ? params : [params]) as TooltipParam[]
  const first = list[0]
  if (!first) {
    return ''
  }
  const time = Number(first.axisValue)
  const lines: string[] = [
    Number.isFinite(time) ? formatShortDateTime(time, props.timezone) : '时间未知',
  ]

  const lineParam = list.find((item) => item.seriesType === 'line')
  if (lineParam && typeof lineParam.dataIndex === 'number') {
    const record = props.records[lineParam.dataIndex]
    if (record) {
      lines.push(`剩余电量: ${formatNumber(record.degree)} 度`)
      const previous = props.records[lineParam.dataIndex - 1]
      if (previous) {
        const delta = record.degree - previous.degree
        if (delta > 0.005) {
          lines.push(`较上一点: +${formatNumber(delta)} 度, 可能充值或换表`)
        } else if (delta < -0.005) {
          lines.push(`较上一点: -${formatNumber(Math.abs(delta))} 度`)
        } else {
          lines.push('较上一点: 持平')
        }
      }
    }
  }

  const barParam = list.find((item) => item.seriesType === 'bar')
  if (barParam && typeof barParam.dataIndex === 'number') {
    const item = dayData.value[barParam.dataIndex]
    if (item) {
      lines.push(`${item.date} 用量: ${formatNumber(item.usage)} 度`)
    }
  }
  return lines.join('\n')
}

function buildOption(): EChartsCoreOption {
  const colors = readColors()
  const lineData = props.records.map((record) => [parseTime(record.time) ?? 0, record.degree])
  const barData = dayData.value.map((item) => [item.startMs, item.usage])
  const markPointData = recharges.value.map((item) => ({
    name: '充值或换表',
    coord: [item.timeMs, item.degree],
    value: item.degree,
    symbol: 'circle',
    symbolSize: 8,
    itemStyle: { color: colors.warn },
    label: { show: false },
  }))
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  const fontFamily = getComputedStyle(document.body).fontFamily

  return {
    backgroundColor: 'transparent',
    animation: !reducedMotion,
    animationDuration: 280,
    textStyle: {
      color: colors.text,
      fontFamily,
    },
    color: [colors.accent, colors.warn],
    grid: {
      left: 12,
      right: 18,
      top: 54,
      bottom: 78,
      containLabel: true,
    },
    legend: {
      top: 4,
      right: 8,
      textStyle: { color: colors.muted },
      data: ['剩余电量', '每日用量'],
    },
    tooltip: {
      trigger: 'axis',
      renderMode: 'richText',
      backgroundColor: colors.surface,
      borderColor: colors.border,
      borderWidth: 1,
      textStyle: {
        color: colors.text,
        fontFamily,
        fontSize: 12,
      },
      axisPointer: {
        type: 'cross',
        lineStyle: { color: colors.border },
        label: { backgroundColor: colors.muted },
      },
      formatter: tooltipFormatter,
    },
    xAxis: {
      type: 'time',
      boundaryGap: false,
      axisLine: { lineStyle: { color: colors.border } },
      axisTick: { show: false },
      axisLabel: {
        color: colors.muted,
        hideOverlap: true,
        formatter: (value: number) => formatAxisLabel(value),
      },
      splitLine: { show: false },
    },
    yAxis: [
      {
        type: 'value',
        name: '剩余电量 (度)',
        nameTextStyle: { color: colors.muted, padding: [0, 0, 0, 8] },
        axisLabel: { color: colors.muted },
        splitLine: { lineStyle: { color: colors.border, type: 'dashed' } },
      },
      {
        type: 'value',
        name: '每日用量 (度)',
        nameTextStyle: { color: colors.muted, padding: [0, 8, 0, 0] },
        axisLabel: { color: colors.muted },
        splitLine: { show: false },
      },
    ],
    dataZoom: [
      {
        type: 'inside',
        zoomOnMouseWheel: true,
        moveOnMouseWheel: false,
        moveOnMouseMove: !props.brushEnabled,
        preventDefaultMouseMove: true,
      },
      {
        type: 'slider',
        height: 22,
        bottom: 16,
        borderColor: colors.border,
        backgroundColor: colors.surface,
        fillerColor: colors.accentSoft,
        handleStyle: { color: colors.accent },
        moveHandleStyle: { color: colors.accent },
        textStyle: { color: colors.muted },
      },
    ],
    brush: {
      xAxisIndex: 0,
      brushMode: 'single',
      transformable: false,
      throttleType: 'debounce',
      throttleDelay: 200,
      inBrush: { colorAlpha: 1 },
      outOfBrush: { colorAlpha: 0.25 },
    },
    series: [
      {
        name: '剩余电量',
        type: 'line',
        yAxisIndex: 0,
        data: lineData,
        showSymbol: false,
        smooth: false,
        connectNulls: false,
        sampling: props.records.length > 2000 ? 'lttb' : undefined,
        lineStyle: { width: 2, color: colors.accent },
        itemStyle: { color: colors.accent },
        areaStyle: { color: colors.accentSoft, opacity: 0.35 },
        markPoint: {
          symbolSize: 8,
          data: markPointData,
        },
        emphasis: {
          focus: 'series',
        },
      },
      {
        name: '每日用量',
        type: 'bar',
        yAxisIndex: 1,
        data: barData,
        barMaxWidth: 18,
        itemStyle: {
          color: colors.warn,
          opacity: 0.72,
          borderRadius: [2, 2, 0, 0],
        },
        emphasis: {
          itemStyle: { opacity: 0.95 },
        },
      },
    ],
  }
}

function applyOption(): void {
  if (!chart) {
    return
  }
  chart.setOption(buildOption(), true)
  applyBrushMode()
}

function applyBrushMode(): void {
  if (!chart) {
    return
  }
  chart.dispatchAction({
    type: 'takeGlobalCursor',
    key: 'brush',
    brushOption: { brushType: props.brushEnabled ? 'rect' : false },
  } as never)
}

function readAreas(params: unknown): BrushArea[] {
  const value = params as {
    areas?: BrushArea[]
    batch?: Array<{ areas?: BrushArea[] }>
  }
  return value.batch?.[0]?.areas ?? value.areas ?? []
}

function handleBrushEvent(params: unknown): void {
  const areas = readAreas(params)
  const area = areas.find(
    (item) => Array.isArray(item.coordRange) && item.coordRange.length >= 2,
  )
  if (!area?.coordRange) {
    if (lastSelection) {
      lastSelection = ''
      emit('selection', null)
    }
    return
  }
  const start = Number(area.coordRange[0])
  const end = Number(area.coordRange[1])
  if (!Number.isFinite(start) || !Number.isFinite(end)) {
    return
  }
  const from = Math.min(start, end)
  const to = Math.max(start, end)
  if (to - from < 1000) {
    return
  }
  const key = `${from}-${to}`
  if (key === lastSelection) {
    return
  }
  lastSelection = key
  emit('selection', {
    from: new Date(from).toISOString(),
    to: new Date(to).toISOString(),
  })
}

function ensureChart(): void {
  if (!chartEl.value || chart) {
    return
  }
  chart = echarts.init(chartEl.value)
  chart.on('brushSelected', handleBrushEvent)
  chart.on('brushEnd', handleBrushEvent)
  resizeObserver = new ResizeObserver(() => {
    chart?.resize()
  })
  resizeObserver.observe(chartEl.value)
  applyOption()
}

function disposeChart(): void {
  resizeObserver?.disconnect()
  resizeObserver = null
  chart?.dispose()
  chart = null
  lastSelection = ''
}

function clearBrush(): void {
  if (chart) {
    chart.dispatchAction({ type: 'brush', areas: [] } as never)
  }
  lastSelection = ''
  emit('selection', null)
}

async function refreshChart(): Promise<void> {
  if (!hasData.value) {
    disposeChart()
    return
  }
  await nextTick()
  ensureChart()
  applyOption()
}

watch(
  hasData,
  () => {
    void refreshChart()
  },
  { immediate: true },
)

watch(
  () => props.records,
  () => {
    lastSelection = ''
    if (chart) {
      applyOption()
    }
  },
)

watch(
  () => [props.timezone, resolved.value],
  () => {
    if (chart) {
      applyOption()
    }
  },
)

watch(
  () => props.brushEnabled,
  () => {
    if (chart) {
      applyOption()
    }
  },
)

onBeforeUnmount(() => {
  disposeChart()
})

defineExpose({ clearBrush })
</script>

<template>
  <div class="usage-chart">
    <Skeleton v-if="loading" variant="chart" :height="`${height}px`" />
    <ErrorState v-else-if="error" :error="error" @retry="$emit('retry')" />
    <EmptyState
      v-else-if="records.length === 0"
      icon="records"
      title="该时间范围没有记录"
      description="等待第一次采样, 或调整时间范围后再试."
      action-label="刷新"
      @action="$emit('retry')"
    />
    <template v-else>
      <div
        v-show="hasData"
        ref="chartEl"
        class="chart-canvas"
        :style="chartStyle"
        role="img"
        aria-label="剩余电量折线与每日用量柱状图"
      />
      <p class="chart-hint">
        <template v-if="brushEnabled">
          框选模式已开启: 在图表中横向拖动选择区间, 松开后查看统计.
        </template>
        <template v-else>
          滚轮缩放, 拖动平移, 双击复位. 切换到框选模式可查看区间统计.
        </template>
      </p>
    </template>
  </div>
</template>

<style scoped>
.usage-chart {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 8px;
}

.chart-canvas {
  width: 100%;
  height: var(--chart-height, 340px);
  min-height: 220px;
}

.chart-hint {
  color: var(--text-muted);
  font-size: 12px;
}

@media (max-width: 768px) {
  .chart-canvas {
    height: 240px;
  }
}
</style>
