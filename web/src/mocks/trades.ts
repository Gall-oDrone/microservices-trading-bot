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
import { isRealCanary, mockFlag } from './flag'
import { ledgerState, nextMockId, type Entry } from './controls'
import etoroLive from './fixtures/etoro-demo/etoro-live.json'
import forwardTests from './fixtures/etoro-demo/etoro-forward-tests.json'
import positionsFixture from './fixtures/etoro-demo/etoro-positions.json'
import type { EtoroTrade, EtoroTradeSummary } from '../api/etoroSchemas'

type Json = Record<string, unknown>

interface Position {
  state: 'long' | 'flat'
  position_id?: string
  units?: number
  amount?: number
  open_rate?: number
  opened_at?: string
  stop_loss_rate?: number
  take_profit_rate?: number
  stop_type?: string
  /** 2026-10-22: the position's leverage (1 when absent). */
  leverage?: number
}

type Side = 'open' | 'close' | 'protect'

interface Claims {
  id: string
  ledger: string
  client_ref: string
  request_id: string
  book: string
  side: Side
  amount_usd: number
  position_id: string
  units: number
  bid: number
  ask: number
  ref_price: number
  confirmation: string
  exp: number
  sl_rate: number
  tp_rate: number
  profile: string
  max_loss_usd: number
  /** 2026-10-22: the leverage of an open and the account the token was issued for. */
  leverage: number
  env: 'demo' | 'real'
}

