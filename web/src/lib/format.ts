/** Formatting helpers. Daily bars are Mexico City days, so times show there. */

const MX_TZ = 'America/Mexico_City'

const quoteDigits: Record<string, number> = { mxn: 0, usd: 0 }

export function fmtPrice(v: number, quote: string): string {
  const d = quoteDigits[quote.toLowerCase()] ?? 2
  return v.toLocaleString('en-US', { minimumFractionDigits: d, maximumFractionDigits: d })
}

export function fmtMoney(v: number, quote: string, digits = 2): string {
  return `${v.toLocaleString('en-US', { minimumFractionDigits: digits, maximumFractionDigits: digits })} ${quote.toUpperCase()}`
}

export function fmtBTC(v: number): string {
  return `${v.toFixed(8)} BTC`
}

/** 0.0123 -> "1.23%". */
export function fmtFrac(v: number, digits = 2): string {
  return `${(v * 100).toFixed(digits)}%`
}

/** Already a percentage: 4.5 -> "+4.50%". */
export function fmtPct(v: number, digits = 2, signed = true): string {
  const s = v.toFixed(digits)
  return signed && v > 0 ? `+${s}%` : `${s}%`
}

export function fmtBps(v: number, signed = false): string {
  const s = v.toFixed(0)
  return signed && v > 0 ? `+${s} bps` : `${s} bps`
}

/** "2026-10-01T01:00:36Z" -> "30 Sep 2026, 19:00" in Mexico City. */
export function fmtMx(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('en-GB', {
    timeZone: MX_TZ,
    day: '2-digit',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

export function fmtUTC(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? iso
    : d
        .toISOString()
        .replace('T', ' ')
        .replace(/\.\d+Z$/, 'Z')
}

/** "2026-10-01" -> "1 Oct 2026". Pure date, no timezone shift. */
export function fmtDate(day: string): string {
  const [y, m, d] = day.split('-').map(Number)
  if (!y || !m || !d) return day
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString('en-GB', {
    timeZone: 'UTC',
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  })
}

/** Minutes between two ISO times, as "2h 01m". */
export function fmtDuration(fromIso: string, toIso: string): string {
  const ms = new Date(toIso).getTime() - new Date(fromIso).getTime()
  if (!Number.isFinite(ms) || ms < 0) return '—'
  const m = Math.round(ms / 60_000)
  return m < 60 ? `${m}m` : `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, '0')}m`
}

export function bookLabel(book: string): string {
  return book.replace('_', ' / ').toUpperCase()
}

export function ago(iso: string, now = Date.now()): string {
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  if (s < 90) return `${s}s ago`
  const m = Math.round(s / 60)
  if (m < 90) return `${m} min ago`
  const h = Math.round(m / 60)
  if (h < 48) return `${h} h ago`
  return `${Math.round(h / 24)} days ago`
}
