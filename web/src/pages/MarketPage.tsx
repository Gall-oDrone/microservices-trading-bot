/**
 * Market (Phase 3): live ticker, spread, order-book depth and the trade tape
 * per book, streamed from Bitso's public WebSocket through ui-api
 * (/api/ui/stream?market=1), plus the daily candles with volume context.
 *
 * Display only: the executor decides on closed daily candles, never on
 * anything on this page.
 */
import { useState, type CSSProperties } from 'react'
import { useSearchParams } from 'react-router'
import { useCandles, useForwardTests } from '../api/client'
import { useLiveStream, type LiveState } from '../api/live'
import type { BookSnapshot, Fill, Market, TapeTrade } from '../api/schemas'
import { PriceChart } from '../components/charts'
import { LiveBadge, TickPrice } from '../components/live'
import { Badge, Banner, CardSkeleton, Empty, ErrorState, Stat } from '../components/ui'
import { ago, bookLabel, fmtDate, fmtPrice, fmtUTC } from '../lib/format'
import { fmtSize, ladder, quoteOf, tapeStats, volumeContext, type LadderRow } from '../lib/market'
import { usePageTitle } from '../lib/usePageTitle'

/** The books ui-api streams; the forward-test list replaces this once loaded. */
const DEFAULT_BOOKS = ['btc_mxn', 'btc_usd']
const RANGES = [90, 180, 365] as const
const LADDER_ROWS = 12
const TAPE_ROWS = 50
/**
 * The Market chart shows no stage fills. One shared array, because `fills` is
 * a chart build dependency: a fresh `[]` each render (the live stream
 * re-renders every second) rebuilt the chart every second.
 */
const NO_FILLS: Fill[] = []

function spreadText(m: Market, quote: string): string {
  return `${fmtPrice(m.spread, quote)} ${quote.toUpperCase()}`
}

/** One book's ticker: last trade, bid/ask, spread. Doubles as the book switcher. */
function Ticker({
  book,
  snap,
  market,
  live,
  active,
  onSelect,
}: {
  book: string
  snap: BookSnapshot | undefined
  market: Market | undefined
  live: LiveState
  active: boolean
  onSelect: () => void
}) {
  const quote = quoteOf(book)
  const has = snap != null && snap.last > 0
  // 25 bps fills the gauge: Bitso's BTC books usually quote well inside that.
  const gauge = market ? Math.min(1, market.spread_bps / 25) : 0
  return (
    <button
      type="button"
      className={`ticker ${active ? 'active' : ''} ${live.phase === 'live' ? '' : 'is-stale'}`}
      aria-pressed={active}
      onClick={onSelect}
      id={`market-book-${book}`}
      data-testid={`ticker-${book}`}
    >
      <span className="ticker-head">
        <span className="ticker-book">{bookLabel(book)}</span>
        {has && snap.last_side && (
          <span
            className={`side-chip ${snap.last_side === 'buy' ? 'buy' : 'sell'}`}
            title="Taker side of the last trade"
          >
            {snap.last_side === 'buy' ? 'taker buy' : 'taker sell'}
          </span>
        )}
        <span className="spacer" />
        {has && snap.last_at && (
          <span className="faint num ticker-ago" title={fmtUTC(snap.last_at)}>
            {ago(snap.last_at, live.now)}
          </span>
        )}
      </span>
      <span className="ticker-price">
        {has ? <TickPrice value={snap.last} quote={quote} /> : <span className="faint">—</span>}
        <span className="ticker-quote">{quote.toUpperCase()}</span>
      </span>
      <span className="ticker-quotes num">
        <span>
          <span className="faint">bid</span> <span className="pos">{market ? fmtPrice(market.bid, quote) : '—'}</span>
        </span>
        <span>
          <span className="faint">ask</span> <span className="neg">{market ? fmtPrice(market.ask, quote) : '—'}</span>
        </span>
      </span>
      <span className="ticker-spread">
        <span className="spread-gauge" aria-hidden="true">
          <span style={{ '--g': gauge } as CSSProperties} />
        </span>
        <span className="num" data-testid={`spread-${book}`}>
          {market ? (
            <>
              {spreadText(market, quote)} · <strong>{market.spread_bps.toFixed(1)} bps</strong>
            </>
          ) : (
            'spread —'
          )}
        </span>
      </span>
    </button>
  )
}

