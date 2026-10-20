/**
 * Positions & round trips (GET /api/ui/etoro/positions): one row per position
 * the ledger has seen, open with the account's live unrealised P&L or closed
 * with the close price, who closed it and eToro's realised figures. Shown on
 * the Trade page (every book) and on a book's forward-test page (that book).
 * The figures' source is always named: "live" when the account was read,
 * "ledger" otherwise, and eToro's net is marked confirmed.
 */
import { useState, type KeyboardEvent, type ReactNode } from 'react'
import { Link } from 'react-router'
import { useEtoroPositions, useLedgerSearch } from '../../api/client'
import { tradePnL, type ClosedBy, type EtoroTrade, type EtoroPositionsResponse } from '../../api/etoroSchemas'
import { fmtBps, fmtDate, fmtFrac, fmtMoney, fmtNY, fmtPrice, fmtUTC, fmtUnits } from '../../lib/format'
import { Badge, CardSkeleton, Empty, ErrorState, Stat } from '../ui'
import { IconRefresh, IconShield, IconTrade } from '../icons'

type Filter = 'all' | 'open' | 'closed'

function signed(v: number, quote: string, digits = 2): string {
  const s = fmtMoney(Math.abs(v), quote, digits)
  return v < 0 ? `−${s}` : v > 0 ? `+${s}` : s
}

function pnlClass(v: number): string {
  return v > 0 ? 'pos' : v < 0 ? 'neg' : ''
}

function statusTone(t: EtoroTrade): 'long' | 'flat' | 'warn' | 'block' {
  if (t.status === 'open') return 'long'
  if (t.status === 'unknown') return 'warn'
  if (!t.recorded_close) return 'block'
  return 'flat'
}

function closedByText(by: ClosedBy | undefined, kind: string | undefined): string {
  switch (by) {
    case 'rule':
      return 'rule'
    case 'operator':
      return 'operator'
    case 'venue':
      return kind === 'took_profit' ? 'eToro · target' : 'eToro · stop'
    case 'outside':
      return 'outside the ledger'
    default:
      return by ?? '—'
  }
}

function PnL({ t, quote }: { t: EtoroTrade; quote: string }) {
  const p = tradePnL(t)
  if (!p) return <span className="faint">—</span>
  const pct = p.kind === 'unrealized' ? t.unrealized_pct : t.return_pct
  return (
    <span className={`positions-pnl ${pnlClass(p.value)}`} data-testid={`position-pnl-${t.position_id}`}>
      <b className="num">{signed(p.value, quote)}</b>
      <small className="num">
        {pct != null ? fmtFrac(pct) : ''} ·{' '}
        {p.kind === 'net' ? 'net, eToro' : p.kind === 'gross' ? 'gross, fills' : 'unrealised'}
      </small>
    </span>
  )
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="positions-fact">
      <span className="stat-label">{label}</span>
      <span className="num">{children}</span>
    </div>
  )
}

