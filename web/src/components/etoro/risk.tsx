/**
 * The eToro Risk page's pieces: the EtoroDemoPolicy summary, one book's
 * exposure card (meters against the 5 % of equity and 1,500 USD caps, the
 * executor's last check, findings, blocked days), and the P4 risk report
 * card (VaR, stress, Monte Carlo, attribution, fill quality vs the frozen
 * spread).
 */
import { useState } from 'react'
import type { EtoroBookLimits, EtoroBookRisk, EtoroRiskReport } from '../../api/etoroSchemas'
import {
  bookLabel,
  bookSymbol,
  fmtBps,
  fmtDate,
  fmtFrac,
  fmtMoney,
  fmtNY,
  fmtPrice,
  fmtUnits,
  fmtUTC,
} from '../../lib/format'
import { Badge, Empty, FindingRow, Meter, Stat } from '../ui'
import { OrderLine } from './parts'

/** The limits of shared/pkg/risk.EtoroDemoPolicy as the operator reads them. */
export function PolicySummary({ l, version }: { l: EtoroBookLimits; version: string }) {
  const chips: { k: string; v: string; title?: string }[] = [
    { k: 'Leverage', v: `x${l.max_leverage ?? 1} only`, title: 'max_leverage: no leveraged CFDs on the demo' },
    { k: 'Per order', v: fmtMoney(l.max_order_notional, 'USD', 0), title: 'max_order_notional' },
    {
      k: 'Legs per day',
      v: `${l.max_orders_per_day} per instrument`,
      title: 'max_orders_per_day: one open or close per instrument per New York day',
    },
    {
      k: 'Exposure cap',
      v: l.max_exposure_pct_equity ? `${fmtFrac(l.max_exposure_pct_equity, 0)} of equity` : 'off',
      title: 'max_exposure_pct_equity: all open positions against the demo account equity',
    },
    {
      k: 'Price deviation',
      v: `${fmtFrac(l.max_price_deviation_bps / 1e4, 0)} max`,
      title: `max_price_deviation_bps ${l.max_price_deviation_bps}: the quote against the decision close`,
    },
    {
      k: 'Session',
      v: l.require_market_open ? 'cash session required' : 'any time',
      title: 'require_market_open: NYSE 09:30-16:00 New York',
    },
    { k: 'Drawdown review', v: fmtFrac(l.drawdown_warn, 0), title: 'drawdown_warn: a warning, never a block' },
    { k: 'Leg cost warning', v: fmtBps(l.cost_warn_bps), title: 'cost_warn_bps' },
  ]
  return (
    <section className="card venue-etoro" aria-labelledby="policy-h" data-testid="policy-summary">
      <div className="card-head">
        <div>
          <h2 id="policy-h">Demo policy</h2>
          <div className="sub">
            shared/pkg/risk.EtoroDemoPolicy <span className="num">{version}</span>, built into the executor; the halt
            file is the only operator override. Closes that reduce a position always pass the size checks.
          </div>
        </div>
      </div>
      <div className="policy-chips">
        {chips.map((c) => (
          <div key={c.k} className="policy-chip" title={c.title}>
            <span className="k">{c.k}</span>
            <span className="v">{c.v}</span>
          </div>
        ))}
      </div>
    </section>
  )
}

