/**
 * One eToro book in detail: the executor's candles with SMA50 and the demo
 * orders marked, the paper account against holding the CFD with the demo
 * account alongside, the day's reconcile status, and the ledger records.
 */
import { useMemo, useState } from 'react'
import { Link, useParams } from 'react-router'
import {
  useEtoroCandles,
  useEtoroForwardTests,
  useEtoroLedger,
  useEtoroReconcile,
  useLedgerSearch,
} from '../../api/client'
import type { EtoroLedgerRecord, EtoroReconcileReport } from '../../api/etoroSchemas'
import { EtoroEquityChart, PriceChart, type FillMarker } from '../../components/charts'
import { DemoStrip, NextRun, OrderLine, RunBadge, SessionClock } from '../../components/etoro/parts'
import { ReconcileBadge, ReconcileFindings } from '../../components/etoro/reconcile'
import { Badge, Banner, CardSkeleton, Empty, ErrorState, LedgerBadge, SignalPill, Stat } from '../../components/ui'
import {
  bookLabel,
  bookSymbol,
  fmtBps,
  fmtDate,
  fmtFrac,
  fmtMoney,
  fmtNY,
  fmtPct,
  fmtPrice,
  fmtUnits,
  fmtUTC,
  positionText,
  quoteDigits,
} from '../../lib/format'
import { usePageTitle } from '../../lib/usePageTitle'

const RANGES = [90, 180, 365] as const
const NO_FILLS: FillMarker[] = []

function demoActionTone(a: string | undefined): 'long' | 'flat' | 'block' | undefined {
  if (a === 'open') return 'long'
  if (a === 'close') return 'flat'
  if (a === 'blocked') return 'block'
  return undefined
}

function RecordsTable({ records, quote, book }: { records: EtoroLedgerRecord[]; quote: string; book: string }) {
  const rows = [...records].reverse()
  return (
    <div className="stack" style={{ gap: 10 }}>
      <div className="table-wrap">
        <table className="data" id="etoro-ledger-table" data-testid="etoro-records">
          <thead>
            <tr>
              <th scope="col">Bar</th>
              <th scope="col">Fill day</th>
              <th scope="col">Signal</th>
              <th scope="col">Action</th>
              <th scope="col">Demo</th>
              <th scope="col">Order</th>
              <th scope="col" className="r">
                Avg price
              </th>
              <th scope="col" className="r">
                vs mid / close
              </th>
              <th scope="col">Position after</th>
              <th scope="col" className="r">
                Account equity
              </th>
              <th scope="col">Recorded (NY)</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const d = r.demo
              const o = d?.order
              return (
                <tr key={r.decision.bar_date} data-testid={`etoro-record-${r.decision.bar_date}`}>
                  <td>{fmtDate(r.decision.bar_date)}</td>
                  <td>{fmtDate(r.decision.fill_date)}</td>
                  <td>
                    <Badge tone={r.decision.signal === 'long' ? 'long' : 'flat'}>{r.decision.signal}</Badge>
                  </td>
                  <td>{r.decision.action}</td>
                  <td>
                    {d ? (
                      <Badge tone={demoActionTone(d.action)} title={d.notes?.join(' · ')}>
                        {d.action}
                      </Badge>
                    ) : (
                      <span className="faint">dry run</span>
                    )}
                  </td>
                  <td>
                    {o ? (
                      <span className="row" style={{ gap: 6 }}>
                        <Badge
                          tone={o.status === 'executed' ? 'ok' : o.error ? 'block' : 'warn'}
                          dot
                          title={o.client_ref}
                        >
                          {o.status}
                        </Badge>
                        {o.resumed && <Badge tone="info">resumed</Badge>}
                      </span>
                    ) : (
                      <span className="faint">—</span>
                    )}
                  </td>
                  <td className="r num">{o?.avg_price != null ? fmtPrice(o.avg_price, quote, book) : '—'}</td>
                  <td className="r num">
                    {o?.fill_vs_mid_bps != null ? fmtBps(o.fill_vs_mid_bps, true) : '—'}
                    <span className="faint"> / </span>
                    {o?.fill_vs_close_bps != null ? fmtBps(o.fill_vs_close_bps, true) : '—'}
                  </td>
                  <td className="num">{d ? positionText(d.position_after) : '—'}</td>
                  <td className="r num">{d?.account ? fmtMoney(d.account.equity, d.account.currency, 2) : '—'}</td>
                  <td title={fmtUTC(r.recorded_at)}>{fmtNY(r.recorded_at)}</td>
                </tr>
              )
            })}
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

