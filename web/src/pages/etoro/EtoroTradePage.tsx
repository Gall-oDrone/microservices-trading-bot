/**
 * Trade (porting plan P5d): assisted prepare → place on the eToro DEMO
 * account. Proposals come from the rule (etoro/forward-tests: the rule's
 * signal against the demo position) or are typed by hand; prepare runs
 * ui-api's checks and returns a preview with a two-minute token; place goes
 * through the shared ConfirmDialog (reason, who, the phrase "PLACE NSDQ100"
 * typed again, the operator token). ui-api does every check, writes the
 * intent first, sends, appends the ledger line with source "ui" and audits
 * every step; this page only collects input and shows what came back.
 *
 * The executor plans from the ledger: a position the operator leaves
 * against the rule is brought back to the rule's target at the next 09:35
 * New York run unless the ledger is halted. The page says so everywhere a
 * trade is offered.
 *
 * Cancel (2026-10-21): a live preview can be discarded with its Cancel
 * button or Escape; ui-api burns the token (POST /trades/cancel) and audits
 * trade.cancel, so a stray later place is refused and the decision not to
 * trade is on record. Blocked, expired or refused previews hold no usable
 * token and are dismissed locally.
 */
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router'
import { ApiError, useControls, useEtoroForwardTests, useLedgerName } from '../../api/client'
import { useLiveStream } from '../../api/live'
import { LiveQuoteLine } from '../../components/live'
import { PositionsCard } from '../../components/etoro/positions'
import type {
  EtoroForwardTest,
  StopProfile,
  TradeCancelResponse,
  TradePlaceResponse,
  TradePreview,
  TradeRecommendation,
  TradeSide,
} from '../../api/etoroSchemas'
import { tradePlaceResponseSchema } from '../../api/etoroSchemas'
import type { TradingInfo } from '../../api/schemas'
import {
  deriveProposals,
  fmtCountdown,
  getOperatorToken,
  secondsLeft,
  useCancelTradeMutation,
  usePlaceTradeMutation,
  usePrepareTradeMutation,
  useRecommendTradeMutation,
  type Proposal,
} from '../../api/trades'
import { ConfirmDialog, Field } from '../../components/ConfirmDialog'
import { AuditLog } from '../../components/controls'
import { SessionClock } from '../../components/etoro/parts'
import { ProtectionFacts, ProtectionPanel } from '../../components/etoro/protection'
import {
  IconAlert,
  IconArrowRight,
  IconBlock,
  IconCheck,
  IconRefresh,
  IconShield,
  IconTrend,
} from '../../components/icons'
import { Badge, Banner, CardSkeleton, ErrorState, LedgerBadge, Stat } from '../../components/ui'
import { bookLabel, bookSymbol, fmtBps, fmtMoney, fmtNY, fmtPrice, fmtUnits, fmtUTC } from '../../lib/format'
import { amountLabel, sizingLine } from '../../lib/env'
import { useClock } from '../../lib/tz'
import { usePageTitle } from '../../lib/usePageTitle'
import { PageIntro } from '../../components/PageIntro'

/** Whole seconds until the token expires, ticking once a second. */
function useCountdown(expiresAt: string | undefined): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!expiresAt) return
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [expiresAt])
  return secondsLeft(expiresAt, now)
}

/** The executor's books; a ?book= outside this list falls back to the first. */
const BOOK_IDS = ['nsdq100', 'spx500']

/** The outcome kept in sessionStorage for the session, or null when absent or unparsable. */
function readOutcome(key: string): TradePlaceResponse | null {
  try {
    const raw = sessionStorage.getItem(key)
    if (!raw) return null
    const r = tradePlaceResponseSchema.safeParse(JSON.parse(raw))
    return r.success ? r.data : null
  } catch {
    return null
  }
}

const DEFAULT_TRADING: TradingInfo = {
  enabled: false,
  disabled_reason: 'this ui-api does not report the assisted trades (older build)',
  token_ttl_s: 120,
  min_amount_usd: 1000,
  max_amount_usd: 1500,
  default_amount_usd: 1100,
  environment: 'demo',
  leverage_options: [1],
  default_leverage: 1,
  policy_version: '',
  books: [],
  rehearsal: false,
}

interface Form {
  book: string
  side: TradeSide
  amount: string
  positionId: string
  /** Protection (opens and protects): the profile, and the custom distances in percent. */
  profile: StopProfile
  slPct: string
  tpPct: string
}

/**
 * The note shown wherever a trade is offered or reported: on a demo cell's
 * ledger what the executor does next; on the real canary's (or the
 * rehearsal's) ledger that nothing automatic ever acts on it.
 */
function ExecutorNote({ trading }: { trading?: TradingInfo }) {
  if (trading && (trading.environment === 'real' || trading.rehearsal)) {
    return (
      <p className="trade-note" data-testid="trade-executor-note">
        <IconAlert /> No executor acts on this ledger: nothing opens, closes, ratchets or re-enters on its own. A
        position stays until you close it here or eToro closes it at its stop; a venue close is recorded by hand
        (etoro-reconcile -record-venue-close). At most three opens in the window, each by hand.
      </p>
    )
  }
  return (
    <p className="trade-note" data-testid="trade-executor-note">
      <IconAlert /> The executor plans from the ledger: a position left against the rule is brought back to the
      rule&apos;s target at the next 09:35 New York run unless the ledger is halted. One leg per instrument per trading
      day is policy, whoever sends it.
    </p>
  )
}

function Reasons({ items, tone, testId }: { items: string[]; tone: 'block' | 'warn'; testId: string }) {
  if (items.length === 0) return null
  return (
    <ul className={`trade-reasons ${tone}`} data-testid={testId}>
      {items.map((r) => (
        <li key={r}>
          {tone === 'block' ? <IconBlock /> : <IconAlert />}
          <span>{r}</span>
        </li>
      ))}
    </ul>
  )
}