interface LastOrder {
  kind: Side
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

// --- Protection (phase B): a small copy of shared/pkg/stops for mock mode.

interface Levels {
  profile: string
  sl_rate: number
  sl_pct: number
  tp_rate?: number
  tp_pct?: number
  sl_atr?: number
  tp_atr?: number
  max_loss_usd: number
  target_usd?: number
  rr?: number
  clamps: string[]
  feasible: boolean
  reason?: string
}

const MULT: Record<string, { sl: number; tp: number }> = {
  conservative: { sl: 1.5, tp: 2.25 },
  moderate: { sl: 2, tp: 4 },
  aggressive: { sl: 3, tp: 6 },
}
/** ATR(14) as a fraction of price per book (the fixture's October regime). */
const ATR_PCT: Record<string, number> = { nsdq100: 0.0105, spx500: 0.0092 }
const BOUNDS: Record<string, { min_sl_pct: number; max_sl_pct: number; max_tp_pct: number; tick: number }> = {
  nsdq100: { min_sl_pct: 0.01, max_sl_pct: 1, max_tp_pct: 10, tick: 0.01 },
  spx500: { min_sl_pct: 0, max_sl_pct: 1, max_tp_pct: 10, tick: 0.01 },
}
const LIMITS = { min_sl_pct: 0, max_sl_pct: 0.1, max_loss_pct_equity: 0.005 }

function volatilityOf(book: string, close: number) {
  const atrPct = ATR_PCT[book] ?? 0.01
  return {
    period: 14,
    atr: Math.round(atrPct * close * 100) / 100,
    atr_pct: atrPct,
    close,
    bars: 120,
    regime: 'normal' as const,
    regime_ratio: 1.08,
    median_atr_pct: atrPct / 1.08,
    regime_lookback: 106,
  }
}

type Limits = { min_sl_pct: number; max_sl_pct: number; max_loss_pct_equity: number }
type Bounds = { min_sl_pct: number; max_sl_pct: number; max_tp_pct: number; tick: number }

/** The guard chain of stops.build: floor, caps, venue, rounding, mark. */
function buildLevels(
  book: string,
  profile: string,
  entry: number,
  mark: number,
  units: number,
  spreadPct: number,
  equity: number,
  slPctIn: number,
  tpPctIn: number,
  limits: Limits = LIMITS,
  bounds?: Bounds,
): Levels {
  const b = bounds ?? BOUNDS[book] ?? BOUNDS.spx500
  const atr = (ATR_PCT[book] ?? 0.01) * entry
  const clamps: string[] = []
  let slPct = slPctIn
  let tpPct = tpPctIn
  if (profile !== 'custom') {
    const m = MULT[profile] ?? MULT.moderate
    slPct = (entry - (mark - m.sl * atr)) / entry
    tpPct = (mark + m.tp * atr - entry) / entry
  }
  const floor = Math.max(b.min_sl_pct, 2 * spreadPct + 0.0025, limits.min_sl_pct)
  if (slPct < floor) {
    clamps.push(
      `stop widened from ${(slPct * 100).toFixed(2)}% to the ${(floor * 100).toFixed(2)}% floor (twice the spread + 0.25%, the venue's minimum, or the policy's minimum)`,
    )
    slPct = floor
  }
  let ceiling = limits.max_sl_pct
  let why = `the policy's ${(limits.max_sl_pct * 100).toFixed(1)}% maximum stop distance`
  if (limits.max_loss_pct_equity > 0 && equity > 0 && units > 0) {
    const maxLoss = limits.max_loss_pct_equity * equity
    const byEq = maxLoss / (units * entry)
    if (byEq < ceiling) {
      ceiling = byEq
      why = `the policy's maximum loss of ${(limits.max_loss_pct_equity * 100).toFixed(2)}% of equity (${maxLoss.toFixed(2)})`
    }
  }
  let feasible = true
  let reason: string | undefined
  if (slPct > ceiling) {
    if (ceiling < floor) {
      feasible = false
      reason = `no stop satisfies both the ${(floor * 100).toFixed(2)}% floor and ${why} (${(ceiling * 100).toFixed(2)}%)`
    } else {
      clamps.push(`stop tightened from ${(slPct * 100).toFixed(2)}% to ${(ceiling * 100).toFixed(2)}%: ${why}`)
    }
    slPct = ceiling
  }
  if (tpPct > b.max_tp_pct) {
    clamps.push(
      `target lowered from ${(tpPct * 100).toFixed(2)}% to the venue's ${(b.max_tp_pct * 100).toFixed(0)}% maximum`,
    )
    tpPct = b.max_tp_pct
  }
  const slRate0 = Math.floor((entry * (1 - slPct)) / b.tick + 1e-9) * b.tick
  // At the ceiling the stop rounds up, never past the cap (stops.go, 2026-10-22).
  const slRate =
    (entry - slRate0) / entry > ceiling + 1e-12 ? Math.ceil((entry * (1 - ceiling)) / b.tick - 1e-9) * b.tick : slRate0
  const tpRate = tpPct > 0 ? Math.ceil((entry * (1 + tpPct)) / b.tick - 1e-9) * b.tick : 0
  const sl = Math.round(slRate * 100) / 100
  const tp = Math.round(tpRate * 100) / 100
  if (sl >= mark) {
    feasible = false
    reason = `the stop ${sl.toFixed(2)} is not below the current price ${mark.toFixed(2)} (the position is already past it)`
  }
  const lv: Levels = {
    profile,
    sl_rate: sl,
    sl_pct: (entry - sl) / entry,
    max_loss_usd: units * (entry - sl),
    clamps,
    feasible,
    ...(reason ? { reason } : {}),
    ...(atr > 0 ? { sl_atr: (mark - sl) / atr } : {}),
  }
  if (tp > 0) {
    lv.tp_rate = tp
    lv.tp_pct = (tp - entry) / entry
    lv.target_usd = units * (tp - entry)
    lv.rr = lv.max_loss_usd > 0 ? lv.target_usd / lv.max_loss_usd : 0
    if (atr > 0) lv.tp_atr = (tp - mark) / atr
  }
  return lv
}

function protectionContext(
  book: string,
  side: 'open' | 'protect',
  entry: number,
  mark: number,
  profile: string,
  lv: Levels,
  before?: Position,
) {
  const { close } = refClose(book)
  const b = forwardTests.books.find((x) => x.book === book)
  return {
    profile,
    levels: lv,
    volatility: volatilityOf(book, close),
    entry,
    mark,
    bounds: BOUNDS[book] ?? BOUNDS.spx500,
    limits: LIMITS,
    bounds_source: 'venue',
    sma50: b?.decision.sma50 ?? 0,
    ...(side === 'protect' && before
      ? {
          before: {
            ...(before.stop_loss_rate ? { stop_loss_rate: before.stop_loss_rate, stop_type: 'fixed' } : {}),
            ...(before.take_profit_rate ? { take_profit_rate: before.take_profit_rate } : {}),
            protected: (before.stop_loss_rate ?? 0) > 0,
          },
        }
      : {}),
  }
}

const BOOKS: Record<string, { symbol: string; id: number }> = {
  nsdq100: { symbol: 'NSDQ100', id: 28 },
  spx500: { symbol: 'SPX500', id: 27 },
}
const TTL_S = 120
const MIN = 1000
const MAX = 1500
const DEFAULT_AMOUNT = 1100

/**
 * The real-account canary's form facts under `?mock=real-canary` (plan §6,
 * risk.EtoroRealCanaryPolicy as built): x20 only, 50-60 USD invested, the
 * stop held between 2.3 % and 2.45 % of the price (23-24.5 USD at risk on
 * 50 USD), no equity caps, nsdq100 only.
 */
const CANARY = {
  min: 50,
  max: 60,
  default: 50,
  leverage: 20,
  policy: 'etoro-real-canary-2026-10-22',
  limits: { min_sl_pct: 0.023, max_sl_pct: 0.0245, max_loss_pct_equity: 0 } as Limits,
  bounds: { min_sl_pct: 0.0005, max_sl_pct: 0.025, max_tp_pct: 0.5, tick: 0.01 } as Bounds,
  account: { currency: 'USD', cash: 60, equity: 60 },
}

/** What a request's scenario allows: the bounds, the leverage set, the policy and the account. */
function policyOf(request: Request) {
  if (isRealCanary(request)) {
    return {
      real: true,
      min: CANARY.min,
      max: CANARY.max,
      default: CANARY.default,
      leverages: [CANARY.leverage],
      policy: CANARY.policy,
      limits: CANARY.limits,
      bounds: CANARY.bounds,
      account: CANARY.account,
      books: ['nsdq100'],
    }
  }
  return {
    real: false,
    min: MIN,
    max: MAX,
    default: DEFAULT_AMOUNT,
    leverages: [1],
    policy: 'etoro-demo-2026-10-19',
    limits: LIMITS,
    bounds: undefined as Bounds | undefined,
    account,
    books: ['nsdq100', 'spx500'],
  }
}

/** The mock account: the fixture's 10-16 numbers. */
const account = { currency: 'USD', cash: 97793.95, equity: 99949.9 }

const state = {
  positions: new Map<string, Position>(),
  legs: new Map<string, number>(), // book -> legs today (the fixture's 10-16 close of NSDQ100 counts as 1)
  tokens: new Map<string, Claims>(),
  used: new Set<string>(),
  lastOrder: new Map<string, LastOrder>(),
  nextPos: 3616700000,
  // The positions view (GET /etoro/positions): the fixture's rows, moved by
  // every console open / protect / close placed through the mock.
  trades: [] as EtoroTrade[],
}

function seed() {
  state.positions.clear()
  state.legs.clear()
  for (const b of forwardTests.books) {
    state.positions.set(b.book, (b.demo_position as Position | null) ?? { state: 'flat' })
    state.legs.set(b.book, b.last_order && b.last_order.fill_date === forwardTests.session.date ? 1 : 0)
  }
  state.trades = (positionsFixture.trades as EtoroTrade[]).map((t) => ({ ...t }))
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
  return mockFlag(request)
}

/**
 * The key of a book's mock position / legs: the canary ledger (real) is a
 * ledger of its own and starts flat with no leg today, whatever the demo
 * fixture holds, so its state lives under a `real:` prefix.
 */
function bookKey(book: string, real: boolean): string {
  return real ? `real:${book}` : book
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
  for (const p of state.positions.values()) if (p.state === 'long') t += (p.amount ?? 0) * (p.leverage ?? 1)
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
  const p = policyOf(request)
  return {
    enabled: !off,
    ...(off
      ? {
          disabled_reason:
            'assisted trading is off: start ui-api with -etoro-trading (UI_API_ETORO_TRADING=1), -operator-token-file and -etoro-env-file naming a 0600 file with ETORO_ENV=demo',
        }
      : {}),
    token_ttl_s: TTL_S,
    min_amount_usd: p.min,
    max_amount_usd: p.max,
    default_amount_usd: p.default,
    environment: p.real ? 'real' : 'demo',
    leverage_options: p.leverages,
    default_leverage: p.leverages[0],
    policy_version: p.policy,
    books: p.books,
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

/** The summary the server computes, from the mock's rows. */
function summarizeTrades(ts: EtoroTrade[]): EtoroTradeSummary {
  const s: EtoroTradeSummary = {
    open: 0,
    closed: 0,
    unknown: 0,
    open_exposure: 0,
    unrealized_pnl: 0,
    fees_to_date: 0,
    realized_gross: 0,
    realized_net: 0,
    realized_confirmed: 0,
  }
  for (const t of ts) {
    if (t.status === 'open') {
      s.open++
      s.open_exposure += t.amount
      s.unrealized_pnl += t.unrealized_pnl ?? 0
      s.fees_to_date += t.fees_to_date ?? 0
    } else if (t.status === 'closed') {
      s.closed++
      s.realized_gross += t.gross_pnl ?? 0
      if (t.net != null) {
        s.realized_net += t.net
        s.realized_confirmed++
      }
    } else s.unknown++
  }
  return s
}

/**
 * GET /api/ui/etoro/positions in mock mode: the fixture's shape with the
 * mock's rows (moved by console trades), filtered by ?book. With
 * `?mock=trading-off` the account is not read: the rows lose their live
 * figures and `live` is false, as ui-api answers without -etoro-trading.
 */
export function mockPositions(request: Request): Response {
  const url = new URL(request.url)
  const book = url.searchParams.get('book') ?? ''
  const off = flagOf(request) === 'trading-off'
  let trades = state.trades.filter((t) => !book || t.book === book)
  if (off) {
    trades = trades.map((t) => {
      if (t.status !== 'open') return t
      const rest: EtoroTrade = { ...t }
      delete rest.account_holds
      delete rest.mark_source
      delete rest.mark_rate
      delete rest.unrealized_pnl
      delete rest.unrealized_pct
      delete rest.fees_to_date
      delete rest.account_stop_loss_rate
      delete rest.account_take_profit_rate
      return rest
    })
  }
  const { account, live: _live, ...fixture } = positionsFixture
  void _live
  return HttpResponse.json({
    ...fixture,
    generated_at: new Date().toISOString(),
    live: !off,
    ...(off
      ? {
          live_reason:
            'the account is not read (assisted trading is off: start ui-api with -etoro-trading): the figures are the ledger\u2019s',
        }
      : { account: { ...account, at: new Date().toISOString() }, cached: false }),
    summary: summarizeTrades(trades),
    trades,
    warnings: [],
  })
}

export const tradeHandlers = [
  http.post('/api/ui/etoro/trades/recommend', async ({ request }) => {
    const flag = flagOf(request)
    if (flag === 'trading-off')
      return HttpResponse.json({ error: mockTradingInfo(request).disabled_reason }, { status: 403 })
    const denied = auth(request)
    if (denied) return denied
    let body: Json
    try {
      body = (await request.json()) as Json
    } catch {
      return HttpResponse.json({ error: 'body: invalid JSON' }, { status: 400 })
    }
    const book = String(body.book ?? '')
    const side = body.side === 'protect' ? 'protect' : body.side === 'open' ? 'open' : null
    const inst = BOOKS[book]
    if (!inst || !side) return HttpResponse.json({ error: 'side: "open" or "protect"' }, { status: 400 })
    const pol = policyOf(request)
    const pos = state.positions.get(bookKey(book, pol.real)) ?? { state: 'flat' as const }
    if (side === 'protect' && pos.state !== 'long')
      return HttpResponse.json({ error: `the ledger holds no position on ${inst.symbol} to protect` }, { status: 409 })
    const q = quoteOf(book)
    const spreadPct = q.spread_bps / 1e4
    const lev = side === 'open' ? Number(body.leverage) || pol.leverages[0] : (pos.leverage ?? 1)
    if (side === 'open' && !pol.leverages.includes(lev))
      return HttpResponse.json(
        { error: `leverage: must be one of [${pol.leverages.join(' ')}] (the policy)` },
        { status: 400 },
      )
    const amount = side === 'open' ? Number(body.amount_usd) || pol.default : 0
    const units = side === 'open' ? (amount * lev) / q.ask : (pos.units ?? 0)
    const entry = side === 'open' ? q.ask : (pos.open_rate ?? q.bid)
    const mark = q.bid
    const profiles: Record<string, Levels> = {}
    for (const p of Object.keys(MULT))
      profiles[p] = buildLevels(
        book,
        p,
        entry,
        mark,
        units,
        spreadPct,
        pol.account.equity,
        0,
        0,
        pol.limits,
        pol.bounds,
      )
    const { signal } = refClose(book)
    return HttpResponse.json({
      ledger: new URL(request.url).searchParams.get('ledger') || (pol.real ? 'etoro-real-canary' : 'etoro-demo'),
      book,
      side,
      instrument: inst,
      quote: q,
      account: { ...pol.account, exposure: exposure() },
      position: pos,
      rule_signal: signal,
      ...(side === 'open' ? { amount_usd: amount } : {}),
      units,
      leverage: lev,
      exposure_usd: units * q.ask,
      environment: pol.real ? 'real' : 'demo',
      profiles,
      default_profile: 'moderate',
      context: protectionContext(book, side, entry, mark, 'moderate', profiles.moderate, pos),
      policy_version: pol.policy,
      warnings: [],
      prepared_at: new Date().toISOString(),
    })
  }),

  http.post('/api/ui/etoro/trades/prepare', async ({ request }) => {
    const pol = policyOf(request)
    const ledger = new URL(request.url).searchParams.get('ledger') || (pol.real ? 'etoro-real-canary' : 'etoro-demo')
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
      (k) =>
        ![
          'book',
          'side',
          'amount_usd',
          'position_id',
          'leverage',
          'profile',
          'stop_loss_pct',
          'take_profit_pct',
        ].includes(k),
    )
    if (unknown) return HttpResponse.json({ error: `body: json: unknown field "${unknown}"` }, { status: 400 })
    const book = String(body.book ?? '')
    const side: Side | null =
      body.side === 'close' ? 'close' : body.side === 'open' ? 'open' : body.side === 'protect' ? 'protect' : null
    const inst = BOOKS[book]
    const lev = Number(body.leverage)
    if (!inst || !side || !pol.leverages.includes(lev) || !pol.books.includes(book)) {
      log({ outcome: 'refused', error: 'book, side or leverage invalid' })
      const want =
        pol.leverages.length === 1 ? `leverage ${pol.leverages[0]}` : `leverage in [${pol.leverages.join(' ')}]`
      return HttpResponse.json(
        {
          error:
            !inst || !side
              ? `book, side ("open" | "close" | "protect") and ${want} are required`
              : `leverage: must be ${pol.leverages[0]} (the policy allows x${pol.leverages[0]} only)`,
        },
        { status: 400 },
      )
    }
    const amount = side === 'open' ? Number(body.amount_usd) : 0
    const positionId = side !== 'open' ? String(body.position_id ?? '') : ''
    if (side === 'open' && (!(amount >= pol.min) || !(amount <= pol.max))) {
      log({ outcome: 'refused', error: 'amount_usd out of bounds' })
      return HttpResponse.json({ error: `amount_usd: must be between ${pol.min} and ${pol.max} USD` }, { status: 400 })
    }
    if (side !== 'open' && !/^\d{1,32}$/.test(positionId)) {
      log({ outcome: 'refused', error: 'position_id missing' })
      return HttpResponse.json({ error: 'position_id: the numeric id of the ledger’s position' }, { status: 400 })
    }
    const profile = String(body.profile ?? '') || 'moderate'
    const slPctIn = Number(body.stop_loss_pct ?? 0)
    const tpPctIn = Number(body.take_profit_pct ?? 0)
    if (!['conservative', 'moderate', 'aggressive', 'custom'].includes(profile))
      return HttpResponse.json({ error: `profile: unknown profile "${profile}"` }, { status: 400 })
    if (side !== 'close' && profile === 'custom' && !(slPctIn > 0))
      return HttpResponse.json(
        { error: 'stop_loss_pct: required with the custom profile (the policy requires a stop)' },
        { status: 400 },
      )
    if (side !== 'close' && profile !== 'custom' && (slPctIn || tpPctIn))
      return HttpResponse.json(
        { error: `stop_loss_pct / take_profit_pct: only with profile "custom" (got profile "${profile}")` },
        { status: 400 },
      )

    const pos = state.positions.get(bookKey(book, pol.real)) ?? { state: 'flat' as const }
    const q = quoteOf(book)
    const { close, signal } = refClose(book)
    const units = side === 'open' ? (amount * lev) / q.ask : (pos.units ?? 0)
    const exposureUsd = units * (side === 'open' ? q.ask : q.bid)
    const account = pol.account
    const reasons: string[] = []
    const warnings: string[] = []
    const halt = s.halt?.halted ? s.halt : null
    if (halt) reasons.push(`halted: trading is halted: ${halt.reason} (halt file, by ${halt.by} at ${halt.at})`)
    const marketClosed = flag === 'market-closed'
    if (marketClosed && side !== 'protect') reasons.push(`market_closed: the market for ${book} is closed`)
    const legs = state.legs.get(bookKey(book, pol.real)) ?? 0
    if (side === 'open' && legs >= 1) reasons.push(`max_orders_per_day: would be order ${legs + 1} today, max 1`)
    const exp = exposure() + (side === 'open' ? amount * lev : 0)
    if (side === 'open' && !pol.real && exp / account.equity > 0.05) {
      reasons.push(
        `max_exposure_pct_equity: exposure after the order ${exp.toFixed(2)} is ${((exp / account.equity) * 100).toFixed(1)}% of equity ${account.equity.toFixed(2)} (max 5.0%)`,
      )
    }
    const dev = close > 0 ? Math.abs((side === 'open' ? q.ask : q.bid) / close - 1) * 1e4 : 0
    if (dev > 1000 && side !== 'protect')
      reasons.push(`max_price_deviation_bps: price is ${dev.toFixed(0)} bps from reference ${close} (max 1000)`)

    // Protection levels (opens and protects).
    let protection: Json | null = null
    if (side !== 'close') {
      if (!body.profile && side === 'open')
        warnings.push("no stop requested: the moderate profile's levels were applied (the policy requires a stop)")
      const entry = side === 'open' ? q.ask : (pos.open_rate ?? q.bid)
      const lv = buildLevels(
        book,
        profile,
        entry,
        q.bid,
        units,
        q.spread_bps / 1e4,
        account.equity,
        slPctIn,
        tpPctIn,
        pol.limits,
        pol.bounds,
      )
      if (!lv.feasible) reasons.push(`protection: ${lv.reason}`)
      protection = protectionContext(book, side, entry, q.bid, profile, lv, pos)
      if (side === 'protect' && pos.stop_loss_rate && lv.sl_rate < pos.stop_loss_rate)
        warnings.push(
          `the new stop ${lv.sl_rate.toFixed(2)} is below the current stop ${pos.stop_loss_rate.toFixed(2)}: this loosens the protection`,
        )
    }

    if (side === 'open') {
      if (pos.state === 'long')
        reasons.push(
          `the ledger already holds position ${pos.position_id} on ${inst.symbol} (${(pos.units ?? 0).toFixed(6)} units): close it before opening again`,
        )
    } else if (pos.state !== 'long') {
      reasons.push(`the ledger holds no position on ${inst.symbol} to ${side}`)
    } else if (positionId !== pos.position_id) {
      reasons.push(`position ${positionId} is not the ledger's position on ${inst.symbol} (${pos.position_id})`)
    }
    const cash = flag === 'short-cash' ? 500 : account.cash
    if (side === 'open' && cash < amount)
      reasons.push(`available cash ${cash.toFixed(2)} USD is below the ${amount.toFixed(0)} USD amount`)
    if (side === 'open' && signal === 'flat' && !pol.real)
      warnings.push(
        'the rule says flat: the executor closes this position at its next 09:35 New York run unless the ledger is halted',
      )
    if (side === 'close' && signal === 'long' && !pol.real)
      warnings.push(
        'the rule says long: the executor re-opens at its next 09:35 New York run unless the ledger is halted',
      )

    const ref = `ui-${pol.real ? 'real-' : ''}${book}-${nyDate(now)}-${side}-${hex6()}`
    const requestId = requestIdFor(ref)
    const confirmation = `${side === 'open' ? 'PLACE' : side === 'protect' ? 'PROTECT' : 'CLOSE'} ${pol.real ? 'REAL ' : ''}${inst.symbol}`
    const allow = reasons.length === 0
    const ttl = flag === 'short-ttl' ? 3 : TTL_S
    const lv = protection ? (protection.levels as Levels) : null
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
        policy_version: pol.policy,
        order: {
          book,
          side: side === 'open' ? 'buy' : side === 'close' ? 'sell' : 'protect',
          qty_btc: units,
          price: side === 'open' ? q.ask : q.bid,
          ref_price: close,
          leverage: lev,
          ...(lv && side === 'open'
            ? { stop_loss: lv.sl_rate, ...(lv.tp_rate ? { take_profit: lv.tp_rate } : {}) }
            : {}),
        },
        state: {
          position_btc: side !== 'open' ? units : 0,
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
      leverage: lev,
      exposure_usd: exposureUsd,
      environment: pol.real ? 'real' : 'demo',
      ...(lv && side === 'open' ? { max_loss_usd: lv.max_loss_usd } : {}),
      ref_price: close,
      confirmation,
      client_ref: ref,
      request_id: requestId,
      prepared_at: now.toISOString(),
      ...(protection ? { protection } : {}),
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
        sl_rate: lv?.sl_rate ?? 0,
        tp_rate: lv?.tp_rate ?? 0,
        profile: lv ? profile : '',
        max_loss_usd: lv?.max_loss_usd ?? 0,
        leverage: lev,
        env: pol.real ? 'real' : 'demo',
      }
      state.tokens.set(id, c)
      preview.token = `mock.${id}`
      preview.expires_at = new Date(c.exp).toISOString()
      preview.expires_in_s = ttl
    }
    const detail =
      side === 'open'
        ? `${pol.real ? 'REAL ' : ''}open ${book} ${amount.toFixed(0)} USD x${lev} (${units.toFixed(6)} units)`
        : `${pol.real ? 'REAL ' : ''}${side} ${book} position ${positionId} (${units.toFixed(6)} units)`
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
    if (flag === 'market-closed' && c.side !== 'protect')
      return fail(422, 'refused', 'the NYSE cash session is closed; nothing sent', who)
    const key = bookKey(c.book, c.env === 'real')
    const pos = state.positions.get(key) ?? { state: 'flat' as const }
    if (c.side === 'open' && pos.state !== 'flat')
      return fail(
        422,
        'refused',
        `the ledger changed since the preview: it holds position ${pos.position_id}; nothing sent`,
        who,
      )
    if (c.side !== 'open' && (pos.state !== 'long' || pos.position_id !== c.position_id)) {
      return fail(
        422,
        'refused',
        `the ledger changed since the preview: it no longer holds position ${c.position_id}; nothing sent`,
        who,
      )
    }
    const detail =
      c.side === 'open'
        ? `${c.env === 'real' ? 'REAL ' : ''}open ${c.book} ${c.amount_usd.toFixed(0)} USD x${c.leverage || 1} (${c.units.toFixed(6)} units)`
        : `${c.env === 'real' ? 'REAL ' : ''}${c.side} ${c.book} position ${c.position_id} (${c.units.toFixed(6)} units)`
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

    // A protect edits the held position: not a leg, no fill.
    if (c.side === 'protect') {
      const before = { stop_loss_rate: pos.stop_loss_rate ?? 0, take_profit_rate: pos.take_profit_rate ?? 0 }
      const after: Position = {
        ...pos,
        ...(c.sl_rate > 0 ? { stop_loss_rate: c.sl_rate, stop_type: 'fixed' } : {}),
        ...(c.tp_rate > 0 ? { take_profit_rate: c.tp_rate } : {}),
      }
      state.positions.set(c.book, after)
      const opId = `op-${c.client_ref}`
      const today = nyDate(now)
      const tr = state.trades.find((t) => t.position_id === c.position_id)
      if (tr) {
        if (c.sl_rate > 0) tr.stop_loss_rate = c.sl_rate
        if (c.tp_rate > 0) tr.take_profit_rate = c.tp_rate
        tr.stop_type = 'fixed'
        tr.protect_edits = (tr.protect_edits ?? 0) + 1
        tr.account_stop_loss_rate = tr.stop_loss_rate
        tr.account_take_profit_rate = tr.take_profit_rate
        tr.protection_drift = false
      }
      state.lastOrder.set(c.book, {
        kind: 'protect',
        client_ref: c.client_ref,
        request_id: c.request_id,
        order_id: opId,
        status: 'executed',
        position_ids: [c.position_id],
        avg_price: 0,
        units: c.units,
        amount: pos.amount ?? 0,
        intent_at: now.toISOString(),
        started: now.toISOString(),
        finished: finished.toISOString(),
        fill_vs_mid_bps: 0,
        fill_vs_close_bps: 0,
        bar_date: today,
        fill_date: today,
        source: 'ui',
        by: who.by,
      })
      const protection = {
        profile: c.profile,
        atr: volatilityOf(c.book, c.ref_price).atr,
        atr_pct: volatilityOf(c.book, c.ref_price).atr_pct,
        ...(before.stop_loss_rate ? { stop_loss_before: before.stop_loss_rate } : {}),
        ...(before.take_profit_rate ? { take_profit_before: before.take_profit_rate } : {}),
        max_loss_usd: c.max_loss_usd,
        reason: 'set from the console',
        operation_id: opId,
      }
      const done = log({
        outcome: 'done',
        ...who,
        detail: `${detail} -> executed: stop ${before.stop_loss_rate.toFixed(2)} -> ${(after.stop_loss_rate ?? 0).toFixed(2)}, target ${before.take_profit_rate.toFixed(2)} -> ${(after.take_profit_rate ?? 0).toFixed(2)} (operation ${opId})`,
      })
      done.id = req1.id
      return HttpResponse.json({
        status: 'executed',
        book: c.book,
        side: c.side,
        client_ref: c.client_ref,
        request_id: c.request_id,
        order_id: opId,
        position_ids: [c.position_id],
        avg_price: pos.open_rate ?? 0,
        units: c.units,
        amount_usd: pos.amount ?? 0,
        fill_vs_mid_bps: 0,
        fill_vs_close_bps: 0,
        position_after: after,
        ledger_recorded: true,
        audit_path: '../strategy-executor/etoro-daily-data/demo/ui-audit.jsonl',
        audit: done,
        protection,
        warnings: [],
      })
    }

    let after: Position
    let avg: number
    let positionIds: string[]
    let units: number
    let amount: number
    if (c.side === 'open') {
      state.nextPos++
      const pid = String(state.nextPos)
      avg = c.ask + 0.5
      units = (c.amount_usd * (c.leverage || 1)) / avg
      amount = c.amount_usd
      positionIds = [pid]
      after = {
        state: 'long',
        position_id: pid,
        units,
        amount,
        open_rate: avg,
        opened_at: finished.toISOString(),
        ...(c.leverage > 1 ? { leverage: c.leverage } : {}),
        ...(c.sl_rate > 0 ? { stop_loss_rate: c.sl_rate, stop_type: 'fixed' } : {}),
        ...(c.tp_rate > 0 ? { take_profit_rate: c.tp_rate } : {}),
      }
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
    state.positions.set(key, after)
    state.legs.set(key, (state.legs.get(key) ?? 0) + 1)
    const orderId = `o${state.nextPos + 100}`
    const today = nyDate(now)
    if (c.side === 'open') {
      state.trades.unshift({
        book: c.book,
        symbol: BOOKS[c.book].symbol,
        position_id: positionIds[0],
        status: 'open',
        source: 'manual',
        operator: { by: who.by, reason: who.reason },
        recorded_open: true,
        opened_at: finished.toISOString(),
        open_rate: avg,
        units,
        amount,
        open_client_ref: c.client_ref,
        open_bar_date: today,
        open_fill_vs_mid_bps: fillVsMid,
        open_costs: { spread: 0.04, markup: 0, transaction_fee: 0, overnight_fee: 0.26, weekend_fee: 0 },
        ...(c.sl_rate > 0 ? { stop_loss_rate: c.sl_rate, stop_type: 'fixed', account_stop_loss_rate: c.sl_rate } : {}),
        ...(c.tp_rate > 0 ? { take_profit_rate: c.tp_rate, account_take_profit_rate: c.tp_rate } : {}),
        account_holds: true,
        mark_source: 'account',
        mark_rate: c.bid,
        unrealized_pnl: units * (c.bid - avg),
        unrealized_pct: (units * (c.bid - avg)) / amount,
        fees_to_date: 0,
        held_days: 0,
      })
    } else {
      const tr = state.trades.find((t) => t.position_id === c.position_id)
      if (tr) {
        const gross = tr.units * (avg - tr.open_rate)
        Object.assign(tr, {
          status: 'closed',
          recorded_close: true,
          closed_at: finished.toISOString(),
          close_rate: avg,
          closed_by: 'operator',
          close_reason: who.reason,
          close_client_ref: c.client_ref,
          close_bar_date: today,
          close_fill_vs_mid_bps: fillVsMid,
          gross_pnl: gross,
          return_pct: tr.amount > 0 ? gross / tr.amount : 0,
          held_days: tr.opened_at
            ? Math.round((finished.getTime() - new Date(tr.opened_at).getTime()) / 86_400_000)
            : 0,
          financing_to_date: tr.fees_to_date ?? 0,
          mark_source: undefined,
          mark_rate: undefined,
          unrealized_pnl: undefined,
          unrealized_pct: undefined,
          account_holds: undefined,
        } satisfies Partial<EtoroTrade>)
      }
    }
    state.lastOrder.set(key, {
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
      ...(c.side === 'open' && c.sl_rate > 0
        ? {
            protection: {
              profile: c.profile,
              atr: volatilityOf(c.book, c.ref_price).atr,
              atr_pct: volatilityOf(c.book, c.ref_price).atr_pct,
              max_loss_usd: units * (avg - c.sl_rate),
              reason: 'set with the open from the console',
            },
          }
        : {}),
    })
  }),

  // POST /etoro/trades/cancel (2026-10-21): burns the token, audits
  // trade.cancel, touches nothing else. Mirrors ui-api's statuses.
  http.post('/api/ui/etoro/trades/cancel', async ({ request }) => {
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
        action: 'trade.cancel',
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
    const unknown = Object.keys(body).find((k) => !['token', 'reason', 'by'].includes(k))
    if (unknown) return fail(400, 'refused', `body: json: unknown field "${unknown}"`)
    const who = { by: String(body.by ?? '').trim(), reason: String(body.reason ?? '').trim() }
    if (/[\r\n]/.test(who.reason)) return fail(400, 'refused', 'reason: one line, no control characters', who)
    if (who.by && !BY.test(who.by)) return fail(400, 'refused', 'by: 1-64 letters, digits, spaces or . _ @ -', who)
    const id = String(body.token ?? '').replace(/^mock\./, '')
    const c = state.tokens.get(id)
    if (!c || !String(body.token ?? '').startsWith('mock.')) {
      return fail(401, 'denied', 'trade token invalid or issued for another operator session: prepare again', who)
    }
    const ref = { ...who, ref: c.client_ref }
    const detail =
      c.side === 'open'
        ? `${c.env === 'real' ? 'REAL ' : ''}open ${c.book} ${c.amount_usd.toFixed(0)} USD x${c.leverage || 1} (${c.units.toFixed(6)} units)`
        : `${c.env === 'real' ? 'REAL ' : ''}${c.side} ${c.book} position ${c.position_id} (${c.units.toFixed(6)} units)`
    const expiresAt = new Date(c.exp).toISOString()
    let status: 'cancelled' | 'expired'
    if (now.getTime() >= c.exp) {
      status = 'expired'
    } else if (state.used.has(id)) {
      return fail(409, 'refused', 'trade token already used: it was placed or cancelled', ref)
    } else {
      state.used.add(id)
      status = 'cancelled'
    }
    const done = log({
      outcome: 'done',
      ...ref,
      detail:
        status === 'expired'
          ? `${detail} -> token had expired at ${expiresAt}; nothing to cancel, nothing sent`
          : `${detail} -> cancelled: token burned, nothing sent`,
    })
    return HttpResponse.json({
      status,
      ledger,
      book: c.book,
      side: c.side,
      client_ref: c.client_ref,
      request_id: c.request_id,
      expires_at: expiresAt,
      audit_path: '../strategy-executor/etoro-daily-data/demo/ui-audit.jsonl',
      audit: done,
    })
  }),
]
