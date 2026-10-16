/**
 * One book's card on the Forward tests page for an eToro ledger: the frozen
 * SMA50 decision, the pre-registered paper account against holding the CFD,
 * and what the demo account did about it (position, order, run status).
 */
import { Link } from 'react-router'
import type { EtoroForwardTest } from '../../api/etoroSchemas'
import { useLedgerSearch } from '../../api/client'
import { bookLabel, fmtDate, fmtFrac, fmtNY, fmtPct, fmtPrice, fmtUTC } from '../../lib/format'
import { Badge, Empty, LedgerBadge, SignalPill, Stat } from '../ui'
import { CostsBadge, DemoStrip, OrderLine, RunBadge } from './parts'

export function EtoroForwardTestCard({ ft }: { ft: EtoroForwardTest }) {
  const search = useLedgerSearch()
  if (!ft.recorded_at) {
    return (
      <article className="card venue-etoro" aria-labelledby={`ft-${ft.book}`} data-testid={`ft-card-${ft.book}`}>
        <header className="ft-head">
          <div>
            <h2 className="ft-book" id={`ft-${ft.book}`}>
              {bookLabel(ft.book)}
            </h2>
            <div className="ft-meta">Frozen SMA50 rule · eToro demo</div>
          </div>
          <LedgerBadge name={ft.ledger} venue="etoro" />
        </header>
        <Empty title={`No records in the ${ft.ledger} ledger yet`}>
          {ft.run.message || 'The eToro executor has not recorded this book in this ledger.'}
        </Empty>
        <footer className="ft-foot">
          <RunBadge run={ft.run} />
        </footer>
      </article>
    )
  }
  const d = ft.decision
  const p = ft.paper
  const excess = ft.excess_vs_hold_pct
  const flipWord = d.signal === 'long' ? 'falls' : 'rises'
  const nextWord =
    p.pending_action === 'hold' ? (d.signal === 'long' ? 'keep the CFD' : 'stay in cash') : p.pending_action
  return (
    <article
      className="card interactive venue-etoro"
      aria-labelledby={`ft-${ft.book}`}
      data-testid={`ft-card-${ft.book}`}
      data-venue="etoro"
    >
      <header className="ft-head">
        <div>
          <h2 className="ft-book" id={`ft-${ft.book}`}>
            {bookLabel(ft.book)}
          </h2>
          <div className="ft-meta">
            Paper + eToro demo · bar {fmtDate(d.bar_date)} ·{' '}
            <span title={fmtUTC(ft.recorded_at)}>recorded {fmtNY(ft.recorded_at)} NY</span>
          </div>
        </div>
        <div className="row" style={{ gap: 8 }}>
          <LedgerBadge name={ft.ledger} venue="etoro" />
          <SignalPill signal={d.signal} />
        </div>
      </header>

      <div className="grid grid-4" style={{ gap: 14 }}>
        <Stat
          label="Close vs SMA50"
          value={fmtPrice(d.close, ft.quote, ft.book)}
          hint={<span className="num">SMA {fmtPrice(d.sma50, ft.quote, ft.book)}</span>}
        />
        <Stat
          label="Paper equity"
          value={p.equity_if_closed.toFixed(4)}
          tone={p.equity_if_closed >= 1 ? 'pos' : 'neg'}
          hint={<span className="num">CFD hold {p.hold_equity_if_closed.toFixed(4)}</span>}
        />
        <Stat
          label="vs CFD hold"
          value={fmtPct(excess)}
          tone={excess > 0.005 ? 'pos' : excess < -0.005 ? 'neg' : undefined}
          hint={`${p.spread_rt_bps} bps round trip · ${p.overnight_bps} bps/night`}
        />
        <Stat
          label="Max drawdown"
          value={fmtFrac(p.max_drawdown)}
          tone={p.max_drawdown > 0.15 ? 'neg' : undefined}
          hint={`${p.days} days · ${p.round_trips} round trip${p.round_trips === 1 ? '' : 's'}`}
        />
      </div>

      <p className="ft-explain">
        Signal <b>{d.signal}</b>
        {d.prev_signal && d.prev_signal !== d.signal ? (
          <>
            {' '}
            (was <b>{d.prev_signal}</b>)
          </>
        ) : (
          ' (unchanged)'
        )}
        , action <b>{d.action}</b>:{' '}
        {d.signal === 'long' ? (
          <>
            the close is{' '}
            <b className="num">
              {fmtPct(ft.distance_to_sma_pct, Math.abs(ft.distance_to_sma_pct) < 0.1 ? 2 : 1, false)}
            </b>{' '}
            above its 50-day average; it flips to flat if the price {flipWord} about that much.
          </>
        ) : (
          <>
            the close is{' '}
            <b className="num">
              {fmtPct(-ft.distance_to_sma_pct, Math.abs(ft.distance_to_sma_pct) < 0.1 ? 2 : 1, false)}
            </b>{' '}
            below its 50-day average; it goes long if the price {flipWord} above it.
          </>
        )}{' '}
        Next cash session ({fmtDate(d.fill_date)}): <b>{nextWord.toUpperCase()}</b>.
      </p>

      <DemoStrip position={ft.demo_position} account={ft.account} book={ft.book} />

      {ft.last_order && (
        <div style={{ marginTop: 12 }}>
          <OrderLine o={ft.last_order} book={ft.book} quote={ft.quote} />
        </div>
      )}

      <div className="divider" />

      <footer className="ft-foot">
        <RunBadge run={ft.run} />
        <CostsBadge status={p.costs_status} />
        {ft.demo_action === 'blocked' && <Badge tone="block">last order blocked</Badge>}
        {ft.risk_blocks > 0 && <Badge tone="block">{ft.risk_blocks} risk block</Badge>}
        {ft.risk_warnings > 0 && <Badge tone="warn">{ft.risk_warnings} risk warning</Badge>}
        {ft.candles.missing_trading_days > 0 && (
          <Badge tone="warn">{ft.candles.missing_trading_days} missing trading days</Badge>
        )}
        {(ft.candles.revised_closes ?? 0) > 0 && (
          <Badge
            tone="info"
            title={`${ft.candles.revised_closes} closes revised by eToro, at most ${(ft.candles.max_revision_bps ?? 0).toFixed(1)} bps`}
          >
            {ft.candles.revised_closes} revised closes
          </Badge>
        )}
        <span className="spacer" />
        <Link to={{ pathname: `/forward-tests/${ft.book}`, search }} className="btn" id={`open-${ft.book}`}>
          Details
        </Link>
      </footer>
    </article>
  )
}
