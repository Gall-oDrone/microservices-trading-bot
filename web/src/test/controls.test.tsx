import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { beforeEach, describe, expect, it } from 'vitest'
import { controlProblems, getOperatorToken, setOperatorToken } from '../api/controls'
import { controlResponseSchema, controlsInfoSchema } from '../api/schemas'
import dryControls from '../mocks/fixtures/dry-run/controls.json'
import controls from '../mocks/fixtures/stage/controls.json'
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

const TOKEN = 'a'.repeat(64)

async function openDialog(action: 'halt' | 'resume' = 'halt') {
  const user = userEvent.setup()
  const btn = await screen.findByTestId(`control-${action}`)
  await user.click(btn)
  const dialog = await screen.findByTestId('control-dialog')
  return { user, dialog }
}

async function fill(
  user: ReturnType<typeof userEvent.setup>,
  v: { reason?: string; by?: string; confirm?: string; token?: string },
) {
  if (v.reason !== undefined) await user.type(screen.getByTestId('control-reason'), v.reason)
  if (v.by !== undefined) await user.type(screen.getByTestId('control-by'), v.by)
  if (v.confirm !== undefined) await user.type(screen.getByTestId('control-confirm'), v.confirm)
  if (v.token !== undefined) await user.type(screen.getByTestId('control-token'), v.token)
}

beforeEach(() => {
  sessionStorage.clear()
  localStorage.clear()
})

describe('contract: controls fixtures parse', () => {
  it.each([
    ['stage', controls],
    ['dry-run', dryControls],
  ])('%s controls', (_n, data) => {
    const r = controlsInfoSchema.safeParse(data)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
  })
})

describe('controlProblems', () => {
  const ok = { reason: 'Bitso incident, pausing', by: 'diego', confirm: 'stage', token: TOKEN }
  it('accepts a complete form', () => {
    expect(controlProblems(ok, 'stage')).toEqual([])
  })
  it('flags each field like ui-api does', () => {
    const fields = (i: typeof ok) => controlProblems(i, 'stage').map((p) => p.field)
    expect(fields({ ...ok, reason: 'short' })).toEqual(['reason'])
    expect(fields({ ...ok, reason: 'two\nlines here' })).toEqual(['reason'])
    expect(fields({ ...ok, by: '-bad' })).toEqual(['by'])
    expect(fields({ ...ok, by: 'x'.repeat(65) })).toEqual(['by'])
    expect(fields({ ...ok, confirm: 'Stage' })).toEqual(['confirm'])
    expect(fields({ ...ok, token: '  ' })).toEqual(['token'])
  })
})

