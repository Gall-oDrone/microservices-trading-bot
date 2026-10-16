/**
 * Mock-mode assisted trades (P5d). POST /api/ui/etoro/trades/prepare and
 * /place mirror ui-api's trades.go on in-memory state: tokens with a real
 * expiry (120 s; 3 s with `?mock=short-ttl`), single use, the halt from the
 * controls mock (a halted ledger blocks a prepare and refuses a place with
 * 422), the policy's one-leg-per-day and the ledger-vs-account eligibility,
 * and two staged scenarios for the tests: `?mock=short-cash` and
 * `?mock=market-closed`. `?mock=trading-off` reports the trades as
 * disabled in GET /controls. A placed trade moves the mock position, so the
 * forward-tests view (withMockTrades) and the next proposal follow it.
 *
 * Like the controls mock, any non-empty bearer token is accepted. A page
 * reload (or resetMockTrades in tests) forgets everything.
 */
import { http, HttpResponse } from 'msw'
import { ledgerState, nextMockId, type Entry } from './controls'
import etoroLive from './fixtures/etoro-demo/etoro-live.json'
import forwardTests from './fixtures/etoro-demo/etoro-forward-tests.json'

type Json = Record<string, unknown>

interface Position {
  state: 'long' | 'flat'
  position_id?: string
  units?: number
  amount?: number
  open_rate?: number
  opened_at?: string
}

interface Claims {
  id: string
  ledger: string
  client_ref: string
  request_id: string
  book: string
  side: 'open' | 'close'
  amount_usd: number
  position_id: string
  units: number
  bid: number
  ask: number
  ref_price: number
  confirmation: string
  exp: number
}

interface LastOrder {
  kind: 'open' | 'close'
  client_ref: string
  request_id: string
  order_id: string
  status: string
  position_ids: string[]
  avg_price: number
  units: number
  amount: number
  intent_at: string
  started: string
  finished: string
  fill_vs_mid_bps: number
  fill_vs_close_bps: number
  bar_date: string
  fill_date: string
  source: 'ui'
  by: string
}

const BOOKS: Record<string, { symbol: string; id: number }> = {
  nsdq100: { symbol: 'NSDQ100', id: 28 },
  spx500: { symbol: 'SPX500', id: 27 },
}
const TTL_S = 120
const MIN = 1000
const MAX = 1500
const DEFAULT_AMOUNT = 1100

/** The mock account: the fixture's 10-16 numbers. */
const account = { currency: 'USD', cash: 97793.95, equity: 99949.9 }

const state = {
  positions: new Map<string, Position>(),
  legs: new Map<string, number>(), // book -> legs today (the fixture's 10-16 close of NSDQ100 counts as 1)
  tokens: new Map<string, Claims>(),
  used: new Set<string>(),
  lastOrder: new Map<string, LastOrder>(),
  nextPos: 3616700000,
}

function seed() {
  state.positions.clear()
  state.legs.clear()
  for (const b of forwardTests.books) {
    state.positions.set(b.book, (b.demo_position as Position | null) ?? { state: 'flat' })
    state.legs.set(b.book, b.last_order && b.last_order.fill_date === forwardTests.session.date ? 1 : 0)
  }
}
seed()

/** Forget every mock trade (tests call this between cases). */
export function resetMockTrades() {
  state.tokens.clear()
  state.used.clear()
  state.lastOrder.clear()
  seed()
}

function flagOf(request: Request): string {
  return new URL(request.url).searchParams.get('mock') ?? ''
}

function quoteOf(book: string): { bid: number; ask: number; at: string; spread_bps: number } {
  const b = etoroLive.books.find((x) => x.book === book)
  const bid = b?.bid ?? 0
  const ask = b?.ask ?? 0
  const mid = (bid + ask) / 2
  return { bid, ask, at: new Date().toISOString(), spread_bps: mid > 0 ? ((ask - bid) / mid) * 1e4 : 0 }
}

function refClose(book: string): { close: number; signal: string } {
  const b = forwardTests.books.find((x) => x.book === book)
  return { close: b?.decision.close ?? 0, signal: b?.decision.signal ?? '' }
}

export function exposure(): number {
  let t = 0
  for (const p of state.positions.values()) if (p.state === 'long') t += p.amount ?? 0
  return t
}

function hex6() {
  return Math.floor(Math.random() * 0xffffff)
    .toString(16)
    .padStart(6, '0')
}

