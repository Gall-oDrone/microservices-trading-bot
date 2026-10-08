/**
 * Mock-mode Strategies page. GET /api/ui/strategies starts from the
 * captured fixture (fixtures/strategies.json); POST
 * /api/ui/strategies/:name/start|stop and POST /api/ui/risk/halt-all
 * mirror ui-api's checks (strategies.go) and keep the strategy state, the
 * holds and the audit log in memory. A page reload (or resetMockStrategies
 * in tests) forgets everything.
 *
 * The kill switch halts the same in-memory ledgers as the Risk page mock
 * (controls.ts), so a halt-all shows up there too. Like that mock, any
 * non-empty bearer token is accepted.
 */
import { http, HttpResponse } from 'msw'
import type { StrategyView as Strategy } from '../api/schemas'
import { haltFileInfo, ledgerState, nextMockId, type Entry, type HaltState } from './controls'
import fixture from './fixtures/strategies.json'

type Json = Record<string, unknown>

const HALT_ALL = fixture.kill_switch.confirm
const NAME = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/
const BY = /^[A-Za-z0-9][A-Za-z0-9 ._@-]{0,63}$/

let strategies = new Map<string, Strategy>()
let audit: Entry[] = []

/** Back to the captured fixture (tests call this between cases). */
export function resetMockStrategies() {
  strategies = new Map(fixture.strategies.map((s) => [s.name, { ...s } as Strategy]))
  audit = []
}
resetMockStrategies()

/** Tests: change a strategy, e.g. give it an open position. */
export function patchMockStrategy(name: string, patch: Partial<Strategy>) {
  const s = strategies.get(name)
  if (!s) throw new Error(`no mock strategy ${name}`)
  strategies.set(name, { ...s, ...patch })
}

/** Mirrors ui-api's validateControl (controls.go), messages included. */
function validate(body: Json, want: string, what: string, extra: string[] = []): string | null {
  const unknown = Object.keys(body).find((k) => !['reason', 'by', 'confirm', ...extra].includes(k))
  if (unknown) return `body: json: unknown field "${unknown}"`
  const reason = typeof body.reason === 'string' ? body.reason.trim() : ''
  const by = typeof body.by === 'string' ? body.by.trim() : ''
  if (reason.length < 8) return 'reason: at least 8 characters, so the halt is explained in the ledger'
  if (reason.length > 500) return 'reason: at most 500 characters'
  if (/[\r\n\0]/.test(reason)) return 'reason: one line, no control characters'
  if (!BY.test(by)) return 'by: 1-64 letters, digits, spaces or . _ @ -'
  if (body.confirm !== want) return `confirm: type ${what} "${want}" to confirm`
  return null
}

/** The token, content-type and JSON checks of ui-api's admit(); a Response when refused. */
async function admit(request: Request, refuse: (status: number, error: string) => Response) {
  if (!/^Bearer \S+$/.test(request.headers.get('Authorization') ?? '')) {
    return refuse(401, 'missing or wrong operator token')
  }
  if (!(request.headers.get('Content-Type') ?? '').startsWith('application/json')) {
    return refuse(415, 'send Content-Type: application/json')
  }
  try {
    return (await request.json()) as Json
  } catch {
    return refuse(400, 'body: invalid JSON')
  }
}

function ledgersNow() {
  return fixture.ledgers.map((l) => {
    const h: HaltState | null = ledgerState(l.name).halt
    return h ? { ...l, halt_file: haltFileInfo(l.halt_file.path, h) } : l
  })
}

function strategiesInfo() {
  const ledgers = ledgersNow()
  return {
    ...fixture,
    ledgers,
    kill_switch: {
      ...fixture.kill_switch,
      already_halted: ledgers.filter((l) => !l.remote && l.halt_file.halted).map((l) => l.name),
    },
    executor: { ...fixture.executor, fetched_at: new Date().toISOString().replace(/\.\d+Z$/, 'Z') },
    strategies: [...strategies.values()].sort((a, b) => a.name.localeCompare(b.name)),
    audit: audit.slice(0, 50),
  }
}

