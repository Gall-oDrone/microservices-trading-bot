import { QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { createChart } from 'lightweight-charts'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { liveSnapshotSchema, type CandlePoint, type Fill, type LiveCandle, type Market } from '../api/schemas'
import { PriceChart } from '../components/charts'
import { fmtSize, ladder, quoteOf, tapeStats, volumeContext } from '../lib/market'
import liveMarket from '../mocks/fixtures/live-market.json'
import { makeQueryClient, routes } from '../router'

/** Stands in for the browser's EventSource; tests push events with emit(). */
class FakeEventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2
  static instances: FakeEventSource[] = []
  readonly url: string
  readyState = FakeEventSource.CONNECTING
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  private listeners: Record<string, ((e: MessageEvent) => void)[]> = {}
  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }
  addEventListener(name: string, f: (e: MessageEvent) => void) {
    ;(this.listeners[name] ??= []).push(f)
  }
  close() {
    this.readyState = FakeEventSource.CLOSED
  }
  open() {
    act(() => {
      this.readyState = FakeEventSource.OPEN
      this.onopen?.()
    })
  }
  emit(name: string, data: unknown) {
    const e = new MessageEvent(name, { data: typeof data === 'string' ? data : JSON.stringify(data) })
    act(() => this.listeners[name]?.forEach((f) => f(e)))
  }
}

let qc: ReturnType<typeof makeQueryClient>

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  qc = makeQueryClient()
  qc.setDefaultOptions({ queries: { retry: false } })
  render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return router
}

async function stream(): Promise<FakeEventSource> {
  await waitFor(() => {
    expect(FakeEventSource.instances.length).toBeGreaterThan(0)
    expect(qc.isFetching()).toBe(0)
  })
  const es = FakeEventSource.instances[FakeEventSource.instances.length - 1]
  es.open()
  return es
}

const snapshot = () => liveSnapshotSchema.parse(structuredClone(liveMarket))
const marketOf = (book: string): Market => structuredClone(snapshot().markets!.find((m) => m.book === book)!)

