/**
 * Plan §6.4.11: stage profit and loss with its attribution, the rule's open
 * trade, paper statistics, the backtest trade distribution and the Monte
 * Carlo of the rule against buy-and-hold. Reporting only.
 */
import { useState } from 'react'
import { useCapacity, useMonteCarlo, usePrereg, type MonteCarloParams } from '../api/client'
import type {
  BookSampleSummary,
  Calendar,
  Dist,
  MonteCarloResponse,
  PerformanceResponse,
  Prob,
  StagePnL,
  TaxLots,
  Trips,
} from '../api/schemas'
import { fmtBps, fmtDate, fmtFrac, fmtMoney, fmtPrice, fmtUTC } from '../lib/format'
import { PnLHistoryChart } from './charts'
import { Badge, CardSkeleton, Empty, ErrorState, Stat } from './ui'

/** Signed fraction: 0.0123 -> "+1.23%"; never "-0%". */
function sfrac(v: number, digits = 1): string {
  const s = fmtFrac(v, digits)
  if (Number(s.replace('%', '')) === 0) return fmtFrac(0, digits)
  return v > 0 ? `+${s}` : s
}

function smoney(v: number, quote: string): string {
  const s = fmtMoney(Math.abs(v), quote)
  return v > 0 ? `+${s}` : v < 0 ? `−${s}` : s
}

const tone = (v: number): 'pos' | 'neg' | undefined => (v > 0 ? 'pos' : v < 0 ? 'neg' : undefined)

/* ---------------- Stage P&L ---------------- */

function Attribution({ p, quote }: { p: StagePnL; quote: string }) {
  const rows: [string, number, string][] = [
    [
      'Market move at the fill days\u2019 opens',
      p.market_pnl,
      'what the same trades earned with no fees and no slippage',
    ],
    ['Fees', -p.fees, 'Bitso commission (taken in BTC on buys)'],
    [
      'Slippage vs the open',
      -p.slippage,
      'fill price against the fill day\u2019s open, the paper account\u2019s price',
    ],
  ]
  const scale = Math.max(...rows.map((r) => Math.abs(r[1])), Math.abs(p.total), 1e-9)
  return (
    <div className="attrib" role="table" aria-label="P&L attribution" data-testid="pnl-attribution">
      {rows.map(([label, v, hint]) => (
        <div className="attrib-row" role="row" key={label} title={hint}>
          <span role="cell" className="attrib-label">
            {label}
          </span>
          <span role="cell" className="attrib-bar">
            <span
              className={`attrib-fill ${v >= 0 ? 'up' : 'down'}`}
              style={{ width: `${(Math.abs(v) / scale) * 100}%` }}
            />
          </span>
          <span role="cell" className={`num attrib-val ${tone(v) ?? ''}`}>
            {smoney(v, quote)}
          </span>
        </div>
      ))}
      <div className="attrib-row total" role="row">
        <span role="cell" className="attrib-label">
          Total P&amp;L
        </span>
        <span role="cell" className="attrib-bar">
          <span
            className={`attrib-fill ${p.total >= 0 ? 'up' : 'down'}`}
            style={{ width: `${(Math.abs(p.total) / scale) * 100}%` }}
          />
        </span>
        <span role="cell" className={`num attrib-val ${tone(p.total) ?? ''}`}>
          {smoney(p.total, quote)}
        </span>
      </div>
    </div>
  )
}

export function PnLSection({ perf }: { perf: PerformanceResponse }) {
  const p = perf.pnl
  const q = perf.quote
  if (!p) {
    return <Empty title="No stage fills yet">P&amp;L appears after the executor trades on Bitso stage.</Empty>
  }
  return (
    <div className="stack" style={{ gap: 16 }}>
      <div className="grid grid-4" data-testid="pnl-stats">
        <Stat
          label="Total P&L"
          value={smoney(p.total, q)}
          tone={tone(p.total)}
          hint={`${sfrac(p.return_on_invested, 2)} on ${fmtMoney(p.invested, q)} invested`}
          large
        />
        <Stat
          label="Unrealized / realized"
          value={`${smoney(p.unrealized, q)}`}
          tone={tone(p.unrealized)}
          hint={`realized ${smoney(p.realized, q)} · marked at ${fmtPrice(p.mark, q)} (${fmtDate(p.mark_date)})`}
        />
        <Stat
          label="Stage vs paper, same days"
          value={fmtBps(p.shortfall_bps, true)}
          tone={tone(p.shortfall_bps)}
          hint={`stage ${sfrac(p.return_on_invested, 2)} · paper ${sfrac(p.paper_return, 2)} since ${fmtDate(p.since)}`}
          title="Implementation shortfall: the stage position's return minus the paper account's over the same days"
        />
        <Stat
          label="Break-even sell price"
          value={p.position_btc > 0 ? fmtPrice(p.break_even_primary, q) : '—'}
          hint={
            p.position_btc > 0
              ? `${sfrac(p.break_even_primary / p.mark - 1, 2)} from the mark · ${fmtPrice(p.break_even_pessimistic, q)} at taker cost`
              : 'flat'
          }
        />
      </div>
      <Attribution p={p} quote={q} />
      <div className="faint" style={{ fontSize: 12 }}>
        {p.legs} leg{p.legs === 1 ? '' : 's'} since {fmtDate(p.since)} · holds {p.position_btc.toFixed(8)} BTC at an
        average cost of {fmtPrice(p.avg_cost, q)} (fees included) · average-cost method
      </div>
    </div>
  )
}

