/**
 * The Today page's arithmetic (U2, 2026-10-20), pure so it is unit-tested:
 * ATR(14) from daily bars (Wilder, as shared/pkg/stops), the distance of a
 * held position to its stop and target, and the money at risk at the stops.
 */
import type { EtoroTrade } from '../api/etoroSchemas'

export interface Bar {
  high: number
  low: number
  close: number
}

/** Wilder ATR over the last n+1 bars; 0 when there are not enough bars. */
export function atr(bars: Bar[], n = 14): number {
  if (bars.length < n + 1) return 0
  const tr = (i: number) => {
    const b = bars[i]
    const prev = bars[i - 1].close
    return Math.max(b.high - b.low, Math.abs(b.high - prev), Math.abs(b.low - prev))
  }
  let a = 0
  for (let i = 1; i <= n; i++) a += tr(i)
  a /= n
  for (let i = n + 1; i < bars.length; i++) a = (a * (n - 1) + tr(i)) / n
  return a
}

export interface Distance {
  /** Price distance from the mark (positive = room before it is hit). */
  points: number
  /** As a fraction of the mark. */
  pct: number
  /** In ATRs (0 when no ATR). */
  atrs: number
}

/** Distance from `mark` down to the stop (or up to the target). */
export function distanceTo(mark: number, level: number, atrValue: number, kind: 'stop' | 'target'): Distance | null {
  if (!(mark > 0) || !(level > 0)) return null
  const points = kind === 'stop' ? mark - level : level - mark
  return { points, pct: points / mark, atrs: atrValue > 0 ? points / atrValue : 0 }
}

export interface OpenRisk {
  open: number
  exposure: number
  unrealized: number
  /** Sum over protected open rows of units x (entry - stop). */
  maxLossAtStops: number
  /** Open rows without a stop: their loss is unbounded by a stop. */
  unprotected: number
  /** Exposure of the unprotected rows. */
  unprotectedExposure: number
}

/** Totals the open rows; the mark for an open row is the one the card shows. */
export function openRisk(trades: EtoroTrade[]): OpenRisk {
  const r: OpenRisk = { open: 0, exposure: 0, unrealized: 0, maxLossAtStops: 0, unprotected: 0, unprotectedExposure: 0 }
  for (const t of trades) {
    if (t.status !== 'open') continue
    r.open++
    r.exposure += t.amount
    r.unrealized += t.unrealized_pnl ?? 0
    if (t.stop_loss_rate && t.stop_loss_rate > 0) {
      r.maxLossAtStops += Math.max(0, t.units * (t.open_rate - t.stop_loss_rate))
    } else {
      r.unprotected++
      r.unprotectedExposure += t.amount
    }
  }
  return r
}

/** The mark an open row offers: the account's implied mark, else the entry. */
export function markOf(t: EtoroTrade): number {
  return t.mark_rate && t.mark_rate > 0 ? t.mark_rate : t.open_rate
}

/**
 * The stop's history for the chart (U2): one point per ledger line whose
 * position after the line carries a stop, at the line's fill date (the bar
 * the stop was live from); a console line on the same date supersedes the
 * run's. Pure: records in any order.
 */
export function stopHistory(
  records: {
    decision: { fill_date: string; bar_date: string }
    recorded_at: string
    demo?: { position_after: { stop_loss_rate?: number } } | null
  }[],
): { date: string; value: number }[] {
  const sorted = [...records].sort((a, b) =>
    a.decision.bar_date === b.decision.bar_date
      ? a.recorded_at.localeCompare(b.recorded_at)
      : a.decision.bar_date.localeCompare(b.decision.bar_date),
  )
  const byDate = new Map<string, number>()
  for (const r of sorted) {
    const stop = r.demo?.position_after?.stop_loss_rate ?? 0
    const date = r.decision.fill_date || r.decision.bar_date
    if (stop > 0) byDate.set(date, stop)
    else if (r.demo && byDate.has(date)) byDate.delete(date)
  }
  return [...byDate.entries()].map(([date, value]) => ({ date, value }))
}
