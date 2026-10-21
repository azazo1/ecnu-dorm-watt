import type { RecordSample } from '@/api/types'
import { formatDate, parseTime, zonedInputToRfc3339 } from './time'

export interface RangeStats {
  from: string | null
  to: string | null
  samples: number
  usage: number
  recharges: number
  rechargeAmount: number
  days: number | null
  avgPerDay: number | null
  estimatedDays: number | null
  lastDegree: number | null
}

export interface DailyUsage {
  date: string
  startMs: number
  usage: number
}

export interface RechargePoint {
  timeMs: number
  degree: number
  amount: number
}

export function computeRangeStats(
  records: RecordSample[],
  fromMs: number,
  toMs: number,
  currentDegree: number | null,
): RangeStats {
  let previous: { timeMs: number; degree: number } | null = null
  let firstTime: number | null = null
  let lastTime: number | null = null
  let samples = 0
  let usage = 0
  let recharges = 0
  let rechargeAmount = 0
  let lastDegree: number | null = null

  for (const record of records) {
    const timeMs = parseTime(record.time)
    if (timeMs === null) {
      continue
    }
    if (timeMs < fromMs) {
      previous = { timeMs, degree: record.degree }
      continue
    }
    if (timeMs > toMs) {
      continue
    }
    if (previous) {
      const delta = previous.degree - record.degree
      if (delta > 0) {
        usage += delta
      } else if (record.degree > previous.degree) {
        recharges += 1
        rechargeAmount += record.degree - previous.degree
      }
    }
    firstTime ??= timeMs
    lastTime = timeMs
    samples += 1
    lastDegree = record.degree
    previous = { timeMs, degree: record.degree }
  }

  if (samples === 0 || firstTime === null || lastTime === null) {
    return {
      from: null,
      to: null,
      samples: 0,
      usage: 0,
      recharges: 0,
      rechargeAmount: 0,
      days: null,
      avgPerDay: null,
      estimatedDays: null,
      lastDegree: null,
    }
  }

  const spanDays = (lastTime - firstTime) / 86400000
  const days = Math.max(spanDays, 1 / 24)
  const avgPerDay = usage / days
  const estimatedDays =
    currentDegree !== null && avgPerDay > 0 ? currentDegree / avgPerDay : null

  return {
    from: new Date(firstTime).toISOString(),
    to: new Date(lastTime).toISOString(),
    samples,
    usage,
    recharges,
    rechargeAmount,
    days,
    avgPerDay,
    estimatedDays,
    lastDegree,
  }
}

export function dailyUsage(records: RecordSample[], timeZone: string): DailyUsage[] {
  const totals = new Map<string, number>()
  let previous: number | null = null

  for (const record of records) {
    if (previous !== null) {
      const delta = previous - record.degree
      if (delta > 0) {
        const key = formatDate(record.time, timeZone)
        totals.set(key, (totals.get(key) ?? 0) + delta)
      }
    }
    previous = record.degree
  }

  const result: DailyUsage[] = []
  for (const [date, usage] of totals) {
    const iso = zonedInputToRfc3339(`${date}T00:00:00`, timeZone)
    if (!iso) {
      continue
    }
    result.push({ date, startMs: Date.parse(iso), usage })
  }
  result.sort((left, right) => left.startMs - right.startMs)
  return result
}

export function rechargePoints(records: RecordSample[]): RechargePoint[] {
  const result: RechargePoint[] = []
  for (let index = 1; index < records.length; index += 1) {
    const previous = records[index - 1]
    const current = records[index]
    if (current.degree > previous.degree) {
      const timeMs = parseTime(current.time)
      if (timeMs !== null) {
        result.push({
          timeMs,
          degree: current.degree,
          amount: current.degree - previous.degree,
        })
      }
    }
  }
  return result
}

export function degreeChange(records: RecordSample[]): number | null {
  if (records.length < 2) {
    return null
  }
  const previous = records[records.length - 2]
  const latest = records[records.length - 1]
  return latest.degree - previous.degree
}
