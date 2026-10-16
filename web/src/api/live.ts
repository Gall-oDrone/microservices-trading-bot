/**
 * Live market data from ui-api's Server-Sent Events stream (GET /api/ui/stream,
 * or /api/ui/etoro/stream for the eToro venue).
 *
 * Display only: the executor decides on closed production candles, never on
 * anything here. Every event is parsed with the same zod contract as the REST
 * responses; a mismatch shows as an error instead of a wrong number.
 */
import { useEffect, useReducer, useState } from 'react'
import type { z } from 'zod'
import { etoroLiveSnapshotSchema, etoroSessionEventSchema, type EtoroSession } from './etoroSchemas'
import {
  bookSnapshotSchema,
  heartbeatSchema,
  liveSnapshotSchema,
  liveStatusSchema,
  marketSchema,
  type BookSnapshot,
  type LiveStatus,
  type Market,
} from './schemas'

/** No event at all (ui-api sends a heartbeat every 15 s) for this long means the stream is stale. */
export const STALE_MS = 25_000
/** After the stream fails for good (e.g. 503 when ui-api runs with -live=false), try again this often. */
export const REOPEN_MS = 15_000

export type LivePhase = 'connecting' | 'live' | 'stale' | 'reconnecting' | 'off' | 'error'

/** Which ui-api stream to open: Bitso's /stream (default) or the eToro venue's /etoro/stream. */
export type LiveVenue = 'bitso' | 'etoro'

export const STREAM_PATH: Record<LiveVenue, string> = { bitso: '/api/ui/stream', etoro: '/api/ui/etoro/stream' }

export interface LiveState {
  /** Derived for display: stale and upstream-reconnecting are folded in. */
  phase: LivePhase
  upstream: LiveStatus | null
  books: Record<string, BookSnapshot>
  /** Depth, spread and tape per book; only filled with `{ market: true }` (the Market page). */
  markets: Record<string, Market>
  /** The NYSE session, on the eToro stream only (snapshot and every "session" event); null on Bitso. */
  session: EtoroSession | null
  /** Date.now() of the last event received, or null before the first. */
  lastEventAt: number | null
  /** Contract mismatch or why the stream is off. */
  error: string | null
  /** Ticks every second, for "Ns ago" labels. */
  now: number
}

export type LiveRaw = Omit<LiveState, 'phase' | 'now'> & { conn: 'connecting' | 'open' | 'reconnecting' | 'off' }

type Action =
  | { type: 'reset' }
  | { type: 'open' }
  | {
      type: 'snapshot'
      upstream: LiveStatus
      books: BookSnapshot[]
      markets?: Market[]
      session?: EtoroSession
      at: number
    }
  | { type: 'book'; book: BookSnapshot; at: number }
  | { type: 'market'; market: Market; at: number }
  | { type: 'status'; upstream: LiveStatus; at: number }
  | { type: 'session'; session: EtoroSession; at: number }
  | { type: 'conn'; conn: LiveRaw['conn']; error?: string }
  | { type: 'contract'; error: string }

const initial: LiveRaw = {
  conn: 'connecting',
  upstream: null,
  books: {},
  markets: {},
  session: null,
  lastEventAt: null,
  error: null,
}

function reducer(s: LiveRaw, a: Action): LiveRaw {
  switch (a.type) {
    case 'reset':
      return initial
    case 'open':
      return { ...s, conn: 'open', error: null }
    case 'snapshot':
      return {
        ...s,
        conn: 'open',
        upstream: a.upstream,
        books: Object.fromEntries(a.books.map((b) => [b.book, b])),
        markets: a.markets ? Object.fromEntries(a.markets.map((m) => [m.book, m])) : s.markets,
        session: a.session ?? s.session,
        lastEventAt: a.at,
      }
    case 'book':
      return { ...s, books: { ...s.books, [a.book.book]: a.book }, lastEventAt: a.at }
    case 'market':
      return { ...s, markets: { ...s.markets, [a.market.book]: a.market }, lastEventAt: a.at }
    case 'status':
      return { ...s, upstream: a.upstream, lastEventAt: a.at }
    case 'session':
      return { ...s, session: a.session, lastEventAt: a.at }
    case 'conn':
      return { ...s, conn: a.conn, error: a.error ?? s.error }
    case 'contract':
      return { ...s, error: a.error }
  }
}