function Details({ t, quote }: { t: EtoroTrade; quote: string }) {
  const book = t.book
  const price = (v: number | undefined) => (v && v > 0 ? fmtPrice(v, quote, book) : '—')
  return (
    <div className="positions-details" data-testid={`position-details-${t.position_id}`}>
      <div className="positions-facts">
        <Fact label="Units · amount">
          {fmtUnits(t.units)} · {fmtMoney(t.amount, quote, 2)} x1
        </Fact>
        <Fact label="Opened">
          {t.opened_at ? <span title={fmtUTC(t.opened_at)}>{fmtNY(t.opened_at)} NY</span> : '—'}
          {t.open_bar_date ? ` · bar ${fmtDate(t.open_bar_date)}` : ''}
          {t.recorded_open ? '' : ' · open not recorded'}
        </Fact>
        <Fact label="Open fill">
          {price(t.open_rate)}
          {t.open_fill_vs_mid_bps != null ? ` · ${fmtBps(t.open_fill_vs_mid_bps, true)} vs mid` : ''}
          {t.open_costs
            ? ` · spread ${fmtMoney(t.open_costs.spread, quote)} · overnight ${fmtMoney(t.open_costs.overnight_fee, quote)}/night`
            : ''}
        </Fact>
        <Fact label="Protection (ledger)">
          stop {price(t.stop_loss_rate)} · target {price(t.take_profit_rate)}
          {t.protect_edits ? ` · ${t.protect_edits} edit${t.protect_edits === 1 ? '' : 's'}` : ''}
        </Fact>
        {t.status === 'open' && t.mark_source === 'account' && (
          <>
            <Fact label="Account now">
              mark ≈ {price(t.mark_rate)} · unrealised {signed(t.unrealized_pnl ?? 0, quote)} · fees to date{' '}
              {fmtMoney(t.fees_to_date ?? 0, quote)}
            </Fact>
            <Fact label="Protection (account)">
              stop {price(t.account_stop_loss_rate)} · target {price(t.account_take_profit_rate)}
              {t.protection_drift ? ' · differs from the ledger' : ''}
            </Fact>
          </>
        )}
        {t.status === 'open' && t.mark_source !== 'account' && (
          <Fact label="Account now">
            {t.account_holds === false ? 'not held by the account' : 'not read (ledger only)'}
            {t.financing_to_date ? ` · financing at the last run ${fmtMoney(t.financing_to_date, quote)}` : ''}
          </Fact>
        )}
        {t.status !== 'open' && (
          <>
            <Fact label="Closed">
              {t.closed_at ? <span title={fmtUTC(t.closed_at)}>{fmtNY(t.closed_at)} NY</span> : '—'}
              {t.close_bar_date ? ` · bar ${fmtDate(t.close_bar_date)}` : ''}
              {t.held_days != null ? ` · held ${t.held_days} d` : ''}
              {t.recorded_close === false ? ' · close not in the ledger' : ''}
            </Fact>
            <Fact label="Close fill">
              {price(t.close_rate)}
              {t.close_fill_vs_mid_bps != null ? ` · ${fmtBps(t.close_fill_vs_mid_bps, true)} vs mid` : ''}
              {t.venue_close_rate && t.venue_close_rate !== t.close_rate ? ` · eToro ${price(t.venue_close_rate)}` : ''}
            </Fact>
            <Fact label="P&L breakdown">
              gross {t.gross_pnl != null ? signed(t.gross_pnl, quote) : '—'}
              {t.venue_confirmed
                ? ` · eToro net profit ${signed(t.net_profit ?? 0, quote)} · fees ${fmtMoney(t.fees ?? 0, quote)} · net ${signed(t.net ?? 0, quote)}`
                : t.financing_to_date
                  ? ` · financing at the last run ${fmtMoney(t.financing_to_date, quote)} · eToro's net not read`
                  : " · eToro's net not read"}
            </Fact>
            <Fact label="Closed by">
              {closedByText(t.closed_by, t.close_kind)}
              {t.close_reason ? ` · ${t.close_reason}` : ''}
            </Fact>
          </>
        )}
        {t.operator && (
          <Fact label="Operator">
            {t.operator.by} · {t.operator.reason}
          </Fact>
        )}
        <Fact label="References">
          position {t.position_id}
          {t.open_client_ref ? ` · ${t.open_client_ref}` : ''}
          {t.close_client_ref ? ` · ${t.close_client_ref}` : ''}
        </Fact>
      </div>
      {t.notes && t.notes.length > 0 && (
        <ul className="positions-notes" data-testid={`position-notes-${t.position_id}`}>
          {t.notes.map((n) => (
            <li key={n}>{n}</li>
          ))}
        </ul>
      )}
    </div>
  )
}

