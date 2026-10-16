/**
 * MSW handlers serving responses captured from a real ui-api run
 * (src/mocks/fixtures, one directory per ledger). Regenerate with
 * `npm run fixtures` while ui-api is running.
 */
import { http, HttpResponse } from 'msw'
import { controlHandlers, withMockHalt } from './controls'
import { strategyHandlers } from './strategies'
import healthz from './fixtures/healthz.json'
import etoroLive from './fixtures/etoro-demo/etoro-live.json'
import ledgers from './fixtures/ledgers.json'
import live from './fixtures/live.json'
import liveMarket from './fixtures/live-market.json'
import runs from './research/runs.json'
import studies from './research/studies.json'

// research/study-<name>.json, one per study in research/studies.json
const studyFiles = import.meta.glob<Record<string, unknown>>('./research/study-*.json', {
  eager: true,
  import: 'default',
})
// research/run-<date>--<name>.json, one per run in research/runs.json
const runFiles = import.meta.glob<Record<string, unknown>>('./research/run-*.json', {
  eager: true,
  import: 'default',
})

// fixtures/prereg-<book>.json: the latest verdict report, not ledger-specific
const preregFiles = import.meta.glob<Record<string, unknown>>('./fixtures/prereg-*.json', {
  eager: true,
  import: 'default',
})

type Json = Record<string, unknown>

// fixtures/<ledger>/<name>.json, e.g. fixtures/stage/forward-tests.json
const files = import.meta.glob<Json>('./fixtures/*/*.json', { eager: true, import: 'default' })
const byLedger: Record<string, Record<string, Json>> = {}
for (const [path, data] of Object.entries(files)) {
  const m = /\.\/fixtures\/([^/]+)\/([^/]+)\.json$/.exec(path)
  if (!m) continue
  ;(byLedger[m[1]] ??= {})[m[2]] = data
}
const defaultLedger = ledgers.ledgers.find((l) => l.default)?.name ?? 'stage'

type Venue = 'bitso' | 'etoro'
const venueOf: Record<string, Venue> = Object.fromEntries(ledgers.ledgers.map((l) => [l.name, l.venue as Venue]))
const firstOf = (venue: Venue) => ledgers.ledgers.find((l) => venueOf[l.name] === venue)?.name

/**
 * Mirrors ui-api's pickVenue: no ?ledger= means the default (the first ledger
 * of the route's venue for /etoro/*), an unknown name is a 400, and a ledger
 * on the other venue is a 400 too, so a Bitso page can never show data for
 * the eToro ledger or the other way round. `venue` undefined is a
 * venue-neutral route (controls).
 */
export function fixture(request: Request, name: string, venue?: Venue): Response {
  const want = new URL(request.url).searchParams.get('ledger')
  const ledger = want || (venue ? firstOf(venue) : undefined) || defaultLedger
  const files = byLedger[ledger]
  if (!files || !(ledger in venueOf)) return HttpResponse.json({ error: `unknown ledger ${ledger}` }, { status: 400 })
  if (venue && venueOf[ledger] !== venue) {
    return HttpResponse.json(
      { error: `ledger ${ledger} is on venue ${venueOf[ledger]}; this route serves ${venue} ledgers` },
      { status: 400 },
    )
  }
  const f = files[name]
  if (!f) return HttpResponse.json({ error: `no fixture ${ledger}/${name}` }, { status: 404 })
  return HttpResponse.json(f)
}

const bitso = (request: Request, name: string) => fixture(request, name, 'bitso')
const etoro = (request: Request, name: string) => fixture(request, name, 'etoro')

/** The captured candles trimmed to ?days=, like ui-api's buildCandles. */
async function candlesFor(res: Response, request: Request) {
  if (!res.ok) return res
  const c = (await res.json()) as { candles: unknown[] }
  const days = Number(new URL(request.url).searchParams.get('days') ?? 180)
  return HttpResponse.json({ ...c, candles: c.candles.slice(-days) })
}

const TIME_KEYS = new Set([
  'generated_at',
  'since',
  'last_message_at',
  'last_at',
  'updated_at',
  'time',
  'at',
  'depth_at',
])

/**
 * The captured live snapshot for `?books=`, with its timestamps moved to now
 * so it reads as fresh. `?market=1` serves the capture that also has depth,
 * spread and the tape (live-market.json), like ui-api. With `src` it is
 * another capture (the eToro one: its `session` is served as captured, so
 * the NYSE badge in the mock is deterministic).
 */
function liveNow(request: Request, src?: Json) {
  const params = new URL(request.url).searchParams
  const want = (params.get('books') ?? '').split(',').filter(Boolean)
  const market = ['1', 'true'].includes(params.get('market') ?? '')
  src ??= market ? liveMarket : live
  const shift = Date.now() - Date.parse(String(src.generated_at))
  const move = (v: unknown): unknown => {
    if (Array.isArray(v)) return v.map(move)
    if (v && typeof v === 'object') {
      return Object.fromEntries(
        Object.entries(v).map(([k, x]) => [
          k,
          TIME_KEYS.has(k) && typeof x === 'string' && x ? new Date(Date.parse(x) + shift).toISOString() : move(x),
        ]),
      )
    }
    return v
  }
  const snap = move(src) as typeof liveMarket & { session?: unknown }
  const keep = (b: { book: string }) => want.length === 0 || want.includes(b.book)
  return {
    ...snap,
    books: snap.books.filter(keep),
    ...(market && snap.markets ? { markets: snap.markets.filter(keep) } : { markets: undefined }),
  }
}

/**
 * Replays the snapshot as Server-Sent Events, then a heartbeat every 10 s,
 * like ui-api's /stream. The eToro stream (`src`) follows each heartbeat
 * with a "session" event, like /etoro/stream.
 */