export const strategyHandlers = [
  http.get('/api/ui/strategies', () => HttpResponse.json(strategiesInfo())),

  http.post('/api/ui/strategies/:name/:action', async ({ params, request }) => {
    const name = String(params.name)
    const action = String(params.action)
    if (action !== 'start' && action !== 'stop') return HttpResponse.json({ error: 'not found' }, { status: 404 })
    if (!NAME.test(name)) return HttpResponse.json({ error: 'invalid strategy name' }, { status: 400 })
    const now = new Date().toISOString()
    const base: Entry = {
      id: nextMockId(),
      at: now,
      action: `strategy_${action}`,
      outcome: 'requested',
      ledger: '',
      user_agent: 'msw',
      strategy: name,
      executor: fixture.executor.url,
    }
    const log = (e: Partial<Entry>) => {
      const entry = { ...base, ...e }
      audit.unshift(entry)
      return entry
    }
    const refuse = (status: number, error: string, outcome: Entry['outcome'] = 'refused') => {
      log({ outcome, error })
      return HttpResponse.json({ error }, { status })
    }

    const body = await admit(request, (status, error) => refuse(status, error, status === 401 ? 'denied' : 'refused'))
    if (body instanceof Response) return body
    base.by = String(body.by ?? '').trim()
    base.reason = String(body.reason ?? '').trim()
    const problem = validate(body, name, 'the strategy name', ['ack_position'])
    if (problem) return refuse(400, problem)

    const cur = strategies.get(name)
    if (!cur) return refuse(404, `strategy-executor has no strategy "${name}"`)
    const busy = cur.has_position || cur.pending_buy || cur.pending_sell
    if (action === 'start' && cur.running) return refuse(409, 'already running')
    if (action === 'stop' && !cur.running && cur.hold) {
      return refuse(409, `already stopped and held by ${cur.hold.by} at ${cur.hold.at}: ${cur.hold.reason}`)
    }
    if (action === 'stop' && busy && body.ack_position !== true) {
      return refuse(
        409,
        'the strategy has an open position or pending order; stopping leaves it unmanaged. Confirm with ack_position to stop anyway',
      )
    }
    const details: string[] = []
    if (action === 'stop' && busy) {
      details.push(
        `stopped with an open position or pending order (acknowledged): side=${cur.position_side ?? ''} size=${cur.position_size} pending_buy=${cur.pending_buy} pending_sell=${cur.pending_sell}`,
      )
    }
    if (action === 'stop' && !cur.running) details.push('not running; recording a hold so it stays stopped')
    if (details.length) base.detail = details.join(' ')

    log({ outcome: 'requested' })
    const hold = { reason: base.reason, by: base.by, at: now }
    const next: Strategy =
      action === 'start' ? { ...cur, running: true, hold: undefined } : { ...cur, running: false, hold }
    strategies.set(name, next)
    const done = log({ outcome: 'done', upstream_status: 200 })
    // The shape of strategy-executor's start (release_hold) / stop (hold) reply.
    const upstream =
      action === 'start'
        ? { name, status: 'started', ...(cur.hold ? { released_hold: cur.hold } : {}) }
        : { name, status: 'stopped', held: true, hold, was_running: cur.running }
    return HttpResponse.json({ strategy: name, action, upstream, audit: done })
  }),

  http.post('/api/ui/risk/halt-all', async ({ request }) => {
    const targets = fixture.ledgers.filter((l) => !l.remote)
    const now = new Date().toISOString()
    const entry = (ledger: string, e: Partial<Entry>): Entry => ({
      id: nextMockId(),
      at: now,
      action: 'halt_all',
      outcome: 'refused',
      ledger,
      user_agent: 'msw',
      ...e,
    })
    // Refusals before the halt go to every target ledger's log, like noteAll.
    const refuse = (status: number, error: string, outcome: Entry['outcome'] = 'refused', who: Partial<Entry> = {}) => {
      for (const l of targets) ledgerState(l.name).audit.unshift(entry(l.name, { outcome, error, ...who }))
      return HttpResponse.json({ error }, { status })
    }
    const body = await admit(request, (status, error) => refuse(status, error, status === 401 ? 'denied' : 'refused'))
    if (body instanceof Response) return body
    const who = { by: String(body.by ?? '').trim(), reason: String(body.reason ?? '').trim() }
    const problem = validate(body, HALT_ALL, 'the phrase')
    if (problem) return refuse(400, problem, 'refused', who)

    const group = nextMockId()
    const results = targets.map((l) => {
      const s = ledgerState(l.name)
      const before: HaltState = s.halt ?? { halted: false }
      if (before.halted) {
        s.audit.unshift(
          entry(l.name, {
            ...who,
            group,
            before,
            error: `already halted by ${before.by} at ${before.at}: ${before.reason}`,
          }),
        )
        return { ledger: l.name, outcome: 'already_halted', halt_file: haltFileInfo(l.halt_file.path, before) }
      }
      const after: HaltState = { halted: true, reason: who.reason, by: who.by, at: now }
      s.audit.unshift(entry(l.name, { ...who, group, before, after, outcome: 'requested' }))
      s.halt = after
      s.audit.unshift(entry(l.name, { ...who, group, before, after, outcome: 'done' }))
      return { ledger: l.name, outcome: 'halted', halt_file: haltFileInfo(l.halt_file.path, after) }
    })
    return HttpResponse.json({ action: 'halt_all', group, results })
  }),
]
