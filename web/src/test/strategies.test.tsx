import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { beforeEach, describe, expect, it } from 'vitest'
import { getOperatorToken } from '../api/controls'
import { controlsInfoSchema, strategiesInfoSchema } from '../api/schemas'
import strategies from '../mocks/fixtures/strategies.json'
import { patchMockStrategy } from '../mocks/strategies'
import { makeQueryClient, routes } from '../router'
import { server } from './setup'

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  const qc = makeQueryClient()
  qc.setDefaultOptions({ queries: { retry: false } })
  return render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

const TOKEN = 'b'.repeat(64)

type User = ReturnType<typeof userEvent.setup>

async function open(testId: string) {
  const user = userEvent.setup()
  await user.click(await screen.findByTestId(testId))
  const dialog = await screen.findByTestId('control-dialog')
  return { user, dialog }
}

async function fill(user: User, v: { reason?: string; by?: string; confirm?: string; token?: string }) {
  if (v.reason !== undefined) await user.type(screen.getByTestId('control-reason'), v.reason)
  if (v.by !== undefined) await user.type(screen.getByTestId('control-by'), v.by)
  if (v.confirm !== undefined) await user.type(screen.getByTestId('control-confirm'), v.confirm)
  if (v.token !== undefined) await user.type(screen.getByTestId('control-token'), v.token)
}

function capturePosts(path: string) {
  const sent: { auth: string | null; body: unknown }[] = []
  server.events.on('request:start', async ({ request }) => {
    if (request.method === 'POST' && new URL(request.url).pathname === path) {
      sent.push({ auth: request.headers.get('Authorization'), body: await request.clone().json() })
    }
  })
  return sent
}

function info() {
  return strategiesInfoSchema.parse(structuredClone(strategies))
}

beforeEach(() => {
  sessionStorage.clear()
  localStorage.clear()
  server.events.removeAllListeners()
})

describe('contract: strategies fixture parses', () => {
  it('GET /api/ui/strategies', () => {
    const r = strategiesInfoSchema.safeParse(strategies)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
  })
})

