/**
 * Live market data from ui-api's Server-Sent Events stream (GET /api/ui/stream).
 *
 * Display only: the executor decides on closed production candles, never on
 * anything here. Every event is parsed with the same zod contract as the REST
 * responses; a mismatch shows as an error instead of a wrong number.
 */
import { useEffect, useReducer, useState } from 'react'
import type { z } from 'zod'
import {
  bookSnapshotSchema,
  heartbeatSchema,
  liveSnapshotSchema,
  liveStatusSchema,
  type BookSnapshot,
  type LiveStatus,
} from './schemas'

/** No event at all (ui-api sends a heartbeat every 15 s) for this long means the stream is stale. */
export const STALE_MS = 25_000
/** After the stream fails for good (e.g. 503 when ui-api runs with -live=false), try again this often. */
export const REOPEN_MS = 15_000

export type LivePhase = 'connecting' | 'live' | 'stale' | 'reconnecting' | 'off' | 'error'

export interface LiveState {
  /** Derived for display: stale and upstream-reconnecting are folded in. */
  phase: LivePhase
  upstream: LiveStatus | null
  books: Record<string, BookSnapshot>
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
  | { type: 'snapshot'; upstream: LiveStatus; books: BookSnapshot[]; at: number }
  | { type: 'book'; book: BookSnapshot; at: number }
  | { type: 'status'; upstream: LiveStatus; at: number }
  | { type: 'conn'; conn: LiveRaw['conn']; error?: string }
  | { type: 'contract'; error: string }

const initial: LiveRaw = { conn: 'connecting', upstream: null, books: {}, lastEventAt: null, error: null }

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
        lastEventAt: a.at,
      }
    case 'book':
      return { ...s, books: { ...s.books, [a.book.book]: a.book }, lastEventAt: a.at }
    case 'status':
      return { ...s, upstream: a.upstream, lastEventAt: a.at }
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
 */
export function useLiveStream(books: string[]): LiveState {
  const key = [...books].sort().join(',')
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
      es = new EventSource(`/api/ui/stream?books=${encodeURIComponent(key)}`)
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
      on('snapshot', liveSnapshotSchema, (s) =>
        dispatch({ type: 'snapshot', upstream: s.upstream, books: s.books, at: Date.now() }),
      )
      on('book', bookSnapshotSchema, (b) => dispatch({ type: 'book', book: b, at: Date.now() }))
      on('status', liveStatusSchema, (u) => dispatch({ type: 'status', upstream: u, at: Date.now() }))
      on('heartbeat', heartbeatSchema, (h) => dispatch({ type: 'status', upstream: h.upstream, at: Date.now() }))
    }
    open()
    return () => {
      clearTimeout(reopen)
      es?.close()
    }
  }, [key])

  return {
    phase: livePhase(raw, now),
    upstream: raw.upstream,
    books: raw.books,
    lastEventAt: raw.lastEventAt,
    error: raw.error,
    now,
  }
}
