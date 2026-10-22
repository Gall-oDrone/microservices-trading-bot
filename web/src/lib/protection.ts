/**
 * Protection helpers shared by the Trade page components and tests
 * (kept out of the component file for fast refresh).
 */
import type { StopProfile, Volatility } from '../api/etoroSchemas'
import { fmtFrac } from './format'

export const PROFILES: { id: StopProfile; label: string; hint: string }[] = [
  { id: 'conservative', label: 'Conservative', hint: 'SL 1.5×ATR · TP 2.25×ATR · 1.5R' },
  { id: 'moderate', label: 'Moderate', hint: 'SL 2×ATR · TP 4×ATR · 2R' },
  { id: 'aggressive', label: 'Aggressive', hint: 'SL 3×ATR · TP 6×ATR · 2R' },
  { id: 'custom', label: 'Custom', hint: 'your own distances, still guarded' },
]

/** 0.0105 -> "1.05%" without a sign; undefined -> "—". */
export function pct(v: number | undefined, digits = 2): string {
  return v === undefined ? '—' : fmtFrac(v, digits)
}

export function regimeTone(r: Volatility['regime']): 'ok' | 'info' | 'warn' | 'block' | 'flat' {
  switch (r) {
    case 'calm':
      return 'ok'
    case 'normal':
      return 'info'
    case 'elevated':
      return 'warn'
    case 'extreme':
      return 'block'
    default:
      return 'flat'
  }
}

/**
 * The policy's stop caps in words (2026-10-22): the demo policy caps the
 * distance at 10 % and the loss at 0.5 % of equity; the real canary's holds
 * it between 2.3 % and 2.45 % with no equity cap (the account is the canary).
 */
export function policyCapsSentence(limits: {
  min_sl_pct: number
  max_sl_pct: number
  max_loss_pct_equity: number
}): string {
  const whole = (v: number) => (Math.abs(v * 100 - Math.round(v * 100)) < 1e-9 ? 0 : 2)
  const distance =
    limits.min_sl_pct > 0
      ? `between ${pct(limits.min_sl_pct, whole(limits.min_sl_pct))} and ${pct(limits.max_sl_pct, whole(limits.max_sl_pct))} from the entry`
      : `at most ${pct(limits.max_sl_pct, whole(limits.max_sl_pct))} from the entry`
  const equity =
    limits.max_loss_pct_equity > 0
      ? `${pct(limits.max_loss_pct_equity)} of equity at the stop`
      : 'no equity cap: the whole account is the position'
  return `${distance} and ${equity}`
}
