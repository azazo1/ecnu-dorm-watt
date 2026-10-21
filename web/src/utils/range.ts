export type RangePreset = '24h' | '7d' | '30d' | 'all' | 'custom'

export interface RangeSelection {
  preset: RangePreset
  from: string | null
  to: string | null
}

export const PRESET_LABELS: Array<{ value: RangePreset; label: string }> = [
  { value: '24h', label: '24 小时' },
  { value: '7d', label: '7 天' },
  { value: '30d', label: '30 天' },
  { value: 'all', label: '全部' },
  { value: 'custom', label: '自定义' },
]

export function isRangePreset(value: unknown): value is RangePreset {
  return value === '24h' || value === '7d' || value === '30d' || value === 'all' || value === 'custom'
}

export function presetRange(preset: RangePreset, now = Date.now()): RangeSelection {
  if (preset === 'custom') {
    return { preset, from: null, to: null }
  }
  if (preset === 'all') {
    return { preset, from: null, to: null }
  }
  const durations: Record<Exclude<RangePreset, 'all' | 'custom'>, number> = {
    '24h': 24 * 60 * 60 * 1000,
    '7d': 7 * 24 * 60 * 60 * 1000,
    '30d': 30 * 24 * 60 * 60 * 1000,
  }
  return {
    preset,
    from: new Date(now - durations[preset]).toISOString(),
    to: new Date(now).toISOString(),
  }
}

export function rangeDurationMs(range: RangeSelection): number | null {
  if (!range.from || !range.to) {
    return null
  }
  const from = Date.parse(range.from)
  const to = Date.parse(range.to)
  if (!Number.isFinite(from) || !Number.isFinite(to) || to < from) {
    return null
  }
  return to - from
}