describe('Strategies page', () => {
  it('lists the ledgers, the kill switch and the strategies with their state', async () => {
    renderAt('/strategies')
    expect(await screen.findByRole('heading', { level: 1, name: 'Strategies' })).toBeInTheDocument()
    expect(document.title).toMatch(/Strategies/)

    const stage = await screen.findByTestId('ledger-card-stage')
    expect(stage).toHaveAttribute('data-state', 'running')
    expect(within(stage).getByText('trading allowed')).toBeInTheDocument()
    expect(screen.getByTestId('ledger-card-dry-run')).toHaveAttribute('data-state', 'running')
    expect(screen.getByTestId('killswitch-count')).toHaveTextContent('2 of 2 to halt')
    expect(screen.getByTestId('kill-switch-open')).toBeEnabled()

    const card = screen.getByTestId('strategies-card')
    expect(within(card).getByText('armed · local token')).toBeInTheDocument()
    const lp = screen.getByTestId('strategy-row-lp_btc_demo')
    expect(within(lp).getByText('running')).toBeInTheDocument()
    expect(within(lp).getByText('dry-run')).toBeInTheDocument()
    expect(within(lp).getByTestId('strategy-stop-lp_btc_demo')).toBeEnabled()
    const mr = screen.getByTestId('strategy-row-mr_btc_demo')
    expect(mr).toHaveClass('is-held')
    expect(within(mr).getByText('held')).toBeInTheDocument()
    expect(within(mr).getByText(/live verification of hold/)).toBeInTheDocument()
    expect(within(mr).getByTestId('strategy-start-mr_btc_demo')).toBeEnabled()
    expect(within(screen.getByTestId('strategy-row-mom_eth_demo')).getByText('stopped')).toBeInTheDocument()
    expect(within(card).getByTestId('audit-empty')).toBeInTheDocument()
  })

  it('links each ledger card to its Risk page', async () => {
    renderAt('/strategies')
    const links = await screen.findAllByRole('link', { name: /Open the Risk page/ })
    expect(links.map((a) => a.getAttribute('href'))).toEqual(['/risk?ledger=stage', '/risk?ledger=dry-run'])
  })

  it('stops a running strategy: the confirm is checked, the hold shows and both audit lines appear', async () => {
    const sent = capturePosts('/api/ui/strategies/lp_btc_demo/stop')
    renderAt('/strategies')
    const { user, dialog } = await open('strategy-stop-lp_btc_demo')
    expect(dialog).toHaveTextContent('Stop and hold strategy')
    expect(screen.queryByTestId('control-ack')).not.toBeInTheDocument() // flat: no acknowledgement
    await fill(user, { reason: 'Fee drift, pausing to look', by: 'diego', confirm: 'lp_btc', token: TOKEN })
    await user.click(screen.getByTestId('control-submit'))
    expect(sent).toHaveLength(0)
    expect(within(dialog).getByText('Type “lp_btc_demo” exactly.')).toBeInTheDocument()

    await user.type(screen.getByTestId('control-confirm'), '_demo')
    await user.click(screen.getByTestId('control-submit'))
    expect(await screen.findByTestId('strategy-flash')).toHaveTextContent(/Stopped and held lp_btc_demo/)
    expect(sent[0]).toEqual({
      auth: `Bearer ${TOKEN}`,
      body: { reason: 'Fee drift, pausing to look', by: 'diego', confirm: 'lp_btc_demo' },
    })
    expect(getOperatorToken()).toBe(TOKEN)

    const row = screen.getByTestId('strategy-row-lp_btc_demo')
    await waitFor(() => expect(within(row).getByText('held')).toBeInTheDocument())
    expect(within(row).getByTestId('strategy-start-lp_btc_demo')).toBeInTheDocument()
    const rows = within(screen.getByTestId('strategies-card')).getAllByTestId('audit-row')
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent('done')
    expect(rows[0]).toHaveTextContent('lp_btc_demo')
    expect(rows[1]).toHaveTextContent('requested')
  })

  it('needs the acknowledgement to stop a strategy with an open position', async () => {
    patchMockStrategy('lp_btc_demo', { has_position: true, position_side: 'long', position_size: 0.002 })
    const sent = capturePosts('/api/ui/strategies/lp_btc_demo/stop')
    renderAt('/strategies')
    const row = await screen.findByTestId('strategy-row-lp_btc_demo')
    expect(within(row).getByText('long 0.002')).toBeInTheDocument()

    const { user } = await open('strategy-stop-lp_btc_demo')
    expect(screen.getByTestId('control-current')).toHaveTextContent('Open exposure: long 0.002')
    await fill(user, { reason: 'Stopping despite the position', by: 'diego', confirm: 'lp_btc_demo', token: TOKEN })
    await user.click(screen.getByTestId('control-submit'))
    expect(sent).toHaveLength(0)
    expect(screen.getByTestId('control-ack')).toHaveAttribute('aria-invalid', 'true')

    await user.click(screen.getByTestId('control-ack'))
    await user.click(screen.getByTestId('control-submit'))
    expect(await screen.findByTestId('strategy-flash')).toHaveTextContent(/Stopped and held lp_btc_demo/)
    expect(sent[0].body).toMatchObject({ ack_position: true })
    const done = within(screen.getByTestId('strategies-card')).getAllByTestId('audit-row')[0]
    expect(done).toHaveTextContent(/acknowledged/)
  })

  it('starts a held strategy, which releases the hold', async () => {
    renderAt('/strategies')
    const { user } = await open('strategy-start-mr_btc_demo')
    expect(screen.getByTestId('control-dialog')).toHaveTextContent('Releases the operator hold and starts')
    expect(screen.getByTestId('control-current')).toHaveTextContent('live verification of hold')
    await fill(user, { reason: 'Hold reviewed, starting', by: 'diego', confirm: 'mr_btc_demo', token: TOKEN })
    await user.click(screen.getByTestId('control-submit'))
    expect(await screen.findByTestId('strategy-flash')).toHaveTextContent(/Started mr_btc_demo/)
    const row = screen.getByTestId('strategy-row-mr_btc_demo')
    await waitFor(() => expect(within(row).getByText('running')).toBeInTheDocument())
    expect(row).not.toHaveClass('is-held')
  })

  it('shows a 409 from the executor and keeps the dialog open', async () => {
    server.use(
      http.post('/api/ui/strategies/:name/start', () =>
        HttpResponse.json({ error: 'strategy-executor: 409 held: strategy is held' }, { status: 409 }),
      ),
    )
    renderAt('/strategies')
    const { user } = await open('strategy-start-mom_eth_demo')
    await fill(user, { reason: 'Starting the momentum demo', by: 'diego', confirm: 'mom_eth_demo', token: TOKEN })
    await user.click(screen.getByTestId('control-submit'))
    expect(await screen.findByTestId('control-error')).toHaveTextContent('Nothing changed. strategy-executor: 409')
    expect(screen.getByTestId('control-dialog')).toBeInTheDocument()
  })

  it('the kill switch needs the phrase, halts every ledger and shows it on the Risk page', async () => {
    const sent = capturePosts('/api/ui/risk/halt-all')
    const { unmount } = renderAt('/strategies')
    const { user, dialog } = await open('kill-switch-open')
    expect(dialog).toHaveTextContent('Halt all ledgers')
    await fill(user, { reason: 'Exchange incident, stopping all', by: 'diego', confirm: 'halt all', token: TOKEN })
    await user.click(screen.getByTestId('control-submit'))
    expect(sent).toHaveLength(0)
    expect(within(dialog).getByText('Type “HALT ALL” exactly.')).toBeInTheDocument()

    await user.clear(screen.getByTestId('control-confirm'))
    await user.type(screen.getByTestId('control-confirm'), 'HALT ALL')
    expect(screen.getByTestId('control-submit')).toHaveTextContent('Halt 2 ledgers')
    await user.click(screen.getByTestId('control-submit'))
    const flash = await screen.findByTestId('halt-all-flash')
    expect(flash).toHaveTextContent(/stage halted/)
    expect(flash).toHaveTextContent(/dry-run halted/)
    expect(sent[0].body).toEqual({ reason: 'Exchange incident, stopping all', by: 'diego', confirm: 'HALT ALL' })

    await waitFor(() => expect(screen.getByTestId('ledger-card-stage')).toHaveAttribute('data-state', 'halted'))
    expect(screen.getByTestId('ledger-card-dry-run')).toHaveAttribute('data-state', 'halted')
    expect(screen.getByTestId('killswitch-count')).toHaveTextContent('all halted')
    expect(screen.getByTestId('kill-switch-open')).toBeDisabled()
    expect(screen.getAllByRole('link', { name: /Resume on the Risk page/ })).toHaveLength(2)

    // Each ledger's own audit log has the grouped halt.
    const c = controlsInfoSchema.parse(await (await fetch(location.origin + '/api/ui/controls?ledger=stage')).json())
    expect(c.halt_file.halted).toBe(true)
    expect(c.audit[0]).toMatchObject({ action: 'halt_all', outcome: 'done', ledger: 'stage', by: 'diego' })
    expect(c.audit[0].group).toBeTruthy()
    unmount()
  })

  it('is read-only without a token, and explains a missing executor', async () => {
    const off = info()
    off.kill_switch.enabled = false
    off.kill_switch.disabled_reason = 'controls are off: start ui-api with -operator-token-file'
    off.executor.controls_enabled = false
    off.executor.disabled_reason = 'controls are off: start ui-api with -operator-token-file'
    server.use(http.get('/api/ui/strategies', () => HttpResponse.json(off)))
    const { unmount } = renderAt('/strategies')
    expect(await screen.findByTestId('killswitch-disabled')).toHaveTextContent('controls are off')
    expect(screen.getByTestId('kill-switch-open')).toBeDisabled()
    expect(screen.getByTestId('executor-readonly')).toHaveTextContent('-operator-token-file')
    expect(screen.getByTestId('strategy-stop-lp_btc_demo')).toBeDisabled()
    expect(screen.getByText('read-only')).toBeInTheDocument()
    unmount()

    const none = info()
    none.executor = {
      configured: false,
      reachable: false,
      controls_enabled: false,
      disabled_reason: 'no strategy-executor',
      holds_supported: false,
    }
    none.strategies = []
    server.use(http.get('/api/ui/strategies', () => HttpResponse.json(none)))
    renderAt('/strategies')
    expect(await screen.findByTestId('executor-off')).toHaveTextContent('-strategy-executor-url')
    expect(screen.queryByTestId('strategies-table')).not.toBeInTheDocument()
    expect(screen.getByText('not connected')).toBeInTheDocument()
  })

  it('reports an unreachable executor and an old one without holds', async () => {
    const down = info()
    down.executor.reachable = false
    down.executor.controls_enabled = false
    down.executor.disabled_reason = 'strategy-executor is unreachable'
    down.executor.error = 'dial tcp 127.0.0.1:18081: connect: connection refused'
    down.strategies = []
    server.use(http.get('/api/ui/strategies', () => HttpResponse.json(down)))
    const { unmount } = renderAt('/strategies')
    expect(await screen.findByText('strategy-executor did not answer')).toBeInTheDocument()
    expect(screen.getByText(/connection refused/)).toBeInTheDocument()
    expect(screen.getByText('unreachable')).toBeInTheDocument()
    unmount()

    const old = info()
    old.executor.holds_supported = false
    old.holds = [{ name: 'gone_strategy', reason: 'paused for review', by: 'ops', at: '2026-10-08T12:00:00Z' }]
    server.use(http.get('/api/ui/strategies', () => HttpResponse.json(old)))
    renderAt('/strategies')
    expect(await screen.findByText('This strategy-executor has no hold list')).toBeInTheDocument()
    expect(screen.getByTestId('orphan-holds')).toHaveTextContent('gone_strategy')
  })

  it('the mock mirrors ui-api: a wrong confirm and a held stop are refused and audited', async () => {
    const post = (name: string, action: string, body: unknown) =>
      fetch(`${location.origin}/api/ui/strategies/${name}/${action}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${TOKEN}` },
        body: JSON.stringify(body),
      })
    const bad = await post('lp_btc_demo', 'stop', { reason: 'Testing confirm', by: 'diego', confirm: 'lp' })
    expect(bad.status).toBe(400)
    expect(((await bad.json()) as { error: string }).error).toBe(
      'confirm: type the strategy name "lp_btc_demo" to confirm',
    )
    const held = await post('mr_btc_demo', 'stop', { reason: 'Testing held', by: 'diego', confirm: 'mr_btc_demo' })
    expect(held.status).toBe(409)
    const unknown = await post('nope', 'start', { reason: 'Testing unknown', by: 'diego', confirm: 'nope' })
    expect(unknown.status).toBe(404)
    const noToken = await fetch(`${location.origin}/api/ui/risk/halt-all`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    })
    expect(noToken.status).toBe(401)
    const i = strategiesInfoSchema.parse(await (await fetch(location.origin + '/api/ui/strategies')).json())
    expect(i.audit.map((e) => [e.strategy, e.outcome])).toEqual([
      ['nope', 'refused'],
      ['mr_btc_demo', 'refused'],
      ['lp_btc_demo', 'refused'],
    ])
  })
})
