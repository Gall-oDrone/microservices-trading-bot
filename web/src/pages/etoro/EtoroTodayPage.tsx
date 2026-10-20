/**
 * Today (U2, 2026-10-20): the eToro operator's first screen. At 09:40 New
 * York the questions are: did the run happen and what did it do; what do I
 * hold, where is it against its stop and target, what is the most I can lose
 * at the stops; is anything waiting on me. One page answers them from the
 * endpoints that already exist (forward tests, positions, health, risk,
 * candles for the ATR); it sends nothing and computes only distances.
 */
import { Link } from 'react-router'
import {
  useEtoroCandles,
  useEtoroForwardTests,
  useEtoroHealth,
  useEtoroPositions,
  useEtoroRisk,
  useLedgerName,
  useLedgerSearch,
} from '../../api/client'
import type { EtoroForwardTest, EtoroTrade } from '../../api/etoroSchemas'
import { tradePnL } from '../../api/etoroSchemas'
import { PageIntro } from '../../components/PageIntro'
import { NextRun, OrderLine, RunBadge, SessionClock } from '../../components/etoro/parts'
import { IconArrowRight, IconShield } from '../../components/icons'
import { Badge, CardSkeleton, Empty, ErrorState, LedgerBadge, SignalPill, Stat } from '../../components/ui'
import { attentionItems } from '../../lib/attention'
import { bookSymbol, fmtDate, fmtFrac, fmtMoney, fmtPrice, fmtUnits, fmtUTC } from '../../lib/format'
import { atr, distanceTo, markOf, openRisk } from '../../lib/today'
import { useClock } from '../../lib/tz'
import { usePageTitle } from '../../lib/usePageTitle'

function signed(v: number, quote: string, digits = 2): string {
  const s = fmtMoney(Math.abs(v), quote, digits)
  return v < 0 ? `−${s}` : v > 0 ? `+${s}` : s
}

function DistanceCell({
  mark,
  level,
  atrValue,
  kind,
  quote,
  book,
}: {
  mark: number
  level: number | undefined
  atrValue: number
  kind: 'stop' | 'target'
  quote: string
  book: string
}) {
  if (!level) return <span className="faint">{kind === 'stop' ? 'none' : '—'}</span>
  const d = distanceTo(mark, level, atrValue, kind)
  if (!d) return <span className="faint">—</span>
  const close = kind === 'stop' && d.atrs > 0 && d.atrs < 1
  return (
    <span className={`today-dist ${close ? 'close' : ''}`} title={`${kind} at ${fmtPrice(level, quote, book)}`}>
      <b className="num">{fmtPrice(level, quote, book)}</b>
      <small className="num">
        {fmtFrac(d.pct)} away{d.atrs > 0 ? ` · ${d.atrs.toFixed(1)} ATR` : ''}
      </small>
    </span>
  )
}

