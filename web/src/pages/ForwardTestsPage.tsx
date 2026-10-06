import { Link } from 'react-router'
import { useForwardTests, useLedgerSearch } from '../api/client'
import type { ForwardTest } from '../api/schemas'
import { Badge, Banner, CardSkeleton, Empty, ErrorState, LedgerBadge, SignalPill, Stat } from '../components/ui'
import { bookLabel, fmtBTC, fmtDate, fmtFrac, fmtMx, fmtPct, fmtPrice, fmtUTC } from '../lib/format'
import { usePageTitle } from '../lib/usePageTitle'

function WindowProgress({ ft }: { ft: ForwardTest }) {
  const m = ft.milestones
  const total = m.days_elapsed + m.days_to_evaluation
  const interimAt = total > 0 ? (m.days_elapsed + m.days_to_interim) / total : 0.5
  return (
    <div>
      <div className="row" style={{ justifyContent: 'space-between', fontSize: 12, marginBottom: 8 }}>
        <span className="muted">
          Day <span className="num">{m.days_elapsed}</span> of <span className="num">{total}</span>
        </span>
        <span className="faint">
          interim {fmtDate(m.interim)} · evaluation {fmtDate(m.evaluation)}
        </span>
      </div>
      <div
        className="progress-window"
        role="progressbar"
        aria-label="Forward window progress"
        aria-valuenow={Math.round(m.window_progress * 100)}
        aria-valuemin={0}
        aria-valuemax={100}
      >
        <div className="fill" style={{ width: `${Math.max(1, m.window_progress * 100)}%` }} />
        <span className="tick" style={{ left: `${interimAt * 100}%` }} title={`Interim look ${m.interim}`} />
      </div>
    </div>
  )
}

function RunBadge({ ft }: { ft: ForwardTest }) {
  const s = ft.run.status
  const tone = s === 'ok' ? 'ok' : s === 'pending' ? 'info' : 'warn'
  const text = s === 'ok' ? 'up to date' : s === 'pending' ? 'run pending' : s === 'missed' ? 'run missed' : 'no data'
  return (
    <Badge tone={tone} dot title={ft.run.message}>
      {text}
    </Badge>
  )
}