/** Pure, for tests: the phase shown to the operator. */
export function livePhase(s: LiveRaw, now: number): LivePhase {
  if (s.error && s.error.startsWith('contract')) return 'error'
  if (s.conn === 'off') return 'off'
  if (s.conn === 'connecting' && s.lastEventAt == null) return 'connecting'
  if (s.conn === 'reconnecting') return 'reconnecting'
  if (s.lastEventAt == null || now - s.lastEventAt > STALE_MS) return 'stale'
  if (s.upstream && !s.upstream.connected) return 'reconnecting'
  return 'live'
}

function parse<S extends z.ZodTypeAny>(ev: Event, name: string, schema: S): z.infer<S> | string {
  let body: unknown
  try {
    body = JSON.parse((ev as MessageEvent<string>).data)
  } catch {
    return `contract: "${name}" event is not JSON`
  }
  const r = schema.safeParse(body)
  if (r.success) return r.data
  const issue = r.error.issues[0]
  return `contract: live "${name}" event does not match the UI contract at "${issue?.path.join('.')}": ${issue?.message}`
}

/**
 * Subscribes to live data for `books` (one EventSource per page). The
 * browser reconnects on its own after a dropped stream (ui-api sends
 * `retry: 3000`); if the server refuses the stream we retry every REOPEN_MS.
 * With `market: true` the stream also carries depth, spread and the tape.
 * With `venue: 'etoro'` it is the eToro rates stream, whose snapshot and
 * "session" events carry the NYSE session.
 */
export function useLiveStream(books: string[], opts: { market?: boolean; venue?: LiveVenue } = {}): LiveState {
  const key = [...books].sort().join(',')
  const market = opts.market === true
  const venue: LiveVenue = opts.venue ?? 'bitso'
  const [raw, dispatch] = useReducer(reducer, initial)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  useEffect(() => {
    dispatch({ type: 'reset' })
    if (!key) return
    if (typeof EventSource === 'undefined') {
      dispatch({ type: 'conn', conn: 'off', error: 'this browser has no EventSource' })
      return
    }
    let es: EventSource | null = null
    let reopen: ReturnType<typeof setTimeout> | undefined
    const on = <S extends z.ZodTypeAny>(name: string, schema: S, f: (v: z.infer<S>) => void) => {
      es!.addEventListener(name, (ev) => {
        const v = parse(ev, name, schema)
        if (typeof v === 'string') dispatch({ type: 'contract', error: v })
        else f(v)
      })
    }
    const open = () => {
      es = new EventSource(`${STREAM_PATH[venue]}?books=${encodeURIComponent(key)}${market ? '&market=1' : ''}`)
      es.onopen = () => dispatch({ type: 'open' })
      es.onerror = () => {
        if (es?.readyState === EventSource.CLOSED) {
          // Not text/event-stream (ui-api down, or running with -live=false).
          dispatch({ type: 'conn', conn: 'off', error: 'live stream unavailable; retrying' })
          es.close()
          reopen = setTimeout(open, REOPEN_MS)
        } else {
          dispatch({ type: 'conn', conn: 'reconnecting' })
        }
      }
      if (venue === 'etoro') {
        on('snapshot', etoroLiveSnapshotSchema, (s) =>
          dispatch({
            type: 'snapshot',
            upstream: s.upstream,
            books: s.books,
            markets: s.markets,
            session: s.session,
            at: Date.now(),
          }),
        )
        on('session', etoroSessionEventSchema, (s) => dispatch({ type: 'session', session: s, at: Date.now() }))
      } else {
        on('snapshot', liveSnapshotSchema, (s) =>
          dispatch({ type: 'snapshot', upstream: s.upstream, books: s.books, markets: s.markets, at: Date.now() }),
        )
      }
      on('book', bookSnapshotSchema, (b) => dispatch({ type: 'book', book: b, at: Date.now() }))
      if (market) on('market', marketSchema, (m) => dispatch({ type: 'market', market: m, at: Date.now() }))
      on('status', liveStatusSchema, (u) => dispatch({ type: 'status', upstream: u, at: Date.now() }))
      on('heartbeat', heartbeatSchema, (h) => dispatch({ type: 'status', upstream: h.upstream, at: Date.now() }))
    }
    open()
    return () => {
      clearTimeout(reopen)
      es?.close()
    }
  }, [key, market, venue])

  return {
    phase: livePhase(raw, now),
    upstream: raw.upstream,
    books: raw.books,
    markets: raw.markets,
    session: raw.session,
    lastEventAt: raw.lastEventAt,
    error: raw.error,
    now,
  }
}