function OpenRow({ t, atrValue, quote, search }: { t: EtoroTrade; atrValue: number; quote: string; search: string }) {
  const mark = markOf(t)
  const p = tradePnL(t)
  const maxLoss = t.stop_loss_rate ? Math.max(0, t.units * (t.open_rate - t.stop_loss_rate)) : null
  return (
    <tr data-testid={`today-open-${t.position_id}`}>
      <td data-label="Position">
        <div className="positions-id">
          <Link to={`/forward-tests/${encodeURIComponent(t.book)}${search}`} className="positions-book">
            {t.symbol}
          </Link>
          <span className="row" style={{ gap: 6 }}>
            <Badge tone={t.source === 'manual' ? 'etoro' : 'info'}>{t.source}</Badge>
            <span className="faint num">{fmtUnits(t.units)}</span>
          </span>
        </div>
      </td>
      <td data-label="Entry → mark" className="num">
        {fmtPrice(t.open_rate, quote, t.book)} <span className="faint">→</span>{' '}
        {t.mark_source === 'account' ? `≈ ${fmtPrice(mark, quote, t.book)}` : <span className="faint">not read</span>}
      </td>
      <td data-label="Unrealised" className="r">
        {p ? (
          <span className={`positions-pnl ${p.value > 0 ? 'pos' : p.value < 0 ? 'neg' : ''}`}>
            <b className="num">{signed(p.value, quote)}</b>
            <small className="num">{t.unrealized_pct != null ? fmtFrac(t.unrealized_pct) : ''}</small>
          </span>
        ) : (
          <span className="faint">—</span>
        )}
      </td>
      <td data-label="Stop">
        {t.stop_loss_rate ? (
          <DistanceCell
            mark={mark}
            level={t.stop_loss_rate}
            atrValue={atrValue}
            kind="stop"
            quote={quote}
            book={t.book}
          />
        ) : (
          <Link
            to={`/trade${search}${search ? '&' : '?'}book=${encodeURIComponent(t.book)}&side=protect&position=${encodeURIComponent(t.position_id)}`}
            className="positions-protect"
            data-testid={`today-protect-${t.position_id}`}
          >
            <IconShield /> no stop · protect
          </Link>
        )}
      </td>
      <td data-label="Target">
        <DistanceCell
          mark={mark}
          level={t.take_profit_rate}
          atrValue={atrValue}
          kind="target"
          quote={quote}
          book={t.book}
        />
      </td>
      <td data-label="Max loss at stop" className="r num">
        {maxLoss != null ? (
          <span title="units × (entry − stop), before gap and fees">{fmtMoney(maxLoss, quote)}</span>
        ) : (
          <span className="faint">unbounded</span>
        )}
      </td>
      <td data-label="Held" className="num">
        {t.held_days != null ? `${t.held_days} d` : '—'}
      </td>
    </tr>
  )
}

function LastRunCard({ ft, search }: { ft: EtoroForwardTest; search: string }) {
  const d = ft.decision
  const tone =
    ft.demo_action === 'open'
      ? 'long'
      : ft.demo_action === 'close'
        ? 'flat'
        : ft.demo_action === 'blocked'
          ? 'block'
          : undefined
  return (
    <div className="today-run" data-testid={`today-run-${ft.book}`}>
      <div className="today-run-head">
        <Link to={`/forward-tests/${encodeURIComponent(ft.book)}${search}`} className="today-run-title">
          {bookSymbol(ft.book)} <IconArrowRight />
        </Link>
        <RunBadge run={ft.run} withNext={false} />
      </div>
      {d.bar_date ? (
        <>
          <div className="today-run-line">
            <span className="faint num">
              bar {fmtDate(d.bar_date)} → {fmtDate(d.fill_date)}
            </span>
            <SignalPill signal={d.signal || ''} />
            <span className="faint">{d.action}</span>
            {ft.demo_action ? (
              <Badge tone={tone} title="what the demo account did">
                demo {ft.demo_action}
              </Badge>
            ) : (
              <Badge tone="flat">dry run</Badge>
            )}
            {ft.paper.position && (
              <span className="faint">
                paper {ft.paper.position} · {ft.paper.equity_if_closed.toFixed(4)}
              </span>
            )}
          </div>
          {ft.last_order && ft.last_order.fill_date === ft.run.last_fill_date ? (
            <OrderLine o={ft.last_order} book={ft.book} quote={ft.quote || 'usd'} />
          ) : (
            <span className="faint">no order on the last run</span>
          )}
          <span className="faint" style={{ fontSize: 12 }}>
            {ft.run.message}
          </span>
        </>
      ) : (
        <span className="faint">no record yet</span>
      )}
    </div>
  )
}

