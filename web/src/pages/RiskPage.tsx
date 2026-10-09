import { useRisk } from '../api/client'
import type { BookLimits, BookRisk, RiskResponse } from '../api/schemas'
import { OperatorControls } from '../components/controls'
import { IconCheck, IconShield } from '../components/icons'
import { Badge, Banner, CardSkeleton, ErrorState, FindingRow, LedgerBadge, Meter, Stat } from '../components/ui'
import { bookLabel, fmtBps, fmtBTC, fmtDate, fmtFrac, fmtMoney, fmtMx, fmtUTC } from '../lib/format'
import { usePageTitle } from '../lib/usePageTitle'

function NextOrderPanel({ b }: { b: BookRisk }) {
  const no = b.next_order
  if (!no) {
    return (
      <div className="panel muted" style={{ fontSize: 12.5 }}>
        Dry-run ledger: no stage orders to check.
      </div>
    )
  }
  if (no.action === 'none') {
    return (
      <div className="panel row" style={{ fontSize: 12.5 }}>
        <IconCheck style={{ width: 16, height: 16, color: 'var(--long)' }} />
        <span>No order due at the {fmtDate(no.fill_date)} open: the stage position already matches the signal.</span>
      </div>
    )
  }
  const findings = no.decision.findings ?? []
  return (
    <div className="panel stack" style={{ gap: 8 }}>
      <div className="row" style={{ fontSize: 12.5 }}>
        <Badge tone={no.action === 'buy' ? 'long' : 'flat'}>{no.action}</Badge>
        <span className="num">{fmtBTC(no.qty_btc)}</span>
        <span className="faint">
          ≈ {fmtMoney(no.qty_btc * no.ref_price, b.quote)} at the {fmtDate(no.fill_date)} open
        </span>
        <span className="spacer" />
        <Badge tone={no.decision.allowed ? 'ok' : 'block'} dot>
          {no.decision.allowed ? 'would pass' : 'would be blocked'}
        </Badge>
      </div>
      {findings.length > 0 && (
        <ul className="stack" style={{ gap: 8, margin: 0, padding: 0, listStyle: 'none' }}>
          {findings.map((f, i) => (
            <FindingRow key={i} f={f} />
          ))}
        </ul>
      )}
    </div>
  )
}

function LastCheckPanel({ b }: { b: BookRisk }) {
  const lc = b.last_check
  if (!lc) {
    return (
      <div className="panel muted" style={{ fontSize: 12.5 }} data-testid={`last-check-${b.book}`}>
        No executor check recorded yet: ledger lines before 2026-10-05 predate enforcement.
      </div>
    )
  }
  const o = lc.order
  const dev = o.ref_price > 0 ? Math.abs(o.price / o.ref_price - 1) * 1e4 : null
  const findings = lc.findings ?? []
  return (
    <div className="panel stack" style={{ gap: 8 }} data-testid={`last-check-${b.book}`}>
      <div className="row" style={{ fontSize: 12.5, flexWrap: 'wrap' }}>
        <Badge tone={o.side === 'buy' ? 'long' : 'flat'}>{o.side}</Badge>
        <span className="num">{fmtBTC(o.qty_btc)}</span>
        <span className="faint">
          for the {fmtDate(lc.fill_date)} open · touch {fmtMoney(o.price, b.quote)} vs close{' '}
          {fmtMoney(o.ref_price, b.quote)}
          {dev !== null && ` (${fmtBps(dev)})`} · policy {lc.policy_version}
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
        <div className="faint" style={{ fontSize: 12 }}>
          Blocked days: {b.blocked_days.map(fmtDate).join(', ')}
        </div>
      )}
    </div>
  )
}

