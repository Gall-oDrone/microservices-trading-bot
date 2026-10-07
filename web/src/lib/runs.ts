import type { RuleResult, RunSummary, RunWindow } from '../api/schemas'

/** "trend_sma50" -> "Trend SMA50", "buy_and_hold" -> "Buy and hold". */
export function ruleLabel(rule: string): string {
  const s = rule
    .replace(/_/g, ' ')
    .replace(/\bsma(\d+)\b/i, 'SMA$1')
    .trim()
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export const HOLD = 'buy_and_hold'

/** A window's short name: "2025" for a calendar year, else "2018-01 → 2026-09". */
export function windowLabel(w: { from: string; to: string }): string {
  const fy = w.from.slice(0, 4)
  const ty = w.to.slice(0, 4)
  if (fy === ty && w.from.endsWith('-01-01') && w.to.endsWith('-12-31')) return fy
  if (fy === ty) return `${fy} ${w.from.slice(5)} → ${w.to.slice(5)}`
  return `${w.from.slice(0, 7)} → ${w.to.slice(0, 7)}`
}

const DAY = 86_400_000
export function windowDays(w: { from: string; to: string }): number {
  return Math.round((Date.parse(w.to) - Date.parse(w.from)) / DAY) + 1
}

/**
 * Window indexes in reading order: windows up to about a year, oldest first,
 * then the longer (multi-year) windows. The tool's order puts the in-sample
 * window first, which reads poorly as a time axis.
 */
export function windowOrder(ws: { from: string; to: string }[]): number[] {
  return ws
    .map((w, i) => ({ i, long: windowDays(w) > 370, from: w.from, to: w.to }))
    .sort((a, b) => Number(a.long) - Number(b.long) || a.from.localeCompare(b.from) || a.to.localeCompare(b.to))
    .map((x) => x.i)
}

/** "176 bps round trip" or "frictionless". */
export function costLabel(c: RunSummary['costs']): string {
  return c.round_trip_bps === 0 ? 'frictionless' : `${c.round_trip_bps} bps round trip`
}

/** Commission and slippage per leg, as the text evidence prints them. */
export function costDetail(c: RunSummary['costs']): string {
  return `buy ${c.buy_bps} + sell ${c.sell_bps} bps commission, ${c.slippage_bps} bps slippage per leg`
}

/** Percentage points, signed, one decimal: "+10.6 pp". */
export function fmtPP(v: number): string {
  const s = Math.abs(v) >= 1000 ? Math.round(v).toLocaleString('en-US') : v.toFixed(1)
  return `${v > 0 ? '+' : v < 0 ? '' : '±'}${s} pp`.replace('-', '−')
}

/** Percent, signed, one or two decimals. */
export function fmtRet(v: number): string {
  const s =
    Math.abs(v) >= 1000
      ? Math.round(Math.abs(v)).toLocaleString('en-US')
      : Math.abs(v).toFixed(Math.abs(v) >= 100 ? 1 : 2)
  return `${v > 0 ? '+' : v < 0 ? '−' : ''}${s}%`
}

/**
 * Colour intensity for a heat-map cell, 0..1, on a log scale so a 900 pp
 * window doesn't wash out the 5 pp ones.
 */
export function heat(v: number, maxAbs: number): number {
  if (maxAbs <= 0 || v === 0) return 0
  return Math.min(1, Math.log1p(Math.abs(v)) / Math.log1p(maxAbs))
}

/** The best (highest-return) rule in a window, other than holding. */
export function bestRule(w: RunWindow): RuleResult | undefined {
  return w.results.filter((r) => r.rule !== HOLD).sort((a, b) => b.return_pct - a.return_pct)[0]
}

/**
 * Runs that differ from `run` only in costs: same evidence folder, same
 * price data and the same windows. Sorted by round-trip cost, cheapest first,
 * including `run` itself.
 */
export function costSiblings(run: RunSummary, all: RunSummary[]): RunSummary[] {
  const key = (r: RunSummary) =>
    [
      r.date,
      r.tool,
      r.data.prices,
      r.data.book ?? '',
      r.data.bars,
      r.windows.map((w) => `${w.from}:${w.to}`).join(','),
    ].join('|')
  const k = key(run)
  return all
    .filter((r) => key(r) === k)
    .sort((a, b) => a.costs.round_trip_bps - b.costs.round_trip_bps || a.name.localeCompare(b.name))
}

/** Every word of `q` appears in the run's name, data, cost label or citing studies. */
export function matchesRun(r: RunSummary, q: string): boolean {
  const words = q.toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return true
  const hay =
    `${r.name} ${r.data.prices} ${r.data.book ?? ''} ${costLabel(r.costs)} ${r.studies.join(' ')}`.toLowerCase()
  return words.every((w) => hay.includes(w))
}