function LastCheck({ b }: { b: EtoroBookRisk }) {
  const lc = b.last_check
  if (!lc) {
    return (
      <div className="panel muted" style={{ fontSize: 12.5 }} data-testid={`last-check-${b.book}`}>
        No executor check recorded yet for this book.
      </div>
    )
  }
  const o = lc.order
  const dev = o.ref_price > 0 ? Math.abs(o.price / o.ref_price - 1) * 1e4 : null
  const findings = lc.findings ?? []
  return (
    <div className="panel stack" style={{ gap: 8 }} data-testid={`last-check-${b.book}`}>
      <div className="row" style={{ fontSize: 12.5, flexWrap: 'wrap' }}>
        <Badge tone={o.side === 'buy' ? 'long' : 'flat'}>{o.side === 'buy' ? 'open' : 'close'}</Badge>
        <span className="num">{fmtUnits(o.qty_btc)}</span>
        <span className="faint">
          for the {fmtDate(lc.fill_date)} session · quote {fmtPrice(o.price, b.quote, b.book)} vs close{' '}
          {fmtPrice(o.ref_price, b.quote, b.book)}
          {dev !== null && ` (${fmtBps(dev)})`} · x{o.leverage ?? 1} · policy {lc.policy_version}
        </span>
        <span className="spacer" />
        <Badge tone={lc.allowed ? 'ok' : 'block'} dot>
          {lc.allowed ? 'sent' : 'blocked, not sent'}
        </Badge>
      </div>
      {findings.length > 0 && (
        <ul className="stack" style={{ gap: 8, margin: 0, padding: 0, listStyle: 'none' }}>
          {findings.map((f, i) => (
            <FindingRow key={i} f={f} />
          ))}
        </ul>
      )}
      {b.blocked_days.length > 0 && (
        <div className="faint" style={{ fontSize: 12 }} data-testid={`blocked-days-${b.book}`}>
          Blocked days: {b.blocked_days.map(fmtDate).join(', ')}
        </div>
      )}
    </div>
  )
}

/** One book's exposure against EtoroDemoPolicy; `equity` is the demo account equity the newest run saw. */
export function EtoroBookRiskCard({ b, equity }: { b: EtoroBookRisk; equity: number | null }) {
  const l = b.limits
  const capUsd = equity && l.max_exposure_pct_equity ? equity * l.max_exposure_pct_equity : 0
  const long = b.position.state === 'long' && !!b.position.units
  return (
    <article className="card venue-etoro" aria-labelledby={`risk-${b.book}`} data-testid={`risk-card-${b.book}`}>
      <header className="card-head">
        <div>
          <h2 id={`risk-${b.book}`}>{bookLabel(b.book)}</h2>
          <div className="sub">
            eToro {b.mode} · instrument #{b.instrument.id} · {b.orders_today} leg{b.orders_today === 1 ? '' : 's'} today
          </div>
        </div>
        {b.findings.some((f) => f.severity === 'block') ? (
          <Badge tone="block" dot>
            blocked
          </Badge>
        ) : b.findings.length > 0 ? (
          <Badge tone="warn" dot>
            {b.findings.length} warning{b.findings.length > 1 ? 's' : ''}
          </Badge>
        ) : (
          <Badge tone="ok" dot>
            within limits
          </Badge>
        )}
      </header>

      <div className="grid grid-3" style={{ gap: 14, marginBottom: 16 }}>
        <Stat
          label="Demo position"
          value={long ? fmtUnits(b.position.units!) : 'flat'}
          tone={long ? 'pos' : undefined}
          hint={
            long
              ? `open ${fmtPrice(b.position.open_rate ?? 0, b.quote, b.book)} · ${fmtMoney(b.position.amount ?? 0, b.quote, 0)}`
              : 'no open CFD'
          }
        />
        <Stat
          label="Exposure"
          value={fmtMoney(b.exposure_usd, b.quote, 2)}
          hint={`${b.exposure_pct_equity.toFixed(2)}% of equity · units × close ${fmtPrice(b.last_close, b.quote, b.book)}`}
        />
        <Stat
          label="Paper max drawdown"
          value={fmtFrac(b.paper_max_drawdown)}
          hint={l.drawdown_warn > 0 ? `review at ${fmtFrac(l.drawdown_warn, 0)}` : 'no review level'}
        />
      </div>

      <div className="stack" style={{ gap: 12 }}>
        <div data-testid={`meter-exposure-${b.book}`}>
          <Meter
            label={`Exposure vs ${fmtFrac(l.max_exposure_pct_equity ?? 0, 0)} of equity`}
            value={fmtMoney(b.exposure_usd, b.quote, 0)}
            limitLabel={capUsd > 0 ? fmtMoney(capUsd, b.quote, 0) : '—'}
            ratio={b.utilization.exposure}
            disabled={!l.max_exposure_pct_equity || capUsd === 0}
          />
        </div>
        <div data-testid={`meter-notional-${b.book}`}>
          <Meter
            label="Position vs the per-order cap"
            value={fmtMoney(b.position.amount ?? 0, b.quote, 0)}
            limitLabel={fmtMoney(l.max_order_notional, b.quote, 0)}
            ratio={b.utilization.notional}
            disabled={l.max_order_notional === 0}
          />
        </div>
        <Meter
          label="Drawdown vs review level"
          value={fmtFrac(b.paper_max_drawdown, 1)}
          limitLabel={fmtFrac(l.drawdown_warn, 0)}
          ratio={b.utilization.drawdown}
          disabled={l.drawdown_warn === 0}
        />
      </div>

      <div className="divider" />
      <h3 style={{ marginBottom: 8 }}>Last executor check</h3>
      <LastCheck b={b} />

      {b.last_order && (
        <div style={{ marginTop: 12 }}>
          <OrderLine o={b.last_order} book={b.book} quote={b.quote} />
        </div>
      )}

      {b.findings.length > 0 && (
        <>
          <div className="divider" />
          <h3 style={{ marginBottom: 8 }}>Findings</h3>
          <ul className="stack" style={{ gap: 10, margin: 0, padding: 0, listStyle: 'none' }}>
            {b.findings.map((f, i) => (
              <FindingRow key={i} f={f} />
            ))}
          </ul>
        </>
      )}
    </article>
  )
}