/* ---------------- Open trade and paper statistics ---------------- */

export function OpenTradeSection({ perf }: { perf: PerformanceResponse }) {
  const o = perf.open_trade
  const q = perf.quote
  if (!o) return <Empty title="The rule is flat">No trade in progress in the candle history.</Empty>
  return (
    <div className="grid grid-4" data-testid="open-trade">
      <Stat
        label="Rule long since"
        value={fmtDate(o.entry)}
        hint={`${o.days} days · entered at ${fmtPrice(o.entry_price, q)}`}
      />
      <Stat label="Trade return" value={sfrac(o.return)} tone={tone(o.return)} hint="gross, to the last close" />
      <Stat
        label="Best / worst close"
        value={`${sfrac(o.max_favorable)} / ${sfrac(o.max_adverse)}`}
        hint={`${fmtPrice(o.best_close, q)} / ${fmtPrice(o.worst_close, q)}`}
      />
      <Stat
        label="Exits on a close below"
        value={fmtPrice(o.exit_below, q)}
        tone={o.exit_distance > -0.03 ? 'warn' : undefined}
        hint={`SMA50, ${sfrac(o.exit_distance)} from the last close`}
      />
    </div>
  )
}

export function PaperStatsSection({ perf }: { perf: PerformanceResponse }) {
  const s = perf.paper
  if (s.days === 0) return <Empty title="No forward days yet" />
  const r2 = (v: number) => v.toFixed(2)
  return (
    <div className="stack" style={{ gap: 10 }}>
      {!s.meaningful && (
        <div className="faint" style={{ fontSize: 12 }} data-testid="stats-caveat">
          {s.days} forward days: ratios need about 90 days before they mean much.
        </div>
      )}
      <div className="table-wrap">
        <table className="data compact" id="paper-stats">
          <thead>
            <tr>
              <th scope="col">Paper account</th>
              <th scope="col" className="r">
                Return
              </th>
              <th scope="col" className="r">
                Sharpe
              </th>
              <th scope="col" className="r">
                Sortino
              </th>
              <th scope="col" className="r">
                Calmar
              </th>
              <th scope="col" className="r">
                Ann. vol
              </th>
              <th scope="col" className="r">
                Max DD
              </th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>SMA50 rule</td>
              <td className={`r num ${tone(s.return) ?? ''}`}>{sfrac(s.return, 2)}</td>
              <td className="r num">{r2(s.sharpe)}</td>
              <td className="r num">{r2(s.sortino)}</td>
              <td className="r num">{r2(s.calmar)}</td>
              <td className="r num">{fmtFrac(s.ann_vol, 1)}</td>
              <td className="r num">{fmtFrac(s.max_dd, 1)}</td>
            </tr>
            <tr className="faint">
              <td>Buy-and-hold</td>
              <td className="r num">{sfrac(s.hold_return, 2)}</td>
              <td className="r num">{r2(s.hold_sharpe)}</td>
              <td className="r num">—</td>
              <td className="r num">—</td>
              <td className="r num">—</td>
              <td className="r num">{fmtFrac(s.hold_max_dd, 1)}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  )
}

/* ---------------- Trade distribution ---------------- */