function LadderLine({ r, side, quote }: { r: LadderRow; side: 'bid' | 'ask'; quote: string }) {
  return (
    <div className={`ladder-row ${side}`} style={{ '--w': r.share } as CSSProperties} role="row">
      <span className="num price" role="cell">
        {fmtPrice(r.price, quote)}
      </span>
      <span className="num r" role="cell">
        {fmtSize(r.amount)}
      </span>
      <span className="num r faint" role="cell">
        {fmtSize(r.cum)}
      </span>
    </div>
  )
}

/** The order book: asks above (best at the bottom), the spread, bids below, with cumulative-depth bars. */
function DepthCard({ book, market, live }: { book: string; market: Market; live: LiveState }) {
  const quote = quoteOf(book)
  const l = ladder(market.bids, market.asks, LADDER_ROWS)
  const bidPct = Math.round(l.bidShare * 100)
  return (
    <section className="card depth-card" aria-labelledby="depth-h" data-testid="depth">
      <div className="card-head">
        <div>
          <h2 id="depth-h">Order book</h2>
          <span className="sub">
            top {Math.max(l.bids.length, l.asks.length)} levels per side · updated{' '}
            <span className="num" title={fmtUTC(market.depth_at)}>
              {ago(market.depth_at, live.now)}
            </span>
          </span>
        </div>
        <Badge tone="flat" mono>
          {bookLabel(book)}
        </Badge>
      </div>

      <div className="imbalance" title="Share of the shown size resting on each side">
        <div className="imbalance-labels num">
          <span className="pos">bids {bidPct}%</span>
          <span className="neg">{100 - bidPct}% asks</span>
        </div>
        <div
          className="imbalance-track"
          role="meter"
          aria-label="Bid share of shown depth"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={bidPct}
        >
          <span style={{ width: `${bidPct}%` }} />
        </div>
      </div>

      <div className="ladder" role="table" aria-label={`${bookLabel(book)} order book`}>
        <div className="ladder-row head" role="row">
          <span role="columnheader">Price ({quote.toUpperCase()})</span>
          <span role="columnheader" className="r">
            Size (BTC)
          </span>
          <span role="columnheader" className="r">
            Total
          </span>
        </div>
        <div className="ladder-side asks" role="rowgroup" data-testid="asks">
          {l.asks.length === 0 && <div className="ladder-empty faint">No asks</div>}
          {[...l.asks].reverse().map((r) => (
            <LadderLine key={`a${r.price}`} r={r} side="ask" quote={quote} />
          ))}
        </div>
        <div className="ladder-spread" role="row" data-testid="ladder-spread">
          <span className="num" role="cell">
            {fmtPrice(market.mid, quote)} <span className="faint">mid</span>
          </span>
          <span className="num r" role="cell">
            {spreadText(market, quote)} · {market.spread_bps.toFixed(1)} bps
          </span>
        </div>
        <div className="ladder-side bids" role="rowgroup" data-testid="bids">
          {l.bids.map((r) => (
            <LadderLine key={`b${r.price}`} r={r} side="bid" quote={quote} />
          ))}
          {l.bids.length === 0 && <div className="ladder-empty faint">No bids</div>}
        </div>
      </div>
      <div className="ladder-foot faint num">
        <span>Σ bids {fmtSize(l.bidTotal)} BTC</span>
        <span>Σ asks {fmtSize(l.askTotal)} BTC</span>
      </div>
    </section>
  )
}

