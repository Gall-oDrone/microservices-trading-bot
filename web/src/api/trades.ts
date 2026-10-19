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
  isProtected,
  tradePlaceResponseSchema,
  tradePreviewSchema,
  tradeRecommendationSchema,
  type EtoroForwardTest,
  type StopProfile,
  type TradePlaceResponse,
  type TradePreview,
  type TradeRecommendation,
  type TradeSide,
} from './etoroSchemas'
import { getOperatorToken, setOperatorToken } from './controls'

export interface PrepareInput {
  book: string
  side: TradeSide
  /** USD exposure of an open (x1); ignored for a close or a protect. */
  amountUsd?: number
  /** The ledger's position to close or protect; ignored for an open. */
  positionId?: string
  /**
   * Protection (opens and protects, 2026-10-19): a named profile, or
   * 'custom' with the distances as fractions of the entry (tpPct 0 = none).
   * Omitted: ui-api applies the default profile and says so.
   */
  profile?: StopProfile
  stopLossPct?: number
  takeProfitPct?: number
  /** The operator token (bearer); required by ui-api's admit gate. */
  token: string
}

export function prepareTrade(i: PrepareInput, ledger: string): Promise<TradePreview> {
  const body: Record<string, unknown> = { book: i.book, side: i.side, leverage: 1 }
  if (i.side === 'open') body.amount_usd = i.amountUsd
  else body.position_id = i.positionId
  if (i.side !== 'close' && i.profile) {
    body.profile = i.profile
    if (i.profile === 'custom') {
      body.stop_loss_pct = i.stopLossPct
      if (i.takeProfitPct) body.take_profit_pct = i.takeProfitPct
    }
  }
  return fetchJSON(withLedger('/etoro/trades/prepare', ledger), tradePreviewSchema, undefined, post(i.token, body))
}

export interface RecommendInput {
  book: string
  side: 'open' | 'protect'
  amountUsd?: number
  positionId?: string
  token: string
}

/** The stop / target levels per profile for an open or the held position (read-only). */
export function recommendTrade(i: RecommendInput, ledger: string): Promise<TradeRecommendation> {
  const body: Record<string, unknown> = { book: i.book, side: i.side }
  if (i.side === 'open' && i.amountUsd) body.amount_usd = i.amountUsd
  if (i.side === 'protect' && i.positionId) body.position_id = i.positionId
  return fetchJSON(
    withLedger('/etoro/trades/recommend', ledger),
    tradeRecommendationSchema,
    undefined,
    post(i.token, body),
  )
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

/** Recommend stop / target levels (nothing is sent, nothing is audited unless refused). */
export function useRecommendTradeMutation() {
  const ledger = useLedgerName()
  return useMutation({
    mutationFn: (i: RecommendInput) => recommendTrade(i, ledger),
    onSuccess: (_r, i) => rememberToken(i.token),
    onError: forgetRejectedToken,
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
 * operator can do it now (or see there is nothing to do). Since 2026-10-19
 * a held position without a stop loss is a 'protect' proposal: the
 * position is in line with the rule but unprotected.
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
  /** Set on a held position: whether it carries a stop at eToro (per the ledger). */
  protected?: boolean
}

/** Derives one proposal per book (etoro/forward-tests): rule signal vs demo position. */
export function deriveProposals(books: EtoroForwardTest[], defaultAmountUsd: number): Proposal[] {
  return books.map((b) => {
    const symbol = b.instrument.symbol || b.book.toUpperCase()
    const pos = b.demo_position
    const long = pos?.state === 'long'
    const positionText = long ? `long ${(pos?.units ?? 0).toFixed(6)} u` : 'flat'
    const rule = b.decision.signal
    const base = {
      book: b.book,
      symbol,
      ruleSignal: rule,
      positionText,
      protected: long ? isProtected(pos) : undefined,
    }
    if (rule === 'long' && !long) {
      const amount = defaultAmountUsd
      return {
        ...base,
        kind: 'open' as const,
        amountUsd: amount,
        text: `rule says long, demo is flat → open ${amount.toLocaleString('en-US')} USD x1 (with a stop)`,
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
    if (long && !isProtected(pos)) {
      return {
        ...base,
        kind: 'protect' as const,
        positionId: pos?.position_id,
        text: `in line (rule long, demo long) but position ${pos?.position_id ?? ''} has no stop loss → protect it`.replace(
          '  ',
          ' ',
        ),
      }
    }
    return {
      ...base,
      kind: 'none' as const,
      text: `in line (rule ${rule}, demo ${long ? 'long' : 'flat'}), nothing to do`,
    }
  })
}