/** The reconcile report for the book's last fill day (today's run), else the newest. */
function pickReport(reports: EtoroReconcileReport[], fillDate: string): EtoroReconcileReport | undefined {
  return reports.find((r) => r.date === fillDate) ?? reports[0]
}

function ReconcileCard({ fillDate, book }: { fillDate: string; book: string }) {
  const q = useEtoroReconcile()
  const search = useLedgerSearch()
  const rep = q.data ? pickReport(q.data.reports, fillDate) : undefined
  const mine = rep?.books.find((b) => b.book === book)
  const findings = rep?.findings.filter((f) => !f.book || f.book === book) ?? []
  return (
    <section className="card venue-etoro" aria-labelledby="reconcile-h" data-testid="detail-reconcile">
      <div className="card-head">
        <div>
          <h2 id="reconcile-h">Reconcile</h2>
          <div className="sub">
            After each run, cmd/etoro-reconcile compares the ledger with the demo account; an operator re-check can
            clear a drift later.
          </div>
        </div>
        {rep && <ReconcileBadge r={rep} />}
      </div>
      {q.isLoading && <CardSkeleton lines={2} />}
      {q.isError && <ErrorState error={q.error} onRetry={() => q.refetch()} />}
      {q.data && !rep && <Empty title="No reconcile report yet" />}
      {rep && (
        <div className="stack" style={{ gap: 10 }}>
          <div className="row" style={{ gap: 14, flexWrap: 'wrap', fontSize: 12.5 }}>
            <span>
              <span className="muted">Report</span> <b>{fmtDate(rep.date)}</b>
              {rep.date !== fillDate && <span className="faint"> (newest; none for {fmtDate(fillDate)})</span>}
            </span>
            <span className="faint" title={fmtUTC(rep.generated_at)}>
              {fmtNY(rep.generated_at)} NY
            </span>
            {mine && (
              <span className="num faint">
                ledger {mine.ledger_state}
                {mine.ledger_units ? ` ${fmtUnits(mine.ledger_units)}` : ''} · account positions{' '}
                {mine.account_positions.length} · {mine.orders_checked} order{mine.orders_checked === 1 ? '' : 's'}{' '}
                checked
              </span>
            )}
          </div>
          {findings.length === 0 && rep.findings.length > 0 && (
            <div className="faint" style={{ fontSize: 12.5 }}>
              No findings for {bookSymbol(book)}; the report&apos;s {rep.findings.length} finding
              {rep.findings.length === 1 ? '' : 's'} concern{rep.findings.length === 1 ? 's' : ''} other books.
            </div>
          )}
          <ReconcileFindings
            findings={findings}
            recheck={rep.recheck}
            quiet={findings.length === 0 && rep.findings.length > 0}
          />
          <Link to={{ pathname: '/data-health', search }} className="lp-link" id="detail-reconcile-more">
            All reports on Data health
          </Link>
        </div>
      )}
    </section>
  )
}

