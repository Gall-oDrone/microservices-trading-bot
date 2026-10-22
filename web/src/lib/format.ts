/**
 * Formatting helpers. Bitso daily bars are Mexico City days, eToro index
 * CFDs trade on New York time, so each venue has its own clock (fmtMx, fmtNY).
 */

const MX_TZ = 'America/Mexico_City'
export const NY_TZ = 'America/New_York'

/**
 * The eToro index-CFD books (porting plan): symbol, quote currency and price
 * precision. Bitso books (btc_mxn, btc_usd) are not here: their quote comes
 * from the book name and their prices are whole pesos / dollars.
 */
export const CFD_BOOKS: Record<string, { symbol: string; label: string; quote: string; digits: number }> = {
  nsdq100: { symbol: 'NSDQ100', label: 'NSDQ100 · Nasdaq-100 CFD', quote: 'usd', digits: 2 },
  spx500: { symbol: 'SPX500', label: 'SPX500 · S&P 500 CFD', quote: 'usd', digits: 2 },
}

const QUOTE_DIGITS: Record<string, number> = { mxn: 0, usd: 0 }

/** Decimals a price is shown with: whole MXN/USD on Bitso, cents on the index CFDs. */
export function quoteDigits(quote: string, book?: string): number {
  if (book && CFD_BOOKS[book]) return CFD_BOOKS[book].digits
  return QUOTE_DIGITS[quote.toLowerCase()] ?? 2
}

export function fmtPrice(v: number, quote: string, book?: string): string {
  const d = quoteDigits(quote, book)
  return v.toLocaleString('en-US', { minimumFractionDigits: d, maximumFractionDigits: d })
}

export function fmtMoney(v: number, quote: string, digits = 2): string {
  return `${v.toLocaleString('en-US', { minimumFractionDigits: digits, maximumFractionDigits: digits })} ${quote.toUpperCase()}`
}

export function fmtBTC(v: number): string {
  return `${v.toFixed(8)} BTC`
}

/** CFD instrument units (eToro quotes them to six decimals): 0.140735 -> "0.140735 u". */
export function fmtUnits(v: number): string {
  return `${v.toFixed(6)} u`
}

/** An eToro demo position in words: "flat" or "long 0.140735 u". */
export function positionText(p: { state: string; units?: number } | null | undefined): string {
  if (!p || p.state !== 'long' || !p.units) return 'flat'
  return `long ${fmtUnits(p.units)}`
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

/**
 * A day change in quote units and percent, as a venue dashboard shows it
 * next to the price: { change: 330.1, change_pct: 1.12 } -> "+330.10 (+1.12%)".
 */
export function fmtDayChange(dc: { change: number; change_pct: number }, quote: string, book?: string): string {
  const sign = dc.change > 0 ? '+' : dc.change < 0 ? '-' : ''
  return `${sign}${fmtPrice(Math.abs(dc.change), quote, book)} (${fmtPct(dc.change_pct, 2)})`
}

export function fmtBps(v: number, signed = false): string {
  const r = Math.round(v) === 0 ? 0 : v // no "-0 bps"
  const s = r.toFixed(0)
  return signed && r > 0 ? `+${s} bps` : `${s} bps`
}

/** "2026-10-01T01:00:36Z" -> "30 Sep 2026, 19:00" in Mexico City. */
export function fmtMx(iso: string): string {
  return fmtIn(iso, MX_TZ)
}

/** "2026-10-19T13:35:00Z" -> "19 Oct 2026, 09:35" in New York (EDT or EST as the date falls). */
export function fmtNY(iso: string): string {
  return fmtIn(iso, NY_TZ)
}

export function fmtIn(iso: string, timeZone: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('en-GB', {
    timeZone,
    day: '2-digit',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

/** Only the clock: "2026-10-19T13:35:00Z" -> "09:35" in New York. */
export function fmtNYTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleTimeString('en-GB', { timeZone: NY_TZ, hour: '2-digit', minute: '2-digit', hour12: false })
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

/** "btc_mxn" -> "BTC / MXN"; "nsdq100" -> "NSDQ100 · Nasdaq-100 CFD". */
export function bookLabel(book: string): string {
  return CFD_BOOKS[book]?.label ?? book.replace('_', ' / ').toUpperCase()
}

/** The short form for table headers and chips: "nsdq100" -> "NSDQ100", "btc_mxn" -> "BTC / MXN". */
export function bookSymbol(book: string): string {
  return CFD_BOOKS[book]?.symbol ?? bookLabel(book)
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

/** Whole seconds since an ISO time, never negative (the clock the caller passes ticks once a second). */
export function quoteAgeSeconds(iso: string | undefined, now: number): number | null {
  if (!iso) return null
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return null
  return Math.max(0, Math.round((now - t) / 1000))
}