/** A deterministic-looking request id (the real one is UUIDv5 of the ref). */
function requestIdFor(ref: string): string {
  let h = 0
  for (const c of ref) h = (h * 31 + c.charCodeAt(0)) >>> 0
  const x = h.toString(16).padStart(8, '0')
  return `${x}-${x.slice(0, 4)}-5${x.slice(1, 4)}-a${x.slice(2, 5)}-${x}${x.slice(0, 4)}`
}

function nyDate(now = new Date()): string {
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'America/New_York',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(now)
}

/** The trading block GET /controls reports in mock mode (enabled unless ?mock=trading-off). */
export function mockTradingInfo(request: Request) {
  const off = flagOf(request) === 'trading-off'
  return {
    enabled: !off,
    ...(off
      ? {
          disabled_reason:
            'assisted trading is off: start ui-api with -etoro-trading (UI_API_ETORO_TRADING=1), -operator-token-file and -etoro-env-file naming a 0600 file with ETORO_ENV=demo',
        }
      : {}),
    token_ttl_s: TTL_S,
    min_amount_usd: MIN,
    max_amount_usd: MAX,
    default_amount_usd: DEFAULT_AMOUNT,
  }
}

/**
 * The forward-tests fixture with the mock positions and last orders merged
 * in, the way ui-api would read the ledger after a console trade.
 */
export async function withMockTrades(res: Response): Promise<Response> {
  if (!res.ok || state.lastOrder.size === 0) return res
  const ft = (await res.json()) as Json & { books: (Json & { book: string })[] }
  return HttpResponse.json({
    ...ft,
    books: ft.books.map((b) => {
      const o = state.lastOrder.get(b.book)
      if (!o) return b
      const pos = state.positions.get(b.book) ?? { state: 'flat' }
      return { ...b, demo_position: pos, demo_action: o.kind, last_order: o }
    }),
  })
}

function auth(request: Request): Response | null {
  const a = request.headers.get('Authorization') ?? ''
  if (!/^Bearer \S+$/.test(a)) return HttpResponse.json({ error: 'missing or wrong operator token' }, { status: 401 })
  if (!(request.headers.get('Content-Type') ?? '').startsWith('application/json')) {
    return HttpResponse.json({ error: 'send Content-Type: application/json' }, { status: 415 })
  }
  return null
}

const BY = /^[A-Za-z0-9][A-Za-z0-9 ._@-]{0,63}$/

