/**
 * Pure helpers for the Market page: depth ladder, book/tape imbalance and
 * the 20-day volume context. Display only; nothing here feeds a decision.
 */
import type { CandlePoint, Level, TapeTrade } from '../api/schemas'

/** "btc_mxn" -> "mxn". */
export function quoteOf(book: string): string {
  return book.split('_')[1] ?? ''
}

/** BTC size, trimmed to what matters for retail-sized levels: 0.00032901 -> "0.000329". */
export function fmtSize(v: number): string {
  if (!Number.isFinite(v)) return '—'
  return v >= 1 ? v.toFixed(4) : v.toFixed(6)
}

export interface LadderRow extends Level {
  /** Running total from the best price outwards. */
  cum: number
  /** cum / the deepest cumulative total on either side shown, 0..1, for the depth bar. */
  share: number
}

export interface Ladder {
  bids: LadderRow[]
  asks: LadderRow[]
  /** Sum of the shown bid and ask sizes. */
  bidTotal: number
  askTotal: number
  /** bidTotal / (bidTotal + askTotal); 0.5 when both are empty. */
  bidShare: number
}

function cumulate(levels: Level[], rows: number): Omit<LadderRow, 'share'>[] {
  let cum = 0
  return levels.slice(0, rows).map((l) => {
    cum += l.amount
    return { ...l, cum }
  })
}

/**
 * The depth ladder: the best `rows` levels per side (already sorted best
 * first by ui-api), with cumulative size and a shared bar scale so both sides
 * compare at a glance.
 */
export function ladder(bids: Level[], asks: Level[], rows = 12): Ladder {
  const b = cumulate(bids, rows)
  const a = cumulate(asks, rows)
  const bidTotal = b.at(-1)?.cum ?? 0
  const askTotal = a.at(-1)?.cum ?? 0
  const max = Math.max(bidTotal, askTotal)
  const withShare = (r: Omit<LadderRow, 'share'>): LadderRow => ({ ...r, share: max > 0 ? r.cum / max : 0 })
  const total = bidTotal + askTotal
  return {
    bids: b.map(withShare),
    asks: a.map(withShare),
    bidTotal,
    askTotal,
    bidShare: total > 0 ? bidTotal / total : 0.5,
  }
}

export interface TapeStats {
  count: number
  /** Taker-buy share of the traded size, 0..1 (0.5 when empty). */
  buyShare: number
  buyVolume: number
  sellVolume: number
  /** Size-weighted average price of the tape, or null when empty. */
  vwap: number | null
  /** Oldest and newest trade times (ISO), or null when empty. */
  from: string | null
  to: string | null
}

/** Who was aggressive on the recent tape: taker buys lift the ask, taker sells hit the bid. */
export function tapeStats(trades: TapeTrade[]): TapeStats {
  let buy = 0
  let sell = 0
  let notional = 0
  for (const t of trades) {
    if (t.side === 'buy') buy += t.amount
    else sell += t.amount
    notional += t.price * t.amount
  }
  const vol = buy + sell
  const times = trades.map((t) => t.at).sort()
  return {
    count: trades.length,
    buyShare: vol > 0 ? buy / vol : 0.5,
    buyVolume: buy,
    sellVolume: sell,
    vwap: vol > 0 ? notional / vol : null,
    from: times[0] ?? null,
    to: times.at(-1) ?? null,
  }
}

export interface VolumeContext {
  /** Latest closed daily bar. */
  date: string
  volume: number
  /** Latest bar's volume over its 20-day average (null before 20 bars). */
  ratio: number | null
  /** 20-day average volume implied by the ratio, or the plain mean of the last 20 bars. */
  avg20: number | null
  /** Bars in the window with volume_ratio_20d >= 1.5 (the chart's highlighted bars). */
  highDays: number
}

/** The 20-day volume context of the newest closed bar. */
export function volumeContext(candles: CandlePoint[]): VolumeContext | null {
  const last = candles.at(-1)
  if (!last) return null
  const ratio = last.volume_ratio_20d
  let avg20: number | null = null
  if (ratio != null && ratio > 0) avg20 = last.volume / ratio
  else if (candles.length >= 20) avg20 = candles.slice(-20).reduce((s, c) => s + c.volume, 0) / 20
  return {
    date: last.date,
    volume: last.volume,
    ratio,
    avg20,
    highDays: candles.filter((c) => (c.volume_ratio_20d ?? 0) >= 1.5).length,
  }
}
