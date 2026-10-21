/**
 * Market for an eToro ledger (porting plan P5c): live NSDQ100 and SPX500
 * quotes from ui-api's rates poll (/api/ui/etoro/stream, one GetRates every
 * 5 s for both books), the NYSE session, the forming UTC-day bar and the
 * provisional SMA50 flip level from the executor's stored candles, next to
 * the last close the executor decided on.
 *
 * Display only: the executor decides on closed daily candles at 09:35 New
 * York, never on these numbers. eToro publishes quotes, not trades: the
 * price shown is the mid of the newest poll, and "quotes" counts polls.
 */
import type { CSSProperties } from 'react'
import { Link } from 'react-router'
import { useEtoroForwardTests, useLedgerSearch } from '../../api/client'
import type { EtoroForwardTest, EtoroSession } from '../../api/etoroSchemas'
import { useLiveStream, type LiveState } from '../../api/live'
import type { BookSnapshot } from '../../api/schemas'
import { SessionClock } from '../../components/etoro/parts'
import { IconArrowRight } from '../../components/icons'
import { DayChangePill, LiveBadge, TickPrice } from '../../components/live'
import { Badge, Banner, CardSkeleton, ErrorState, SignalPill, Stat } from '../../components/ui'
import { ago, bookLabel, fmtDate, fmtNY, fmtPct, fmtPrice } from '../../lib/format'
import { usePageTitle } from '../../lib/usePageTitle'
import { PageIntro } from '../../components/PageIntro'

/** The books ui-api polls; the forward-test list replaces this once loaded. */
const DEFAULT_BOOKS = ['nsdq100', 'spx500']

/** Spread in bps that fills the gauge: the index CFDs quote well inside 2 bps in the cash session. */
const GAUGE_FULL_BPS = 2

function midOf(snap: BookSnapshot | undefined): { mid: number; spread: number; bps: number } | null {
  if (!snap || snap.bid <= 0 || snap.ask <= 0) return null
  const mid = (snap.bid + snap.ask) / 2
  const spread = snap.ask - snap.bid
  return { mid, spread, bps: (spread / mid) * 1e4 }
}

