import { QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { etoroLiveSnapshotSchema } from '../api/etoroSchemas'
import { livePhase, STALE_MS, type LiveRaw } from '../api/live'
import { liveSnapshotSchema, type LiveStatus } from '../api/schemas'
import etoroLive from '../mocks/fixtures/etoro-demo/etoro-live.json'
import live from '../mocks/fixtures/live.json'
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
  fail(closed: boolean) {
    act(() => {
      if (closed) this.readyState = FakeEventSource.CLOSED
      this.onerror?.()
    })
  }
}

let qc: ReturnType<typeof makeQueryClient>

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  qc = makeQueryClient()
  qc.setDefaultOptions({ queries: { retry: false } })
  return render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

/** Waits for the page's REST queries to settle and its EventSource to exist, then opens it. */
async function stream(): Promise<FakeEventSource> {
  await waitFor(() => {
    expect(FakeEventSource.instances.length).toBeGreaterThan(0)
    expect(qc.isFetching()).toBe(0)
  })
  const es = FakeEventSource.instances[FakeEventSource.instances.length - 1]
  es.open()
  return es
}

const snapshot = () => liveSnapshotSchema.parse(structuredClone(live))

beforeEach(() => {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => vi.unstubAllGlobals())

describe('live contract', () => {
  it('the captured /api/ui/live response parses with the UI schema', () => {
    const r = liveSnapshotSchema.safeParse(live)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
  })
})

describe('livePhase', () => {
  const up: LiveStatus = { source: 'wss://x', connected: true, since: '', last_message_at: '', reconnects: 0 }
  const base: LiveRaw = {
    conn: 'open',
    upstream: up,
    books: {},
    markets: {},
    session: null,
    lastEventAt: 1_000,
    error: null,
  }
  it.each<[string, Partial<LiveRaw>, number, string]>([
    ['fresh event', {}, 1_000 + STALE_MS, 'live'],
    ['no event for longer than STALE_MS', {}, 1_001 + STALE_MS, 'stale'],
    ['upstream (Bitso) disconnected', { upstream: { ...up, connected: false } }, 2_000, 'reconnecting'],
    ['browser reconnecting', { conn: 'reconnecting' }, 2_000, 'reconnecting'],
    ['before the first event', { conn: 'connecting', lastEventAt: null }, 2_000, 'connecting'],
    ['server refused the stream', { conn: 'off' }, 2_000, 'off'],
    ['contract mismatch', { error: 'contract: bad' }, 2_000, 'error'],
  ])('%s', (_n, patch, now, want) => {
    expect(livePhase({ ...base, ...patch }, now)).toBe(want)
  })
})

describe('live strip', () => {
  it('opens one stream for all books and shows provisional live values on each card', async () => {
    renderAt('/forward-tests')
    const es = await stream()
    expect(es.url).toBe('/api/ui/stream?books=btc_mxn%2Cbtc_usd')
    const snap = snapshot()
    es.emit('snapshot', snap)

    const mxn = await screen.findByTestId('live-btc_mxn')
    expect(within(mxn).getByTestId('live-badge')).toHaveTextContent(/^live$/i)
    expect(within(mxn).getByText('provisional: if today closed now')).toBeInTheDocument()
    const b = snap.books.find((x) => x.book === 'btc_mxn')!
    expect(within(mxn).getByTestId('live-price')).toHaveTextContent(b.last.toLocaleString('en-US'))
    expect(within(mxn).getByText('Flip level')).toBeInTheDocument()
    expect(within(mxn).queryByText('would flip')).not.toBeInTheDocument()
    expect(screen.getByTestId('live-btc_usd')).toBeInTheDocument()
  })

  it('warns when the rule would flip if today closed now', async () => {
    renderAt('/forward-tests')
    const es = await stream()
    const snap = snapshot()
    es.emit('snapshot', snap)
    const b = structuredClone(snap.books.find((x) => x.book === 'btc_mxn')!)
    b.last = b.provisional!.flip_level * 0.99
    b.provisional = { ...b.provisional!, price: b.last, signal: 'flat', distance_to_flip_pct: -1 }
    es.emit('book', b)
    const mxn = await screen.findByTestId('live-btc_mxn')
    expect(within(mxn).getByText('would flip')).toBeInTheDocument()
    expect(within(mxn).getByText('-1.0%')).toBeInTheDocument()
  })

  it('shows reconnecting when Bitso drops, and an error on a contract mismatch', async () => {
    renderAt('/forward-tests')
    const es = await stream()
    es.emit('snapshot', snapshot())
    es.emit('status', { ...snapshot().upstream, connected: false, last_error: 'read: timeout' })
    expect((await screen.findAllByTestId('live-badge'))[0]).toHaveTextContent(/reconnecting/i)
    es.emit('book', { book: 'btc_mxn', last: 'not a number' })
    expect(screen.getAllByTestId('live-badge')[0]).toHaveTextContent(/live error/i)
    expect(screen.getAllByTestId('live-badge')[0].title).toMatch(/does not match the UI contract at "last"/)
  })

  it('turns off and retries when the server refuses the stream (ui-api -live=false)', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      renderAt('/forward-tests')
      const es = await stream()
      es.fail(true)
      expect((await screen.findAllByTestId('live-badge'))[0]).toHaveTextContent(/live off/i)
      const n = FakeEventSource.instances.length
      await act(async () => {
        await vi.advanceTimersByTimeAsync(15_000)
      })
      expect(FakeEventSource.instances.length).toBe(n + 1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('detail page streams only its book', async () => {
    renderAt('/forward-tests/btc_usd')
    const es = await stream()
    expect(es.url).toBe('/api/ui/stream?books=btc_usd')
    es.emit('snapshot', { ...snapshot(), books: snapshot().books.filter((b) => b.book === 'btc_usd') })
    const usd = await screen.findByTestId('live-btc_usd')
    expect(within(usd).getByText('If today closed now')).toBeInTheDocument()
    expect(await screen.findByText('flip level (provisional)')).toBeInTheDocument()
    expect(await screen.findByText(/records, newest first/)).toBeInTheDocument()
  })
})

describe('eToro market (rates stream, P5c)', () => {
  const etoroSnapshot = () => etoroLiveSnapshotSchema.parse(structuredClone(etoroLive))

  it('the captured /api/ui/etoro/live response parses with the UI schema', () => {
    const r = etoroLiveSnapshotSchema.safeParse(etoroLive)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
  })

  it('opens the eToro stream for both books and shows bid, ask, spread, session and the flip level', async () => {
    renderAt('/market?ledger=etoro-demo')
    const es = await stream()
    expect(es.url).toBe('/api/ui/etoro/stream?books=nsdq100%2Cspx500')
    const snap = etoroSnapshot()
    es.emit('snapshot', snap)

    const nsdq = await screen.findByTestId('market-nsdq100')
    expect(within(nsdq).getByTestId('bid-nsdq100')).toHaveTextContent('29,189.40')
    expect(within(nsdq).getByTestId('ask-nsdq100')).toHaveTextContent('29,190.10')
    expect(within(nsdq).getByTestId('live-price')).toHaveTextContent('29,189.75')
    expect(within(nsdq).getByTestId('spread-nsdq100')).toHaveTextContent('0.70 USD · 0.24 bps')
    // Recorded flat, provisional flat below the flip level: "goes long above".
    expect(within(nsdq).getByTestId('flip-nsdq100')).toHaveTextContent('goes long above 29,882.02')
    expect(within(nsdq).queryByText('would flip')).not.toBeInTheDocument()
    expect(within(nsdq).getByTestId('forming-nsdq100')).toHaveTextContent('UTC day 16 Oct 2026')
    expect(within(nsdq).getByTestId('forming-nsdq100')).toHaveTextContent('7 quotes')
    expect(within(nsdq).getByTestId('closes-nsdq100')).toHaveTextContent('29,868.70')

    // SPX500 is recorded long and the mid is below its flip level: the rule would flip.
    const spx = screen.getByTestId('market-spx500')
    expect(within(spx).getByTestId('bid-spx500')).toHaveTextContent('7,649.08')
    expect(within(spx).getByTestId('flip-spx500')).toHaveTextContent('goes long above 7,707.38')
    expect(within(spx).getByText('would flip')).toBeInTheDocument()

    // The session comes from the stream's snapshot; the badge is live.
    const clock = screen.getByTestId('session-clock')
    expect(clock).toHaveAttribute('data-open', 'true')
    expect(clock).toHaveTextContent('NYSE open · closes 16:00 NY')
    expect(screen.getByTestId('live-badge')).toHaveTextContent(/^live$/i)
    expect(screen.queryByText('Live eToro quotes unavailable')).not.toBeInTheDocument()

    // A "session" event (after each heartbeat) moves the badge; a "book" event moves the quote.
    es.emit('session', { ...snap.session, open: false, now_new_york: '2026-10-16T16:05:00-04:00' })
    await waitFor(() => expect(screen.getByTestId('session-clock')).toHaveAttribute('data-open', 'false'))
    expect(screen.getByTestId('session-clock')).toHaveTextContent('closed · since 16:00 NY')
    const b = structuredClone(snap.books.find((x) => x.book === 'nsdq100')!)
    b.bid = 29900.1
    b.ask = 29900.9
    b.last = 29900.5
    b.provisional = { ...b.provisional!, price: 29900.5, signal: 'long', distance_to_flip_pct: 0.06 }
    es.emit('book', b)
    await waitFor(() => expect(within(nsdq).getByTestId('bid-nsdq100')).toHaveTextContent('29,900.10'))
    expect(within(nsdq).getByTestId('flip-nsdq100')).toHaveTextContent('flips to flat below 29,882.02')
    expect(within(nsdq).getByText('would flip')).toBeInTheDocument()
  })

  it('a contract mismatch on the eToro snapshot is an error, not a wrong number', async () => {
    renderAt('/market?ledger=etoro-demo')
    const es = await stream()
    const snap = etoroSnapshot() as unknown as { session: unknown }
    snap.session = { open: 'yes' }
    es.emit('snapshot', snap)
    expect(await screen.findByText('Live eToro quotes unavailable')).toBeInTheDocument()
    expect(screen.getByTestId('live-badge')).toHaveTextContent(/live error/i)
  })
})