// --- P4 risk report ---------------------------------------------------------

function Prob({ label, p, lo, hi, good }: { label: string; p: number; lo: number; hi: number; good: boolean }) {
  return (
    <div className="prob-item">
      <div className="stat-label">{label}</div>
      <div className={`p num ${good ? 'pos' : ''}`}>{fmtFrac(p, 0)}</div>
      <div className="ci num">
        95% CI {fmtFrac(lo, 0)} – {fmtFrac(hi, 0)}
      </div>
      <div
        className="meter-track"
        role="meter"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(p * 100)}
      >
        <div className="meter-fill etoro" style={{ width: `${Math.min(1, p) * 100}%` }} />
      </div>
    </div>
  )
}

export function RiskReportCard({ r }: { r: EtoroRiskReport }) {
  const scenarios = Array.from(new Set(r.monte_carlo.map((m) => m.scenario)))
  const [scenario, setScenario] = useState(scenarios.includes('frozen') ? 'frozen' : (scenarios[0] ?? ''))
  const mc = r.monte_carlo.filter((m) => m.scenario === scenario)
  const capital = r.params.capital_usd
  const worst = r.stress?.episodes.find((e) => e.id === r.stress?.worst_episode)
  const a = r.attribution
  const fq = r.fill_quality
  return (
    <section className="card venue-etoro" aria-labelledby="report-h" data-testid="risk-report">
      <div className="card-head">
        <div>
          <h2 id="report-h">Risk report</h2>
          <div className="sub">
            cmd/etoro-risk-report ({r.schema}) as of <b>{fmtDate(r.as_of)}</b>, written{' '}
            <span title={fmtUTC(r.generated_at)}>{fmtNY(r.generated_at)} NY</span> ·{' '}
            <span className="num" title={r.path}>
              risk/{r.date}/risk.json
            </span>
          </div>
        </div>
        <Badge tone="etoro" title={r.params.costs_status}>
          {/frozen/i.test(r.params.costs_status) ? 'costs frozen' : 'costs provisional'}
        </Badge>
      </div>

      <div className="report-grid">
        <Stat
          label="Portfolio VaR 99 %"
          value={<span data-testid="report-var">{fmtMoney(r.var.var_99_usd, 'USD', 2)}</span>}
          hint={`${fmtFrac(r.var.var_99_fraction_of_capital, 3)} of ${fmtMoney(capital, 'USD', 0)} capital · 1 day, EWMA`}
          large
        />
        <Stat
          label="Historical VaR 99 %"
          value={r.var.var_99_historical_usd != null ? fmtMoney(r.var.var_99_historical_usd, 'USD', 2) : '—'}
          hint={r.var.es_975_normal_usd != null ? `ES 97.5 % ${fmtMoney(r.var.es_975_normal_usd, 'USD', 2)}` : ''}
        />
        <Stat
          label="Implied correlation"
          value={r.var.implied_correlation.toFixed(3)}
          hint={`NSDQ100 · SPX500 · diversified vs ${r.var.var_99_undiversified_usd != null ? fmtMoney(r.var.var_99_undiversified_usd, 'USD', 2) : '—'}`}
        />
        <Stat
          label="Net exposure"
          value={fmtMoney(r.var.net_exposure_usd, 'USD', 0)}
          hint={r.positions.map((p) => `${p.symbol} ${p.state}`).join(' · ')}
        />
      </div>

      {r.stress && (
        <div className="report-section" data-testid="report-stress">
          <h3>Stress</h3>
          <div className="report-grid">
            <Stat
              label="Worst historical episode"
              value={worst?.name ?? r.stress.worst_episode}
              tone="warn"
              hint={
                worst
                  ? `${fmtDate(worst.base)} → ${fmtDate(worst.end)}${worst.complete ? '' : ' (partial history)'}`
                  : undefined
              }
            />
            <Stat
              label="Loss in that episode"
              value={fmtMoney(r.stress.worst_episode_loss_usd, 'USD', 2)}
              tone="neg"
              hint={`${fmtFrac(r.stress.worst_episode_fraction_of_capital)} of capital`}
            />
            <Stat
              label={`Index shock ${r.stress.shocks[0] ? fmtFrac(r.stress.shocks[0].move, 0) : ''}`}
              value={r.stress.shocks[0] ? fmtMoney(r.stress.shocks[0].loss_usd, 'USD', 0) : '—'}
              hint={
                <>
                  {r.stress.shocks
                    .slice(1)
                    .map((s) => `${fmtFrac(s.move, 0)} ${fmtMoney(s.loss_usd, 'USD', 0)}`)
                    .join(' · ')}
                  {r.stress.margin_close_out_move != null &&
                    ` · margin close-out at ${fmtFrac(r.stress.margin_close_out_move, 0)} on x1`}
                </>
              }
            />
          </div>
        </div>
      )}

      {mc.length > 0 && (
        <div className="report-section" data-testid="report-mc">
          <div className="row" style={{ justifyContent: 'space-between', marginBottom: 10 }}>
            <h3 style={{ margin: 0 }}>Monte Carlo · next 12 months</h3>
            {scenarios.length > 1 && (
              <div className="seg" role="group" aria-label="Cost scenario">
                {scenarios.map((s) => (
                  <button key={s} aria-pressed={scenario === s} onClick={() => setScenario(s)} id={`mc-scenario-${s}`}>
                    {s} costs
                  </button>
                ))}
              </div>
            )}
          </div>
          <div className="stack" style={{ gap: 14 }}>
            {mc.map((m) => (
              <div key={m.book}>
                <div className="row" style={{ gap: 10, marginBottom: 8, fontSize: 12.5 }}>
                  <b>{bookSymbol(m.book)}</b>
                  <span className="faint num">
                    {m.spread_rt_bps} bps round trip · median trend return {fmtFrac(m.summary.trend_return.p50, 1)} vs
                    CFD hold {fmtFrac(m.summary.cfd_hold_return.p50, 1)} · median max DD{' '}
                    {fmtFrac(m.summary.trend_max_dd.p50, 0)}
                  </span>
                </div>
                <div className="prob-list">
                  <Prob
                    label="P(beats CFD hold)"
                    {...m.summary.p_beats_cfd_hold}
                    good={m.summary.p_beats_cfd_hold.p >= 0.5}
                  />
                  <Prob
                    label="P(beats ETF hold)"
                    {...m.summary.p_beats_etf_hold}
                    good={m.summary.p_beats_etf_hold.p >= 0.5}
                  />
                  <Prob
                    label="P(shallower drawdown)"
                    {...m.summary.p_shallower_dd}
                    good={m.summary.p_shallower_dd.p >= 0.5}
                  />
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {a && (
        <div className="report-section" data-testid="report-attribution">
          <h3>Attribution · as of {fmtDate(a.as_of)}</h3>
          <div className="report-grid">
            <Stat
              label="Net P&L"
              value={fmtMoney(a.total.net, 'USD', 2)}
              tone={a.total.net > 0 ? 'pos' : a.total.net < 0 ? 'neg' : undefined}
              hint={`price ${fmtMoney(a.total.price_pnl, 'USD', 2)} · spread ${fmtMoney(-a.total.spread_paid, 'USD', 2)} · financing ${fmtMoney(-a.total.financing_modelled, 'USD', 2)}`}
              large
            />
            <Stat label="Realized" value={fmtMoney(a.realized.net, 'USD', 2)} hint="closed lots" />
            <Stat label="Unrealized" value={fmtMoney(a.unrealized.net, 'USD', 2)} hint="open lots at the mark close" />
            {a.books.map((b) => (
              <Stat
                key={b.book}
                label={`${bookSymbol(b.book)} tracking`}
                value={fmtBps(b.tracking_bps, true)}
                tone={Math.abs(b.tracking_bps) > 50 ? 'warn' : undefined}
                hint={`demo ${b.demo_equity.toFixed(4)} vs paper ${b.paper_equity.toFixed(4)} · net ${fmtMoney(b.total.net, 'USD', 2)}`}
              />
            ))}
          </div>
        </div>
      )}

      {fq && fq.books.length > 0 && (
        <div className="report-section" data-testid="report-fills">
          <h3>Fill quality · measured spread per leg vs the frozen assumption</h3>
          {fq.books.map((b) => (
            <div key={b.book} className="fillq-row" data-testid={`fillq-${b.book}`}>
              <b>{bookSymbol(b.book)}</b>
              <Meter
                label={`${b.n} fill${b.n === 1 ? '' : 's'} · vs mid ${fmtBps(b.fill_vs_mid_bps.mean, true)} mean · ${b.latency_s.mean.toFixed(1)} s`}
                value={`${b.measured_spread_per_leg_bps.toFixed(2)} bps`}
                limitLabel={`frozen ${b.frozen_spread_per_leg_bps} bps`}
                ratio={b.measured_vs_frozen}
              />
              <span className={`num ${b.measured_vs_frozen <= 1 ? 'pos' : 'neg'}`}>
                {fmtFrac(b.measured_vs_frozen, 0)}
              </span>
            </div>
          ))}
        </div>
      )}

      {r.notes.length > 0 && (
        <details className="disclosure" style={{ marginTop: 12 }}>
          <summary>Report notes ({r.notes.length})</summary>
          <ul className="stack" style={{ gap: 4, margin: '8px 0 0', paddingLeft: 18, fontSize: 12.5 }}>
            {r.notes.map((n, i) => (
              <li key={i}>{n}</li>
            ))}
          </ul>
        </details>
      )}
    </section>
  )
}

export function NoRiskReport({ message }: { message: string }) {
  return (
    <section className="card venue-etoro" aria-labelledby="report-h" data-testid="risk-report">
      <div className="card-head">
        <h2 id="report-h">Risk report</h2>
      </div>
      <Empty title="No risk report yet">{message}</Empty>
    </section>
  )
}
