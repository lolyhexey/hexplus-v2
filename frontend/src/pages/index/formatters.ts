// formatters.ts — value-shape helpers used by the Dashboard cards.
// Kept minimal (no locale, no plural) so the wire looks identical to
// what 3x-ui puts on screen for the same numbers.

const B_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

export function formatBytes(n: number): string {
  if (!n || n <= 0) return '0 B'
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), B_UNITS.length - 1)
  return `${(n / Math.pow(1024, i)).toFixed(2)} ${B_UNITS[i]}`
}

export function formatCpuCores(cores: number): string {
  return `${cores} ${cores === 1 ? 'core' : 'cores'}`
}

export function formatMhz(mhz: number): string {
  if (!mhz) return '?'
  if (mhz >= 1000) return `${(mhz / 1000).toFixed(2)} GHz`
  return `${mhz.toFixed(0)} MHz`
}

export function formatUptime(seconds: number): string {
  if (!seconds) return '0s'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  const parts: string[] = []
  if (d) parts.push(`${d}d`)
  if (h) parts.push(`${h}h`)
  if (m) parts.push(`${m}m`)
  if (!d && !h) parts.push(`${s}s`)
  return parts.join(' ')
}

export function formatBps(n: number): string {
  return `${formatBytes(n)}/s`
}