/** One book: the quote block, the provisional rule and the last close. */
function BookCard({ ft, snap, live }: { ft: EtoroForwardTest; snap: BookSnapshot | undefined; live: LiveState }) {
  const search = useLedgerSearch()
  const quote = ft.quote || 'USD'
  const q = midOf(snap)
  const p = snap?.provisional ?? null
  const recorded = ft.recorded_at ? ft.decision.signal : ''
  const wouldFlip = p != null && recorded !== '' && p.signal !== recorded
  const fresh = live.phase === 'live'
  const gauge = q ? Math.min(1, q.bps / GAUGE_FULL_BPS) : 0
  const c = snap?.candle ?? null
  return (
    <article className="card venue-etoro" data-testid={`market-${ft.book}`}>
      <div className="card-head">
        <div>
          <h2>{bookLabel(ft.book)}</h2>
          <div className="sub">
            {ft.instrument.symbol ? `${ft.instrument.symbol} · instrument #${ft.instrument.id}` : 'eToro index CFD'}
            {snap?.updated_at && (
              <>
                {' '}
                · updated{' '}
                <span className="num" title={`${fmtNY(snap.updated_at)} NY`} data-testid={`updated-${ft.book}`}>
                  {ago(snap.updated_at, live.now)}
                </span>
              </>
            )}
          </div>
        </div>
        {p ? <SignalPill signal={p.signal} /> : <SignalPill signal={recorded} />}
      </div>

      <div className={`etoro-quote ${fresh ? '' : 'is-stale'}`} data-testid={`quote-${ft.book}`}>
        <span className="ticker-price">
          {q ? <TickPrice value={q.mid} quote={quote} book={ft.book} /> : <span className="faint">—</span>}
          <span className="ticker-quote">{quote.toUpperCase()} mid</span>
          <DayChangePill dc={snap?.day_change} quote={quote} book={ft.book} large />
        </span>
        <span className="ticker-quotes num">
          <span>
            <span className="faint">bid</span>{' '}
            <span className="pos" data-testid={`bid-${ft.book}`}>
              {snap && snap.bid > 0 ? fmtPrice(snap.bid, quote, ft.book) : '—'}
            </span>
          </span>
          <span>
            <span className="faint">ask</span>{' '}
            <span className="neg" data-testid={`ask-${ft.book}`}>
              {snap && snap.ask > 0 ? fmtPrice(snap.ask, quote, ft.book) : '—'}
            </span>
          </span>
        </span>
        <span className="ticker-spread">
          <span className="spread-gauge" aria-hidden="true">
            <span style={{ '--g': gauge } as CSSProperties} />
          </span>
          <span className="num" data-testid={`spread-${ft.book}`}>
            {q ? (
              <>
                {fmtPrice(q.spread, quote, ft.book)} {quote.toUpperCase()} · <strong>{q.bps.toFixed(2)} bps</strong>
              </>
            ) : live.phase === 'connecting' ? (
              'connecting…'
            ) : (
              'spread —'
            )}
          </span>
        </span>
      </div>

      <div className={`grid grid-4 live-grid etoro-rule ${fresh ? '' : 'is-stale'}`} data-testid={`rule-${ft.book}`}>
        <Stat
          label="Flip level"
          value={p ? fmtPrice(p.flip_level, quote, ft.book) : '—'}
          hint={
            p ? (
              <span data-testid={`flip-${ft.book}`}>
                {p.signal === 'long' ? 'flips to flat below' : 'goes long above'}{' '}
                {fmtPrice(p.flip_level, quote, ft.book)}
              </span>
            ) : (
              (snap?.provisional_note ?? 'waiting for the first quote')
            )
          }
          title="Mean of the last 49 closed closes: a close above it means long"
        />
        <Stat
          label="Distance to flip"
          value={p ? fmtPct(p.distance_to_flip_pct, 2) : '—'}
          tone={p ? (p.distance_to_flip_pct > 0 ? 'pos' : 'neg') : undefined}
          hint={p ? `bars to ${fmtDate(p.based_on)}` : undefined}
        />
        <Stat
          label="SMA50 now"
          value={p ? fmtPrice(p.sma50, quote, ft.book) : '—'}
          hint={p ? 'if today closed at the mid' : undefined}
          title={p?.label}
        />
        <Stat
          label="If today closed now"
          value={
            p ? (
              <span className="row" style={{ gap: 6 }}>
                <SignalPill signal={p.signal} />
                {wouldFlip && (
                  <Badge tone="warn" title="Provisional: the recorded signal would change if today closed now">
                    would flip
                  </Badge>
                )}
              </span>
            ) : (
              '—'
            )
          }
          hint={`recorded: ${recorded || 'none'}`}
        />
      </div>

      {c && (
        <div className="etoro-forming num" data-testid={`forming-${ft.book}`}>
          <span className="faint">forming bar (UTC day {fmtDate(c.date)})</span>
          <span>
            O {fmtPrice(c.open, quote, ft.book)} · H {fmtPrice(c.high, quote, ft.book)} · L{' '}
            {fmtPrice(c.low, quote, ft.book)} · C {fmtPrice(c.close, quote, ft.book)}
          </span>
          <span className="faint">
            {c.trade_count} quote{c.trade_count === 1 ? '' : 's'} since ui-api connected · closes 00:00 UTC
          </span>
        </div>
      )}

      <div className="etoro-closes" data-testid={`closes-${ft.book}`}>
        {ft.recorded_at ? (
          <>
            <Stat
              label="Last close"
              value={fmtPrice(ft.decision.close, quote, ft.book)}
              hint={`bar ${fmtDate(ft.decision.bar_date)} · executor's decision`}
            />
            <Stat label="SMA50 recorded" value={fmtPrice(ft.decision.sma50, quote, ft.book)} hint="50 NYSE closes" />
            <Stat
              label="Close vs SMA50"
              value={fmtPct(ft.distance_to_sma_pct, 2)}
              tone={ft.distance_to_sma_pct >= 0 ? 'pos' : 'neg'}
              hint={`signal ${ft.decision.signal}`}
            />
          </>
        ) : (
          <span className="faint">No executor records for this book yet.</span>
        )}
      </div>

      <div style={{ marginTop: 14 }}>
        <Link to={{ pathname: `/forward-tests/${ft.book}`, search }} className="lp-link" id={`market-open-${ft.book}`}>
          Candles and demo orders <IconArrowRight />
        </Link>
      </div>
    </article>
  )
}

export function EtoroMarketPage() {
  usePageTitle(
    'Market · eToro',
    'Live NSDQ100 and SPX500 quotes from eToro, the NYSE session and the provisional SMA50 flip level. Display only.',
  )
  const q = useEtoroForwardTests()
  const books = q.data?.books.map((b) => b.book) ?? DEFAULT_BOOKS
  const live = useLiveStream(books, { venue: 'etoro' })
  const session: EtoroSession | undefined = live.session ?? q.data?.session
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Market</h1>
          <PageIntro>
            eToro&rsquo;s index-CFD quotes, polled by ui-api every 5 s (read only). Display only: the executor decides
            on closed daily candles at 09:35 New York, never on these numbers.
          </PageIntro>
        </div>
        <div className="row" style={{ gap: 10, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
          {session && <SessionClock s={session} />}
          <LiveBadge live={live} />
        </div>
      </div>

      {(live.phase === 'off' || live.phase === 'error') && (
        <Banner tone="warn" title="Live eToro quotes unavailable" id="market-etoro-off">
          {live.error ?? 'The eToro rates feed is off.'} ui-api needs the demo env file (-etoro-env-file) and
          -etoro-live on; the last closes below still load from the ledger.
        </Banner>
      )}
      {session && !session.open && live.phase === 'live' && (
        <Banner tone="info" title="NYSE cash session closed" id="market-etoro-closed">
          eToro keeps quoting the index CFDs off the futures; the executor only acts inside the cash session, and the
          forming bar still closes at 00:00 UTC.
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
        <div className="grid grid-2 etoro-market">
          {q.data.books.map((b) => (
            <BookCard key={b.book} ft={b} snap={live.books[b.book]} live={live} />
          ))}
        </div>
      )}
    </>
  )
}
