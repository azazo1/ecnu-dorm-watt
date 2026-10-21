const formatterCache = new Map<string, Intl.DateTimeFormat>()

function formatterFor(timeZone: string): Intl.DateTimeFormat {
  const cached = formatterCache.get(timeZone)
  if (cached) {
    return cached
  }
  const formatter = new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  })
  formatterCache.set(timeZone, formatter)
  return formatter
}

interface ZonedParts {
  year: string
  month: string
  day: string
  hour: string
  minute: string
  second: string
}

function zonedParts(date: Date, timeZone: string): ZonedParts {
  const parts = formatterFor(timeZone).formatToParts(date)
  const values: Record<string, string> = {}
  for (const part of parts) {
    if (part.type !== 'literal') {
      values[part.type] = part.value
    }
  }
  return {
    year: values.year ?? '0000',
    month: values.month ?? '01',
    day: values.day ?? '01',
    hour: values.hour === '24' ? '00' : (values.hour ?? '00'),
    minute: values.minute ?? '00',
    second: values.second ?? '00',
  }
}

export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai'
}

export function isValidTimeZone(value: string): boolean {
  if (!value) {
    return false
  }
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: value })
    return true
  } catch {
    return false
  }
}

export function parseTime(value: string | number | Date | null | undefined): number | null {
  if (value === null || value === undefined || value === '') {
    return null
  }
  const date = value instanceof Date ? value : new Date(value)
  const time = date.getTime()
  return Number.isFinite(time) ? time : null
}

export function formatDateTime(
  value: string | number | Date | null | undefined,
  timeZone: string,
): string {
  const time = parseTime(value)
  if (time === null) {
    return '-'
  }
  const parts = zonedParts(new Date(time), timeZone)
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`
}

export function formatShortDateTime(
  value: string | number | Date | null | undefined,
  timeZone: string,
): string {
  const time = parseTime(value)
  if (time === null) {
    return '-'
  }
  const parts = zonedParts(new Date(time), timeZone)
  return `${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`
}

export function formatDate(
  value: string | number | Date | null | undefined,
  timeZone: string,
): string {
  const time = parseTime(value)
  if (time === null) {
    return '-'
  }
  const parts = zonedParts(new Date(time), timeZone)
  return `${parts.year}-${parts.month}-${parts.day}`
}

export function formatTime(
  value: string | number | Date | null | undefined,
  timeZone: string,
): string {
  const time = parseTime(value)
  if (time === null) {
    return '-'
  }
  const parts = zonedParts(new Date(time), timeZone)
  return `${parts.hour}:${parts.minute}:${parts.second}`
}

export function formatRelative(
  value: string | number | Date | null | undefined,
  now = Date.now(),
): string {
  const time = parseTime(value)
  if (time === null) {
    return '-'
  }
  const diff = now - time
  const seconds = Math.round(Math.abs(diff) / 1000)
  const suffix = diff >= 0 ? '前' : '后'
  if (seconds < 10) {
    return '刚刚'
  }
  if (seconds < 60) {
    return `${seconds} 秒${suffix}`
  }
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) {
    return `${minutes} 分钟${suffix}`
  }
  const hours = Math.round(minutes / 60)
  if (hours < 24) {
    return `${hours} 小时${suffix}`
  }
  const days = Math.round(hours / 24)
  return `${days} 天${suffix}`
}

export function toZonedInput(
  value: string | number | Date | null | undefined,
  timeZone: string,
): string {
  const time = parseTime(value)
  if (time === null) {
    return ''
  }
  const parts = zonedParts(new Date(time), timeZone)
  return `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}`
}

function timeZoneOffsetMs(utcTime: number, timeZone: string): number {
  const parts = zonedParts(new Date(utcTime), timeZone)
  const asUtc = Date.UTC(
    Number(parts.year),
    Number(parts.month) - 1,
    Number(parts.day),
    Number(parts.hour),
    Number(parts.minute),
    Number(parts.second),
  )
  return asUtc - utcTime
}

export function zonedInputToRfc3339(value: string, timeZone: string): string | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(value)
  if (!match || !isValidTimeZone(timeZone)) {
    return null
  }
  const [, year, month, day, hour, minute, second = '00'] = match
  const desiredWallTime = Date.UTC(
    Number(year),
    Number(month) - 1,
    Number(day),
    Number(hour),
    Number(minute),
    Number(second),
  )
  if (!Number.isFinite(desiredWallTime)) {
    return null
  }
  let utcTime = desiredWallTime
  for (let index = 0; index < 3; index += 1) {
    const offset = timeZoneOffsetMs(utcTime, timeZone)
    utcTime = desiredWallTime - offset
  }
  return new Date(utcTime).toISOString()
}

export function startOfDayMs(value: string | number | Date, timeZone: string): number {
  const date = formatDate(value, timeZone)
  const iso = zonedInputToRfc3339(`${date}T00:00:00`, timeZone)
  return iso ? Date.parse(iso) : Date.now()
}

export function formatNumber(value: number | null | undefined, digits = 2): string {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return '-'
  }
  return value.toFixed(digits)
}

export function formatDays(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return '-'
  }
  if (value >= 100) {
    return value.toFixed(0)
  }
  if (value >= 10) {
    return value.toFixed(1)
  }
  return value.toFixed(2)
}