function Row({ t, quote, search, showBook }: { t: EtoroTrade; quote: string; search: string; showBook: boolean }) {
  const [open, setOpen] = useState(false)
  const book = t.book
  const toggle = () => setOpen((v) => !v)
  const onKey = (e: KeyboardEvent<HTMLTableRowElement>) => {
    if (e.target !== e.currentTarget) return
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      toggle()
    }
  }
  const unprotected = t.status === 'open' && !t.stop_loss_rate
  return (
    <>
      <tr
        className={`positions-row ${open ? 'open' : ''}`}
        data-testid={`position-${t.position_id}`}
        onClick={toggle}
        onKeyDown={onKey}
        role="button"
        tabIndex={0}
        aria-expanded={open}
        aria-controls={`position-details-${t.position_id}`}
      >
        <td>
          <div className="positions-id">
            {showBook ? (
              <Link
                to={`/forward-tests/${encodeURIComponent(book)}${search}`}
                className="positions-book"
                onClick={(e) => e.stopPropagation()}
                title={`Open the ${t.symbol} forward-test page`}
              >
                {t.symbol}
              </Link>
            ) : (
              <b>{t.symbol}</b>
            )}
            <span className="row" style={{ gap: 6 }}>
              <Badge tone={t.source === 'manual' ? 'etoro' : t.source === 'rule' ? 'info' : 'flat'}>{t.source}</Badge>
              <span className="faint num">{t.position_id}</span>
            </span>
          </div>
        </td>
        <td>
          {t.opened_at ? (
            <span title={fmtUTC(t.opened_at)}>{fmtNY(t.opened_at)}</span>
          ) : (
            <span className="faint">—</span>
          )}
        </td>
        <td className="r num">{fmtPrice(t.open_rate, quote, book)}</td>
        <td className="r num">
          {t.status === 'open' ? (
            t.mark_source === 'account' && t.mark_rate ? (
              <span title="implied from the account's unrealised P&L">≈ {fmtPrice(t.mark_rate, quote, book)}</span>
            ) : (
              <span className="faint">—</span>
            )
          ) : t.close_rate ? (
            fmtPrice(t.close_rate, quote, book)
          ) : (
            <span className="faint">—</span>
          )}
        </td>
        <td className="r">
          <PnL t={t} quote={quote} />
        </td>
        <td className="num">
          {t.stop_loss_rate ? (
            <span title={t.protection_drift ? 'the account holds different rates (details)' : undefined}>
              {fmtPrice(t.stop_loss_rate, quote, book)}
              {t.take_profit_rate ? ` / ${fmtPrice(t.take_profit_rate, quote, book)}` : ''}
              {t.protection_drift ? ' !' : ''}
            </span>
          ) : unprotected ? (
            <Link
              to={`/trade${search}${search ? '&' : '?'}book=${encodeURIComponent(book)}&side=protect&position=${encodeURIComponent(t.position_id)}`}
              className="positions-protect"
              onClick={(e) => e.stopPropagation()}
              data-testid={`position-protect-${t.position_id}`}
              title="Set a stop from the Trade page (prepare → place)"
            >
              <IconShield /> no stop · protect
            </Link>
          ) : (
            <span className="faint">none</span>
          )}
        </td>
        <td>
          {t.status === 'open' ? (
            <span className="faint">{t.held_days != null ? `${t.held_days} d held` : ''}</span>
          ) : (
            closedByText(t.closed_by, t.close_kind)
          )}
        </td>
        <td>
          <span data-testid={`position-status-${t.position_id}`}>
            <Badge tone={statusTone(t)} dot>
              {t.status === 'closed' && t.recorded_close === false ? 'closed · unrecorded' : t.status}
            </Badge>
          </span>
        </td>
      </tr>
      {open && (
        <tr className="positions-detail-row" id={`position-details-${t.position_id}`}>
          <td colSpan={8}>
            <Details t={t} quote={quote} />
          </td>
        </tr>
      )}
    </>
  )
}

function Summary({ r }: { r: EtoroPositionsResponse }) {
  const quote = r.account?.currency ?? 'USD'
  const s = r.summary
  return (
    <div className="positions-strip" data-testid="positions-summary">
      <Stat
        label="Open"
        value={<span className="num">{s.open}</span>}
        hint={s.open ? `${fmtMoney(s.open_exposure, quote, 0)} exposure` : 'no open position'}
        tone={s.open ? 'pos' : undefined}
      />
      <Stat
        label="Unrealised"
        value={
          r.live ? (
            <span className="num">{signed(s.unrealized_pnl, quote)}</span>
          ) : (
            <span className="faint">not read</span>
          )
        }
        hint={r.live ? `fees to date ${fmtMoney(s.fees_to_date, quote)}` : 'needs the account'}
        tone={r.live && s.unrealized_pnl !== 0 ? (s.unrealized_pnl > 0 ? 'pos' : 'neg') : undefined}
      />
      <Stat
        label="Realised"
        value={
          <span className="num">
            {s.realized_confirmed === s.closed && s.closed > 0
              ? signed(s.realized_net, quote)
              : signed(s.realized_gross, quote)}
          </span>
        }
        hint={
          s.closed === 0
            ? 'no round trip yet'
            : s.realized_confirmed === s.closed
              ? `net after fees, eToro's figures · ${s.closed} closed`
              : `gross from the fills · ${s.realized_confirmed}/${s.closed} confirmed by eToro`
        }
        tone={
          s.closed
            ? (s.realized_confirmed === s.closed ? s.realized_net : s.realized_gross) >= 0
              ? 'pos'
              : 'neg'
            : undefined
        }
      />
      <Stat
        label="Account"
        value={
          r.account ? (
            <span className="num">{fmtMoney(r.account.equity, quote, 2)}</span>
          ) : (
            <span className="faint">—</span>
          )
        }
        hint={
          r.account ? (
            <>
              cash {fmtMoney(r.account.cash, quote, 0)} · read{' '}
              <span title={fmtUTC(r.account.at)}>{fmtNY(r.account.at)} NY</span>
              {r.cached ? ' (cached)' : ''}
            </>
          ) : (
            'equity and cash need the account'
          )
        }
      />
    </div>
  )
}

