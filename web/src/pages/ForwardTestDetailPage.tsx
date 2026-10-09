import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { useCandles, useForwardTests, useLedger, useLedgerSearch, usePerformance } from '../api/client'
import { useLiveStream } from '../api/live'
import type { Fill, LedgerRecord } from '../api/schemas'
import { EquityChart, PriceChart } from '../components/charts'
import { LiveStrip } from '../components/live'
import {
  CapacitySection,
  MonteCarloSection,
  MXNTermsSection,
  OpenTradeSection,
  PaperStatsSection,
  PnLHistorySection,
  PnLSection,
  TradeDistributionSection,
} from '../components/performance'
import { Badge, Banner, CardSkeleton, Empty, ErrorState, LedgerBadge, SignalPill, Stat } from '../components/ui'
import {
  bookLabel,
  fmtBps,
  fmtBTC,
  fmtDate,
  fmtDuration,
  fmtFrac,
  fmtMoney,
  fmtMx,
  fmtPct,
  fmtPrice,
  fmtUTC,
} from '../lib/format'
import { usePageTitle } from '../lib/usePageTitle'

const RANGES = [90, 180, 365] as const
/** Stable fallback while the ledger loads: `fills` is a chart build dependency. */
const NO_FILLS: Fill[] = []

function costTone(total: number, assumed: number): 'pos' | 'neg' | undefined {
  if (total > assumed) return 'neg'
  if (total < assumed) return 'pos'
  return undefined
}