export function ForwardTestCard({ ft }: { ft: ForwardTest }) {
  const search = useLedgerSearch()
  if (!ft.recorded_at) {
    return (
      <article className="card" aria-labelledby={`ft-${ft.book}`} data-testid={`ft-card-${ft.book}`}>
        <header className="ft-head">
          <div>
            <h2 className="ft-book" id={`ft-${ft.book}`}>
              {bookLabel(ft.book)}
            </h2>
            <div className="ft-meta">{ft.prereg ? `Pre-registration ${ft.prereg}` : 'Frozen SMA50 rule'}</div>
          </div>
          <LedgerBadge name={ft.ledger} />
        </header>
        <Empty title={`No records in the ${ft.ledger} ledger yet`}>
          {ft.run.message || 'The daily executor has not recorded this book in this ledger.'}
        </Empty>
      </article>
    )
  }
  const d = ft.decision
  const p = ft.paper
  const excess = ft.excess_vs_hold_pct
  const flipWord = d.signal === 'long' ? 'falls' : 'rises'
  return (
    <article className="card interactive" aria-labelledby={`ft-${ft.book}`} data-testid={`ft-card-${ft.book}`}>
      <header className="ft-head">
        <div>
          <h2 className="ft-book" id={`ft-${ft.book}`}>
            {bookLabel(ft.book)}
          </h2>
          <div className="ft-meta">
            {ft.mode === 'stage' ? 'Paper + Bitso stage' : 'Paper (dry run)'} · bar {fmtDate(d.bar_date)} ·{' '}
            <span title={fmtUTC(ft.recorded_at)}>recorded {fmtMx(ft.recorded_at)}</span>
          </div>
        </div>
        <div className="row" style={{ gap: 8 }}>
          <LedgerBadge name={ft.ledger} />
          <SignalPill signal={d.signal} />
        </div>
      </header>

      <div className="grid grid-4" style={{ gap: 14 }}>
        <Stat
          label="Close vs SMA50"
          value={fmtPrice(d.close, ft.quote)}
          hint={<span className="num">SMA {fmtPrice(d.sma50, ft.quote)}</span>}
        />
        <Stat
          label="Paper equity"
          value={p.equity.toFixed(4)}
          tone={p.equity >= 1 ? 'pos' : 'neg'}
          hint={<span className="num">hold {p.hold_equity.toFixed(4)}</span>}
        />
        <Stat
          label="vs buy-and-hold"
          value={fmtPct(excess)}
          tone={excess > 0.005 ? 'pos' : excess < -0.005 ? 'neg' : undefined}
          hint={`after ${p.leg_cost_bps} bps/leg`}
        />
        <Stat
          label="Max drawdown"
          value={fmtFrac(p.max_drawdown)}
          tone={p.max_drawdown > 0.15 ? 'neg' : undefined}
          hint={`${p.days} days · ${p.fills} fills`}
        />
      </div>

      <p className="ft-explain">
        {d.signal === 'long' ? (
          <>
            The rule is <b>long</b>: the close is <b className="num">{fmtPct(ft.distance_to_sma_pct, 1, false)}</b>{' '}
            above its 50-day average. It flips to flat if the price {flipWord} about that much (the average moves too).
          </>
        ) : (
          <>
            The rule is <b>flat</b>: the close is <b className="num">{fmtPct(-ft.distance_to_sma_pct, 1, false)}</b>{' '}
            below its 50-day average. It goes long if the price {flipWord} above it.
          </>
        )}{' '}
        Next open ({fmtDate(d.fill_date)}): <b>{p.pending_action.toUpperCase()}</b>.
      </p>

      <div className="divider" />
      <WindowProgress ft={ft} />

      <footer className="ft-foot">
        <RunBadge ft={ft} />
        {ft.stage_position && (
          <Badge tone={ft.stage_position.state === 'long' ? 'long' : 'flat'} mono>
            stage {fmtBTC(ft.stage_position.btc)}
          </Badge>
        )}
        {ft.risk_blocks > 0 && <Badge tone="block">{ft.risk_blocks} risk block</Badge>}
        {ft.risk_warnings > 0 && <Badge tone="warn">{ft.risk_warnings} risk warning</Badge>}
        {ft.candles.recent_gaps && <Badge tone="warn">candle gaps</Badge>}
        <span className="spacer" />
        <Link to={{ pathname: `/forward-tests/${ft.book}`, search }} className="btn" id={`open-${ft.book}`}>
          Details
        </Link>
      </footer>
    </article>
  )
}

export function ForwardTestsPage() {
  usePageTitle('Forward tests')
  const q = useForwardTests()
  const missed = q.data?.books.filter((b) => b.run.status === 'missed') ?? []
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Forward tests</h1>
          <p>
            The frozen SMA50 trend rule, one card per pre-registered book. Paper numbers always appear next to
            buy-and-hold, after the pre-registered costs.
          </p>
        </div>
        {q.data && (
          <span className="faint" title={fmtUTC(q.data.generated_at)}>
            updated {fmtMx(q.data.generated_at)} (Mexico City)
          </span>
        )}
      </div>

      {missed.length > 0 && (
        <Banner tone="warn" title="The daily executor has missed a day" id="banner-missed">
          {missed.map((b) => `${bookLabel(b.book)}: ${b.run.missing_days.join(', ')}`).join(' · ')}. Expected bar{' '}
          {missed[0].run.expected_bar_date}. Run the executor, or check its logs.
        </Banner>
      )}

      {q.isLoading && (
        <div className="grid grid-2">
          <CardSkeleton lines={6} />
          <CardSkeleton lines={6} />
        </div>
      )}
      {q.isError && (
        <div className="card">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      )}
      {q.data && (
        <div className="grid grid-2">
          {q.data.books.map((b) => (
            <ForwardTestCard key={b.book} ft={b} />
          ))}
        </div>
      )}
    </>
  )
}
