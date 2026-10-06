/**
 * MSW handlers serving responses captured from a real ui-api run
 * (src/mocks/fixtures, one directory per ledger). Regenerate with
 * `npm run fixtures` while ui-api is running.
 */
import { http, HttpResponse } from 'msw'
import healthz from './fixtures/healthz.json'
import ledgers from './fixtures/ledgers.json'

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

export const handlers = [
  http.get('/api/ui/healthz', () => HttpResponse.json(healthz)),
  http.get('/api/ui/ledgers', () => HttpResponse.json(ledgers)),
  http.get('/api/ui/forward-tests', ({ request }) => fixture(request, 'forward-tests')),
  http.get('/api/ui/risk', ({ request }) => fixture(request, 'risk')),
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
