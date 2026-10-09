/**
 * Plan §6.4.11: stage profit and loss with its attribution, the rule's open
 * trade, paper statistics, the backtest trade distribution and the Monte
 * Carlo of the rule against buy-and-hold. Reporting only.
 */
import { useState } from 'react'
import { useMonteCarlo, type MonteCarloParams } from '../api/client'
import type { Calendar, Dist, MonteCarloResponse, PerformanceResponse, Prob, StagePnL, Trips } from '../api/schemas'
import { fmtBps, fmtDate, fmtFrac, fmtMoney, fmtPrice } from '../lib/format'
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
