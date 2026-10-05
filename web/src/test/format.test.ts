import { describe, expect, it } from 'vitest'
import { ago, bookLabel, fmtBps, fmtDate, fmtDuration, fmtFrac, fmtMx, fmtPct } from '../lib/format'

describe('format', () => {
  it('formats dates without timezone drift', () => {
    expect(fmtDate('2026-10-01')).toBe('1 Oct 2026')
  })
  it('shows times in Mexico City', () => {
    // 01:00 UTC on 1 Oct is 19:00 on 30 Sep in Mexico City (UTC-6).
    expect(fmtMx('2026-10-01T01:00:36Z')).toMatch(/30 Sept? 2026, 19:00/)
  })
  it('formats percentages and bps', () => {
    expect(fmtPct(4.5)).toBe('+4.50%')
    expect(fmtPct(-1.234, 1)).toBe('-1.2%')
    expect(fmtFrac(0.0177)).toBe('1.77%')
    expect(fmtBps(40.4, true)).toBe('+40 bps')
  })
  it('formats durations and labels', () => {
    expect(fmtDuration('2026-10-01T00:58:04Z', '2026-10-01T01:58:15Z')).toBe('1h 00m')
    expect(fmtDuration('2026-10-01T00:58:04Z', '2026-10-01T01:00:36Z')).toBe('3m')
    expect(bookLabel('btc_mxn')).toBe('BTC / MXN')
    expect(ago('2026-10-01T00:00:00Z', Date.parse('2026-10-01T00:30:00Z'))).toBe('30 min ago')
  })
})