describe('Operator controls on the Risk page', () => {
  it('shows the card armed, not halted, with an empty audit log', async () => {
    renderAt('/risk')
    const card = await screen.findByTestId('operator-controls')
    expect(card).toHaveAttribute('data-state', 'running')
    expect(within(card).getByText('armed · local token')).toBeInTheDocument()
    expect(within(card).getByTestId('controls-state')).toHaveTextContent('No operator halt')
    expect(within(card).getByTestId('control-halt')).toBeEnabled()
    expect(within(card).getByTestId('audit-empty')).toBeInTheDocument()
  })

  it('is read-only when ui-api has no operator token', async () => {
    const off = controlsInfoSchema.parse(structuredClone(controls))
    off.enabled = false
    off.disabled_reason = 'operator controls are off (start ui-api with -operator-token-file)'
    server.use(http.get('/api/ui/controls', () => HttpResponse.json(off)))
    renderAt('/risk')
    const card = await screen.findByTestId('operator-controls')
    expect(await within(card).findByTestId('controls-disabled')).toHaveTextContent('operator controls are off')
    expect(within(card).getByText('off')).toBeInTheDocument()
    expect(within(card).getByTestId('control-halt')).toBeDisabled()
  })

  it('validates the dialog and keeps submit from sending a bad form', async () => {
    let posts = 0
    server.use(
      http.post('/api/ui/risk/halt', () => {
        posts++
        return HttpResponse.json({ error: 'should not be called' }, { status: 500 })
      }),
    )
    renderAt('/risk')
    const { user, dialog } = await openDialog()
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByTestId('control-reason')).toHaveFocus()
    await fill(user, { reason: 'short', by: 'diego', confirm: 'stag' })
    await user.click(screen.getByTestId('control-submit'))
    expect(posts).toBe(0)
    expect(screen.getByTestId('control-reason')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByTestId('control-confirm')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByTestId('control-token')).toHaveAttribute('aria-invalid', 'true')
    expect(within(dialog).getByText('Type “stage” exactly.')).toBeInTheDocument()
    expect(screen.getByTestId('control-submit')).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByTestId('control-reason')).toHaveFocus() // first bad field
    // Escape closes it without sending anything.
    await user.keyboard('{Escape}')
    expect(screen.queryByTestId('control-dialog')).not.toBeInTheDocument()
    expect(posts).toBe(0)
  })

  it('halts, refreshes the card and the risk banner, audits, then resumes', async () => {
    let sent: { auth: string | null; body: unknown } | undefined
    server.events.on('request:start', async ({ request }) => {
      if (request.method === 'POST' && new URL(request.url).pathname === '/api/ui/risk/halt') {
        sent = { auth: request.headers.get('Authorization'), body: await request.clone().json() }
      }
    })
    renderAt('/risk')
    const { user } = await openDialog('halt')
    await fill(user, { reason: 'Bitso API incident, pausing', by: 'diego', confirm: 'stage', token: TOKEN })
    await user.click(screen.getByTestId('control-submit'))

    const flash = await screen.findByTestId('controls-flash')
    expect(flash).toHaveTextContent(/Halted stage/)
    expect(screen.queryByTestId('control-dialog')).not.toBeInTheDocument()
    expect(sent?.auth).toBe(`Bearer ${TOKEN}`)
    expect(sent?.body).toEqual({ reason: 'Bitso API incident, pausing', by: 'diego', confirm: 'stage' })
    server.events.removeAllListeners()

    // The halt shows everywhere: card state, the risk banner and the audit log.
    const card = screen.getByTestId('operator-controls')
    await waitFor(() => expect(card).toHaveAttribute('data-state', 'halted'))
    expect(within(card).getByTestId('controls-state')).toHaveTextContent('Halted by operator')
    expect(await screen.findByTestId('halt-file')).toHaveTextContent('Bitso API incident, pausing')
    const rows = within(card).getAllByTestId('audit-row')
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent('done')
    expect(rows[1]).toHaveTextContent('requested')
    expect(getOperatorToken()).toBe(TOKEN) // remembered for the tab
    expect(localStorage.getItem('mtb-operator-by')).toBe('diego')

    // Resume: token and name are prefilled.
    const r = await openDialog('resume')
    expect(screen.getByTestId('control-token')).toHaveValue(TOKEN)
    expect(screen.getByTestId('control-by')).toHaveValue('diego')
    expect(screen.getByTestId('control-current')).toHaveTextContent('Bitso API incident, pausing')
    await fill(r.user, { reason: 'Incident resolved, checks green', confirm: 'stage' })
    await r.user.click(screen.getByTestId('control-submit'))
    expect(await screen.findByTestId('controls-flash')).toHaveTextContent(/Resumed stage/)
    await waitFor(() => expect(card).toHaveAttribute('data-state', 'running'))
    expect(within(card).getAllByTestId('audit-row')).toHaveLength(4)
    await waitFor(() => expect(screen.queryByTestId('halt-file')).not.toBeInTheDocument())
  })

  it('a rejected token is cleared and the server error is shown', async () => {
    setOperatorToken('stale-token')
    server.use(
      http.post('/api/ui/risk/halt', () =>
        HttpResponse.json({ error: 'missing or wrong operator token' }, { status: 401 }),
      ),
    )
    renderAt('/risk')
    const { user } = await openDialog()
    expect(screen.getByTestId('control-token')).toHaveValue('stale-token')
    await fill(user, { reason: 'Testing the token gate', by: 'diego', confirm: 'stage' })
    await user.click(screen.getByTestId('control-submit'))
    const err = await screen.findByTestId('control-error')
    expect(err).toHaveTextContent('Token rejected. missing or wrong operator token')
    expect(screen.getByTestId('control-token')).toHaveValue('')
    expect(getOperatorToken()).toBe('')
    expect(screen.getByTestId('control-dialog')).toBeInTheDocument()
  })

  it('shows a 409 from ui-api and keeps the dialog open', async () => {
    server.use(
      http.post('/api/ui/risk/halt', () =>
        HttpResponse.json({ error: 'already halted by ops at 2026-10-08T06:00:00Z: drill' }, { status: 409 }),
      ),
    )
    renderAt('/risk')
    const { user } = await openDialog()
    await fill(user, { reason: 'Second halt attempt', by: 'diego', confirm: 'stage', token: TOKEN })
    await user.click(screen.getByTestId('control-submit'))
    expect(await screen.findByTestId('control-error')).toHaveTextContent('Nothing changed. already halted by ops')
  })

  it('the mock mirrors ui-api: a wrong confirm is refused and audited', async () => {
    const res = await fetch(location.origin + '/api/ui/risk/halt?ledger=dry-run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${TOKEN}` },
      body: JSON.stringify({ reason: 'Testing confirm', by: 'diego', confirm: 'stage' }),
    })
    expect(res.status).toBe(400)
    const info = controlsInfoSchema.parse(
      await (await fetch(location.origin + '/api/ui/controls?ledger=dry-run')).json(),
    )
    expect(info.audit[0]).toMatchObject({ outcome: 'refused', ledger: 'dry-run', action: 'halt' })
    const ok = await fetch(location.origin + '/api/ui/risk/halt?ledger=dry-run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${TOKEN}` },
      body: JSON.stringify({ reason: 'Testing confirm', by: 'diego', confirm: 'dry-run' }),
    })
    expect(ok.status).toBe(200)
    expect(controlResponseSchema.parse(await ok.json()).halt_file.halted).toBe(true)
  })
})
