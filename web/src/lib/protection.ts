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