export function EtoroTodayPage() {
  usePageTitle('Today · eToro', 'The run, the positions against their stops, the money at risk, what needs you.')
  const ledger = useLedgerName() || 'etoro-demo'
  const search = useLedgerSearch()
  const clock = useClock()
  const ft = useEtoroForwardTests()
  const pos = useEtoroPositions()
  const health = useEtoroHealth()
  const risk = useEtoroRisk()
  // The ATR per book for the distances (the executor's own bars; 90 days is
  // plenty for ATR(14)). Two fixed hooks: the books are the executor's.
  const cN = useEtoroCandles('nsdq100', 90)
  const cS = useEtoroCandles('spx500', 90)
  const atrOf: Record<string, number> = {
    nsdq100: atr(cN.data?.candles ?? []),
    spx500: atr(cS.data?.candles ?? []),
  }

  const books = ft.data?.books ?? []
  const trades = pos.data?.trades ?? []
  const open = trades.filter((t) => t.status === 'open')
  const r = openRisk(trades)
  const quote = pos.data?.account?.currency ?? 'USD'
  const equity = pos.data?.account?.equity ?? books[0]?.account?.equity ?? 0
  const halt = risk.data
    ? {
        halted: risk.data.halted,
        reason: risk.data.halt_reason || risk.data.halt_file.reason,
        by: risk.data.halt_file.by,
      }
    : null
  const needs = attentionItems(halt, health.data?.checks, search)
  for (const t of trades) {
    if (t.status === 'unknown' || (t.status === 'closed' && t.recorded_close === false)) {
      needs.push({
        id: `pos-${t.position_id}`,
        tone: 'warn',
        label: `${t.symbol} ${t.position_id}`,
        message:
          t.notes?.[0] ?? (t.status === 'unknown' ? 'not held, not in the history yet' : 'closed outside the ledger'),
        to: `/trade${search}`,
      })
    } else if (t.status === 'open' && t.protection_drift) {
      needs.push({
        id: `drift-${t.position_id}`,
        tone: 'warn',
        label: `${t.symbol} protection`,
        message: t.notes?.[0] ?? 'account and ledger disagree on the stop',
        to: `/trade${search}`,
      })
    }
  }
  const loading = ft.isLoading || pos.isLoading

  return (
    <>
      <div className="page-head">
        <div>
          <div className="row" style={{ gap: 12 }}>
            <h1>Today</h1>
            <LedgerBadge name={ledger} venue="etoro" />
            <Badge tone="etoro">demo account</Badge>
          </div>
          <PageIntro>
            The eToro operator&apos;s first screen: what the 09:35 New York run did, what the account holds against its
            stops and targets (distance in % and ATR(14) of the executor&apos;s own bars), the most that can be lost at
            the stops, and what is waiting on you. Nothing here sends anything.
          </PageIntro>
        </div>
        {ft.data && (
          <div className="stack" style={{ alignItems: 'flex-end', gap: 6 }}>
            <SessionClock s={ft.data.session} />
            {books[0] && <NextRun run={books[0].run} />}
            {pos.data?.account && (
              <span className="faint" title={fmtUTC(pos.data.account.at)}>
                account read {clock.fmt(pos.data.account.at)} {clock.label}
              </span>
            )}
          </div>
        )}
      </div>

      {loading && (
        <div className="grid grid-2">
          <CardSkeleton lines={5} />
          <CardSkeleton lines={5} />
        </div>
      )}
      {ft.isError && (
        <div className="card">
          <ErrorState error={ft.error} onRetry={() => ft.refetch()} />
        </div>
      )}

      {ft.data && pos.data && (
        <>
          <section className="card venue-etoro today-risk" aria-labelledby="today-risk-h" data-testid="today-risk">
            <div className="card-head">
              <div>
                <h2 id="today-risk-h">Open risk</h2>
                <span className="sub">
                  {pos.data.live
                    ? 'account read live'
                    : `ledger only — ${pos.data.live_reason ?? 'the account was not read'}`}
                </span>
              </div>
              <Link to={`/trade${search}`} className="btn btn-ghost btn-sm" id="today-to-trade">
                Trade page <IconArrowRight />
              </Link>
            </div>
            <div className="today-strip" data-testid="today-strip">
              <Stat
                label="Equity"
                value={<span className="num">{equity ? fmtMoney(equity, quote, 2) : '—'}</span>}
                hint={pos.data.account ? `cash ${fmtMoney(pos.data.account.cash, quote, 0)}` : 'from the last run'}
              />
              <Stat
                label="Open exposure"
                value={<span className="num">{fmtMoney(r.exposure, quote, 0)}</span>}
                hint={
                  equity > 0
                    ? `${fmtFrac(r.exposure / equity)} of equity · ${r.open} position${r.open === 1 ? '' : 's'}`
                    : `${r.open} positions`
                }
                tone={r.open ? 'pos' : undefined}
              />
              <Stat
                label="Unrealised"
                value={
                  pos.data.live ? (
                    <span className="num">{signed(r.unrealized, quote)}</span>
                  ) : (
                    <span className="faint">not read</span>
                  )
                }
                hint={pos.data.live ? 'account figures' : 'needs the account'}
                tone={pos.data.live && r.unrealized !== 0 ? (r.unrealized > 0 ? 'pos' : 'neg') : undefined}
              />
              <Stat
                label="Max loss at stops"
                value={
                  <span className="num" data-testid="today-max-loss">
                    {r.open === 0
                      ? '—'
                      : r.unprotected > 0 && r.maxLossAtStops === 0
                        ? 'unbounded'
                        : fmtMoney(r.maxLossAtStops, quote)}
                  </span>
                }
                hint={
                  r.open === 0
                    ? 'flat on every book'
                    : r.unprotected > 0
                      ? `${r.unprotected} position${r.unprotected === 1 ? '' : 's'} without a stop (${fmtMoney(r.unprotectedExposure, quote, 0)})`
                      : equity > 0
                        ? `${fmtFrac(r.maxLossAtStops / equity)} of equity, before gap and fees`
                        : 'before gap and fees'
                }
                tone={r.open === 0 ? undefined : r.unprotected > 0 ? 'warn' : 'neg'}
              />
            </div>
            {open.length === 0 ? (
              <Empty title="Flat on every book">
                The rule holds nothing on the demo account; the next run may open.
              </Empty>
            ) : (
              <div className="table-wrap">
                <table className="data cards-sm today-open" data-testid="today-open">
                  <thead>
                    <tr>
                      <th scope="col">Position</th>
                      <th scope="col">Entry → mark</th>
                      <th scope="col" className="r">
                        Unrealised
                      </th>
                      <th scope="col">Stop</th>
                      <th scope="col">Target</th>
                      <th scope="col" className="r">
                        Max loss at stop
                      </th>
                      <th scope="col">Held</th>
                    </tr>
                  </thead>
                  <tbody>
                    {open.map((t) => (
                      <OpenRow key={t.position_id} t={t} atrValue={atrOf[t.book] ?? 0} quote={quote} search={search} />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <div className="grid grid-2 today-grid">
            <section className="card venue-etoro" aria-labelledby="today-runs-h" data-testid="today-runs">
              <div className="card-head">
                <div>
                  <h2 id="today-runs-h">Last run</h2>
                  <span className="sub">per book: the decision, what the demo did, the fill</span>
                </div>
              </div>
              <div className="stack" style={{ gap: 12 }}>
                {books.map((b) => (
                  <LastRunCard key={b.book} ft={b} search={search} />
                ))}
              </div>
            </section>

            <section className="card venue-etoro" aria-labelledby="today-needs-h" data-testid="today-needs">
              <div className="card-head">
                <div>
                  <h2 id="today-needs-h">Needs you</h2>
                  <span className="sub">the halt, failing or warning checks, positions to resolve by hand</span>
                </div>
                {needs.length > 0 && (
                  <Badge tone={needs.some((n) => n.tone === 'block') ? 'block' : 'warn'}>{needs.length}</Badge>
                )}
              </div>
              {needs.length === 0 ? (
                <Empty title="Nothing waiting on you">Every check is ok, no halt, every position accounted for.</Empty>
              ) : (
                <ul className="today-needs">
                  {needs.map((n) => (
                    <li key={n.id} data-testid={`today-need-${n.id}`}>
                      <Link to={n.to} className="attention-link">
                        <Badge tone={n.tone} dot>
                          {n.label}
                        </Badge>
                        <span className="today-need-msg">{n.message}</span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </div>
        </>
      )}
    </>
  )
}