function Proposals({
  books,
  trading,
  busy,
  onPropose,
}: {
  books: EtoroForwardTest[]
  trading: TradingInfo
  busy: boolean
  onPropose: (p: Proposal) => void
}) {
  const proposals = deriveProposals(books, trading.default_amount_usd)
  return (
    <section className="card venue-etoro" aria-labelledby="trade-proposals-h" data-testid="trade-proposals">
      <div className="card-head">
        <div>
          <h2 id="trade-proposals-h">
            <IconTrend className="controls-h-icon" /> Proposals from the rule
          </h2>
          <div className="sub">
            The rule&apos;s latest signal against the demo position: what the executor would do at its next run.
          </div>
        </div>
      </div>
      <div className="trade-proposals">
        {proposals.map((p) => (
          <div
            className="panel trade-proposal"
            key={p.book}
            data-testid={`trade-proposal-${p.book}`}
            data-kind={p.kind}
            data-protected={p.protected}
          >
            <div className="trade-proposal-head">
              <b>{p.symbol}</b>
              <Badge tone={p.ruleSignal === 'long' ? 'long' : 'flat'}>rule {p.ruleSignal || 'n/a'}</Badge>
              <span className="faint">demo {p.positionText}</span>
              {p.protected !== undefined && (
                <Badge
                  tone={p.protected ? 'ok' : 'block'}
                  dot
                  title={p.protected ? 'a stop loss rests at eToro' : 'no stop loss at eToro (per the ledger)'}
                >
                  <span data-testid={`trade-protected-${p.book}`}>{p.protected ? 'protected' : 'no stop'}</span>
                </Badge>
              )}
            </div>
            <div
              className={`trade-proposal-text ${p.kind === 'protect' ? 'protect' : ''}`}
              data-testid={`trade-proposal-text-${p.book}`}
            >
              {p.kind === 'none' ? (
                <IconCheck style={{ color: 'var(--long)' }} />
              ) : p.kind === 'protect' ? (
                <IconShield />
              ) : (
                <IconArrowRight />
              )}
              {p.text}
            </div>
            {p.kind !== 'none' && trading.enabled && (
              <button
                type="button"
                className={`btn ${p.kind === 'protect' ? 'btn-primary' : 'btn-ghost'}`}
                id={`trade-propose-${p.book}`}
                data-testid={`trade-propose-${p.book}`}
                disabled={busy}
                onClick={() => onPropose(p)}
              >
                {p.kind === 'open'
                  ? `Prepare open ${p.amountUsd?.toLocaleString('en-US')} USD`
                  : p.kind === 'protect'
                    ? 'Prepare protection'
                    : 'Prepare close'}
              </button>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}

function PreviewCard({
  pv,
  left,
  refused,
  placing,
  cancelling,
  cancelError,
  trading,
  onPlace,
  onReprepare,
  onCancel,
  onDismiss,
}: {
  pv: TradePreview
  left: number
  refused: string | null
  placing: boolean
  cancelling: boolean
  cancelError: string | null
  trading?: TradingInfo
  onPlace: () => void
  onReprepare: () => void
  /** Burns the token at ui-api (a live allow preview only). */
  onCancel: () => void
  /** Drops a preview that holds no usable token (blocked, expired, refused): local only. */
  onDismiss: () => void
}) {
  const allow = pv.verdict === 'allow'
  const expired = allow && left === 0
  const canPlace = allow && !expired && !refused && !!pv.token
  const ttl = pv.expires_in_s ?? 120
  const quote = 'usd'
  const real = pv.environment === 'real'
  return (
    <section
      className={`card venue-etoro trade-preview ${allow ? 'allow' : 'block'}`}
      aria-labelledby="trade-preview-h"
      data-testid="trade-preview"
      data-verdict={pv.verdict}
      data-expired={expired}
    >
      <div className="card-head">
        <div>
          <h2 id="trade-preview-h">
            Preview · {pv.side} {real ? 'REAL ' : ''}
            {pv.instrument.symbol}
          </h2>
          <div className="sub">
            <span className="trade-sizing" data-testid="trade-sizing">
              {sizingLine(pv)}
            </span>{' '}
            · {fmtUnits(pv.units)} · prepared <span title={fmtUTC(pv.prepared_at)}>{fmtNY(pv.prepared_at)} NY</span> ·
            ref <span className="num">{pv.client_ref}</span>
          </div>
        </div>
        <div className="row" style={{ gap: 6 }}>
          {real && (
            <Badge tone="real" dot>
              <span data-testid="trade-preview-real">REAL</span>
            </Badge>
          )}
          <Badge tone={allow ? 'ok' : 'block'} dot title={pv.request_id}>
            <span data-testid="trade-verdict">{allow ? 'allowed' : 'blocked'}</span>
          </Badge>
        </div>
      </div>

      <Reasons items={pv.reasons} tone="block" testId="trade-reasons" />
      <Reasons items={pv.warnings} tone="warn" testId="trade-warnings" />

      {pv.protection && <ProtectionFacts p={pv.protection} book={pv.book} />}

      <div className="trade-facts">
        <Stat
          label="Quote"
          value={
            pv.quote ? (
              <span className="num">
                {fmtPrice(pv.quote.bid, quote, pv.book)} / {fmtPrice(pv.quote.ask, quote, pv.book)}
              </span>
            ) : (
              '—'
            )
          }
          hint={pv.quote ? `spread ${fmtBps(pv.quote.spread_bps)} · ${fmtNY(pv.quote.at)} NY` : 'no quote'}
        />
        <Stat
          label="Units"
          value={<span className="num">{fmtUnits(pv.units)}</span>}
          hint={`at the ${pv.side === 'open' ? 'ask' : 'bid'} · ref close ${fmtPrice(pv.ref_price, quote, pv.book)}`}
        />
        <Stat
          label="Costs"
          value={
            pv.costs ? (
              <span className="num" data-testid="trade-costs">
                spread {fmtMoney(pv.costs.spread, 'usd')}
              </span>
            ) : pv.side === 'close' ? (
              'none to preview'
            ) : pv.side === 'protect' ? (
              'none'
            ) : (
              '—'
            )
          }
          hint={
            pv.costs
              ? `overnight ${fmtMoney(pv.costs.overnight_fee, 'usd')} per night · weekend ${fmtMoney(pv.costs.weekend_fee, 'usd')}${
                  pv.costs.markup || pv.costs.transaction_fee
                    ? ` · markup ${fmtMoney(pv.costs.markup, 'usd')} · fee ${fmtMoney(pv.costs.transaction_fee, 'usd')}`
                    : ''
                }`
              : pv.side === 'close'
                ? 'a close pays the spread only'
                : pv.side === 'protect'
                  ? 'editing a stop costs nothing'
                  : 'cost preview failed'
          }
        />
        <Stat
          label="Account"
          value={pv.account ? fmtMoney(pv.account.cash, pv.account.currency) : '—'}
          hint={
            pv.account
              ? `cash · equity ${fmtMoney(pv.account.equity, pv.account.currency)} · exposure ${fmtMoney(pv.account.exposure, pv.account.currency)}`
              : 'no account read'
          }
        />
        <Stat
          label="Ledger before"
          value={pv.position.state === 'long' ? `long ${fmtUnits(pv.position.units ?? 0)}` : 'flat'}
          hint={
            pv.position.position_id ? `position ${pv.position.position_id}` : `rule says ${pv.rule_signal || 'n/a'}`
          }
        />
        <Stat
          label="Risk check"
          value={pv.risk ? (pv.risk.allowed ? 'passed' : 'blocked') : '—'}
          tone={pv.risk ? (pv.risk.allowed ? 'pos' : 'neg') : undefined}
          hint={pv.risk ? `${pv.risk.policy_version} · orders today ${pv.risk.state.orders_today}` : ''}
        />
      </div>

      <div className="trade-confirm-row">
        <div>
          <span className="muted">Confirmation phrase</span>{' '}
          <b className="num" data-testid="trade-confirmation">
            {pv.confirmation}
          </b>
        </div>
        {allow && (
          <div
            className={`trade-countdown ${expired ? 'expired' : left <= 20 ? 'urgent' : ''}`}
            data-testid="trade-countdown"
            data-left={left}
            role="timer"
            aria-live="polite"
          >
            <meter min={0} max={ttl} value={left} aria-label="token time left" />
            <span className="num">{expired ? 'token expired' : `expires in ${fmtCountdown(left)}`}</span>
          </div>
        )}
      </div>

      {refused && (
        <div className="modal-error" role="alert" data-testid="trade-refused">
          <b>Not placed.</b> {refused}
        </div>
      )}
      {cancelError && (
        <div className="modal-error" role="alert" data-testid="trade-cancel-error">
          <b>Not cancelled.</b> {cancelError} The token still expires on its own.
        </div>
      )}

      <ExecutorNote trading={trading} />

      <div className="trade-actions">
        {allow ? (
          <>
            <button
              type="button"
              className="btn btn-danger"
              id="trade-place"
              data-testid="trade-place"
              disabled={!canPlace || placing || cancelling}
              onClick={onPlace}
            >
              <IconBlock /> Place · {pv.confirmation}
            </button>
            {canPlace ? (
              <>
                <button
                  type="button"
                  className="btn btn-ghost"
                  id="trade-cancel"
                  data-testid="trade-cancel"
                  disabled={placing || cancelling}
                  aria-busy={cancelling}
                  aria-keyshortcuts={real ? undefined : 'Escape'}
                  title={
                    real
                      ? 'Burn the token now instead of waiting for it to expire. Nothing is sent; audited as trade.cancel. On the REAL account only this button cancels (Escape does not).'
                      : 'Burn the token now instead of waiting for it to expire (Escape). Nothing is sent; audited as trade.cancel.'
                  }
                  onClick={onCancel}
                >
                  {cancelling ? 'Cancelling…' : 'Cancel · nothing sent'}
                </button>
                {!real && <span className="faint">or Esc</span>}
              </>
            ) : (
              <>
                <button
                  type="button"
                  className="btn btn-ghost"
                  id="trade-reprepare"
                  data-testid="trade-reprepare"
                  onClick={onReprepare}
                >
                  <IconRefresh /> Re-prepare
                </button>
                <button
                  type="button"
                  className="btn btn-ghost"
                  id="trade-dismiss"
                  data-testid="trade-dismiss"
                  onClick={onDismiss}
                >
                  Dismiss
                </button>
              </>
            )}
          </>
        ) : (
          <>
            <button
              type="button"
              className="btn btn-ghost"
              id="trade-reprepare"
              data-testid="trade-reprepare"
              onClick={onReprepare}
            >
              <IconRefresh /> Prepare again
            </button>
            <button
              type="button"
              className="btn btn-ghost"
              id="trade-dismiss"
              data-testid="trade-dismiss"
              onClick={onDismiss}
            >
              Dismiss
            </button>
          </>
        )}
      </div>
    </section>
  )
}

/**
 * What a cancel left behind (2026-10-21): nothing at eToro, nothing in the
 * ledger, one trade.cancel audit line. Shown where the preview was until
 * the operator dismisses it or prepares again.
 */
function CancelledCard({ r, onDismiss }: { r: TradeCancelResponse; onDismiss: () => void }) {
  const expired = r.status === 'expired'
  return (
    <section
      className="card venue-etoro trade-cancelled"
      aria-labelledby="trade-cancelled-h"
      data-testid="trade-cancelled"
      data-status={r.status}
    >
      <div className="card-head">
        <div>
          <h2 id="trade-cancelled-h">
            <IconBlock /> {r.side} {bookSymbol(r.book)} · {expired ? 'discarded' : 'cancelled'}
          </h2>
          <div className="sub">
            {expired
              ? `The token had already expired at ${fmtNY(r.expires_at)} NY; nothing was sent.`
              : 'Token burned: nothing was sent to eToro and the ledger is unchanged.'}{' '}
            ref <span className="num">{r.client_ref}</span> · audited <span className="num">trade.cancel</span> at{' '}
            <span title={fmtUTC(r.audit.at)}>{fmtNY(r.audit.at)} NY</span>
            {r.audit_error ? ` · audit log not written: ${r.audit_error}` : ''}
          </div>
        </div>
        <button
          type="button"
          className="btn btn-ghost"
          id="trade-cancelled-dismiss"
          data-testid="trade-cancelled-dismiss"
          onClick={onDismiss}
        >
          Dismiss
        </button>
      </div>
    </section>
  )
}

function OutcomeCard({
  r,
  trading,
  onDismiss,
}: {
  r: TradePlaceResponse
  trading?: TradingInfo
  onDismiss: () => void
}) {
  const clock = useClock()
  const protect = r.side === 'protect'
  const ok = r.status === 'executed' || (protect && r.status === 'pending')
  const pa = r.position_after
  return (
    <section
      className="card venue-etoro trade-outcome"
      aria-labelledby="trade-outcome-h"
      data-testid="trade-outcome"
      data-status={r.status}
    >
      <div className="card-head">
        <div>
          <h2 id="trade-outcome-h">
            {ok ? <IconCheck style={{ color: 'var(--long)' }} /> : <IconAlert />} {r.side} {bookSymbol(r.book)} ·{' '}
            {r.status}
          </h2>
          <div className="sub">
            ref <span className="num">{r.client_ref}</span> · request <span className="num">{r.request_id}</span>
            {r.audit?.at ? (
              <>
                {' '}
                ·{' '}
                <span title={fmtUTC(r.audit.at)}>
                  {clock.fmt(r.audit.at)} {clock.label}
                </span>
              </>
            ) : null}
          </div>
        </div>
        <div className="row" style={{ gap: 8 }}>
          <Badge tone={r.ledger_recorded ? 'ok' : 'block'} dot>
            {r.ledger_recorded ? 'ledger recorded' : 'ledger NOT recorded'}
          </Badge>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={onDismiss}
            id="trade-outcome-dismiss"
            data-testid="trade-outcome-dismiss"
            title="Hide this outcome (it stays in the ledger and the audit log)"
          >
            Dismiss
          </button>
        </div>
      </div>
      <div className="trade-facts">
        <Stat
          label={protect ? 'Operation' : 'Order'}
          value={<span className="num">{r.order_id || '—'}</span>}
          hint={protect ? 'eToro edit operation id' : 'eToro order id'}
        />
        <Stat
          label="Position"
          value={<span className="num">{r.position_ids.join(', ') || '—'}</span>}
          hint={pa.state === 'long' ? `now long ${fmtUnits(pa.units ?? 0)}` : 'now flat'}
        />
        {protect ? (
          <>
            <Stat
              label="Stop loss"
              value={
                <span className="num" data-testid="trade-outcome-stop">
                  {pa.stop_loss_rate ? fmtPrice(pa.stop_loss_rate, 'usd', r.book) : 'none'}
                </span>
              }
              tone={pa.stop_loss_rate ? 'pos' : 'neg'}
              hint={
                r.protection?.stop_loss_before
                  ? `was ${fmtPrice(r.protection.stop_loss_before, 'usd', r.book)}`
                  : 'was none'
              }
            />
            <Stat
              label="Take profit"
              value={
                <span className="num">
                  {pa.take_profit_rate ? fmtPrice(pa.take_profit_rate, 'usd', r.book) : 'none'}
                </span>
              }
              hint={
                r.protection?.max_loss_usd ? `max loss at the stop ${fmtMoney(r.protection.max_loss_usd, 'usd')}` : ''
              }
            />
          </>
        ) : (
          <>
            <Stat
              label="Fill"
              value={<span className="num">{r.avg_price ? fmtPrice(r.avg_price, 'usd', r.book) : '—'}</span>}
              hint={`${fmtUnits(r.units)} · ${fmtMoney(r.amount_usd, 'usd')}`}
            />
            <Stat
              label="vs mid / vs close"
              value={
                <span className="num" data-testid="trade-fill-quality">
                  {fmtBps(r.fill_vs_mid_bps, true)} / {fmtBps(r.fill_vs_close_bps, true)}
                </span>
              }
              hint="positive is worse than the reference"
            />
          </>
        )}
      </div>
      {!protect && pa.state === 'long' && (
        <p className={`protect-outcome ${pa.stop_loss_rate ? 'ok' : 'warn'}`} data-testid="trade-outcome-protection">
          <IconShield />{' '}
          {pa.stop_loss_rate ? (
            <>
              Protected: stop <b className="num">{fmtPrice(pa.stop_loss_rate, 'usd', r.book)}</b>
              {pa.take_profit_rate ? (
                <>
                  {' '}
                  · target <b className="num">{fmtPrice(pa.take_profit_rate, 'usd', r.book)}</b>
                </>
              ) : (
                ' · no target'
              )}
              {r.protection?.max_loss_usd ? <> · max loss {fmtMoney(r.protection.max_loss_usd, 'usd')}</> : null}
              {r.protection?.profile ? <span className="faint"> · {r.protection.profile} profile</span> : null}
            </>
          ) : (
            <>This position has no stop loss. Prepare a protection from the proposals card.</>
          )}
        </p>
      )}
      {r.warnings?.map((w) => (
        <Banner key={w} tone="warn" title="Pending at eToro">
          {w}
        </Banner>
      ))}
      {r.ledger_error && (
        <Banner
          tone="block"
          title={
            protect
              ? 'The edit went through but the ledger line could not be written'
              : 'The order went through but the ledger line could not be written'
          }
        >
          {r.ledger_error}. The intent is on disk; reconcile reports it until the line is added by hand.
        </Banner>
      )}
      {r.audit_error && (
        <Banner tone="warn" title="Audit warning">
          {r.audit_error}
        </Banner>
      )}
      <ExecutorNote trading={trading} />
      <p className="faint">
        Audited as <span className="num">{r.audit.id || r.audit.outcome}</span> in{' '}
        <span className="num">{r.audit_path}</span>.
      </p>
    </section>
  )
}

function DisabledCard({ trading, ledger }: { trading: TradingInfo; ledger: string }) {
  if (trading.environment === 'real') {
    return (
      <Banner tone="warn" title="Assisted trading is off on this ui-api" id="trade-disabled">
        <span data-testid="trade-disabled-reason">{trading.disabled_reason || 'not enabled'}</span>. This is the REAL
        account's ledger ({ledger}); the canary process trades it only when started with{' '}
        <code>-etoro-trading -etoro-real-canary</code> and every condition of the acknowledgement holds (runbook §14).
      </Banner>
    )
  }
  return (
    <Banner tone="warn" title="Assisted trading is off on this ui-api" id="trade-disabled">
      <span data-testid="trade-disabled-reason">{trading.disabled_reason || 'not enabled'}</span>. The page is
      read-only: proposals are shown, previews are not offered. To enable it, start ui-api on 127.0.0.1 with{' '}
      <code>-etoro-trading</code> (or <code>UI_API_ETORO_TRADING=1</code>), <code>-operator-token-file</code> (a 0600
      file), <code>-etoro-ledger</code> naming the local {ledger} ledger and <code>-etoro-env-file</code> naming the
      demo env file with <code>ETORO_ENV=demo</code>. The real account is refused in code; the measurement canary is a
      separate process (runbook §14).
    </Banner>
  )
}

export function EtoroTradePage() {
  const ledger = useLedgerName() || 'etoro-demo'
  const ft = useEtoroForwardTests()
  const controls = useControls()
  const prep = usePrepareTradeMutation()
  const place = usePlaceTradeMutation()
  const cancel = useCancelTradeMutation()
  const trading = controls.data?.trading ?? (controls.data ? DEFAULT_TRADING : undefined)
  const real = trading?.environment === 'real'
  const account = real ? 'REAL' : 'demo'
  usePageTitle(
    'Trade · eToro',
    real
      ? 'Assisted prepare → place on the eToro REAL account: the measurement canary, by hand only.'
      : 'Assisted prepare → place on the eToro demo account, from the rule or by hand.',
  )
  const books = ft.data?.books ?? []

  // The form can arrive pre-filled from a link (the Positions card's
  // "protect" on an unprotected row): ?book=&side=&position=. Applied in an
  // effect because the link may be followed from this very page.
  const [params, setParams] = useSearchParams()
  const [form, setForm] = useState<Form>({
    book: 'nsdq100',
    side: 'open',
    amount: '',
    positionId: '',
    profile: 'moderate',
    slPct: '',
    tpPct: '',
  })
  // The live quote of the selected book (display; the Prepare quote decides).
  const quoteFeed = useLiveStream([form.book], { venue: 'etoro' })
  // The leverage: the policy's default (x1 on demo, x20 on the canary) until
  // the operator picks another of the options the policy offers.
  const [leverageChoice, setLeverageChoice] = useState<number | null>(null)
  const leverageOptions = trading?.leverage_options?.length ? trading.leverage_options : [1]
  const leverage =
    leverageChoice !== null && leverageOptions.includes(leverageChoice)
      ? leverageChoice
      : (trading?.default_leverage ?? leverageOptions[0] ?? 1)
  const pBook = params.get('book')
  const pSide = params.get('side')
  const pPos = params.get('position')
  const pGo = params.get('go') === '1'
  useEffect(() => {
    if (!pBook && !pSide && !pPos) return
    setForm((f) => ({
      ...f,
      book: pBook && BOOK_IDS.includes(pBook) ? pBook : f.book,
      side: pSide === 'open' || pSide === 'close' || pSide === 'protect' ? pSide : f.side,
      positionId: pPos ?? f.positionId,
    }))
  }, [pBook, pSide, pPos])
  // `go=1` (the "Close at market" shortcut on an open row): prepare at once
  // with the pre-filled form, or ask for the token first; the flag is
  // dropped from the URL so a reload does not prepare again. The preview
  // scrolls into view when it arrives this way.
  const autoScroll = useRef(false)
  const [token, setToken] = useState(getOperatorToken)
  const [preview, setPreview] = useState<TradePreview | null>(null)
  const [refused, setRefused] = useState<string | null>(null)
  // What the last cancel left behind, shown where the preview was.
  const [cancelled, setCancelled] = useState<TradeCancelResponse | null>(null)
  // The last outcome is kept for the session (per ledger) so a reload or a
  // detour to another page does not lose the fill the operator wants to read.
  const outcomeKey = `trade-outcome:${ledger}`
  const [outcome, setOutcomeState] = useState<TradePlaceResponse | null>(() => readOutcome(outcomeKey))
  const setOutcome = (o: TradePlaceResponse | null) => {
    setOutcomeState(o)
    try {
      if (o) sessionStorage.setItem(outcomeKey, JSON.stringify(o))
      else sessionStorage.removeItem(outcomeKey)
    } catch {
      // storage unavailable: the outcome lives in memory only
    }
  }
  const [dialog, setDialog] = useState(false)
  const [tried, setTried] = useState(false)
  const [rec, setRec] = useState<TradeRecommendation | null>(null)
  const recommend = useRecommendTradeMutation()
  const placeBtn = useRef<HTMLButtonElement>(null)
  const left = useCountdown(preview?.expires_at)

  const amountDefault = trading?.default_amount_usd ?? 1100
  const amount = form.amount === '' ? amountDefault : Number(form.amount)
  const selected = books.find((b) => b.book === form.book)
  const selectedPos = selected?.demo_position
  const positionId = form.positionId || (selectedPos?.state === 'long' ? (selectedPos.position_id ?? '') : '')
  const protectionSide = form.side === 'open' || form.side === 'protect'
  const slFrac = form.slPct === '' ? 0 : Number(form.slPct) / 100
  const tpFrac = form.tpPct === '' ? 0 : Number(form.tpPct) / 100

  const problems: string[] = []
  if (trading) {
    if (
      form.side === 'open' &&
      (!Number.isFinite(amount) || amount < trading.min_amount_usd || amount > trading.max_amount_usd)
    ) {
      problems.push(
        `Amount: ${trading.min_amount_usd.toLocaleString('en-US')}–${trading.max_amount_usd.toLocaleString('en-US')} USD (policy cap).`,
      )
    }
    if (form.side === 'close' && !positionId) problems.push('Close: the ledger holds no position on this book.')
    if (form.side === 'protect' && !positionId) problems.push('Protect: the ledger holds no position on this book.')
    if (protectionSide && form.profile === 'custom') {
      if (!Number.isFinite(slFrac) || slFrac <= 0 || slFrac >= 1)
        problems.push('Stop loss: a percentage above 0 and below 100 (the policy caps it at 10%).')
      if (!Number.isFinite(tpFrac) || tpFrac < 0 || tpFrac > 1000)
        problems.push('Take profit: a percentage from 0 (none) upward.')
    }
    if (!token.trim()) problems.push('Operator token: required (the token in ui-api’s -operator-token-file).')
  }

  // The recommendation follows the form (book, side, amount) once a token
  // is there, lightly debounced; it sends nothing and only guides the pick.
  const recAmount = form.side === 'open' ? amount : 0
  const recOk =
    !!trading?.enabled &&
    protectionSide &&
    !!token.trim() &&
    (form.side === 'open' ? Number.isFinite(recAmount) : !!positionId)
  useEffect(() => {
    if (!recOk) {
      setRec(null)
      return
    }
    const id = setTimeout(() => {
      recommend.mutate(
        { book: form.book, side: form.side as 'open' | 'protect', amountUsd: recAmount, positionId, token, leverage },
        { onSuccess: setRec, onError: () => setRec(null) },
      )
    }, 250)
    return () => clearTimeout(id)
    // recommend is a stable mutation object; the inputs are what matter.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [recOk, form.book, form.side, recAmount, positionId, token, leverage])

  const runPrepare = (f: Form, tok: string) => {
    setOutcome(null)
    setRefused(null)
    setPreview(null)
    setCancelled(null)
    cancel.reset()
    const amt = f.amount === '' ? amountDefault : Number(f.amount)
    const pid = f.positionId || (books.find((b) => b.book === f.book)?.demo_position?.position_id ?? '')
    const prot =
      f.side === 'close'
        ? {}
        : f.profile === 'custom'
          ? {
              profile: 'custom' as const,
              stopLossPct: Number(f.slPct) / 100,
              takeProfitPct: f.tpPct === '' ? 0 : Number(f.tpPct) / 100,
            }
          : { profile: f.profile }
    prep.mutate(
      { book: f.book, side: f.side, amountUsd: amt, positionId: pid, token: tok, leverage, ...prot },
      {
        onSuccess: (pv) => setPreview(pv),
        onError: (e) => {
          if (e instanceof ApiError && e.status === 401) setToken('')
        },
      },
    )
  }

  // The shortcut's prepare: once the page knows the books and that trading
  // is on. Runs once per link: the flag is removed right away, and goHandled
  // guards the re-run that a controls refetch caused before the URL update
  // landed (2026-10-26: two prepares in the same second on the real ledger).
  // On a REAL ledger an allowed shortcut prepare opens the confirmation
  // dialog by itself (autoDialog): one click from the Positions card to the
  // phrase, instead of a preview parked in the Manual section.
  const goHandled = useRef(false)
  const autoDialog = useRef(false)
  useEffect(() => {
    if (!pGo || !ft.data || !trading || goHandled.current) return
    goHandled.current = true
    const next = new URLSearchParams(params)
    next.delete('go')
    setParams(next, { replace: true })
    if (!trading.enabled) return
    const side: TradeSide = pSide === 'close' || pSide === 'protect' ? pSide : 'open'
    const book = pBook && BOOK_IDS.includes(pBook) ? pBook : 'nsdq100'
    const f: Form = { ...form, book, side, positionId: pPos ?? '' }
    setForm(f)
    setTried(true)
    if (!token.trim()) {
      document.getElementById('trade-token')?.focus()
      return
    }
    autoScroll.current = true
    autoDialog.current = real && side === 'close'
    runPrepare(f, token)
    // runPrepare / form are stable enough for a one-shot; the inputs are the URL and readiness.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pGo, ft.data, trading])
  useEffect(() => {
    if (!preview || !autoDialog.current) return
    autoDialog.current = false
    if (preview.verdict === 'allow' && preview.token) {
      place.reset()
      setDialog(true)
    }
    // place is a mutation handle; the trigger is the preview arriving.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [preview])
  useEffect(() => {
    if (!preview || !autoScroll.current) return
    autoScroll.current = false
    const el = document.querySelector('[data-testid="trade-preview"]')
    if (el && typeof (el as HTMLElement).scrollIntoView === 'function') {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' })
    }
  }, [preview])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTried(true)
    if (problems.length > 0) return
    runPrepare({ ...form, positionId }, token)
  }

  // Cancel: burn the token at ui-api. A 409 (placed or cancelled already)
  // or a 410 means the token is gone anyway: the preview is dropped with
  // the message rather than shown as a failure.
  const live = !!preview?.token && preview.verdict === 'allow' && left > 0 && !refused
  const runCancel = () => {
    if (!preview?.token || cancel.isPending) return
    const pv = preview
    cancel.mutate(
      { tradeToken: pv.token ?? '', token },
      {
        onSuccess: (r) => {
          setPreview(null)
          setRefused(null)
          setCancelled(r)
        },
        onError: (e) => {
          if (!(e instanceof ApiError)) return
          if (e.status === 401) setToken('')
          if (e.status === 409 || e.status === 410) {
            setPreview(null)
            setRefused(e.message)
          }
        },
      },
    )
  }
  const dismissPreview = () => {
    setPreview(null)
    setRefused(null)
    cancel.reset()
  }
  // Escape cancels a live preview while the dialog is closed (the dialog
  // owns Escape while it is open) — on the demo console. On REAL only the
  // Cancel button cancels: on 2026-10-26 an Escape meant to leave the
  // dialog burned a prepared close and the position was closed in the app.
  useEffect(() => {
    if (!live || dialog || real) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return
      e.preventDefault()
      runCancel()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // runCancel closes over the current preview and token; `live` and `dialog` are the gates.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live, dialog, preview?.token, token])

  const propose = (p: Proposal) => {
    const side: TradeSide = p.kind === 'open' ? 'open' : p.kind === 'protect' ? 'protect' : 'close'
    const f: Form = {
      book: p.book,
      side,
      amount: p.kind === 'open' ? String(p.amountUsd ?? amountDefault) : '',
      positionId: p.kind === 'open' ? '' : (p.positionId ?? ''),
      profile: form.profile === 'custom' ? 'moderate' : form.profile,
      slPct: '',
      tpPct: '',
    }
    setForm(f)
    setTried(true)
    if (!token.trim()) {
      document.getElementById('trade-token')?.focus()
      return
    }
    runPrepare(f, token)
  }

  const closeDialog = () => {
    setDialog(false)
    requestAnimationFrame(() => placeBtn.current?.focus())
  }

  const tradeAudit = controls.data
    ? {
        audit: controls.data.audit.filter((e) => e.action.startsWith('trade.')),
        audit_path: controls.data.audit_path,
        audit_error: controls.data.audit_error,
      }
    : null

  return (
    <>
      <div className="page-head">
        <div>
          <div className="row" style={{ gap: 12 }}>
            <h1>Trade</h1>
            <LedgerBadge name={ledger} venue="etoro" />
            <Badge tone={real ? 'real' : 'etoro'}>
              <span data-testid="trade-account-badge">
                {real ? 'REAL account' : trading?.rehearsal ? 'demo account · canary rehearsal' : 'demo account'}
              </span>
            </Badge>
          </div>
          <PageIntro>
            {real ? (
              <>
                Assisted trades on the eToro <b>REAL</b> account, for the measurement canary only: one hand-placed
                position under the canary policy ({trading?.policy_version || 'etoro-real-canary'}). Prepare runs the
                checks (halt file, risk policy, no other {bookSymbol(form.book)} position on the account, live quote,
                costs, cash) and returns a preview with a {trading?.token_ttl_s ?? 120} s token; place needs the phrase
                with <b>REAL</b> typed again, a reason, who, and this process&apos;s operator token. Every step is
                audited and the trade lands in the {ledger} ledger. Nothing here is automatic.
              </>
            ) : (
              <>
                Assisted trades on the eToro demo account: prepare runs the executor&apos;s checks (halt file, risk
                policy, ledger vs account, live quote, costs, cash) and returns a preview with a{' '}
                {trading?.token_ttl_s ?? 120} s token; place needs the phrase typed again, a reason, who, and the
                operator token. Every step is audited and the trade lands in the executor&apos;s ledger.
              </>
            )}
          </PageIntro>
        </div>
        {ft.data && (
          <div className="stack" style={{ alignItems: 'flex-end', gap: 6 }}>
            <SessionClock s={ft.data.session} />
            <span className="faint" title={fmtUTC(ft.data.generated_at)}>
              positions as of {fmtNY(ft.data.generated_at)} NY
            </span>
          </div>
        )}
      </div>

      {(ft.isLoading || controls.isLoading) && (
        <div className="grid grid-2">
          <CardSkeleton lines={6} />
          <CardSkeleton lines={6} />
        </div>
      )}
      {ft.isError && (
        <div className="card">
          <ErrorState error={ft.error} onRetry={() => ft.refetch()} />
        </div>
      )}
      {controls.isError && (
        <div className="card">
          <ErrorState error={controls.error} onRetry={() => controls.refetch()} />
        </div>
      )}

      {trading && !trading.enabled && <DisabledCard trading={trading} ledger={ledger} />}
      {trading?.enabled && controls.data?.halt_file.halted && (
        <Banner tone="block" title="The ledger is halted" id="trade-halted">
          {controls.data.halt_file.reason} · by <b>{controls.data.halt_file.by}</b>. Prepare answers a block verdict and
          place is refused until someone resumes on the Risk page.
        </Banner>
      )}

      {ft.data && trading && (
        <div className="grid grid-2 trade-grid">
          <Proposals books={books} trading={trading} busy={prep.isPending} onPropose={propose} />

          {trading.enabled && (
            <section className="card venue-etoro" aria-labelledby="trade-manual-h" data-testid="trade-manual">
              <div className="card-head">
                <div>
                  <h2 id="trade-manual-h">Manual</h2>
                  <div className="sub">
                    An open of {trading.min_amount_usd.toLocaleString('en-US')}–
                    {trading.max_amount_usd.toLocaleString('en-US')} USD{' '}
                    {leverage > 1 ? `invested at x${leverage}` : 'x1'} with a stop, a close of the ledger&apos;s
                    position, or a protect that sets its stop and target.
                  </div>
                </div>
              </div>
              <LiveQuoteLine live={quoteFeed} book={form.book} quote="usd" />
              <form onSubmit={submit} noValidate className="trade-form" id="trade-form" data-testid="trade-form">
                <div className="field-row">
                  <Field id="trade-book" label="Book">
                    <select
                      id="trade-book"
                      data-testid="trade-book"
                      className="select"
                      value={form.book}
                      onChange={(e) => setForm({ ...form, book: e.target.value, positionId: '' })}
                    >
                      {books.map((b) => (
                        <option key={b.book} value={b.book}>
                          {bookLabel(b.book)}
                        </option>
                      ))}
                    </select>
                  </Field>
                  <Field id="trade-side" label="Side">
                    <div className="seg" role="group" aria-label="Side" id="trade-side">
                      {(['open', 'close', 'protect'] as TradeSide[]).map((s) => (
                        <button
                          type="button"
                          key={s}
                          id={`trade-side-${s}`}
                          data-testid={`trade-side-${s}`}
                          aria-pressed={form.side === s}
                          onClick={() => setForm({ ...form, side: s })}
                        >
                          {s}
                        </button>
                      ))}
                    </div>
                  </Field>
                </div>
                <div className="field-row">
                  {form.side === 'open' ? (
                    <>
                      <Field
                        id="trade-amount"
                        label={amountLabel(leverage)}
                        error={tried ? problems.find((p) => p.startsWith('Amount')) : undefined}
                        hint={
                          leverage > 1
                            ? `default ${amountDefault.toLocaleString('en-US')} · ${trading.min_amount_usd.toLocaleString('en-US')}–${trading.max_amount_usd.toLocaleString('en-US')} USD (policy) · buys ${(
                                (Number.isFinite(amount) ? amount : amountDefault) * leverage
                              ).toLocaleString('en-US', { maximumFractionDigits: 0 })} USD of exposure`
                            : `default ${amountDefault.toLocaleString('en-US')} · max ${trading.max_amount_usd.toLocaleString('en-US')} USD (policy)`
                        }
                      >
                        <input
                          id="trade-amount"
                          data-testid="trade-amount"
                          className="input num"
                          type="number"
                          inputMode="decimal"
                          min={trading.min_amount_usd}
                          max={trading.max_amount_usd}
                          step={leverage > 1 ? 1 : 50}
                          placeholder={String(amountDefault)}
                          value={form.amount}
                          onChange={(e) => setForm({ ...form, amount: e.target.value })}
                        />
                      </Field>
                      <Field
                        id="trade-leverage"
                        label="Leverage"
                        hint={
                          leverageOptions.length === 1
                            ? `the policy allows x${leverageOptions[0]} only`
                            : `one of ${leverageOptions.map((l) => `x${l}`).join(', ')} (policy)`
                        }
                      >
                        {leverageOptions.length === 1 ? (
                          <input
                            id="trade-leverage"
                            data-testid="trade-leverage"
                            className="input num trade-leverage"
                            type="text"
                            readOnly
                            value={`x${leverage}`}
                          />
                        ) : (
                          <select
                            id="trade-leverage"
                            data-testid="trade-leverage"
                            className="select trade-leverage"
                            value={leverage}
                            onChange={(e) => setLeverageChoice(Number(e.target.value))}
                          >
                            {leverageOptions.map((l) => (
                              <option key={l} value={l}>
                                x{l}
                              </option>
                            ))}
                          </select>
                        )}
                      </Field>
                    </>
                  ) : (
                    <Field
                      id="trade-position"
                      label={form.side === 'protect' ? 'Position to protect' : 'Position to close'}
                      error={
                        tried
                          ? problems.find((p) => p.startsWith(form.side === 'protect' ? 'Protect' : 'Close'))
                          : undefined
                      }
                      hint={
                        selectedPos?.state === 'long'
                          ? `the ledger's position, ${fmtUnits(selectedPos.units ?? 0)}${
                              form.side === 'protect'
                                ? selectedPos.stop_loss_rate
                                  ? ` · stop ${fmtPrice(selectedPos.stop_loss_rate, 'usd', form.book)}`
                                  : ' · no stop yet'
                                : ''
                            }`
                          : 'the ledger is flat on this book'
                      }
                    >
                      <input
                        id="trade-position"
                        data-testid="trade-position"
                        className="input mono"
                        type="text"
                        readOnly
                        value={positionId}
                        placeholder="none"
                      />
                    </Field>
                  )}
                  <Field
                    id="trade-token"
                    label="Operator token"
                    error={tried ? problems.find((p) => p.startsWith('Operator token')) : undefined}
                    hint="kept for this tab only"
                  >
                    <input
                      id="trade-token"
                      data-testid="trade-token"
                      className="input mono"
                      type="password"
                      autoComplete="off"
                      spellCheck={false}
                      value={token}
                      onChange={(e) => setToken(e.target.value)}
                    />
                  </Field>
                </div>
                {protectionSide && (
                  <ProtectionPanel
                    side={form.side as 'open' | 'protect'}
                    book={form.book}
                    profile={form.profile}
                    onProfile={(p) => setForm({ ...form, profile: p })}
                    slPct={form.slPct}
                    tpPct={form.tpPct}
                    onSlPct={(v) => setForm({ ...form, slPct: v })}
                    onTpPct={(v) => setForm({ ...form, tpPct: v })}
                    rec={rec}
                    loading={recommend.isPending}
                    error={recommend.isError && !rec ? (recommend.error as Error).message : null}
                    busy={prep.isPending}
                    errors={
                      tried
                        ? {
                            sl: problems.find((p) => p.startsWith('Stop loss')),
                            tp: problems.find((p) => p.startsWith('Take profit')),
                          }
                        : undefined
                    }
                  />
                )}
                {prep.isError && (
                  <div className="modal-error" role="alert" data-testid="trade-prepare-error">
                    <b>Prepare failed.</b> {(prep.error as Error).message}
                  </div>
                )}
                <div className="trade-actions">
                  <button
                    type="submit"
                    className="btn btn-ghost"
                    id="trade-prepare"
                    data-testid="trade-prepare"
                    disabled={prep.isPending}
                    aria-busy={prep.isPending}
                  >
                    {prep.isPending ? 'Preparing…' : `Prepare ${form.side} ${bookSymbol(form.book)}`}
                  </button>
                  <span className="faint">Nothing is sent by preparing.</span>
                </div>
              </form>
            </section>
          )}
        </div>
      )}

      {preview && (
        <PreviewCard
          pv={preview}
          left={left}
          refused={refused}
          placing={place.isPending}
          cancelling={cancel.isPending}
          cancelError={cancel.isError ? (cancel.error as Error).message : null}
          trading={trading}
          onPlace={() => {
            place.reset()
            setDialog(true)
          }}
          onReprepare={() => runPrepare({ ...form, positionId }, token)}
          onCancel={runCancel}
          onDismiss={dismissPreview}
        />
      )}
      {!preview && refused && (
        <Banner tone="info" title="Preview dropped" id="trade-preview-gone">
          {refused}{' '}
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => setRefused(null)}>
            Dismiss
          </button>
        </Banner>
      )}
      {!preview && cancelled && <CancelledCard r={cancelled} onDismiss={() => setCancelled(null)} />}
      {outcome && <OutcomeCard r={outcome} trading={trading} onDismiss={() => setOutcome(null)} />}

      {ft.data && <PositionsCard />}

      {tradeAudit && (
        <section className="card venue-etoro" aria-label="Trade audit" data-testid="trade-audit">
          <AuditLog
            info={tradeAudit}
            empty="No assisted trades yet. Every prepare and place, allowed or not, is appended here (ui-audit.jsonl next to the ledger)."
          />
        </section>
      )}

      {dialog && preview && preview.token && (
        <ConfirmDialog<TradePlaceResponse>
          tone="danger"
          icon={preview.side === 'protect' ? <IconShield /> : <IconBlock />}
          title={`${preview.side === 'protect' ? 'Protect' : 'Place ' + preview.side} ${real ? 'REAL ' : ''}${preview.instrument.symbol} on the ${account} account`}
          sub={
            <>
              {preview.side === 'protect' ? (
                <>
                  Sets the stop loss{preview.protection?.levels.tp_rate ? ' and take profit' : ''} of position{' '}
                  {preview.position_id} ({fmtUnits(preview.units)}) at eToro&apos;s {account.toUpperCase()} account
                </>
              ) : (
                <>
                  Sends{' '}
                  {preview.side === 'open'
                    ? `a market buy: ${sizingLine(preview)}${preview.protection ? ', with its stop loss' : ''}`
                    : `a close of position ${preview.position_id}`}{' '}
                  ({fmtUnits(preview.units)}) to eToro&apos;s {account.toUpperCase()} account
                </>
              )}{' '}
              and records it in the <b>{ledger}</b> ledger with source <span className="num">ui</span>. The token
              expires in {fmtCountdown(left)}.
            </>
          }
          current={
            <>
              {preview.protection ? (
                <>
                  Stop <span className="num">{fmtPrice(preview.protection.levels.sl_rate, 'usd', preview.book)}</span>
                  {preview.protection.levels.tp_rate ? (
                    <>
                      {' '}
                      · target{' '}
                      <span className="num">{fmtPrice(preview.protection.levels.tp_rate, 'usd', preview.book)}</span>
                    </>
                  ) : null}{' '}
                  · max loss {fmtMoney(preview.protection.levels.max_loss_usd, 'usd')} ·{' '}
                </>
              ) : null}
              Quote{' '}
              {preview.quote
                ? `${fmtPrice(preview.quote.bid, 'usd', preview.book)} / ${fmtPrice(preview.quote.ask, 'usd', preview.book)}`
                : '—'}{' '}
              · request <span className="num">{preview.request_id}</span>
            </>
          }
          confirmLabel="Confirmation phrase"
          confirmWord={preview.confirmation}
          reasonPlaceholder={
            preview.side === 'protect'
              ? 'e.g. Stop on the open position before the weekend'
              : 'e.g. Re-entering by hand after the incident review'
          }
          reasonHint="one line, kept in the ledger line and the audit log"
          submitLabel={`${preview.side === 'protect' ? 'Protect' : 'Place'} · ${preview.confirmation}`}
          pendingLabel={
            preview.side === 'protect' ? 'Setting (eToro answers within 30 s)…' : 'Placing (eToro answers within 30 s)…'
          }
          pending={place.isPending}
          error={place.error}
          errorTitle={(e) =>
            e.status === 410
              ? 'Token expired'
              : e.status === 422
                ? 'Refused at the re-check'
                : e.status === 409
                  ? 'Not placed'
                  : e.status === 502
                    ? 'eToro did not execute'
                    : undefined
          }
          run={(v, cb) =>
            place.mutate(
              { tradeToken: preview.token ?? '', confirm: v.confirm, reason: v.reason, by: v.by, token: v.token },
              {
                onSuccess: cb.onSuccess,
                onError: (e) => {
                  cb.onError(e)
                  if (e instanceof ApiError && [409, 410, 422, 502].includes(e.status)) setRefused(e.message)
                },
              },
            )
          }
          onClose={closeDialog}
          onDone={(r) => {
            setOutcome(r)
            setPreview(null)
            closeDialog()
          }}
        />
      )}
    </>
  )
}