function FillsTable({ fills, quote }: { fills: Fill[]; quote: string }) {
  if (fills.length === 0) {
    return <Empty title="No stage fills yet">Fills appear here after the executor trades on Bitso stage.</Empty>
  }
  return (
    <div className="table-wrap">
      <table className="data" id="fills-table">
        <thead>
          <tr>
            <th scope="col">Fill day</th>
            <th scope="col">Side</th>
            <th scope="col" className="r">
              Filled
            </th>
            <th scope="col" className="r">
              Maker / taker
            </th>
            <th scope="col" className="r">
              Avg price
            </th>
            <th scope="col" className="r">
              Ref open
            </th>
            <th scope="col" className="r">
              Fee
            </th>
            <th scope="col" className="r">
              Slippage
            </th>
            <th scope="col" className="r">
              Total cost
            </th>
            <th scope="col" className="r">
              Assumed
            </th>
            <th scope="col" className="r">
              Net BTC
            </th>
            <th scope="col">Duration</th>
          </tr>
        </thead>
        <tbody>
          {fills.map((f) => (
            <tr key={f.fill_date + f.side}>
              <td title={`decided on ${f.bar_date}`}>{fmtDate(f.fill_date)}</td>
              <td>
                <Badge tone={f.side === 'buy' ? 'long' : 'flat'}>{f.side}</Badge>{' '}
                {f.market_fallback && (
                  <Badge tone="warn" title="Post-only order did not fill in time; market fallback used">
                    fallback
                  </Badge>
                )}
              </td>
              <td className="r num">{f.filled_btc.toFixed(8)}</td>
              <td className="r num">
                {((f.maker_btc / (f.filled_btc || 1)) * 100).toFixed(0)}% /{' '}
                {((f.taker_btc / (f.filled_btc || 1)) * 100).toFixed(0)}%
              </td>
              <td className="r num">{fmtPrice(f.avg_price, quote)}</td>
              <td className="r num">{f.ref_open != null ? fmtPrice(f.ref_open, quote) : '—'}</td>
              <td className="r num" title={fmtMoney(f.fee_quote, quote)}>
                {fmtBps(f.fee_bps)}
              </td>
              <td className="r num">{f.slippage_bps != null ? fmtBps(f.slippage_bps, true) : '—'}</td>
              <td className={`r num ${costTone(f.total_cost_bps, f.assumed_leg_bps) ?? ''}`}>
                {fmtBps(f.total_cost_bps)}
              </td>
              <td className="r num faint">{fmtBps(f.assumed_leg_bps)}</td>
              <td className="r num">{f.net_btc.toFixed(8)}</td>
              <td className="num" title={`${fmtUTC(f.started)} → ${fmtUTC(f.finished)}`}>
                {fmtDuration(f.started, f.finished)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function LedgerTable({ records, quote }: { records: LedgerRecord[]; quote: string }) {
  const rows = [...records].reverse()
  return (
    <div className="stack" style={{ gap: 10 }}>
      <div className="table-wrap">
        <table className="data" id="ledger-table">
          <thead>
            <tr>
              <th scope="col">Bar</th>
              <th scope="col">Signal</th>
              <th scope="col">Action</th>
              <th scope="col" className="r">
                Close
              </th>
              <th scope="col" className="r">
                SMA50
              </th>
              <th scope="col" className="r">
                Equity
              </th>
              <th scope="col" className="r">
                Hold
              </th>
              <th scope="col">Stage</th>
              <th scope="col">Candles</th>
              <th scope="col">Recorded</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.decision.bar_date}>
                <td>{fmtDate(r.decision.bar_date)}</td>
                <td>
                  <Badge tone={r.decision.signal === 'long' ? 'long' : 'flat'}>{r.decision.signal}</Badge>
                </td>
                <td>{r.decision.action}</td>
                <td className="r num">{fmtPrice(r.decision.close, quote)}</td>
                <td className="r num">{fmtPrice(r.decision.sma50, quote)}</td>
                <td className="r num">{r.paper.equity.toFixed(4)}</td>
                <td className="r num faint">{r.paper.hold_equity.toFixed(4)}</td>
                <td className="num">
                  {r.stage ? `${r.stage.action} → ${r.stage.position_after.btc.toFixed(8)}` : '—'}
                </td>
                <td className="num faint" title={`${r.candles.bars} bars ${r.candles.first}..${r.candles.last}`}>
                  {r.candles.sha256_prefix}
                  {r.candles.recent_gaps ? ' · gaps' : ''}
                </td>
                <td title={fmtUTC(r.recorded_at)}>{fmtMx(r.recorded_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <details className="disclosure">
        <summary id="toggle-raw-ledger">Raw JSON of the latest record</summary>
        <pre className="json">{JSON.stringify(records[records.length - 1], null, 2)}</pre>
      </details>
    </div>
  )
}

export function ForwardTestDetailPage() {
  const { book = '' } = useParams()
  usePageTitle(`${bookLabel(book)} forward test`)
  const [days, setDays] = useState<(typeof RANGES)[number]>(180)
  const summary = useForwardTests()
  const ledger = useLedger(book)
  const candles = useCandles(book, days)
  const perf = usePerformance(book)
  const ft = summary.data?.books.find((b) => b.book === book)
  const search = useLedgerSearch()
  const live = useLiveStream(ft ? [book] : [])
  const snap = live.books[book]

  return (
    <>
      <div className="page-head">
        <div>
          <div className="crumbs">
            <Link to={{ pathname: '/forward-tests', search }}>Forward tests</Link> <span>/</span>{' '}
            <span>{bookLabel(book)}</span>
          </div>
          <div className="row" style={{ gap: 14 }}>
            <h1>{bookLabel(book)}</h1>
            {ft && <LedgerBadge name={ft.ledger} />}
            {ft && <SignalPill signal={ft.decision.signal} />}
          </div>
          {ft && (
            <p>
              Pre-registration <span className="num">{ft.prereg}</span> · code{' '}
              <span className="num">{ft.code_version}</span> · {ft.mode}
            </p>
          )}
        </div>
      </div>

      {summary.isError && (
        <div className="card">
          <ErrorState error={summary.error} onRetry={() => summary.refetch()} />
        </div>
      )}
      {summary.data && !ft && <Banner tone="warn" title={`Unknown book "${book}"`} />}
      {ft?.run.status === 'missed' && (
        <Banner tone="warn" title="Missed run">
          Missing ledger days: {ft.run.missing_days.join(', ')}. {ft.run.message}.
        </Banner>
      )}

      {ft && (
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="grid grid-4">
            <Stat
              label="Close"
              value={fmtPrice(ft.decision.close, ft.quote)}
              hint={fmtDate(ft.decision.bar_date)}
              large
            />
            <Stat
              label="Distance to SMA50"
              value={fmtPct(ft.distance_to_sma_pct, 1)}
              hint={`SMA ${fmtPrice(ft.decision.sma50, ft.quote)}`}
              large
            />
            <Stat
              label="Rule vs hold"
              value={fmtPct(ft.excess_vs_hold_pct)}
              tone={ft.excess_vs_hold_pct > 0.005 ? 'pos' : ft.excess_vs_hold_pct < -0.005 ? 'neg' : undefined}
              hint={`equity ${ft.paper.equity.toFixed(4)} · if closed ${ft.paper.equity_if_closed.toFixed(4)}`}
              large
            />
            <Stat
              label="Max drawdown"
              value={fmtFrac(ft.paper.max_drawdown)}
              hint={`since ${fmtDate(ft.paper.forward_start)}`}
              large
            />
          </div>
        </div>
      )}

      {ft && <LiveStrip ft={ft} snap={snap} live={live} large />}

      <div className="grid grid-2" style={{ marginBottom: 16 }}>
        <section className="card" aria-labelledby="price-h">
          <div className="card-head">
            <div>
              <h2 id="price-h">Daily candles</h2>
              <div className="legend" style={{ marginTop: 6 }}>
                <span className="key" style={{ color: 'var(--warn)' }}>
                  <span className="swatch" /> SMA50
                </span>
                <span className="key" style={{ color: 'var(--info)' }}>
                  <span className="swatch" /> volume ≥ 1.5× 20-day avg
                </span>
                <span className="key" style={{ color: 'var(--long)' }}>
                  ▲ flip / ● stage fill
                </span>
                {snap?.provisional && (
                  <span className="key" style={{ color: 'var(--info)' }} title={snap.provisional.label}>
                    <span className="swatch dashed" /> flip level (provisional)
                  </span>
                )}
              </div>
            </div>
            <div className="seg" role="group" aria-label="Range">
              {RANGES.map((r) => (
                <button key={r} aria-pressed={days === r} onClick={() => setDays(r)} id={`range-${r}`}>
                  {r}d
                </button>
              ))}
            </div>
          </div>
          {candles.isLoading && <div className="skeleton chart" />}
          {candles.isError && <ErrorState error={candles.error} onRetry={() => candles.refetch()} />}
          {candles.data && (
            <PriceChart
              candles={candles.data.candles}
              fills={ledger.data?.fills ?? NO_FILLS}
              quote={ft?.quote ?? 'mxn'}
              label={`${bookLabel(book)} daily candles with SMA50`}
              live={
                snap
                  ? { candle: snap.candle, flip: snap.provisional?.flip_level ?? null, fresh: live.phase === 'live' }
                  : undefined
              }
            />
          )}
          {candles.data && (
            <div className="faint num" style={{ fontSize: 11, marginTop: 8 }}>
              {candles.data.file}
            </div>
          )}
        </section>

        <section className="card" aria-labelledby="equity-h">
          <div className="card-head">
            <div>
              <h2 id="equity-h">Paper equity vs buy-and-hold</h2>
              <div className="legend" style={{ marginTop: 6 }}>
                <span className="key" style={{ color: 'var(--long)' }}>
                  <span className="swatch" /> SMA50 rule
                </span>
                <span className="key" style={{ color: 'var(--bench)' }}>
                  <span className="swatch dashed" /> buy-and-hold
                </span>
              </div>
            </div>
            {ft && <span className="sub">{ft.paper.leg_cost_bps} bps per leg, both lines</span>}
          </div>
          {ledger.isLoading && <div className="skeleton chart sm" />}
          {ledger.isError && <ErrorState error={ledger.error} onRetry={() => ledger.refetch()} />}
          {ledger.data &&
            (ledger.data.equity.length > 0 ? (
              <EquityChart points={ledger.data.equity} label="Paper equity versus buy-and-hold" />
            ) : (
              <Empty title="No forward days yet" />
            ))}
          {ft && (
            <div className="grid grid-3" style={{ marginTop: 12, gap: 12 }}>
              <Stat
                label="Interim look"
                value={fmtDate(ft.milestones.interim)}
                hint={`in ${ft.milestones.days_to_interim} days · report only`}
              />
              <Stat
                label="Evaluation"
                value={fmtDate(ft.milestones.evaluation)}
                hint={`in ${ft.milestones.days_to_evaluation} days`}
              />
              {ft.stage_position && (
                <Stat label="Stage holds" value={fmtBTC(ft.stage_position.btc)} hint={ft.stage_position.state} />
              )}
            </div>
          )}
        </section>
      </div>

      <section className="card" style={{ marginBottom: 16 }} aria-labelledby="pnl-h" id="pnl">
        <div className="card-head">
          <div>
            <h2 id="pnl-h">Stage profit and loss</h2>
            <div className="sub">
              What the stage position made, split into the market move, fees and slippage, and compared with the paper
              account over the same days.
            </div>
          </div>
        </div>
        {perf.isLoading && <CardSkeleton lines={3} />}
        {perf.isError && <ErrorState error={perf.error} onRetry={() => perf.refetch()} />}
        {perf.data && <PnLSection perf={perf.data} />}
      </section>

      <section className="card" style={{ marginBottom: 16 }} aria-labelledby="pnl-history-h" id="pnl-history">
        <div className="card-head">
          <div>
            <h2 id="pnl-history-h">Daily P&amp;L and NAV</h2>
            <div className="sub">The stage position marked at every close since the first fill.</div>
          </div>
        </div>
        {perf.data && <PnLHistorySection perf={perf.data} />}
      </section>

      {perf.data?.mxn_terms && (
        <section className="card" style={{ marginBottom: 16 }} aria-labelledby="mxn-h" id="mxn-terms">
          <div className="card-head">
            <div>
              <h2 id="mxn-h">In pesos: the pre-registered H2</h2>
              <div className="sub">
                The btc_usd forward test as a peso investor sees it, against holding btc_mxn over the same days.
              </div>
            </div>
          </div>
          <MXNTermsSection perf={perf.data} />
        </section>
      )}

      <div className="grid grid-2" style={{ marginBottom: 16 }}>
        <section className="card" aria-labelledby="open-trade-h">
          <div className="card-head">
            <div>
              <h2 id="open-trade-h">The rule&apos;s trade in progress</h2>
              <div className="sub">Backtest view from the full candle history: entry at the open after the flip.</div>
            </div>
          </div>
          {perf.data && <OpenTradeSection perf={perf.data} />}
        </section>
        <section className="card" aria-labelledby="paper-stats-h">
          <div className="card-head">
            <div>
              <h2 id="paper-stats-h">Forward-test statistics</h2>
              <div className="sub">Daily, 365-day year, no risk-free rate.</div>
            </div>
          </div>
          {perf.data && <PaperStatsSection perf={perf.data} />}
        </section>
      </div>

      <section className="card" style={{ marginBottom: 16 }} aria-labelledby="trades-h" id="trade-distribution">
        <div className="card-head">
          <div>
            <h2 id="trades-h">Expected profit per trade (backtest)</h2>
            <div className="sub">
              The frozen rule&apos;s closed round trips on Bitso&apos;s daily history at the pre-registered primary
              cost, and the rule against buy-and-hold per calendar year (the pre-registration&apos;s base rates).
            </div>
          </div>
        </div>
        {perf.data && <TradeDistributionSection perf={perf.data} />}
      </section>

      <section className="card" style={{ marginBottom: 16 }} aria-labelledby="mc-h" id="monte-carlo">
        <div className="card-head">
          <div>
            <h2 id="mc-h">Monte Carlo: the next 12 months</h2>
            <div className="sub">
              The rule against buy-and-hold on resampled price paths that continue from the last close.
            </div>
          </div>
        </div>
        {ft ? <MonteCarloSection book={book} /> : <CardSkeleton lines={4} />}
      </section>

      <section className="card" style={{ marginBottom: 16 }} aria-labelledby="capacity-h" id="capacity">
        <div className="card-head">
          <div>
            <h2 id="capacity-h">Capacity and market impact</h2>
            <div className="sub">
              How large an order can get before slippage exceeds the pre-registered 10 bps per leg.
            </div>
          </div>
        </div>
        <CapacitySection book={book} />
      </section>

      <section className="card" style={{ marginBottom: 16 }} aria-labelledby="fills-h">
        <div className="card-head">
          <div>
            <h2 id="fills-h">Stage execution</h2>
            <div className="sub">
              Cost = fee + slippage vs the fill day&apos;s open (the paper account&apos;s price). Red when above the
              pre-registered assumption.
            </div>
          </div>
        </div>
        {ledger.isLoading && <CardSkeleton lines={2} />}
        {ledger.data && <FillsTable fills={ledger.data.fills} quote={ft?.quote ?? 'mxn'} />}
      </section>

      <section className="card" aria-labelledby="ledger-h">
        <div className="card-head">
          <h2 id="ledger-h">Ledger</h2>
          {ledger.data && <span className="sub">{ledger.data.records.length} records, newest first</span>}
        </div>
        {ledger.data && ledger.data.records.length > 0 ? (
          <LedgerTable records={ledger.data.records} quote={ft?.quote ?? 'mxn'} />
        ) : (
          ledger.data && <Empty title="No ledger records" />
        )}
      </section>
    </>
  )
}
