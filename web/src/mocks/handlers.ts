/**
 * MSW handlers serving responses captured from a real ui-api run against the
 * stage ledger (src/mocks/fixtures). Regenerate with `npm run fixtures`
 * while ui-api is running.
 */
import { http, HttpResponse } from 'msw'
import candlesMxn from './fixtures/candles-btc_mxn.json'
import candlesUsd from './fixtures/candles-btc_usd.json'
import forwardTests from './fixtures/forward-tests.json'
import healthz from './fixtures/healthz.json'
import ledgerMxn from './fixtures/ledger-btc_mxn.json'
import ledgerUsd from './fixtures/ledger-btc_usd.json'
import risk from './fixtures/risk.json'

const ledgers: Record<string, unknown> = { btc_mxn: ledgerMxn, btc_usd: ledgerUsd }
const candles: Record<string, { book: string; file: string; candles: unknown[] }> = {
  btc_mxn: candlesMxn,
  btc_usd: candlesUsd,
}

export const handlers = [
  http.get('/api/ui/healthz', () => HttpResponse.json(healthz)),
  http.get('/api/ui/forward-tests', () => HttpResponse.json(forwardTests)),
  http.get('/api/ui/risk', () => HttpResponse.json(risk)),
  http.get('/api/ui/forward-tests/:book/ledger', ({ params }) => {
    const l = ledgers[String(params.book)]
    return l ? HttpResponse.json(l) : HttpResponse.json({ error: 'unknown book' }, { status: 404 })
  }),
  http.get('/api/ui/forward-tests/:book/candles', ({ params, request }) => {
    const c = candles[String(params.book)]
    if (!c) return HttpResponse.json({ error: 'unknown book' }, { status: 404 })
    const days = Number(new URL(request.url).searchParams.get('days') ?? 180)
    return HttpResponse.json({ ...c, candles: c.candles.slice(-days) })
  }),
]
