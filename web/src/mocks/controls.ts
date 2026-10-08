/**
 * Mock-mode operator controls (R4). GET /api/ui/controls starts from the
 * captured fixture; POST /api/ui/risk/halt|resume mirror ui-api's checks
 * (controls.go) and keep the halt and the audit log in memory, so the
 * Risk page can be exercised without a backend. A page reload (or
 * resetMockControls in tests) forgets everything.
 *
 * Unlike ui-api, any non-empty bearer token is accepted: mock mode has no
 * token file. The halt also shows up in GET /api/ui/risk, like the real
 * halt file would.
 */
import { http, HttpResponse } from 'msw'

type Json = Record<string, unknown>
type Fixture = (request: Request, name: string) => Response

interface HaltState {
  halted: boolean
  reason?: string
  by?: string
  at?: string
}

interface Entry {
  id: string
  at: string
  action: string
  outcome: 'requested' | 'done' | 'failed' | 'refused' | 'denied'
  ledger: string
  by?: string
  reason?: string
  remote?: string
  user_agent?: string
  before?: HaltState
  after?: HaltState
  error?: string
}

const state = new Map<string, { halt: HaltState | null; audit: Entry[] }>()
let seq = 0

/** Forget every mock halt and audit line (tests call this between cases). */
export function resetMockControls() {
  state.clear()
  seq = 0
}

function ledgerState(ledger: string) {
  let s = state.get(ledger)
  if (!s) {
    s = { halt: null, audit: [] }
    state.set(ledger, s)
  }
  return s
}

function haltFileInfo(path: string, h: HaltState | null) {
  return {
    path,
    found: h !== null,
    halted: !!h?.halted,
    reason: h?.reason ?? '',
    by: h?.by ?? '',
    at: h?.at ?? '',
  }
}

const BY = /^[A-Za-z0-9][A-Za-z0-9 ._@-]{0,63}$/

function validate(body: Json, ledger: string): string | null {
  const keys = Object.keys(body)
  const unknown = keys.find((k) => !['reason', 'by', 'confirm'].includes(k))
  if (unknown) return `body: json: unknown field "${unknown}"`
  const reason = typeof body.reason === 'string' ? body.reason.trim() : ''
  const by = typeof body.by === 'string' ? body.by.trim() : ''
  if (reason.length < 8 || reason.length > 500) return 'reason must be 8 to 500 characters'
  if (/[\r\n]/.test(reason)) return 'reason must be one line'
  if (!BY.test(by)) return 'by must be 1 to 64 letters, digits, spaces or . _ @ -'
  if (body.confirm !== ledger) return `confirm must be the ledger name "${ledger}"`
  return null
}

export function controlHandlers(fixture: Fixture, defaultLedger: string) {
  const ledgerOf = (request: Request) => new URL(request.url).searchParams.get('ledger') || defaultLedger

  return [
    http.get('/api/ui/controls', async ({ request }) => {
      const res = fixture(request, 'controls')
      if (!res.ok) return res
      const info = (await res.json()) as Json & { halt_file: { path: string }; audit: Entry[] }
      const s = state.get(ledgerOf(request))
      if (!s) return HttpResponse.json(info)
      return HttpResponse.json({
        ...info,
        halt_file: s.halt ? haltFileInfo(info.halt_file.path, s.halt) : info.halt_file,
        audit: [...s.audit, ...info.audit].slice(0, 50),
      })
    }),

    http.get('/api/ui/risk', async ({ request }) => {
      const res = fixture(request, 'risk')
      if (!res.ok) return res
      const r = (await res.json()) as Json & {
        halted: boolean
        halt_reason: string
        halt_source: string
        halt_file: { path: string }
        policy: { halted?: boolean }
      }
      const h = state.get(ledgerOf(request))?.halt
      if (!h) return HttpResponse.json(r)
      const policy = !!r.policy.halted
      return HttpResponse.json({
        ...r,
        halted: policy || h.halted,
        halt_reason: h.halted ? (h.reason ?? '') : r.halt_reason,
        halt_source: h.halted ? (policy ? 'both' : 'file') : r.halt_source,
        halt_file: haltFileInfo(r.halt_file.path, h),
      })
    }),

    http.post('/api/ui/risk/:action', async ({ params, request }) => {
      const action = String(params.action)
      if (action !== 'halt' && action !== 'resume') {
        return HttpResponse.json({ error: 'not found' }, { status: 404 })
      }
      const ledger = ledgerOf(request)
      const info = fixture(request, 'controls')
      if (!info.ok) return info
      const { halt_file } = (await info.json()) as { halt_file: { path: string } }
      const s = ledgerState(ledger)
      const now = new Date().toISOString()
      const log = (e: Omit<Entry, 'id' | 'at' | 'action' | 'ledger'>): Entry => {
        const entry: Entry = { id: `mock-${++seq}`, at: now, action, ledger, user_agent: 'msw', ...e }
        s.audit.unshift(entry)
        return entry
      }
      const fail = (status: number, outcome: Entry['outcome'], error: string, extra: Partial<Entry> = {}) => {
        log({ outcome, error, ...extra })
        return HttpResponse.json({ error }, { status })
      }

      const auth = request.headers.get('Authorization') ?? ''
      if (!/^Bearer \S+$/.test(auth)) return fail(401, 'denied', 'missing or wrong operator token')
      if (!(request.headers.get('Content-Type') ?? '').startsWith('application/json')) {
        return fail(415, 'refused', 'send Content-Type: application/json')
      }
      let body: Json
      try {
        body = (await request.json()) as Json
      } catch {
        return fail(400, 'refused', 'body: invalid JSON')
      }
      const who = { by: String(body.by ?? '').trim(), reason: String(body.reason ?? '').trim() }
      const problem = validate(body, ledger)
      if (problem) return fail(400, 'refused', problem, who)

      const before: HaltState = s.halt ?? { halted: false }
      if (action === 'halt' && before.halted) {
        return fail(409, 'refused', `already halted by ${before.by} at ${before.at}: ${before.reason}`, who)
      }
      if (action === 'resume' && !before.halted) {
        return fail(409, 'refused', 'not halted by the halt file; nothing to resume', who)
      }
      const after: HaltState =
        action === 'halt' ? { halted: true, reason: who.reason, by: who.by, at: now } : { halted: false }
      log({ outcome: 'requested', ...who, before, after })
      s.halt = after
      const done = log({ outcome: 'done', ...who, before, after })
      return HttpResponse.json({ ledger, action, halt_file: haltFileInfo(halt_file.path, after), audit: done })
    }),
  ]
}