export const tradeHandlers = [
  http.post('/api/ui/etoro/trades/prepare', async ({ request }) => {
    const ledger = new URL(request.url).searchParams.get('ledger') || 'etoro-demo'
    const flag = flagOf(request)
    if (flag === 'trading-off')
      return HttpResponse.json({ error: mockTradingInfo(request).disabled_reason }, { status: 403 })
    const denied = auth(request)
    const s = ledgerState(ledger)
    const now = new Date()
    const log = (e: Omit<Entry, 'id' | 'at' | 'action' | 'ledger'>) => {
      const entry: Entry = {
        id: nextMockId(),
        at: now.toISOString(),
        action: 'trade.prepare',
        ledger,
        user_agent: 'msw',
        ...e,
      }
      s.audit.unshift(entry)
      return entry
    }
    if (denied) {
      log({ outcome: 'denied', error: 'missing or wrong operator token' })
      return denied
    }
    let body: Json
    try {
      body = (await request.json()) as Json
    } catch {
      return HttpResponse.json({ error: 'body: invalid JSON' }, { status: 400 })
    }
    const unknown = Object.keys(body).find(
      (k) => !['book', 'side', 'amount_usd', 'position_id', 'leverage'].includes(k),
    )
    if (unknown) return HttpResponse.json({ error: `body: json: unknown field "${unknown}"` }, { status: 400 })
    const book = String(body.book ?? '')
    const side = body.side === 'close' ? 'close' : body.side === 'open' ? 'open' : null
    const inst = BOOKS[book]
    if (!inst || !side || body.leverage !== 1) {
      log({ outcome: 'refused', error: 'book, side or leverage invalid' })
      return HttpResponse.json({ error: 'book, side ("open" | "close") and leverage 1 are required' }, { status: 400 })
    }
    const amount = side === 'open' ? Number(body.amount_usd) : 0
    const positionId = side === 'close' ? String(body.position_id ?? '') : ''
    if (side === 'open' && (!(amount >= MIN) || !(amount <= MAX))) {
      log({ outcome: 'refused', error: 'amount_usd out of bounds' })
      return HttpResponse.json({ error: `amount_usd: must be between ${MIN} and ${MAX} USD` }, { status: 400 })
    }
    if (side === 'close' && !/^\d{1,32}$/.test(positionId)) {
      log({ outcome: 'refused', error: 'position_id missing' })
      return HttpResponse.json({ error: 'position_id: the numeric id of the ledger’s position' }, { status: 400 })
    }

    const pos = state.positions.get(book) ?? { state: 'flat' as const }
    const q = quoteOf(book)
    const { close, signal } = refClose(book)
    const units = side === 'open' ? amount / q.ask : (pos.units ?? 0)
    const reasons: string[] = []
    const warnings: string[] = []
    const halt = s.halt?.halted ? s.halt : null
    if (halt) reasons.push(`halted: trading is halted: ${halt.reason} (halt file, by ${halt.by} at ${halt.at})`)
    const marketClosed = flag === 'market-closed'
    if (marketClosed) reasons.push(`market_closed: the market for ${book} is closed`)
    const legs = state.legs.get(book) ?? 0
    if (side === 'open' && legs >= 1) reasons.push(`max_orders_per_day: would be order ${legs + 1} today, max 1`)
    const exp = exposure() + (side === 'open' ? amount : 0)
    if (side === 'open' && exp / account.equity > 0.05) {
      reasons.push(
        `max_exposure_pct_equity: exposure after the order ${exp.toFixed(2)} is ${((exp / account.equity) * 100).toFixed(1)}% of equity ${account.equity.toFixed(2)} (max 5.0%)`,
      )
    }
    const dev = close > 0 ? Math.abs((side === 'open' ? q.ask : q.bid) / close - 1) * 1e4 : 0
    if (dev > 1000)
      reasons.push(`max_price_deviation_bps: price is ${dev.toFixed(0)} bps from reference ${close} (max 1000)`)
    if (side === 'open') {
      if (pos.state === 'long')
        reasons.push(
          `the ledger already holds position ${pos.position_id} on ${inst.symbol} (${(pos.units ?? 0).toFixed(6)} units): close it before opening again`,
        )
    } else if (pos.state !== 'long') {
      reasons.push(`the ledger holds no position on ${inst.symbol} to close`)
    } else if (positionId !== pos.position_id) {
      reasons.push(`position ${positionId} is not the ledger's position on ${inst.symbol} (${pos.position_id})`)
    }
    const cash = flag === 'short-cash' ? 500 : account.cash
    if (side === 'open' && cash < amount)
      reasons.push(`available cash ${cash.toFixed(2)} USD is below the ${amount.toFixed(0)} USD amount`)
    if (side === 'open' && signal === 'flat')
      warnings.push(
        'the rule says flat: the executor closes this position at its next 09:35 New York run unless the ledger is halted',
      )
    if (side === 'close' && signal === 'long')
      warnings.push(
        'the rule says long: the executor re-opens at its next 09:35 New York run unless the ledger is halted',
      )

    const ref = `ui-${book}-${nyDate(now)}-${side}-${hex6()}`
    const requestId = requestIdFor(ref)
    const confirmation = `${side === 'open' ? 'PLACE' : 'CLOSE'} ${inst.symbol}`
    const allow = reasons.length === 0
    const ttl = flag === 'short-ttl' ? 3 : TTL_S
    const preview: Json = {
      verdict: allow ? 'allow' : 'block',
      reasons,
      warnings,
      ledger,
      book,
      side,
      instrument: inst,
      position: pos,
      rule_signal: signal,
      quote: q,
      costs:
        side === 'open' ? { spread: 0.04, markup: 0, transaction_fee: 0, overnight_fee: 0.26, weekend_fee: 0 } : null,
      account: { ...account, cash, exposure: exposure() },
      risk: {
        policy_version: 'etoro-demo-2026-10-10',
        order: {
          book,
          side: side === 'open' ? 'buy' : 'sell',
          qty_btc: units,
          price: side === 'open' ? q.ask : q.bid,
          ref_price: close,
          leverage: 1,
        },
        state: {
          position_btc: side === 'close' ? units : 0,
          orders_today: legs,
          equity: account.equity,
          exposure: exposure(),
          market_closed: marketClosed,
        },
        allowed: !reasons.some((r) => /^(halted|market_closed|max_)/.test(r)),
        findings: [],
        ...(halt ? { halt } : {}),
      },
      units,
      amount_usd: amount,
      ...(positionId ? { position_id: positionId } : {}),
      ref_price: close,
      confirmation,
      client_ref: ref,
      request_id: requestId,
      prepared_at: now.toISOString(),
    }
    if (allow) {
      const id = nextMockId()
      const c: Claims = {
        id,
        ledger,
        client_ref: ref,
        request_id: requestId,
        book,
        side,
        amount_usd: amount,
        position_id: positionId,
        units,
        bid: q.bid,
        ask: q.ask,
        ref_price: close,
        confirmation,
        exp: now.getTime() + ttl * 1000,
      }
      state.tokens.set(id, c)
      preview.token = `mock.${id}`
      preview.expires_at = new Date(c.exp).toISOString()
      preview.expires_in_s = ttl
    }
    const detail =
      side === 'open'
        ? `open ${book} ${amount.toFixed(0)} USD x1 (${units.toFixed(6)} units)`
        : `close ${book} position ${positionId} (${units.toFixed(6)} units)`
    log(
      allow
        ? { outcome: 'done', detail: `${detail} -> allow` }
        : { outcome: 'refused', detail, error: `blocked: ${reasons.join(' | ')}` },
    )
    return HttpResponse.json(preview)
  }),

  http.post('/api/ui/etoro/trades/place', async ({ request }) => {
    const ledger = new URL(request.url).searchParams.get('ledger') || 'etoro-demo'
    const flag = flagOf(request)
    if (flag === 'trading-off')
      return HttpResponse.json({ error: mockTradingInfo(request).disabled_reason }, { status: 403 })
    const denied = auth(request)
    const s = ledgerState(ledger)
    const now = new Date()
    const log = (e: Omit<Entry, 'id' | 'at' | 'action' | 'ledger'>) => {
      const entry: Entry = {
        id: nextMockId(),
        at: now.toISOString(),
        action: 'trade.place',
        ledger,
        user_agent: 'msw',
        ...e,
      }
      s.audit.unshift(entry)
      return entry
    }
    const fail = (status: number, outcome: Entry['outcome'], error: string, extra: Partial<Entry> = {}) => {
      log({ outcome, error, ...extra })
      return HttpResponse.json({ error }, { status })
    }
    if (denied) {
      log({ outcome: 'denied', error: 'missing or wrong operator token' })
      return denied
    }
    let body: Json
    try {
      body = (await request.json()) as Json
    } catch {
      return fail(400, 'refused', 'body: invalid JSON')
    }
    const unknown = Object.keys(body).find((k) => !['token', 'confirm', 'reason', 'by'].includes(k))
    if (unknown) return fail(400, 'refused', `body: json: unknown field "${unknown}"`)
    const id = String(body.token ?? '').replace(/^mock\./, '')
    const c = state.tokens.get(id)
    if (!c || !String(body.token ?? '').startsWith('mock.')) {
      return fail(401, 'denied', 'trade token invalid or issued for another operator session: prepare again')
    }
    const who = { by: String(body.by ?? '').trim(), reason: String(body.reason ?? '').trim(), ref: c.client_ref }
    if (now.getTime() >= c.exp)
      return fail(410, 'refused', `trade token expired at ${new Date(c.exp).toISOString()}: prepare again`, who)
    if (who.reason.length < 8 || who.reason.length > 500 || /[\r\n]/.test(who.reason))
      return fail(400, 'refused', 'reason: at least 8 characters, one line', who)
    if (!BY.test(who.by)) return fail(400, 'refused', 'by: 1-64 letters, digits, spaces or . _ @ -', who)
    if (body.confirm !== c.confirmation)
      return fail(400, 'refused', `confirm: type the confirmation phrase "${c.confirmation}" to confirm`, who)
    if (state.used.has(id)) return fail(409, 'refused', 'trade token already used: prepare again', who)
    state.used.add(id)
    if (s.halt?.halted) {
      return fail(
        422,
        'refused',
        `trading is halted: ${s.halt.reason} (halt file, by ${s.halt.by} at ${s.halt.at}); nothing sent`,
        who,
      )
    }
    if (flag === 'market-closed') return fail(422, 'refused', 'the NYSE cash session is closed; nothing sent', who)
    const pos = state.positions.get(c.book) ?? { state: 'flat' as const }
    if (c.side === 'open' && pos.state !== 'flat')
      return fail(
        422,
        'refused',
        `the ledger changed since the preview: it holds position ${pos.position_id}; nothing sent`,
        who,
      )
    if (c.side === 'close' && (pos.state !== 'long' || pos.position_id !== c.position_id)) {
      return fail(
        422,
        'refused',
        `the ledger changed since the preview: it no longer holds position ${c.position_id}; nothing sent`,
        who,
      )
    }
    const detail =
      c.side === 'open'
        ? `open ${c.book} ${c.amount_usd.toFixed(0)} USD x1 (${c.units.toFixed(6)} units)`
        : `close ${c.book} position ${c.position_id} (${c.units.toFixed(6)} units)`
    const req1 = log({ outcome: 'requested', ...who, detail })
    if (flag === 'broker-down') {
      const err = `${c.side} ${c.client_ref}: broker: order outcome unknown: context deadline exceeded (the intent ${c.client_ref} stays on disk; etoro-reconcile and the data-health page report it until it is resolved by hand)`
      const e = log({ outcome: 'failed', ...who, detail, error: err })
      e.id = req1.id
      return HttpResponse.json(
        {
          status: 'unknown',
          book: c.book,
          side: c.side,
          client_ref: c.client_ref,
          request_id: c.request_id,
          position_ids: [],
          avg_price: 0,
          units: 0,
          amount_usd: 0,
          fill_vs_mid_bps: 0,
          fill_vs_close_bps: 0,
          position_after: pos,
          ledger_recorded: false,
          audit_path: '../strategy-executor/etoro-daily-data/demo/ui-audit.jsonl',
          audit: e,
          error: err,
        },
        { status: 502 },
      )
    }
    const mid = (c.bid + c.ask) / 2
    const finished = new Date(now.getTime() + 2400)
    let after: Position
    let avg: number
    let positionIds: string[]
    let units: number
    let amount: number
    if (c.side === 'open') {
      state.nextPos++
      const pid = String(state.nextPos)
      avg = c.ask + 0.5
      units = c.amount_usd / avg
      amount = c.amount_usd
      positionIds = [pid]
      after = { state: 'long', position_id: pid, units, amount, open_rate: avg, opened_at: finished.toISOString() }
    } else {
      avg = c.bid - 0.2
      units = pos.units ?? c.units
      amount = pos.amount ?? 0
      positionIds = [c.position_id]
      after = { state: 'flat' }
    }
    const fillVsMid = c.side === 'open' ? ((avg - mid) / mid) * 1e4 : ((mid - avg) / mid) * 1e4
    const fillVsClose =
      c.ref_price > 0
        ? c.side === 'open'
          ? ((avg - c.ref_price) / c.ref_price) * 1e4
          : ((c.ref_price - avg) / c.ref_price) * 1e4
        : 0
    state.positions.set(c.book, after)
    state.legs.set(c.book, (state.legs.get(c.book) ?? 0) + 1)
    const orderId = `o${state.nextPos + 100}`
    const today = nyDate(now)
    state.lastOrder.set(c.book, {
      kind: c.side,
      client_ref: c.client_ref,
      request_id: c.request_id,
      order_id: orderId,
      status: 'executed',
      position_ids: positionIds,
      avg_price: avg,
      units,
      amount,
      intent_at: now.toISOString(),
      started: now.toISOString(),
      finished: finished.toISOString(),
      fill_vs_mid_bps: fillVsMid,
      fill_vs_close_bps: fillVsClose,
      bar_date: today,
      fill_date: today,
      source: 'ui',
      by: who.by,
    })
    const done = log({
      outcome: 'done',
      ...who,
      detail: `${detail} -> executed: order ${orderId} positions [${positionIds.join(' ')}] avg ${avg.toFixed(2)} units ${units.toFixed(6)} (vs mid ${fillVsMid >= 0 ? '+' : ''}${fillVsMid.toFixed(1)} bps, vs close ${fillVsClose >= 0 ? '+' : ''}${fillVsClose.toFixed(1)} bps)`,
    })
    done.id = req1.id
    return HttpResponse.json({
      status: 'executed',
      book: c.book,
      side: c.side,
      client_ref: c.client_ref,
      request_id: c.request_id,
      order_id: orderId,
      position_ids: positionIds,
      avg_price: avg,
      units,
      amount_usd: amount,
      fill_vs_mid_bps: fillVsMid,
      fill_vs_close_bps: fillVsClose,
      position_after: after,
      ledger_recorded: true,
      audit_path: '../strategy-executor/etoro-daily-data/demo/ui-audit.jsonl',
      audit: done,
    })
  }),
]