export function EtoroForwardTestDetailPage() {
  const { book = '' } = useParams()
  usePageTitle(`${bookSymbol(book)} forward test · eToro`)
  const [days, setDays] = useState<(typeof RANGES)[number]>(180)
  const summary = useEtoroForwardTests()
  const ledger = useEtoroLedger(book)
  const candles = useEtoroCandles(book, days)
  const ft = summary.data?.books.find((b) => b.book === book)
  const search = useLedgerSearch()
  const quote = ft?.quote ?? 'USD'
  const fills = useMemo<FillMarker[]>(
    () =>
      ledger.data?.records.flatMap((r) =>
        r.demo?.order && r.demo.order.status === 'executed'
          ? [
              {
                fill_date: r.decision.fill_date,
                side: r.demo.order.kind === 'open' ? 'buy' : 'sell',
                label: `demo ${r.demo.order.kind}`,
              } satisfies FillMarker,
            ]
          : [],
      ) ?? NO_FILLS,
    [ledger.data],
  )
  const prereg = ft?.prereg.replace(/\.md$/, '')

  return (
    <>
      <div className="page-head">
        <div>
          <div className="crumbs">
            <Link to={{ pathname: '/forward-tests', search }}>Forward tests</Link> <span>/</span>{' '}
            <span>{bookSymbol(book)}</span>
          </div>
          <div className="row" style={{ gap: 14 }}>
            <h1>{bookLabel(book)}</h1>
            {ft && <LedgerBadge name={ft.ledger} venue="etoro" />}
            {ft && <SignalPill signal={ft.decision.signal} />}
          </div>
          {ft && ft.recorded_at && (
            <p>
              Pre-registration{' '}
              {prereg ? (
                <Link to={{ pathname: `/research/${prereg}`, search }} className="num" id="detail-prereg">
                  {ft.prereg}
                </Link>
              ) : (
                <span className="num">{ft.prereg}</span>
              )}{' '}
              · code <span className="num">{ft.code_version}</span> · {ft.mode} · instrument{' '}
              <span className="num">
                {ft.instrument.symbol} #{ft.instrument.id}
              </span>
            </p>
          )}
        </div>
        {summary.data && (
          <div className="stack" style={{ alignItems: 'flex-end', gap: 6 }}>
            <SessionClock s={summary.data.session} />
            {ft && <NextRun run={ft.run} />}
          </div>
        )}
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
      {ft && ft.recorded_at === '' && (
        <Banner tone="info" title={`No records for ${bookSymbol(book)} in ${ft.ledger} yet`}>
          {ft.run.message}
        </Banner>
      )}

      {ft && ft.recorded_at && (
        <div className="card venue-etoro" style={{ marginBottom: 16 }}>
          <div className="grid grid-4">
            <Stat
              label="Close"
              value={fmtPrice(ft.decision.close, quote, book)}
              hint={`${fmtDate(ft.decision.bar_date)} · USD`}
              large
            />
            <Stat
              label="Distance to SMA50"
              value={fmtPct(ft.distance_to_sma_pct, 1)}
              hint={`SMA ${fmtPrice(ft.decision.sma50, quote, book)}`}
              large
            />
            <Stat
              label="Paper vs CFD hold"
              value={fmtPct(ft.excess_vs_hold_pct)}
              tone={ft.excess_vs_hold_pct > 0.005 ? 'pos' : ft.excess_vs_hold_pct < -0.005 ? 'neg' : undefined}
              hint={`paper ${ft.paper.equity_if_closed.toFixed(4)} · hold ${ft.paper.hold_equity_if_closed.toFixed(4)} · financing ${ft.paper.financing_pct.toFixed(2)}%`}
              large
            />
            <Stat
              label="Max drawdown"
              value={fmtFrac(ft.paper.max_drawdown)}
              hint={
                ft.paper.started
                  ? `since ${fmtDate(ft.paper.forward_start)}`
                  : `forward start ${fmtDate(ft.paper.forward_start)}`
              }
              large
            />
          </div>
          <div style={{ marginTop: 14 }}>
            <DemoStrip position={ft.demo_position} account={ft.account} book={ft.book} />
          </div>
          {ft.last_order && (
            <div style={{ marginTop: 12 }}>
              <OrderLine o={ft.last_order} book={ft.book} quote={quote} />
            </div>
          )}
          <div className="row" style={{ gap: 10, marginTop: 12, flexWrap: 'wrap' }}>
            <RunBadge run={ft.run} withNext={false} />
            <span className="faint">
              {ft.run.today_due
                ? ft.run.today_done
                  ? "today's run recorded"
                  : "today's run not recorded yet"
                : 'no run today'}
            </span>
          </div>
        </div>
      )}

      <div className="grid grid-2" style={{ marginBottom: 16 }}>
        <section className="card" aria-labelledby="price-h">
          <div className="card-head">
            <div>
              <h2 id="price-h">Daily candles</h2>
              <div className="legend" style={{ marginTop: 6 }}>
                <span className="key" style={{ color: 'var(--warn)' }}>
                  <span className="swatch" /> SMA50
                </span>
                <span className="key" style={{ color: 'var(--long)' }}>
                  ▲ flip / ● demo order
                </span>
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
              fills={fills}
              quote={quote}
              digits={quoteDigits(quote, book)}
              volume={false}
              label={`${bookLabel(book)} daily candles with SMA50`}
            />
          )}
          {candles.data && (
            <div className="faint num" style={{ fontSize: 11, marginTop: 8 }}>
              {candles.data.file} · NYSE trading days, eToro OneDay candles (no volume)
            </div>
          )}
        </section>

        <section className="card" aria-labelledby="equity-h">
          <div className="card-head">
            <div>
              <h2 id="equity-h">Paper vs demo equity</h2>
              <div className="legend" style={{ marginTop: 6 }}>
                <span className="key" style={{ color: 'var(--long)' }}>
                  <span className="swatch" /> paper (SMA50 rule)
                </span>
                <span className="key" style={{ color: 'var(--bench)' }}>
                  <span className="swatch dashed" /> CFD hold
                </span>
                <span className="key" style={{ color: 'var(--venue-etoro)' }}>
                  <span className="swatch" /> demo account, USD (left axis)
                </span>
              </div>
            </div>
            {ft && (
              <span className="sub">
                {ft.paper.spread_rt_bps} bps round trip · {ft.paper.overnight_bps} bps/night
              </span>
            )}
          </div>
          {ledger.isLoading && <div className="skeleton chart sm" />}
          {ledger.isError && <ErrorState error={ledger.error} onRetry={() => ledger.refetch()} />}
          {ledger.data &&
            (ledger.data.equity.length > 0 ? (
              <EtoroEquityChart
                points={ledger.data.equity}
                label="Paper equity versus holding the CFD, with the demo account"
              />
            ) : (
              <Empty title="No forward days yet" />
            ))}
          {ledger.data && ledger.data.equity.length > 0 && (
            <div className="grid grid-3" style={{ marginTop: 12, gap: 12 }}>
              {(() => {
                const pts = ledger.data.equity
                const last = pts[pts.length - 1]
                const firstDemo = pts.find((p) => p.demo_equity != null)?.demo_equity ?? null
                const demoRet = firstDemo && last.demo_equity != null ? (last.demo_equity / firstDemo - 1) * 100 : null
                return (
                  <>
                    <Stat label="Paper" value={last.equity.toFixed(4)} hint={`as of ${fmtDate(last.date)}`} />
                    <Stat label="CFD hold" value={last.hold_equity.toFixed(4)} hint="same days, same costs" />
                    <Stat
                      label="Demo account"
                      value={last.demo_equity != null ? fmtMoney(last.demo_equity, quote, 0) : '—'}
                      tone={demoRet == null ? undefined : demoRet >= 0 ? 'pos' : 'neg'}
                      hint={demoRet != null ? `${fmtPct(demoRet)} since the first demo record` : 'no demo record'}
                    />
                  </>
                )
              })()}
            </div>
          )}
        </section>
      </div>

      {ft && ft.recorded_at && (
        <div style={{ marginBottom: 16 }}>
          <ReconcileCard fillDate={ft.run.last_fill_date || ft.decision.fill_date} book={book} />
        </div>
      )}

      <section className="card" aria-labelledby="ledger-h">
        <div className="card-head">
          <div>
            <h2 id="ledger-h">Ledger</h2>
            <div className="sub">
              One line per decision day: the rule, the paper account, and what the demo account did. Fill quality is the
              order&apos;s average price against the quote mid and the decision close, in bps (positive is worse).
            </div>
          </div>
          {ledger.data && <span className="sub">{ledger.data.records.length} records, newest first</span>}
        </div>
        {ledger.isLoading && <CardSkeleton lines={3} />}
        {ledger.data && ledger.data.records.length > 0 ? (
          <RecordsTable records={ledger.data.records} quote={quote} book={book} />
        ) : (
          ledger.data && <Empty title="No ledger records" />
        )}
      </section>
    </>
  )
}
