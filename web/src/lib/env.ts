/**
 * The account environment of the selected ledger (2026-10-22, the
 * real-account canary, docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md §3.4):
 * pure helpers the shell, the Trade page and the tests share. The console
 * must make the REAL account unmistakable: a persistent red bar on every
 * page, a `REAL ·` title prefix, the red venue accent, REAL in every phrase,
 * invested / exposure / the maximum loss in the preview's first line.
 */
import type { Environment } from '../api/schemas'

/** The label next to a ledger name: "REAL" or "demo". */
export function accountLabel(env: Environment | undefined): string {
  return env === 'real' ? 'REAL' : 'demo'
}

/** The document title, prefixed `REAL · ` on the real account. */
export function titleFor(title: string, env: Environment | undefined): string {
  const base = `${title} · Trading Bot Console`
  return env === 'real' ? `REAL · ${base}` : base
}

/**
 * The first line of a preview or a dialog: what is invested, at what
 * leverage, the exposure it buys and, when a stop is set, the most it can
 * lose. `1,000 USD x1 · exposure 1,000 USD` on demo; `50 USD invested · x20 ·
 * exposure 1,000 USD · max loss 24.50 USD` on the canary.
 */
export function sizingLine(p: {
  side: string
  amount_usd: number
  leverage?: number
  exposure_usd?: number
  max_loss_usd?: number
  position_id?: string
  units?: number
}): string {
  const lev = p.leverage ?? 1
  const usd = (v: number, d = 0) =>
    `${v.toLocaleString('en-US', { minimumFractionDigits: d, maximumFractionDigits: d })} USD`
  if (p.side !== 'open') {
    const parts = [`position ${p.position_id ?? '?'}`]
    if (lev > 1) parts.push(`x${lev}`)
    if (p.exposure_usd) parts.push(`exposure ${usd(p.exposure_usd)}`)
    return parts.join(' · ')
  }
  const parts = [lev > 1 ? `${usd(p.amount_usd)} invested` : usd(p.amount_usd), `x${lev}`]
  const exposure = p.exposure_usd ?? p.amount_usd * lev
  parts.push(`exposure ${usd(exposure)}`)
  if (p.max_loss_usd && p.max_loss_usd > 0) parts.push(`max loss ${usd(p.max_loss_usd, 2)}`)
  return parts.join(' · ')
}

/** The amount field's label: invested at the leverage, with the exposure it buys. */
export function amountLabel(leverage: number): string {
  return leverage > 1 ? `Invested (USD, x${leverage})` : 'Amount (USD, x1)'
}

/** The favicon: the console's mark, with a red dot on the real account. */
export function faviconFor(env: Environment | undefined): string {
  const dot =
    env === 'real' ? "%3Ccircle cx='19' cy='5' r='5' fill='%23ff3b4e' stroke='white' stroke-width='1.5'/%3E" : ''
  return (
    "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'%3E%3Crect width='24' height='24' rx='6' fill='%2321a38c'/%3E%3Cpath d='M4 16l5-5 3.5 3.5L20 7' stroke='white' stroke-width='2.2' fill='none' stroke-linecap='round' stroke-linejoin='round'/%3E" +
    dot +
    '%3C/svg%3E'
  )
}
