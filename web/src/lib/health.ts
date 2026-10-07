/**
 * Helpers for the Data health page (GET /api/ui/health/data). Ages are measured
 * against the response's own generated_at, so the page reads the same in a
 * captured fixture as it did live.
 */
import type { HealthCheck, HealthStatus } from '../api/schemas'

export type Tone = 'ok' | 'warn' | 'block' | 'flat' | 'info'

const RANK: Record<HealthStatus, number> = { ok: 0, off: 1, unknown: 2, warn: 3, fail: 4 }

export function rank(s: HealthStatus): number {
  return RANK[s]
}

export function statusTone(s: HealthStatus): Tone {
  switch (s) {
    case 'ok':
      return 'ok'
    case 'warn':
      return 'warn'
    case 'fail':
      return 'block'
    case 'off':
      return 'flat'
    default:
      return 'info'
  }
}

export function statusLabel(s: HealthStatus): string {
  return { ok: 'healthy', off: 'not configured', unknown: 'unknown', warn: 'warning', fail: 'failing' }[s]
}

/** Worst first; equal statuses keep their server order. */
export function sortChecks(checks: HealthCheck[]): HealthCheck[] {
  return checks
    .map((c, i) => ({ c, i }))
    .sort((a, b) => rank(b.c.status) - rank(a.c.status) || a.i - b.i)
    .map((x) => x.c)
}

export function countByStatus(checks: HealthCheck[]): Record<HealthStatus, number> {
  const out: Record<HealthStatus, number> = { ok: 0, off: 0, unknown: 0, warn: 0, fail: 0 }
  for (const c of checks) out[c.status]++
  return out
}

/** Worst status among checks in an area, ignoring "off" unless every check is off. */
export function areaStatus(checks: HealthCheck[], area: HealthCheck['area']): HealthStatus | null {
  const xs = checks.filter((c) => c.area === area)
  if (xs.length === 0) return null
  const on = xs.filter((c) => c.status !== 'off')
  if (on.length === 0) return 'off'
  return on.reduce<HealthStatus>((w, c) => (rank(c.status) > rank(w) ? c.status : w), 'ok')
}

/** "42 min", "3.5 h", "6 days". */
export function fmtMinutes(min: number): string {
  if (!Number.isFinite(min) || min < 0) return '—'
  if (min < 120) return `${Math.round(min)} min`
  if (min < 48 * 60) return `${(min / 60).toFixed(1).replace(/\.0$/, '')} h`
  return `${Math.floor(min / 1440)} days`
}

/** Minutes from `iso` to `refIso` (both RFC 3339); NaN if either is missing. */
export function minutesBetween(iso: string, refIso: string): number {
  const a = Date.parse(iso)
  const b = Date.parse(refIso)
  if (Number.isNaN(a) || Number.isNaN(b)) return NaN
  return (b - a) / 60_000
}

export interface FlushTimeline {
  /** Position of each flush in the window, 0 (24 h ago) .. 1 (now). */
  ticks: number[]
  /** Stretches with no flush longer than `gapMinutes`, as [from, to] fractions. */
  gaps: [number, number][]
}

/**
 * Lays the last `hours` of flush times on a 0..1 axis ending at `refIso`, and
 * marks waits longer than `gapMinutes` (the window start and the wait since the
 * last flush count too, as on the server).
 */
export function flushTimeline(flushes: string[], refIso: string, hours = 24, gapMinutes = 120): FlushTimeline {
  const end = Date.parse(refIso)
  const span = hours * 3_600_000
  const start = end - span
  const ts = flushes
    .map((f) => Date.parse(f))
    .filter((t) => !Number.isNaN(t) && t >= start && t <= end)
    .sort((a, b) => a - b)
  const pos = (t: number) => Math.min(1, Math.max(0, (t - start) / span))
  const gaps: [number, number][] = []
  let prev = start
  for (const t of [...ts, end]) {
    if (t - prev > gapMinutes * 60_000) gaps.push([pos(prev), pos(t)])
    prev = t
  }
  return { ticks: ts.map(pos), gaps }
}

/** "102.6 kB" */
export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} kB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}