function liveStream(request: Request, src?: Json): Response {
  const enc = new TextEncoder()
  let timer: ReturnType<typeof setInterval> | undefined
  const body = new ReadableStream<Uint8Array>({
    start(ctl) {
      const send = (name: string, data: unknown) =>
        ctl.enqueue(enc.encode(`event: ${name}\ndata: ${JSON.stringify(data)}\n\n`))
      ctl.enqueue(enc.encode('retry: 3000\n\n'))
      send('snapshot', liveNow(request, src))
      timer = setInterval(() => {
        const s = liveNow(request, src)
        send('heartbeat', { time: s.generated_at, upstream: s.upstream })
        if (s.session) send('session', s.session)
      }, 10_000)
      request.signal.addEventListener('abort', () => clearInterval(timer))
    },
    cancel() {
      clearInterval(timer)
    },
  })
  return new HttpResponse(body, { headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store' } })
}

export const handlers = [
  http.get('/api/ui/healthz', () => HttpResponse.json(healthz)),
  http.get('/api/ui/ledgers', () => HttpResponse.json(ledgers)),
  http.get('/api/ui/live', ({ request }) => HttpResponse.json(liveNow(request))),
  http.get('/api/ui/stream', ({ request }) => liveStream(request)),
  // The eToro rates feed (P5c): the capture replayed as the snapshot, then heartbeats with the session.
  http.get('/api/ui/etoro/live', ({ request }) => HttpResponse.json(liveNow(request, etoroLive))),
  http.get('/api/ui/etoro/stream', ({ request }) => liveStream(request, etoroLive)),
  http.get('/api/ui/research/studies', () => HttpResponse.json(studies)),
  http.get('/api/ui/research/studies/:name', ({ params }) => {
    const name = String(params.name)
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,150}$/.test(name) || name.includes('..')) {
      return HttpResponse.json({ error: 'invalid study name' }, { status: 400 })
    }
    const doc = studyFiles[`./research/study-${name}.json`]
    return doc ? HttpResponse.json(doc) : HttpResponse.json({ error: `no study ${name}` }, { status: 404 })
  }),
  http.get('/api/ui/research/runs', () => HttpResponse.json(runs)),
  http.get('/api/ui/research/runs/:date/:name', ({ params }) => {
    const date = String(params.date)
    const name = String(params.name)
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,150}$/.test(name) || name.includes('..')) {
      return HttpResponse.json({ error: 'invalid run id' }, { status: 400 })
    }
    const doc = runFiles[`./research/run-${date}--${name}.json`]
    return doc ? HttpResponse.json(doc) : HttpResponse.json({ error: `no run ${date}/${name}` }, { status: 404 })
  }),
  http.get('/api/ui/forward-tests', ({ request }) => bitso(request, 'forward-tests')),
  // GET /strategies, POST /strategies/:name/start|stop and POST /risk/halt-all.
  // Before controlHandlers, whose /risk/:action would otherwise take halt-all.
  ...strategyHandlers,
  // GET /controls, GET /risk (with any mock halt) and POST /risk/halt|resume.
  ...controlHandlers(fixture, defaultLedger),
  http.get('/api/ui/health/data', ({ request }) => bitso(request, 'health-data')),
  http.get('/api/ui/forward-tests/:book/ledger', ({ params, request }) =>
    bitso(request, `ledger-${String(params.book)}`),
  ),
  http.get('/api/ui/forward-tests/:book/performance', ({ params, request }) =>
    bitso(request, `performance-${String(params.book)}`),
  ),
  http.get('/api/ui/forward-tests/:book/capacity', ({ params, request }) =>
    bitso(request, `capacity-${String(params.book)}`),
  ),
  // Not ledger-specific: the report comes from public data (plan §6.4.14).
  http.get('/api/ui/forward-tests/:book/prereg', ({ params }) => {
    const doc = preregFiles[`./fixtures/prereg-${String(params.book)}.json`]
    return doc ? HttpResponse.json(doc) : HttpResponse.json({ error: 'no fixture' }, { status: 404 })
  }),
  // One capture at the defaults; the cost / block / paths query is ignored.
  http.get('/api/ui/forward-tests/:book/montecarlo', ({ params, request }) =>
    bitso(request, `montecarlo-${String(params.book)}`),
  ),
  http.get('/api/ui/forward-tests/:book/candles', ({ params, request }) =>
    candlesFor(bitso(request, `candles-${String(params.book)}`), request),
  ),

  // The eToro venue (/api/ui/etoro/*, P5a): fixtures/etoro-demo/etoro-*.json.
  http.get('/api/ui/etoro/forward-tests', ({ request }) => etoro(request, 'etoro-forward-tests')),
  http.get('/api/ui/etoro/forward-tests/:book/ledger', ({ params, request }) =>
    etoro(request, `etoro-ledger-${String(params.book)}`),
  ),
  http.get('/api/ui/etoro/forward-tests/:book/candles', ({ params, request }) =>
    candlesFor(etoro(request, `etoro-candles-${String(params.book)}`), request),
  ),
  // The Risk page's halt from the controls mock shows here too (controls.ts).
  http.get('/api/ui/etoro/risk', ({ request }) => withMockHalt(etoro(request, 'etoro-risk'), request, defaultLedger)),
  http.get('/api/ui/etoro/health', ({ request }) => etoro(request, 'etoro-health')),
  http.get('/api/ui/etoro/reconcile', ({ request }) => etoro(request, 'etoro-reconcile')),
  http.get('/api/ui/etoro/risk-report', ({ request }) => etoro(request, 'etoro-risk-report')),
]
