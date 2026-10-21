export function bytes(value?: number, digits = 1): string {
  if (value === undefined || value === null || Number.isNaN(value)) return '—'
  if (value < 1024) return `${Math.round(value)} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let v = value
  let i = -1
  do { v /= 1024; i++ } while (v >= 1024 && i < units.length - 1)
  return `${v.toFixed(digits)} ${units[i]}`
}

export function speed(value?: number): string {
  if (value === undefined || value === null) return '—'
  return `${bytes(value)}/s`
}

export function percent(value?: number, digits = 0): string {
  if (value === undefined || value === null || Number.isNaN(value)) return '—'
  return `${value.toFixed(digits)}%`
}

export function uptime(seconds?: number): string {
  if (seconds === undefined || seconds === null || seconds <= 0) return '—'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}天 ${hours}小时`
  if (hours > 0) return `${hours}小时 ${minutes}分`
  return `${minutes}分`
}

export function timeAgo(ts?: number): string {
  if (!ts) return '从未'
  const diff = Date.now() / 1000 - ts
  if (diff < 10) return '刚刚'
  if (diff < 60) return `${Math.floor(diff)} 秒前`
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
  return `${Math.floor(diff / 86400)} 天前`
}

export function clock(ts?: number): string {
  if (!ts) return '—'
  return new Date(ts * 1000).toLocaleString()
}

export function day(ts?: number): string {
  if (!ts) return '—'
  return new Date(ts * 1000).toLocaleDateString()
}

export function latency(value?: number): string {
  if (value === undefined || value === null || value < 0) return '—'
  return `${value.toFixed(1)} ms`
}

export function latencyTone(value?: number, loss?: number): string {
  if (loss !== undefined && loss > 0) return 'bad'
  if (value === undefined || value < 0) return 'muted'
  if (value < 60) return 'good'
  if (value < 150) return 'warn'
  return 'bad'
}

export function clamp(value: number, min = 0, max = 100): number {
  return Math.min(max, Math.max(min, value))
}

export function avatar(name: string): string {
  return (name || '?').trim().slice(0, 2).toUpperCase()
}