function CalendarTable({ c, id }: { c: Calendar; id: string }) {
  return (
    <div className="table-wrap">
      <table className="data compact" id={id}>
        <thead>
          <tr>
            <th scope="col">Year</th>
            <th scope="col" className="r">
              Rule
            </th>
            <th scope="col" className="r">
              Hold
            </th>
            <th scope="col" className="r">
              Rule DD
            </th>
            <th scope="col" className="r">
              Hold DD
            </th>
            <th scope="col" className="r">
              Trips
            </th>
            <th scope="col">H2 beats hold</th>
            <th scope="col">H1 shallower DD</th>
          </tr>
        </thead>
        <tbody>
          {c.years.map((y) => (
            <tr key={y.year}>
              <td title={`${y.from} → ${y.to}`}>{y.year}</td>
              <td className={`r num ${tone(y.trend_return) ?? ''}`}>{sfrac(y.trend_return, 0)}</td>
              <td className="r num faint">{sfrac(y.hold_return, 0)}</td>
              <td className="r num">{fmtFrac(y.trend_max_dd, 0)}</td>
              <td className="r num faint">{fmtFrac(y.hold_max_dd, 0)}</td>
              <td className="r num">{y.round_trips}</td>
              <td>{y.beats_hold ? <Badge tone="ok">yes</Badge> : <span className="faint">no</span>}</td>
              <td>{y.shallower_dd ? <Badge tone="ok">yes</Badge> : <span className="faint">no</span>}</td>
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr>
            <td colSpan={6} className="faint">
              Years the hypothesis held
            </td>
            <td className="num">
              {c.beats_hold} / {c.years.length}
            </td>
            <td className="num">
              {c.shallower_dd} / {c.years.length}
            </td>
          </tr>
        </tfoot>
      </table>
    </div>
  )
}

export function TradeDistributionSection({ perf }: { perf: PerformanceResponse }) {
  const h = perf.history
  if (!h || h.trips.count === 0) {
    return (
      <Empty title="Not enough candle history">The trade distribution needs the book&apos;s full daily history.</Empty>
    )
  }
  const t: Trips = h.trips
  return (
    <div className="stack" style={{ gap: 16 }}>
      <div className="grid grid-4" data-testid="trade-dist">
        <Stat label="Closed trades" value={String(t.count)} hint={`${t.window} · ${h.leg_bps} bps per leg`} />
        <Stat
          label="Win rate"
          value={fmtFrac(t.win_rate, 0)}
          hint={`avg win ${sfrac(t.avg_win)} · avg loss ${sfrac(t.avg_loss)}`}
        />
        <Stat
          label="Expectancy / median trade"
          value={`${sfrac(t.mean)} / ${sfrac(t.median)}`}
          tone={tone(t.mean)}
          hint={`payoff ${t.payoff.toFixed(1)}× · ${t.mean_days.toFixed(0)} days on average`}
        />
        <Stat
          label="Without the best trade"
          value={sfrac(t.compounded_ex_best, 0)}
          tone={tone(t.compounded_ex_best)}
          hint={`all trades ${sfrac(t.compounded, 0)} · without the best 3 ${sfrac(t.compounded_ex_best_three, 0)}`}
          title={`Best: ${t.best.entry} → ${t.best.exit}, ${sfrac(t.best.return, 0)}`}
        />
      </div>
      <p className="faint" style={{ fontSize: 12, margin: 0 }}>
        Most trades lose a little (whipsaws around the average, paying costs both ways); a few long trends pay for all
        of them. Best trade {fmtDate(t.best.entry)} → {fmtDate(t.best.exit)}, {sfrac(t.best.return, 0)}; worst{' '}
        {sfrac(t.worst.return)}. The expectancy is a mean over a skewed distribution, not a typical outcome.
      </p>
      <CalendarTable c={h.calendar} id="calendar-years" />
    </div>
  )
}

/* ---------------- Monte Carlo ---------------- */

function ProbRow({ label, p, hist, hint }: { label: string; p: Prob; hist?: string; hint: string }) {
  return (
    <div className="prob-row" title={hint}>
      <span className="prob-label">{label}</span>
      <span className="prob-track" aria-hidden="true">
        <span className="prob-ci" style={{ left: `${p.lo * 100}%`, width: `${(p.hi - p.lo) * 100}%` }} />
        <span className="prob-dot" style={{ left: `${p.p * 100}%` }} />
      </span>
      <span className="num prob-val">
        {fmtFrac(p.p, 0)}{' '}
        <span className="faint">
          ({fmtFrac(p.lo, 0)}–{fmtFrac(p.hi, 0)})
        </span>
      </span>
      <span className="num faint prob-hist">{hist ?? ''}</span>
    </div>
  )
}

function DistRow({ label, d, kind = 'signed' }: { label: string; d: Dist; kind?: 'signed' | 'magnitude' | 'count' }) {
  const f = (v: number) => (kind === 'count' ? v.toFixed(0) : kind === 'magnitude' ? fmtFrac(v, 0) : sfrac(v, 0))
  return (
    <tr>
      <td>{label}</td>
      <td className="r num">{f(d.p5)}</td>
      <td className="r num">{f(d.p25)}</td>
      <td className="r num strong">{f(d.p50)}</td>
      <td className="r num">{f(d.p75)}</td>
      <td className="r num">{f(d.p95)}</td>
    </tr>
  )
}

/** Overlaid histogram of 12-month returns: rule vs buy-and-hold. */
export function ReturnHistogram({ m }: { m: MonteCarloResponse }) {
  const bins = m.summary.histogram
  const W = 600
  const H = 170
  const pad = 22
  const max = Math.max(1, ...bins.map((b) => Math.max(b.trend, b.hold)))
  const bw = (W - 2 * pad) / bins.length
  const lo = bins[0]?.lo ?? 0
  const hi = bins[bins.length - 1]?.hi ?? 1
  const x0 = pad + ((0 - lo) / (hi - lo)) * (W - 2 * pad)
  const s = m.summary
  return (
    <svg
      className="mc-hist"
      viewBox={`0 0 ${W} ${H + 24}`}
      role="img"
      aria-label={`Distribution of ${s.config.horizon_days}-day returns over ${s.config.paths} paths: rule median ${sfrac(s.trend_return.p50, 0)}, buy-and-hold median ${sfrac(s.hold_return.p50, 0)}`}
      data-testid="mc-histogram"
    >
      {bins.map((b, i) => {
        const x = pad + i * bw
        const th = (b.trend / max) * H
        const hh = (b.hold / max) * H
        return (
          <g key={i}>
            <title>{`${sfrac(b.lo, 0)} to ${sfrac(b.hi, 0)}: rule ${b.trend}, hold ${b.hold} paths`}</title>
            <rect className="bar-hold" x={x + 1} y={H - hh} width={bw - 2} height={hh} />
            <rect className="bar-trend" x={x + bw * 0.25} y={H - th} width={bw * 0.5} height={th} />
          </g>
        )
      })}
      {x0 > pad && x0 < W - pad && <line className="zero" x1={x0} x2={x0} y1={0} y2={H} />}
      <line className="axis" x1={pad} x2={W - pad} y1={H} y2={H} />
      <text className="tick" x={pad} y={H + 16}>
        {sfrac(lo, 0)}
      </text>
      {x0 > pad + 30 && x0 < W - pad - 30 && (
        <text className="tick" x={x0} y={H + 16} textAnchor="middle">
          0%
        </text>
      )}
      <text className="tick" x={W - pad} y={H + 16} textAnchor="end">
        {sfrac(hi, 0)}
      </text>
    </svg>
  )
}

const COSTS: { id: MonteCarloParams['cost']; label: string }[] = [
  { id: 'primary', label: 'primary' },
  { id: 'pessimistic', label: 'taker' },
  { id: 'realized', label: 'realized' },
]
const BLOCKS = [5, 20, 60] as const
const PATHS = [2000, 10000] as const

export function MonteCarloSection({ book }: { book: string }) {
  const [p, setP] = useState<MonteCarloParams>({ cost: 'primary', block: 20, paths: 2000 })
  const mc = useMonteCarlo(book, p)
  const m = mc.data
  return (
    <div className="stack" style={{ gap: 14 }}>
      <div className="row wrap" style={{ gap: 12 }}>
        <div className="seg" role="group" aria-label="Cost per leg">
          {COSTS.map((c) => (
            <button
              key={c.id}
              id={`mc-cost-${c.id}`}
              aria-pressed={p.cost === c.id}
              onClick={() => setP({ ...p, cost: c.id })}
            >
              {c.label}
            </button>
          ))}
        </div>
        <div className="seg" role="group" aria-label="Mean block length">
          {BLOCKS.map((b) => (
            <button key={b} id={`mc-block-${b}`} aria-pressed={p.block === b} onClick={() => setP({ ...p, block: b })}>
              {b}d blocks
            </button>
          ))}
        </div>
        <div className="seg" role="group" aria-label="Paths">
          {PATHS.map((n) => (
            <button key={n} id={`mc-paths-${n}`} aria-pressed={p.paths === n} onClick={() => setP({ ...p, paths: n })}>
              {n.toLocaleString('en-US')} paths
            </button>
          ))}
        </div>
      </div>
      {mc.isLoading && <CardSkeleton lines={5} />}
      {mc.isError && <ErrorState error={mc.error} onRetry={() => mc.refetch()} />}
      {m && <MonteCarloResult m={m} />}
    </div>
  )
}

function MonteCarloResult({ m }: { m: MonteCarloResponse }) {
  const s = m.summary
  const c = m.calibration
  const n = c.years.length
  return (
    <div className="stack" style={{ gap: 14 }} data-testid="mc-result">
      <div className="sub">
        {s.config.paths.toLocaleString('en-US')} paths × {s.config.horizon_days} days from the{' '}
        {fmtPrice(s.start_close, m.book.split('_')[1] ?? '')} close (rule {s.start_long ? 'long' : 'flat'}), resampling{' '}
        {s.sample_days.toLocaleString('en-US')} days of {fmtDate(s.sample_from)} → {fmtDate(s.sample_to)} in{' '}
        {s.config.mean_block_days}-day blocks · {m.leg_bps} bps per leg ({m.cost}) · seed {s.config.seed}
        {m.cached ? ' · cached' : ` · ${m.elapsed_ms} ms`}
      </div>
      <div className="grid grid-2" style={{ gap: 16 }}>
        <div className="stack" style={{ gap: 8 }} data-testid="mc-probs">
          <div className="prob-row head">
            <span />
            <span />
            <span className="faint">simulated (95% CI)</span>
            <span className="faint">history</span>
          </div>
          <ProbRow
            label="H2: rule beats hold"
            p={s.p_beats_hold}
            hist={`${c.beats_hold}/${n} yrs`}
            hint="Share of paths where the rule's return exceeds buy-and-hold's"
          />
          <ProbRow
            label="H1: shallower drawdown"
            p={s.p_shallower_dd}
            hist={`${c.shallower_dd}/${n} yrs`}
            hint="Share of paths where the rule's max drawdown is below buy-and-hold's"
          />
          <ProbRow label="Rule loses money" p={s.p_trend_loss} hint="Share of paths with a negative rule return" />
          <ProbRow
            label="Hold loses money"
            p={s.p_hold_loss}
            hint="Share of paths with a negative buy-and-hold return"
          />
        </div>
        <div>
          <div className="legend" style={{ marginBottom: 6 }}>
            <span className="key" style={{ color: 'var(--long)' }}>
              <span className="swatch" /> SMA50 rule
            </span>
            <span className="key" style={{ color: 'var(--bench)' }}>
              <span className="swatch" /> buy-and-hold
            </span>
          </div>
          <ReturnHistogram m={m} />
        </div>
      </div>
      <div className="table-wrap">
        <table className="data compact" id="mc-quantiles">
          <thead>
            <tr>
              <th scope="col">{s.config.horizon_days}-day outcome</th>
              <th scope="col" className="r">
                5%
              </th>
              <th scope="col" className="r">
                25%
              </th>
              <th scope="col" className="r">
                median
              </th>
              <th scope="col" className="r">
                75%
              </th>
              <th scope="col" className="r">
                95%
              </th>
            </tr>
          </thead>
          <tbody>
            <DistRow label="Rule return" d={s.trend_return} />
            <DistRow label="Buy-and-hold return" d={s.hold_return} />
            <DistRow label="Rule minus hold" d={s.excess} />
            <DistRow label="Rule max drawdown" d={s.trend_max_dd} kind="magnitude" />
            <DistRow label="Hold max drawdown" d={s.hold_max_dd} kind="magnitude" />
            <DistRow label="Rule round trips" d={s.trend_round_trips} kind="count" />
          </tbody>
        </table>
      </div>
      <p className="faint" style={{ fontSize: 12, margin: 0 }}>
        Stationary block bootstrap of real days (overnight gap and intraday move), run through the frozen rule and the
        registered simulator. Blocks cut long trends, so the rule&apos;s upside is understated: longer blocks raise its
        odds against holding. Reporting only. It never changes the pre-registered rule or its evaluation.
      </p>
    </div>
  )
}

/* ---------------- Plan §6.4.12: daily P&L, MXN terms, capacity ---------------- */

export function PnLHistorySection({ perf }: { perf: PerformanceResponse }) {
  const days = perf.pnl_history
  const q = perf.quote
  if (days.length === 0)
    return <Empty title="No stage fills yet">The daily P&amp;L starts on the first fill day.</Empty>
  const last = days[days.length - 1]
  const best = days.reduce((a, d) => (d.daily > a.daily ? d : a), days[0])
  const worst = days.reduce((a, d) => (d.daily < a.daily ? d : a), days[0])
  return (
    <div className="stack" style={{ gap: 12 }}>
      <div className="legend">
        <span className="key" style={{ color: 'var(--info)' }}>
          <span className="swatch" /> stage P&amp;L
        </span>
        <span className="key" style={{ color: 'var(--bench)' }}>
          <span className="swatch dashed" /> paper on the same money
        </span>
        <span className="key" style={{ color: 'var(--long)' }}>
          <span className="swatch" /> daily change
        </span>
      </div>
      <PnLHistoryChart days={days} label={`Stage P&L by day since ${days[0].date}`} />
      <div className="grid grid-4" data-testid="nav-stats">
        <Stat
          label="NAV"
          value={perf.capital > 0 ? fmtMoney(last.nav, q) : '—'}
          hint={perf.capital > 0 ? `capital ${fmtMoney(perf.capital, q, 0)} + P&L` : 'no capital in the policy'}
        />
        <Stat
          label="Stage vs paper (same money)"
          value={smoney(last.total - last.paper_pnl, q)}
          tone={tone(last.total - last.paper_pnl)}
          hint={`paper ${smoney(last.paper_pnl, q)}`}
        />
        <Stat label="Best day" value={smoney(best.daily, q)} tone={tone(best.daily)} hint={fmtDate(best.date)} />
        <Stat label="Worst day" value={smoney(worst.daily, q)} tone={tone(worst.daily)} hint={fmtDate(worst.date)} />
      </div>
      <div className="faint" style={{ fontSize: 12 }}>
        {days.length} day{days.length === 1 ? '' : 's'} since the first fill, marked at each close; capital is the
        policy&apos;s maximum order notional (the stage capital).
      </div>
    </div>
  )
}

export function MXNTermsSection({ perf }: { perf: PerformanceResponse }) {
  const m = perf.mxn_terms
  if (!m) return null
  return (
    <div className="stack" style={{ gap: 12 }} data-testid="mxn-terms">
      <div className="grid grid-4">
        <Stat
          label="Rule in MXN, after conversions"
          value={sfrac(m.paper_return_mxn, 2)}
          tone={tone(m.paper_return_mxn)}
          hint={`${sfrac(m.paper_return_usd, 2)} in USD if closed · ${m.conversion_bps} bps each way`}
          large
        />
        <Stat
          label="Hold btc_mxn (the H2 benchmark)"
          value={sfrac(m.hold_btc_mxn, 2)}
          tone={tone(m.hold_btc_mxn)}
          hint="one round trip at 70 bps per leg"
          large
        />
        <Stat
          label="H2 so far"
          value={
            <>
              {sfrac(m.excess, 2)} <Badge tone={m.h2_so_far ? 'ok' : 'warn'}>{m.h2_so_far ? 'ahead' : 'behind'}</Badge>
            </>
          }
          hint={`evaluated on ${fmtDate('2027-09-26')}; reported, not a decision`}
          large
        />
        <Stat
          label="Implied USD/MXN"
          value={m.fx_end.toFixed(4)}
          hint={`${sfrac(m.fx_change, 2)} since ${m.fx_start.toFixed(4)} (close before ${fmtDate(m.from)})`}
          large
        />
      </div>
      <div className="faint" style={{ fontSize: 12 }}>
        Stage position in pesos: {smoney(m.stage_pnl_mxn, 'mxn')} on {fmtMoney(m.stage_invested_mxn, 'mxn')} at
        today&apos;s implied rate. USD/MXN is btc_mxn&apos;s close over btc_usd&apos;s on the same Mexico City day, as
        the pre-registration defines it.
      </div>
    </div>
  )
}

export function CapacitySection({ book }: { book: string }) {
  const cap = useCapacity(book)
  const c = cap.data
  if (cap.isLoading) return <CardSkeleton lines={5} />
  if (cap.isError) return <ErrorState error={cap.error} onRetry={() => cap.refetch()} />
  if (!c) return null
  const btc = (v: number) => (v >= 1 ? v.toFixed(2) : v >= 0.1 ? v.toFixed(3) : v.toFixed(4))
  const bps = (v: number | null) => (v == null ? '—' : v.toFixed(1))
  return (
    <div className="stack" style={{ gap: 14 }} data-testid="capacity">
      <div className="grid grid-4">
        <Stat
          label={`Capacity at ${c.slippage_budget_bps} bps slippage`}
          value={c.walk_capacity_btc != null ? `${btc(c.walk_capacity_btc)} BTC` : '—'}
          hint={c.walk_capacity_btc != null ? 'visible book, both sides, now' : 'needs the live book'}
          large
        />
        <Stat
          label="Square-root law"
          value={`${btc(c.sqrt_capacity_lo_btc)}–${btc(c.sqrt_capacity_hi_btc)} BTC`}
          hint={`Y = 1.0–0.5 · ADV ${c.adv_btc.toFixed(1)} BTC · σ ${fmtFrac(c.daily_vol, 2)}/day`}
          large
        />
        <Stat
          label="Today's order vs policy cap"
          value={`${c.stage_size_btc} / ${c.policy_max_order_btc} BTC`}
          hint="stage leg / max order"
          large
        />
        <Stat
          label="Book"
          value={
            c.book_status === 'live' ? (
              <>
                {fmtBps(c.spread_bps)} <span className="faint">spread</span>
              </>
            ) : (
              c.book_status
            )
          }
          hint={
            c.book_status === 'none'
              ? 'no live feed'
              : `depth ${c.bid_depth_btc.toFixed(2)} / ${c.ask_depth_btc.toFixed(2)} BTC (top 20)`
          }
          large
        />
      </div>
      <div className="table-wrap">
        <table className="data compact" id="capacity-table">
          <thead>
            <tr>
              <th scope="col">Order</th>
              <th scope="col" className="r">
                Notional
              </th>
              <th scope="col" className="r">
                % of daily volume
              </th>
              <th scope="col" className="r">
                Walk buy / sell (bps)
              </th>
              <th scope="col" className="r">
                Square-root (bps)
              </th>
              <th scope="col" className="r">
                Taker all-in (bps)
              </th>
              <th scope="col">Within {c.slippage_budget_bps} bps</th>
            </tr>
          </thead>
          <tbody>
            {c.rows.map((r) => (
              <tr key={r.qty_btc} className={r.qty_btc === c.stage_size_btc ? 'current' : undefined}>
                <td className="num">{r.qty_btc} BTC</td>
                <td className="r num">{fmtMoney(r.notional, c.quote, 0)}</td>
                <td className="r num">{fmtFrac(r.pct_adv, r.pct_adv < 0.01 ? 2 : 1)}</td>
                <td className="r num" title={r.book_fills ? undefined : 'larger than the visible book'}>
                  {bps(r.buy_walk_bps)} / {bps(r.sell_walk_bps)}
                  {r.buy_walk_bps != null && !r.book_fills ? ' *' : ''}
                </td>
                <td className="r num">
                  {r.sqrt_lo_bps.toFixed(1)}–{r.sqrt_hi_bps.toFixed(1)}
                </td>
                <td className="r num">{r.taker_total_bps.toFixed(0)}</td>
                <td>{r.within_budget ? <Badge tone="ok">yes</Badge> : <Badge tone="warn">no</Badge>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <BookHistory h={c.history ?? null} days={c.history_days ?? 30} btc={btc} />
      <p className="faint" style={{ fontSize: 12, margin: 0 }}>
        The pre-registered costs are a fee ({c.maker_fee_bps} maker / {c.taker_fee_bps} taker bps) plus{' '}
        {c.slippage_budget_bps} bps of slippage per leg; this measures order sizes against that slippage. Walk: a market
        order through the visible book, mid-priced (* past its depth). Square-root law: Y·σ·√(size / daily volume), Y =
        0.5–1. {c.note} The book moves second to second: read the capacity as an order of magnitude.
      </p>
    </div>
  )
}

/** The hourly book samples as percentiles (plan §6.4.13): capacity as a distribution, not one snapshot. */
function BookHistory({ h, days, btc }: { h: BookSampleSummary | null; days: number; btc: (v: number) => string }) {
  if (!h)
    return (
      <p className="faint" style={{ fontSize: 12, margin: 0 }} data-testid="book-history-empty">
        No hourly book samples yet: the sampler (scripts/ops-run.sh sampler, cron at :07) builds the distribution of
        spread, depth and capacity over the last {days} days.
      </p>
    )
  const p = (x: { p10: number; p50: number; p90: number }, f: (v: number) => string) => (
    <>
      <td className="r num">{f(x.p10)}</td>
      <td className="r num">
        <strong>{f(x.p50)}</strong>
      </td>
      <td className="r num">{f(x.p90)}</td>
    </>
  )
  const b1 = (v: number) => v.toFixed(1)
  return (
    <div className="stack" style={{ gap: 6 }} data-testid="book-history">
      <div className="faint" style={{ fontSize: 12 }}>
        Over time: {h.samples} hourly samples of the public book, {fmtUTC(h.from)} → {fmtUTC(h.to)} (last {days} days)
      </div>
      <div className="table-wrap">
        <table className="data compact" id="book-history-table">
          <thead>
            <tr>
              <th scope="col">Measure</th>
              <th scope="col" className="r">
                p10
              </th>
              <th scope="col" className="r">
                median
              </th>
              <th scope="col" className="r">
                p90
              </th>
              <th scope="col" className="r">
                Book covered
              </th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>Spread (bps)</td>
              {p(h.spread_bps, b1)}
              <td />
            </tr>
            <tr>
              <td>Bid / ask depth, top 20 (BTC)</td>
              {p(
                {
                  p10: Math.min(h.bid_depth_btc.p10, h.ask_depth_btc.p10),
                  p50: Math.min(h.bid_depth_btc.p50, h.ask_depth_btc.p50),
                  p90: Math.min(h.bid_depth_btc.p90, h.ask_depth_btc.p90),
                },
                (v) => v.toFixed(2),
              )}
              <td className="r faint">thinner side</td>
            </tr>
            <tr>
              <td>Capacity at {h.budget_bps} bps (BTC)</td>
              {p(h.walk_capacity_btc, btc)}
              <td />
            </tr>
            {h.sizes.map((z) => (
              <tr key={z.qty_btc}>
                <td>Walk cost, {z.qty_btc} BTC (worse side, bps)</td>
                {z.covered_share > 0 ? (
                  p(z.worse_side_bps, b1)
                ) : (
                  <td className="r faint" colSpan={3} title="no sample's visible depth covered this size">
                    past the visible book
                  </td>
                )}
                <td className="r num">{fmtFrac(z.covered_share, 0)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

/** The latest pre-registered verdicts (plan §6.4.14): H1/H2/H3 so far, never a decision before the end date. */
export function PreregSection({ book }: { book: string }) {
  const q = usePrereg(book)
  if (q.isLoading) return <CardSkeleton lines={4} />
  if (q.isError) return <ErrorState error={q.error} onRetry={() => q.refetch()} />
  const p = q.data
  if (!p) return null
  if (!p.found || !p.verdict)
    return (
      <Empty title="No verdict report yet">
        scripts/prereg-evaluation.sh writes one (weekly via scripts/ops-run.sh prereg). Interim look {p.interim_date},
        evaluation {p.final_date}.
      </Empty>
    )
  const v = p.verdict
  const num = (x: number) => x.toFixed(2)
  return (
    <div className="stack" style={{ gap: 14 }} data-testid="prereg">
      <div className="grid grid-4">
        <Stat
          label="Report"
          value={p.phase === 'as-of' ? 'progress' : p.phase}
          hint={`data through ${fmtDate(p.as_of)}`}
          large
        />
        <Stat
          label="Decides?"
          value={p.decides ? 'yes' : 'no'}
          tone={p.decides ? 'warn' : undefined}
          hint={p.decides ? 'primary costs decide' : 'reported only'}
          large
        />
        <Stat
          label="Interim look"
          value={p.days_to_interim > 0 ? `${p.days_to_interim} days` : 'due'}
          hint={`${fmtDate(p.interim_date)} · report only`}
          large
        />
        <Stat
          label="Evaluation"
          value={p.days_to_final > 0 ? `${p.days_to_final} days` : 'due'}
          hint={fmtDate(p.final_date)}
          large
        />
      </div>
      {v.scenarios.map((sc) => (
        <div key={sc.name} className="table-wrap">
          <table className="data compact" id={`prereg-${sc.name}`}>
            <caption className="faint" style={{ textAlign: 'left', fontSize: 12, paddingBottom: 6 }}>
              {sc.name === 'primary' ? 'Primary' : 'Secondary'} costs, {sc.leg_bps} bps each leg (
              {sc.name === 'primary' ? 'decides' : 'reported'}) · {sc.bars} bars since {fmtDate(sc.from)} ·{' '}
              {sc.round_trips} round trips
            </caption>
            <thead>
              <tr>
                <th scope="col">Hypothesis</th>
                <th scope="col">Criterion</th>
                <th scope="col" className="r">
                  Rule
                </th>
                <th scope="col" className="r">
                  Benchmark
                </th>
                <th scope="col">So far</th>
              </tr>
            </thead>
            <tbody>
              {sc.hypotheses.map((h) => (
                <tr key={h.id}>
                  <td className="mono">{h.id}</td>
                  <td>
                    {h.criteria}
                    {h.note ? (
                      <div className="faint" style={{ fontSize: 12 }}>
                        {h.note}
                      </div>
                    ) : null}
                  </td>
                  <td className="r num">{num(h.trend)}</td>
                  <td className="r num">{num(h.benchmark)}</td>
                  <td>{h.pass ? <Badge tone="ok">pass</Badge> : <Badge tone="warn">not yet</Badge>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ))}
      <p className="faint" style={{ fontSize: 12, margin: 0 }}>
        As registered ({v.prereg}): {v.reading} {p.note} Source: {p.source}.
      </p>
    </div>
  )
}

/** FIFO tax lots of the stage fills (plan §6.4.14). Reporting only, not tax advice. */
export function TaxLotsSection({ perf }: { perf: PerformanceResponse }) {
  const t: TaxLots | null | undefined = perf.tax_lots
  if (!t) return <Empty title="No stage fills yet">Tax lots appear with the first filled leg.</Empty>
  const q = t.quote
  const mxn = (v: number | null) => (v == null ? '—' : fmtMoney(v, 'mxn'))
  return (
    <div className="stack" style={{ gap: 14 }} data-testid="tax-lots">
      <div className="grid grid-4">
        <Stat label="Method" value={t.method} hint="oldest lot sold first" large />
        <Stat
          label="Realized"
          value={smoney(t.realized, q)}
          tone={tone(t.realized)}
          hint={`${t.sales.length} matched sales`}
          large
        />
        <Stat
          label="Unrealized"
          value={smoney(t.unrealized, q)}
          tone={tone(t.unrealized)}
          hint={`${t.open.length} open lots at ${fmtPrice(t.mark, q)} (${fmtDate(t.mark_date)})`}
          large
        />
        <Stat
          label="Unmatched sales"
          value={`${t.unmatched_btc} BTC`}
          tone={t.unmatched_btc > 0 ? 'warn' : undefined}
          hint="sold with no open lot (should be 0)"
          large
        />
      </div>
      <div className="table-wrap">
        <table className="data compact" id="tax-open-lots">
          <caption className="faint" style={{ textAlign: 'left', fontSize: 12, paddingBottom: 6 }}>
            Open lots
          </caption>
          <thead>
            <tr>
              <th scope="col">Bought</th>
              <th scope="col" className="r">
                Remaining BTC
              </th>
              <th scope="col" className="r">
                Cost per BTC
              </th>
              <th scope="col" className="r">
                Unrealized
              </th>
              <th scope="col" className="r">
                Held
              </th>
            </tr>
          </thead>
          <tbody>
            {t.open.map((l, i) => (
              <tr key={`${l.buy_date}-${i}`}>
                <td>{fmtDate(l.buy_date)}</td>
                <td className="r num">{l.remaining_btc.toFixed(8)}</td>
                <td className="r num">{fmtPrice(l.cost_per_btc, q)}</td>
                <td className={`r num ${tone(l.unrealized) ?? ''}`}>{smoney(l.unrealized, q)}</td>
                <td className="r num">{l.holding_days} d</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {t.years.length > 0 ? (
        <div className="table-wrap">
          <table className="data compact" id="tax-years">
            <caption className="faint" style={{ textAlign: 'left', fontSize: 12, paddingBottom: 6 }}>
              Realized by calendar year (sale date)
            </caption>
            <thead>
              <tr>
                <th scope="col">Year</th>
                <th scope="col" className="r">
                  Sales
                </th>
                <th scope="col" className="r">
                  Proceeds
                </th>
                <th scope="col" className="r">
                  Cost
                </th>
                <th scope="col" className="r">
                  Gain
                </th>
                <th scope="col" className="r">
                  Gain (MXN)
                </th>
              </tr>
            </thead>
            <tbody>
              {t.years.map((y) => (
                <tr key={y.year}>
                  <td className="num">{y.year}</td>
                  <td className="r num">{y.sales}</td>
                  <td className="r num">{fmtMoney(y.proceeds, q)}</td>
                  <td className="r num">{fmtMoney(y.cost, q)}</td>
                  <td className={`r num ${tone(y.gain) ?? ''}`}>{smoney(y.gain, q)}</td>
                  <td className="r num">{mxn(y.gain_mxn)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="faint" style={{ fontSize: 12, margin: 0 }} data-testid="tax-no-sales">
          No sales yet: nothing realized.
        </p>
      )}
      <p className="faint" style={{ fontSize: 12, margin: 0 }}>
        {t.note}
      </p>
    </div>
  )
}
