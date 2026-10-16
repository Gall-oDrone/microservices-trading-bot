/**
 * Assisted trades on the eToro DEMO account (porting plan P5d): prepare
 * (checks + preview + a two-minute token) and place (confirmation phrase,
 * reason, who). Mirrors controls.ts: the operator token comes from
 * sessionStorage, is sent as a bearer header, remembered on success and
 * forgotten on a 401. ui-api does every check and audits every attempt;
 * the trade token it issues is opaque here and goes back as it came.
 */
import { useMutation, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { ApiError, fetchJSON, useLedgerName, withLedger } from './client'
import {
  tradePlaceResponseSchema,
  tradePreviewSchema,
  type EtoroForwardTest,
  type TradePlaceResponse,
  type TradePreview,
  type TradeSide,
} from './etoroSchemas'
import { getOperatorToken, setOperatorToken } from './controls'

export interface PrepareInput {
  book: string
  side: TradeSide
  /** USD exposure of an open (x1); ignored for a close. */
  amountUsd?: number
  /** The ledger's position to close; ignored for an open. */
  positionId?: string
  /** The operator token (bearer); required by ui-api's admit gate. */
  token: string
}

export function prepareTrade(i: PrepareInput, ledger: string): Promise<TradePreview> {
  const body: Record<string, unknown> = { book: i.book, side: i.side, leverage: 1 }
  if (i.side === 'open') body.amount_usd = i.amountUsd
  else body.position_id = i.positionId
  return fetchJSON(withLedger('/etoro/trades/prepare', ledger), tradePreviewSchema, undefined, post(i.token, body))
}

export interface PlaceInput {
  /** The trade token from the preview (opaque, two-minute TTL, single use). */
  tradeToken: string
  /** The confirmation phrase typed again ("PLACE NSDQ100"). */
  confirm: string
  reason: string
  by: string
  /** The operator token (bearer). */
  token: string
}

export function placeTrade(i: PlaceInput, ledger: string): Promise<TradePlaceResponse> {
  return fetchJSON(
    withLedger('/etoro/trades/place', ledger),
    tradePlaceResponseSchema,
    undefined,
    post(i.token, { token: i.tradeToken, confirm: i.confirm, reason: i.reason.trim(), by: i.by.trim() }),
  )
}

function post(token: string, body: unknown): RequestInit {
  return {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token.trim()}` },
    body: JSON.stringify(body),
  }
}

/** A prepare changes the audit log; a place changes the ledger, the position, the account and the audit log. */
function refreshTradeViews(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: ['controls'] })
  void qc.invalidateQueries({ queryKey: ['etoro'] })
  void qc.invalidateQueries({ queryKey: ['ledgers'] })
}

function rememberToken(token: string) {
  setOperatorToken(token.trim())
}

function forgetRejectedToken(e: unknown) {
  if (e instanceof ApiError && e.status === 401) setOperatorToken('')
}

/** Prepare a trade on the selected (eToro) ledger: the checks, the preview and the token. */
export function usePrepareTradeMutation() {
  const ledger = useLedgerName()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (i: PrepareInput) => prepareTrade(i, ledger),
    onSuccess: (_r, i) => rememberToken(i.token),
    onError: forgetRejectedToken,
    onSettled: () => refreshTradeViews(qc),
  })
}

/** Place a prepared trade. */
export function usePlaceTradeMutation() {
  const ledger = useLedgerName()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (i: PlaceInput) => placeTrade(i, ledger),
    onSuccess: (_r, i) => rememberToken(i.token),
    onError: forgetRejectedToken,
    onSettled: () => refreshTradeViews(qc),
  })
}

export { getOperatorToken }

// --- Pure helpers (vitest) -------------------------------------------------

/** Whole seconds until expires_at (RFC 3339), never negative; 0 for an unparsable time. */
export function secondsLeft(expiresAt: string | undefined, now = Date.now()): number {
  if (!expiresAt) return 0
  const t = Date.parse(expiresAt)
  if (Number.isNaN(t)) return 0
  return Math.max(0, Math.ceil((t - now) / 1000))
}

/** 125 -> "2:05". */
export function fmtCountdown(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

/**
 * A proposal from the rule for one book: what the executor would do at its
 * next run given the rule's latest signal and the demo position, so the
 * operator can do it now (or see there is nothing to do).
 */
export interface Proposal {
  book: string
  symbol: string
  kind: TradeSide | 'none'
  text: string
  ruleSignal: string
  positionText: string
  amountUsd?: number
  positionId?: string
}

/** Derives one proposal per book (etoro/forward-tests): rule signal vs demo position. */
export function deriveProposals(books: EtoroForwardTest[], defaultAmountUsd: number): Proposal[] {
  return books.map((b) => {
    const symbol = b.instrument.symbol || b.book.toUpperCase()
    const pos = b.demo_position
    const long = pos?.state === 'long'
    const positionText = long ? `long ${(pos?.units ?? 0).toFixed(6)} u` : 'flat'
    const rule = b.decision.signal
    const base = { book: b.book, symbol, ruleSignal: rule, positionText }
    if (rule === 'long' && !long) {
      const amount = defaultAmountUsd
      return {
        ...base,
        kind: 'open' as const,
        amountUsd: amount,
        text: `rule says long, demo is flat → open ${amount.toLocaleString('en-US')} USD x1`,
      }
    }
    if (rule === 'flat' && long) {
      return {
        ...base,
        kind: 'close' as const,
        positionId: pos?.position_id,
        text: `rule says flat, demo is long → close position ${pos?.position_id ?? ''}`.trim(),
      }
    }
    if (rule === '') return { ...base, kind: 'none' as const, text: 'no rule record yet, nothing to propose' }
    return {
      ...base,
      kind: 'none' as const,
      text: `in line (rule ${rule}, demo ${long ? 'long' : 'flat'}), nothing to do`,
    }
  })
}