export function PositionsCard({ book = '', title = 'Positions & round trips' }: { book?: string; title?: string }) {
  const q = useEtoroPositions(book)
  const search = useLedgerSearch()
  const [filter, setFilter] = useState<Filter>('all')
  const r = q.data
  const quote = r?.account?.currency ?? 'USD'
  const rows = (r?.trades ?? []).filter((t) =>
    filter === 'all' ? true : filter === 'open' ? t.status !== 'closed' : t.status === 'closed',
  )
  const counts = r ? { all: r.trades.length, open: r.summary.open + r.summary.unknown, closed: r.summary.closed } : null
  return (
    <section className="card venue-etoro positions-card" aria-labelledby="positions-h" data-testid="positions-card">
      <div className="card-head">
        <div>
          <h2 id="positions-h">
            <IconTrade /> {title}
          </h2>
          <span className="sub" data-testid="positions-source">
            {r
              ? r.live
                ? `ledger + account (live${r.cached ? ', cached' : ''})`
                : `ledger only — ${r.live_reason ?? 'the account was not read'}`
              : 'one row per position: open with the account’s unrealised P&L, closed with the fill, who closed it and eToro’s net'}
          </span>
        </div>
        <div className="row" style={{ gap: 8 }}>
          {counts && counts.all > 0 && (
            <div className="seg positions-filter" role="group" aria-label="Show" data-testid="positions-filter">
              {(['all', 'open', 'closed'] as Filter[]).map((f) => (
                <button
                  key={f}
                  type="button"
                  aria-pressed={filter === f}
                  onClick={() => setFilter(f)}
                  data-testid={`positions-filter-${f}`}
                >
                  {f} <span className="faint num">{counts[f]}</span>
                </button>
              ))}
            </div>
          )}
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={() => q.refetch()}
            disabled={q.isFetching}
            aria-busy={q.isFetching}
            id="positions-refresh"
            data-testid="positions-refresh"
            title="Re-read the ledger and the account"
          >
            <IconRefresh /> {q.isFetching ? 'Reading…' : 'Refresh'}
          </button>
        </div>
      </div>
      {q.isLoading && <CardSkeleton lines={4} />}
      {q.isError && <ErrorState error={q.error} onRetry={() => q.refetch()} />}
      {r && (
        <div className="stack" style={{ gap: 12 }}>
          <Summary r={r} />
          {r.warnings.length > 0 && (
            <ul className="positions-warnings" data-testid="positions-warnings">
              {r.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          )}
          {r.trades.length === 0 ? (
            <Empty title={book ? `No position on ${book.toUpperCase()} yet` : 'No positions yet'}>
              A row appears when the 09:35 run or a console trade opens one.
            </Empty>
          ) : rows.length === 0 ? (
            <Empty title={`No ${filter} position`}>Change the filter above.</Empty>
          ) : (
            <div className="table-wrap">
              <table className="data positions-table" id="positions-table" data-testid="positions-table">
                <thead>
                  <tr>
                    <th scope="col">Position</th>
                    <th scope="col">Opened (NY)</th>
                    <th scope="col" className="r">
                      Open
                    </th>
                    <th scope="col" className="r">
                      Mark / close
                    </th>
                    <th scope="col" className="r">
                      P&amp;L
                    </th>
                    <th scope="col">Stop / target</th>
                    <th scope="col">Closed by</th>
                    <th scope="col">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((t) => (
                    <Row key={t.position_id} t={t} quote={quote} search={search} showBook={!book} />
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <p className="positions-foot">
            Click a row for the fills, costs, protection, references and notes. Open and close prices are the fills the
            ledger recorded; the mark is implied from the account&apos;s unrealised P&amp;L; a closed row&apos;s net is
            eToro&apos;s (net profit − fees) once the trading history lists it, the gross from the fills until then.
          </p>
        </div>
      )}
    </section>
  )
}
