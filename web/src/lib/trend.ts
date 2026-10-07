/**
 * Geometry for the landing page's trend chart: daily closes, the SMA50 and
 * the days the frozen rule was long, laid out in a fixed viewBox. Pure, so it
 * is unit tested; the SVG stretches the box to its container.
 */
import type { CandlePoint } from '../api/schemas'

export const TREND_W = 1000
export const TREND_H = 360

export interface TrendPoint {
  date: string
  close: number
  sma50: number | null
  long: boolean | null
  /** Position in the viewBox. */
  x: number
  y: number
}

export interface TrendGeometry {
  points: TrendPoint[]
  /** SVG path data, in viewBox units. */
  price: string
  /** Closed area under the price line, for the gradient fill. */
  area: string
  sma: string
  /** Runs of consecutive days where the rule was long, as x ranges. */
  longBands: { x: number; w: number }[]
  min: number
  max: number
  /** Share of the shown days the rule was long (0..1), over days with a known state. */
  longShare: number
  /** Close-to-close change over the shown days, in percent. */
  changePct: number
}

const PAD_TOP = 24
const PAD_BOTTOM = 18

const f = (v: number) => Math.round(v * 10) / 10

/** Lays out `candles` (oldest first). Returns null when there is nothing to draw. */
export function trendGeometry(candles: CandlePoint[], w = TREND_W, h = TREND_H): TrendGeometry | null {
  const cs = candles.filter((c) => Number.isFinite(c.close) && c.close > 0)
  if (cs.length < 2) return null

  let min = Infinity
  let max = -Infinity
  for (const c of cs) {
    min = Math.min(min, c.close, c.sma50 ?? c.close)
    max = Math.max(max, c.close, c.sma50 ?? c.close)
  }
  if (max === min) max = min + 1

  const step = w / (cs.length - 1)
  const y = (v: number) => PAD_TOP + (1 - (v - min) / (max - min)) * (h - PAD_TOP - PAD_BOTTOM)

  const points: TrendPoint[] = cs.map((c, i) => ({
    date: c.date,
    close: c.close,
    sma50: c.sma50,
    long: c.long,
    x: i * step,
    y: y(c.close),
  }))

  const price = points.map((p, i) => `${i ? 'L' : 'M'}${f(p.x)} ${f(p.y)}`).join('')
  const area = `${price}L${f(w)} ${h}L0 ${h}Z`

  // The SMA50 starts once 50 closes exist; break the line on any gap.
  let sma = ''
  let pen = false
  for (const p of points) {
    if (p.sma50 == null) {
      pen = false
      continue
    }
    sma += `${pen ? 'L' : 'M'}${f(p.x)} ${f(y(p.sma50))}`
    pen = true
  }

  const longBands: { x: number; w: number }[] = []
  let start = -1
  for (let i = 0; i <= points.length; i++) {
    const isLong = i < points.length && points[i].long === true
    if (isLong && start < 0) start = i
    if (!isLong && start >= 0) {
      const x0 = Math.max(0, (start - 0.5) * step)
      const x1 = Math.min(w, (i - 0.5) * step)
      longBands.push({ x: f(x0), w: f(x1 - x0) })
      start = -1
    }
  }

  const known = points.filter((p) => p.long != null)
  const longShare = known.length ? known.filter((p) => p.long).length / known.length : 0
  const changePct = (cs[cs.length - 1].close / cs[0].close - 1) * 100

  return { points, price, area, sma, longBands, min, max, longShare, changePct }
}

/** Index of the point nearest to a horizontal position given as a 0..1 fraction of the width. */
export function nearestIndex(frac: number, n: number): number {
  if (n <= 0) return -1
  return Math.round(Math.min(1, Math.max(0, frac)) * (n - 1))
}

/**
 * The rule's state on the last candle and since when it has held it (the
 * first bar of the current run). `bars` counts the bars in the run. Null when
 * the last candle has no state (fewer than 50 closes).
 */
export function currentRun(candles: CandlePoint[]): { state: 'long' | 'flat'; since: string; bars: number } | null {
  const last = candles[candles.length - 1]
  if (!last || last.long == null) return null
  let i = candles.length - 1
  while (i > 0 && candles[i - 1].long === last.long) i--
  return { state: last.long ? 'long' : 'flat', since: candles[i].date, bars: candles.length - i }
}