function BookRiskCard({ b }: { b: BookRisk }) {
  const l = b.limits
  const c = b.realized_cost
  const overCost = c.legs > 0 && c.avg_total_bps > c.assumed_leg_bps
  return (
    <article className="card" aria-labelledby={`risk-${b.book}`} data-testid={`risk-card-${b.book}`}>
      <header className="card-head">
        <div>
          <h2 id={`risk-${b.book}`}>{bookLabel(b.book)}</h2>
          <div className="sub">{b.mode === 'stage' ? 'Bitso stage execution' : 'dry run'}</div>
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
        <Stat label="Stage position" value={fmtBTC(b.position_btc)} hint={fmtMoney(b.position_notional, b.quote, 0)} />
        <Stat
          label="Realized cost / leg"
          value={c.legs > 0 ? fmtBps(c.avg_total_bps) : '—'}
          tone={overCost ? 'neg' : c.legs > 0 ? 'pos' : undefined}
          hint={
            c.legs > 0
              ? `fee ${fmtBps(c.avg_fee_bps)} + slip ${fmtBps(c.avg_slippage_bps)} · assumed ${fmtBps(c.assumed_leg_bps)}`
              : 'no legs yet'
          }
        />
        <Stat
          label="Paper max drawdown"
          value={fmtFrac(b.paper_max_drawdown)}
          hint={l.drawdown_warn > 0 ? `review at ${fmtFrac(l.drawdown_warn, 0)}` : 'no review level'}
        />
      </div>

      <div className="stack" style={{ gap: 12 }}>
        <Meter
          label="Position"
          value={b.position_btc.toFixed(5)}
          limitLabel={`${l.max_position_btc} BTC`}
          ratio={b.utilization.position}
          disabled={l.max_position_btc === 0}
        />
        <Meter
          label="Entry order size"
          value={(b.utilization.order_size * l.max_order_btc).toFixed(5)}
          limitLabel={`${l.max_order_btc} BTC`}
          ratio={b.utilization.order_size}
          disabled={l.max_order_btc === 0}
        />
        <Meter
          label="Entry order notional"
          value={Math.round(b.utilization.notional * l.max_order_notional).toLocaleString('en-US')}
          limitLabel={`${l.max_order_notional.toLocaleString('en-US')} ${b.quote.toUpperCase()}`}
          ratio={b.utilization.notional}
          disabled={l.max_order_notional === 0}
        />
        <Meter
          label="Drawdown vs review level"
          value={fmtFrac(b.paper_max_drawdown, 1)}
          limitLabel={fmtFrac(l.drawdown_warn, 0)}
          ratio={b.utilization.drawdown}
          disabled={l.drawdown_warn === 0}
        />
        {b.mode === 'stage' && (
          <div data-testid={`cost-budget-${b.book}`}>
            <Meter
              label="Realized cost vs pre-registered budget"
              value={fmtMoney(c.budget.cost_quote, b.quote, 2)}
              limitLabel={`${fmtMoney(c.budget.budget_quote, b.quote, 2)} at ${fmtBps(c.budget.primary_leg_bps)}`}
              ratio={c.budget.budget_used}
              disabled={c.budget.budget_quote === 0}
            />
            {c.budget.legs > 0 && (
              <div className={c.budget.over_pessimistic ? 'warn-text' : 'faint'} style={{ fontSize: 12, marginTop: 4 }}>
                {fmtBps(c.budget.weighted_bps)} per leg, notional-weighted over {c.budget.legs} leg
                {c.budget.legs > 1 ? 's' : ''} · pessimistic scenario {fmtBps(c.budget.secondary_leg_bps)}
                {c.budget.over_pessimistic ? ' (above it)' : ''}
              </div>
            )}
          </div>
        )}
      </div>

      <div className="divider" />
      <h3 style={{ marginBottom: 8 }}>Next stage order, checked against the policy</h3>
      <NextOrderPanel b={b} />

      {b.mode === 'stage' && (
        <>
          <div className="divider" />
          <h3 style={{ marginBottom: 8 }}>Last executor check</h3>
          <LastCheckPanel b={b} />
        </>
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

const LIMIT_ROWS: { key: keyof BookLimits; label: string; fmt: (v: number) => string; kind: string }[] = [
  { key: 'max_order_btc', label: 'Max order size', fmt: (v) => `${v} BTC`, kind: 'block' },
  { key: 'max_position_btc', label: 'Max position', fmt: (v) => `${v} BTC`, kind: 'block' },
  { key: 'max_order_notional', label: 'Max order notional', fmt: (v) => v.toLocaleString('en-US'), kind: 'block' },
  { key: 'max_orders_per_day', label: 'Max legs per day', fmt: (v) => String(v), kind: 'block' },
  { key: 'max_price_deviation_bps', label: 'Max price deviation', fmt: (v) => `${v} bps`, kind: 'block' },
  { key: 'drawdown_warn', label: 'Drawdown review level', fmt: (v) => fmtFrac(v, 0), kind: 'warn' },
  { key: 'cost_warn_bps', label: 'Leg cost warning', fmt: (v) => `${v} bps`, kind: 'warn' },
]

function PolicyTable({ r }: { r: RiskResponse }) {
  const books = Object.keys(r.policy.books).sort()
  return (
    <div className="table-wrap">
      <table className="data" id="policy-table">
        <thead>
          <tr>
            <th scope="col">Limit</th>
            <th scope="col">Type</th>
            {books.map((b) => (
              <th scope="col" key={b} className="r">
                {bookLabel(b)}
              </th>
            ))}
            <th scope="col" className="r">
              Other books
            </th>
          </tr>
        </thead>
        <tbody>
          {LIMIT_ROWS.map((row) => (
            <tr key={row.key}>
              <td>{row.label}</td>
              <td>
                <Badge tone={row.kind === 'block' ? 'info' : 'warn'}>{row.kind}</Badge>
              </td>
              {books.map((b) => {
                const v = r.policy.books[b][row.key]
                return (
                  <td key={b} className="r num">
                    {v === 0 ? <span className="faint">off</span> : row.fmt(v)}
                  </td>
                )
              })}
              <td className="r num faint">
                {r.policy.default[row.key] === 0 ? 'off' : row.fmt(r.policy.default[row.key])}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/** Who halted trading, when and why: from the policy, the ledger's halt file (R2), or both. */
function HaltBanner({ r }: { r: RiskResponse }) {
  const f = r.halt_file
  const fromFile = r.halt_source === 'file' || r.halt_source === 'both'
  const fromPolicy = r.halt_source === 'policy' || r.halt_source === 'both'
  const title = fromFile && !fromPolicy ? 'Trading halted by operator' : 'Trading halted by policy'
  return (
    <Banner tone="block" title={title} id="banner-halt">
      {fromFile ? (
        <span data-testid="halt-file">
          <b>{f.reason}</b> · by <b>{f.by}</b> · <span title={fmtUTC(f.at)}>{fmtMx(f.at)} Mexico City</span> ·{' '}
          <span className="num faint">{f.path}</span>
          {fromPolicy && <> · policy: {r.policy.halt_reason || 'no reason given'}</>}
        </span>
      ) : (
        <>{r.halt_reason || 'No reason given.'}</>
      )}{' '}
      Every new stage order is blocked and recorded.{' '}
      {fromFile ? (
        <>
          Resume from <a href="#operator-controls">Operator controls</a> below (or remove the file by hand).
        </>
      ) : (
        <>Lift it in the policy to resume.</>
      )}
    </Banner>
  )
}

export function RiskPage() {
  usePageTitle('Risk', 'Execution risk limits, exposure and realized costs for the forward tests.')
  const q = useRisk()
  const r = q.data
  return (
    <>
      <div className="page-head">
        <div>
          <div className="row" style={{ gap: 12 }}>
            <h1>Risk</h1>
            {r && <LedgerBadge name={r.ledger} />}
          </div>
          <p>
            Execution limits for the forward tests. They check orders, never signals: the SMA50 rule is frozen by its
            pre-registration, and drawdown levels only flag a review.
          </p>
        </div>
        {r && (
          <span className="faint" title={fmtUTC(r.generated_at)}>
            evaluated {fmtMx(r.generated_at)}
          </span>
        )}
      </div>

      {q.isLoading && (
        <div className="grid grid-2">
          <CardSkeleton lines={8} />
          <CardSkeleton lines={8} />
        </div>
      )}
      {q.isError && (
        <div className="card">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      )}

      {r && (
        <>
          {r.halt_file.error ? (
            <Banner tone="block" title="Halt file is invalid: the executor will not run" id="banner-halt-file-error">
              {r.halt_file.error}. Fix or remove <span className="num">{r.halt_file.path}</span>.
            </Banner>
          ) : null}
          {r.halted ? <HaltBanner r={r} /> : null}
          {r.enforcement === 'enforced' ? (
            <Banner tone="info" title="Enforced" id="banner-enforcement">
              {r.note}
            </Banner>
          ) : (
            <Banner tone="warn" title="Monitor-only" id="banner-enforcement">
              {r.note}
            </Banner>
          )}

          <div className="grid grid-4" style={{ marginBottom: 16 }}>
            <div className="card">
              <Stat
                label={
                  <>
                    <IconShield style={{ width: 13, height: 13 }} /> Halt
                  </>
                }
                value={r.halted ? 'HALTED' : 'off'}
                tone={r.halted ? 'neg' : 'pos'}
                hint={
                  r.halted
                    ? `source: ${r.halt_source === 'both' ? 'policy + halt file' : r.halt_source === 'file' ? 'halt file' : 'policy'}`
                    : 'policy flag or halt file'
                }
              />
            </div>
            <div className="card">
              <Stat
                label="Blocks"
                value={r.blocks}
                tone={r.blocks > 0 ? 'neg' : undefined}
                hint="orders that would be stopped"
              />
            </div>
            <div className="card">
              <Stat
                label="Warnings"
                value={r.warnings}
                tone={r.warnings > 0 ? 'warn' : undefined}
                hint="review, never blocking"
              />
            </div>
            <div className="card">
              <Stat label="Policy" value={r.policy.version} hint={r.policy_source} title={r.policy_source} />
            </div>
          </div>

          <div style={{ marginBottom: 16 }}>
            <OperatorControls policyHalted={r.policy.halted} />
          </div>

          <div className="grid grid-2" style={{ marginBottom: 16 }}>
            {r.books.map((b) => (
              <BookRiskCard key={b.book} b={b} />
            ))}
          </div>

          <section className="card" aria-labelledby="policy-h">
            <div className="card-head">
              <div>
                <h2 id="policy-h">Policy</h2>
                <div className="sub">
                  shared/pkg/risk · stage entry size {r.stage_size_btc} BTC. Sells that reduce a position skip the size
                  limits, so no limit can trap a position.
                </div>
              </div>
            </div>
            <PolicyTable r={r} />
          </section>
        </>
      )}
    </>
  )
}
