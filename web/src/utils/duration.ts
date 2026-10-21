export function parseDurationMs(value: string | null | undefined): number | null {
  if (!value) {
    return null
  }
  const match = /^(\d+(?:\.\d+)?)(ms|s|m|h)$/.exec(value.trim())
  if (!match) {
    return null
  }
  const amount = Number(match[1])
  const unit = match[2]
  const multiplier = unit === 'ms' ? 1 : unit === 's' ? 1000 : unit === 'm' ? 60000 : 3600000
  const result = amount * multiplier
  return Number.isFinite(result) && result > 0 ? result : null
}

export function parseDurationSeconds(value: string | null | undefined): number | null {
  const ms = parseDurationMs(value)
  return ms === null ? null : ms / 1000
}

export function durationFromSeconds(seconds: number): string {
  return `${Math.max(1, Math.round(seconds))}s`
}
