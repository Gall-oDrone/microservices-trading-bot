/**
 * MSW handlers serving responses captured from a real ui-api run
 * (src/mocks/fixtures, one directory per ledger). Regenerate with
 * `npm run fixtures` while ui-api is running.
 */
import { http, HttpResponse } from 'msw'
import { controlHandlers } from './controls'
import healthz from './fixtures/healthz.json'
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

/** Mirrors ui-api: no ?ledger= means the default; an unknown name is a 400. */
function fixture(request: Request, name: string): Response {
  const ledger = new URL(request.url).searchParams.get('ledger') || defaultLedger
  const files = byLedger[ledger]
  if (!files) return HttpResponse.json({ error: `unknown ledger ${ledger}` }, { status: 400 })
  const f = files[name]
  if (!f) return HttpResponse.json({ error: `no fixture ${ledger}/${name}` }, { status: 404 })
  return HttpResponse.json(f)
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
 * spread and the tape (live-market.json), like ui-api.
 */
function liveNow(request: Request) {
  const params = new URL(request.url).searchParams
  const want = (params.get('books') ?? '').split(',').filter(Boolean)
  const market = ['1', 'true'].includes(params.get('market') ?? '')
  const src: Json = market ? liveMarket : live
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
  const snap = move(src) as typeof liveMarket
  const keep = (b: { book: string }) => want.length === 0 || want.includes(b.book)
  return {
    ...snap,
    books: snap.books.filter(keep),
    ...(market ? { markets: snap.markets.filter(keep) } : { markets: undefined }),
  }
}

/** Replays the snapshot as Server-Sent Events, then a heartbeat every 10 s, like ui-api's /stream. */
function liveStream(request: Request): Response {
  const enc = new TextEncoder()
  let timer: ReturnType<typeof setInterval> | undefined
  const body = new ReadableStream<Uint8Array>({
    start(ctl) {
      const send = (name: string, data: unknown) =>
        ctl.enqueue(enc.encode(`event: ${name}\ndata: ${JSON.stringify(data)}\n\n`))
      ctl.enqueue(enc.encode('retry: 3000\n\n'))
      send('snapshot', liveNow(request))
      timer = setInterval(() => {
        const s = liveNow(request)
        send('heartbeat', { time: s.generated_at, upstream: s.upstream })
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
  http.get('/api/ui/forward-tests', ({ request }) => fixture(request, 'forward-tests')),
  // GET /controls, GET /risk (with any mock halt) and POST /risk/halt|resume.
  ...controlHandlers(fixture, defaultLedger),
  http.get('/api/ui/health/data', ({ request }) => fixture(request, 'health-data')),
  http.get('/api/ui/forward-tests/:book/ledger', ({ params, request }) =>
    fixture(request, `ledger-${String(params.book)}`),
  ),
  http.get('/api/ui/forward-tests/:book/candles', async ({ params, request }) => {
    const res = fixture(request, `candles-${String(params.book)}`)
    if (!res.ok) return res
    const c = (await res.json()) as { candles: unknown[] }
    const days = Number(new URL(request.url).searchParams.get('days') ?? 180)
    return HttpResponse.json({ ...c, candles: c.candles.slice(-days) })
  }),
]