beforeEach(() => {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => vi.unstubAllGlobals())

describe('market contract', () => {
  it('the captured /api/ui/live?market=1 response parses with the UI schema', () => {
    const r = liveSnapshotSchema.safeParse(liveMarket)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
    const books = r.data!.markets!.map((m) => m.book).sort()
    expect(books).toEqual(['btc_mxn', 'btc_usd'])
    for (const m of r.data!.markets!) {
      expect(m.bids.length).toBeGreaterThan(0)
      expect(m.asks.length).toBeGreaterThan(0)
      expect(m.bid).toBeLessThan(m.ask)
      // ui-api sorts best first: bids descending, asks ascending.
      expect(m.bids[0].price).toBe(m.bid)
      expect(m.asks[0].price).toBe(m.ask)
    }
  })
})

describe('market helpers', () => {
  it('quoteOf and fmtSize', () => {
    expect(quoteOf('btc_mxn')).toBe('mxn')
    expect(quoteOf('weird')).toBe('')
    expect(fmtSize(0.00032901)).toBe('0.000329')
    expect(fmtSize(1.23456)).toBe('1.2346')
    expect(fmtSize(Number.NaN)).toBe('—')
  })

  it('ladder: cumulative size, one bar scale for both sides, bid share', () => {
    const l = ladder(
      [
        { price: 100, amount: 1 },
        { price: 99, amount: 3 },
        { price: 98, amount: 5 },
      ],
      [{ price: 101, amount: 2 }],
      2,
    )
    expect(l.bids.map((r) => r.cum)).toEqual([1, 4])
    expect(l.bids.map((r) => r.share)).toEqual([0.25, 1])
    expect(l.asks).toEqual([{ price: 101, amount: 2, cum: 2, share: 0.5 }])
    expect(l.bidTotal).toBe(4)
    expect(l.askTotal).toBe(2)
    expect(l.bidShare).toBeCloseTo(4 / 6)
    expect(ladder([], []).bidShare).toBe(0.5)
  })

  it('tapeStats: taker-buy share, VWAP, time range', () => {
    const s = tapeStats([
      { id: 2, price: 110, amount: 1, side: 'buy', at: '2026-10-08T00:00:02Z' },
      { id: 1, price: 100, amount: 3, side: 'sell', at: '2026-10-08T00:00:01Z' },
    ])
    expect(s.count).toBe(2)
    expect(s.buyShare).toBe(0.25)
    expect(s.vwap).toBeCloseTo(102.5)
    expect(s.from).toBe('2026-10-08T00:00:01Z')
    expect(s.to).toBe('2026-10-08T00:00:02Z')
    expect(tapeStats([])).toMatchObject({ buyShare: 0.5, vwap: null, from: null })
  })

  it('volumeContext: latest bar, ratio and implied 20-day average', () => {
    const bar = (date: string, volume: number, r: number | null): CandlePoint => ({
      date,
      open: 1,
      high: 1,
      low: 1,
      close: 1,
      volume,
      trade_count: 1,
      sma50: null,
      volume_ratio_20d: r,
      long: null,
    })
    const v = volumeContext([bar('2026-10-05', 30, 2), bar('2026-10-06', 10, 0.5)])!
    expect(v).toEqual({ date: '2026-10-06', volume: 10, ratio: 0.5, avg20: 20, highDays: 1 })
    expect(volumeContext([])).toBeNull()
    expect(volumeContext([bar('2026-10-06', 10, null)])!.avg20).toBeNull()
  })
})

describe('market page', () => {
  it('streams depth and the tape for every book and renders tickers, ladder and trades', async () => {
    renderAt('/market')
    expect(await screen.findByRole('heading', { level: 1, name: 'Market' })).toBeInTheDocument()
    const es = await stream()
    expect(es.url).toBe('/api/ui/stream?books=btc_mxn%2Cbtc_usd&market=1')
    es.emit('snapshot', snapshot())

    const mxn = await screen.findByTestId('ticker-btc_mxn')
    expect(mxn).toHaveAttribute('aria-pressed', 'true')
    const m = marketOf('btc_mxn')
    expect(within(mxn).getByTestId('spread-btc_mxn')).toHaveTextContent(`${m.spread_bps.toFixed(1)} bps`)
    expect(screen.getByTestId('ticker-btc_usd')).toHaveAttribute('aria-pressed', 'false')

    const depth = screen.getByTestId('depth')
    expect(within(depth).getByText('BTC / MXN')).toBeInTheDocument()
    expect(within(screen.getByTestId('bids')).getAllByRole('row').length).toBe(Math.min(12, m.bids.length))
    expect(within(screen.getByTestId('asks')).getAllByRole('row').length).toBe(Math.min(12, m.asks.length))
    expect(screen.getByTestId('ladder-spread')).toHaveTextContent(m.mid.toLocaleString('en-US'))

    const tape = screen.getByTestId('tape')
    expect(within(tape).getAllByRole('row').length).toBe(Math.min(50, m.trades.length) + 1)
    expect(within(tape).getByText(/Seeded from Bitso REST/)).toBeInTheDocument()
    expect(screen.getByTestId('live-badge')).toHaveTextContent(/^live$/i)
  })

  it('switches book from the ticker and keeps it in the URL', async () => {
    const router = renderAt('/market?ledger=stage')
    const es = await stream()
    es.emit('snapshot', snapshot())
    fireEvent.click(await screen.findByTestId('ticker-btc_usd'))
    expect(screen.getByTestId('ticker-btc_usd')).toHaveAttribute('aria-pressed', 'true')
    expect(within(screen.getByTestId('depth')).getByText('BTC / USD')).toBeInTheDocument()
    const q = new URLSearchParams(router.state.location.search)
    expect(q.get('book')).toBe('btc_usd')
    expect(q.get('ledger')).toBe('stage')
    // Same books, so the same stream: no reconnect on a book switch.
    expect(FakeEventSource.instances.length).toBe(1)
  })

  it('applies "market" events: new spread, new trade on top', async () => {
    renderAt('/market')
    const es = await stream()
    es.emit('snapshot', snapshot())
    await screen.findByTestId('depth')

    const m = marketOf('btc_mxn')
    const ask = m.bid + 150
    m.ask = ask
    m.asks = [{ price: ask, amount: 0.5 }, ...m.asks.filter((l) => l.price > ask)]
    m.spread = 150
    m.mid = (m.bid + ask) / 2
    m.spread_bps = (150 / m.mid) * 10_000
    m.trades = [{ id: 999_999_999, price: ask, amount: 0.5, side: 'buy', at: new Date().toISOString() }, ...m.trades]
    es.emit('market', m)

    await waitFor(() =>
      expect(screen.getByTestId('spread-btc_mxn')).toHaveTextContent(`150 MXN · ${m.spread_bps.toFixed(1)} bps`),
    )
    const first = within(screen.getByTestId('tape')).getAllByRole('row')[1]
    expect(first).toHaveClass('fresh')
    expect(first).toHaveTextContent('0.500000')
  })

  it('flags a "market" event that breaks the contract', async () => {
    renderAt('/market')
    const es = await stream()
    es.emit('snapshot', snapshot())
    es.emit('market', { ...marketOf('btc_mxn'), trades: [{ id: 1, price: 1, amount: 1, side: 'maker', at: '' }] })
    await waitFor(() => expect(screen.getByTestId('live-badge')).toHaveTextContent(/live error/i))
    expect(screen.getByTestId('live-badge').title).toMatch(
      /"market" event does not match the UI contract at "trades.0.side"/,
    )
  })

  it('is in the nav', async () => {
    renderAt('/forward-tests')
    const link = await screen.findByRole('link', { name: /market/i })
    expect(link).toHaveAttribute('id', 'nav-market')
    expect(link.getAttribute('href')).toMatch(/^\/market/)
  })

  // Regression (2026-10-08): `fills={[]}` was a new array on every render, so the
  // chart was torn down and rebuilt on every live tick and its last bar flickered.
  it('builds the candles chart once and keeps it across live updates', async () => {
    renderAt('/market')
    const es = await stream()
    es.emit('snapshot', snapshot())
    await screen.findByTestId('volume-stats')
    await waitFor(() => expect(vi.mocked(createChart).mock.calls.length).toBeGreaterThan(0))
    const built = vi.mocked(createChart).mock.calls.length
    const m = marketOf('btc_mxn')
    const b = snapshot().books.find((x) => x.book === 'btc_mxn')!
    for (let i = 1; i <= 5; i++) {
      es.emit('market', { ...m, bid: m.bid - i })
      es.emit('book', { ...b, last: b.last + i })
    }
    expect(vi.mocked(createChart).mock.calls.length).toBe(built)
  })
})

describe('PriceChart live overlay', () => {
  const bar = (date: string, close: number): CandlePoint => ({
    date,
    open: close,
    high: close,
    low: close,
    close,
    volume: 1,
    trade_count: 1,
    sma50: null,
    volume_ratio_20d: null,
    long: null,
  })
  const candles = [bar('2026-10-06', 100), bar('2026-10-07', 101)]
  const forming: LiveCandle = {
    date: '2026-10-08',
    open: 101,
    high: 103,
    low: 100,
    close: 102,
    volume: 0.5,
    trade_count: 3,
    seeded: true,
  }
  type MockSeries = { update: ReturnType<typeof vi.fn> }
  type MockChart = { addSeries: { mock: { results: { value: MockSeries }[] } } }
  const priceSeriesOf = (i: number) =>
    (vi.mocked(createChart).mock.results[i].value as unknown as MockChart).addSeries.mock.results[0].value

  it('re-applies the forming bar after a rebuild that only changed fills', () => {
    const live = { candle: forming, flip: 100.5, fresh: true }
    const fillsA: Fill[] = []
    const { rerender } = render(<PriceChart candles={candles} fills={fillsA} quote="usd" label="x" live={live} />)
    const first = vi.mocked(createChart).mock.calls.length - 1
    expect(priceSeriesOf(first).update).toHaveBeenCalledWith(
      expect.objectContaining({ time: '2026-10-08', close: 102 }),
    )

    // Same fills identity: no rebuild.
    rerender(<PriceChart candles={candles} fills={fillsA} quote="usd" label="x" live={{ ...live }} />)
    expect(vi.mocked(createChart).mock.calls.length - 1).toBe(first)

    // New fills identity: rebuilt, and the new series gets the forming bar too.
    rerender(<PriceChart candles={candles} fills={[]} quote="usd" label="x" live={live} />)
    const second = vi.mocked(createChart).mock.calls.length - 1
    expect(second).toBe(first + 1)
    expect(priceSeriesOf(second).update).toHaveBeenCalledWith(
      expect.objectContaining({ time: '2026-10-08', close: 102 }),
    )
  })
})