function TapeRow({ t, quote, now, fresh }: { t: TapeTrade; quote: string; now: number; fresh: boolean }) {
  return (
    <tr className={`tape-row ${t.side} ${fresh ? 'fresh' : ''}`}>
      <td className="faint num" title={fmtUTC(t.at)}>
        {ago(t.at, now)}
      </td>
      <td>
        <span className={`side-chip ${t.side}`}>{t.side}</span>
      </td>
      <td className={`r num ${t.side === 'buy' ? 'pos' : 'neg'}`}>{fmtPrice(t.price, quote)}</td>
      <td className="r num">{fmtSize(t.amount)}</td>
    </tr>
  )
}

/** Recent trades, newest first, coloured by the taker's side. */
function TapeCard({ book, market, live, since }: { book: string; market: Market; live: LiveState; since: number }) {
  const quote = quoteOf(book)
  const s = tapeStats(market.trades)
  const buyPct = Math.round(s.buyShare * 100)
  return (
    <section className="card tape-card" aria-labelledby="tape-h" data-testid="tape">
      <div className="card-head">
        <div>
          <h2 id="tape-h">Recent trades</h2>
          <span className="sub">taker side · newest first · last {s.count}</span>
        </div>
        {s.vwap != null && (
          <span className="tape-vwap num" title="Size-weighted average price of the trades shown">
            <span className="faint">VWAP</span> {fmtPrice(s.vwap, quote)}
          </span>
        )}
      </div>

      <div className="imbalance" title="Share of the traded size where the taker bought (lifted the ask)">
        <div className="imbalance-labels num">
          <span className="pos">taker buys {buyPct}%</span>
          <span className="neg">{100 - buyPct}% taker sells</span>
        </div>
        <div
          className="imbalance-track"
          role="meter"
          aria-label="Taker-buy share of traded size"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={buyPct}
        >
          <span style={{ width: `${buyPct}%` }} />
        </div>
      </div>

      {market.trades.length === 0 ? (
        <Empty title="No trades yet">Trades appear as they print on Bitso.</Empty>
      ) : (
        <div className="tape-scroll">
          <table className="data tape" id="tape-table">
            <thead>
              <tr>
                <th scope="col">Time</th>
                <th scope="col">Side</th>
                <th scope="col" className="r">
                  Price
                </th>
                <th scope="col" className="r">
                  Size (BTC)
                </th>
              </tr>
            </thead>
            <tbody>
              {market.trades.slice(0, TAPE_ROWS).map((t) => (
                <TapeRow key={t.id} t={t} quote={quote} now={live.now} fresh={Date.parse(t.at) > since} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className="ladder-foot faint">
        {market.tape_seeded
          ? 'Seeded from Bitso REST on connect, then live from the WebSocket.'
          : 'Live from the WebSocket since ui-api connected.'}
      </div>
    </section>
  )
}

function CandlesCard({ book, snap, live }: { book: string; snap: BookSnapshot | undefined; live: LiveState }) {
  const [days, setDays] = useState<(typeof RANGES)[number]>(180)
  const candles = useCandles(book, days)
  const quote = quoteOf(book)
  const v = candles.data ? volumeContext(candles.data.candles) : null
  return (
    <section className="card market-candles" aria-labelledby="candles-h" data-testid="market-candles">
      <div className="card-head">
        <div>
          <h2 id="candles-h">Daily candles · {bookLabel(book)}</h2>
          <div className="legend" style={{ marginTop: 6 }}>
            <span className="key" style={{ color: 'var(--warn)' }}>
              <span className="swatch" /> SMA50
            </span>
            <span className="key" style={{ color: 'var(--info)' }}>
              <span className="swatch" /> volume ≥ 1.5× 20-day avg
            </span>
            {snap?.candle && (
              <span
                className="key"
                style={{ color: 'var(--text-2)' }}
                title="Translucent: still forming, not a closed bar"
              >
                ▮ today (forming)
              </span>
            )}
            {snap?.provisional && (
              <span className="key" style={{ color: 'var(--info)' }} title={snap.provisional.label}>
                <span className="swatch dashed" /> flip level (provisional)
              </span>
            )}
          </div>
        </div>
        <div className="seg" role="group" aria-label="Range">
          {RANGES.map((r) => (
            <button key={r} aria-pressed={days === r} onClick={() => setDays(r)} id={`market-range-${r}`}>
              {r}d
            </button>
          ))}
        </div>
      </div>

      {v && (
        <div className="grid grid-4 vol-stats" data-testid="volume-stats">
          <Stat label="Last closed day" value={`${v.volume.toFixed(2)} BTC`} hint={fmtDate(v.date)} />
          <Stat
            label="vs 20-day average"
            value={v.ratio != null ? `${v.ratio.toFixed(2)}×` : '—'}
            tone={v.ratio != null && v.ratio >= 1.5 ? 'warn' : undefined}
            hint="volume_ratio_20d"
            title="The closed day's volume over its trailing 20-day average"
          />
          <Stat label="20-day average" value={v.avg20 != null ? `${v.avg20.toFixed(2)} BTC` : '—'} hint="per day" />
          <Stat label="High-volume days" value={v.highDays} hint={`≥ 1.5× in the last ${days}d`} />
        </div>
      )}

      {candles.isLoading && <div className="skeleton chart" />}
      {candles.isError && <ErrorState error={candles.error} onRetry={() => candles.refetch()} />}
      {candles.data && (
        <PriceChart
          candles={candles.data.candles}
          fills={NO_FILLS}
          quote={quote}
          label={`${bookLabel(book)} daily candles with SMA50 and volume`}
          live={
            snap
              ? { candle: snap.candle, flip: snap.provisional?.flip_level ?? null, fresh: live.phase === 'live' }
              : undefined
          }
        />
      )}
    </section>
  )
}

export function MarketPage() {
  usePageTitle(
    'Market',
    'Live Bitso order books, spreads and trades for the forward-test books, with daily candles and volume. Display only.',
  )
  const [params, setParams] = useSearchParams()
  const ft = useForwardTests()
  const books = ft.data?.books.map((b) => b.book) ?? DEFAULT_BOOKS
  const want = params.get('book') ?? ''
  const book = books.includes(want) ? want : (books[0] ?? DEFAULT_BOOKS[0])
  const live = useLiveStream(books, { market: true })
  // Trades newer than the page are highlighted as they arrive.
  const [since] = useState(() => Date.now())
  const market = live.markets[book]
  const snap = live.books[book]

  const select = (b: string) =>
    setParams(
      (p) => {
        const n = new URLSearchParams(p)
        n.set('book', b)
        return n
      },
      { replace: true },
    )

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Market</h1>
          <p>
            Bitso&rsquo;s public order books and trades, streamed through ui-api. Display only: the executor decides on
            closed daily candles, never on these numbers.
          </p>
        </div>
        <LiveBadge live={live} />
      </div>

      {(live.phase === 'off' || live.phase === 'error') && (
        <Banner tone="warn" title="Live market data unavailable" id="market-live-off">
          {live.error ?? 'The live stream is off.'} Candles below still load from the ledger.
        </Banner>
      )}

      <div className="tickers" role="group" aria-label="Books">
        {books.map((b) => (
          <Ticker
            key={b}
            book={b}
            snap={live.books[b]}
            market={live.markets[b]}
            live={live}
            active={b === book}
            onSelect={() => select(b)}
          />
        ))}
      </div>

      <div className="market-grid">
        {market ? (
          <>
            <DepthCard book={book} market={market} live={live} />
            <TapeCard book={book} market={market} live={live} since={since} />
          </>
        ) : (
          <>
            <CardSkeleton lines={8} />
            <CardSkeleton lines={8} />
          </>
        )}
      </div>

      <CandlesCard book={book} snap={snap} live={live} />
    </>
  )
}
