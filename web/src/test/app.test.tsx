import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it } from 'vitest'
import { makeQueryClient, routes } from '../router'
import {
  candlesResponseSchema,
  forwardTestsResponseSchema,
  healthSchema,
  ledgerResponseSchema,
  ledgersResponseSchema,
  riskResponseSchema,
} from '../api/schemas'
import dryForwardTests from '../mocks/fixtures/dry-run/forward-tests.json'
import dryRisk from '../mocks/fixtures/dry-run/risk.json'
import healthz from '../mocks/fixtures/healthz.json'
import ledgers from '../mocks/fixtures/ledgers.json'
import candlesMxn from '../mocks/fixtures/stage/candles-btc_mxn.json'
import forwardTests from '../mocks/fixtures/stage/forward-tests.json'
import ledgerMxn from '../mocks/fixtures/stage/ledger-btc_mxn.json'
import ledgerUsd from '../mocks/fixtures/stage/ledger-btc_usd.json'
import risk from '../mocks/fixtures/stage/risk.json'
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

describe('contract: Go ui-api responses parse with the UI schemas', () => {
  it.each([
    ['forward-tests', forwardTestsResponseSchema, forwardTests],
    ['risk', riskResponseSchema, risk],
    ['ledger btc_mxn', ledgerResponseSchema, ledgerMxn],
    ['ledger btc_usd', ledgerResponseSchema, ledgerUsd],
    ['candles btc_mxn', candlesResponseSchema, candlesMxn],
    ['healthz', healthSchema, healthz],
    ['ledgers', ledgersResponseSchema, ledgers],
    ['dry-run forward-tests', forwardTestsResponseSchema, dryForwardTests],
    ['dry-run risk', riskResponseSchema, dryRisk],
  ])('%s', (_name, schema, data) => {
    const r = schema.safeParse(data)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
  })
})

describe('Forward tests page', () => {
  it('renders one card per book with signal, paper vs hold and run status', async () => {
    renderAt('/')
    const mxn = await screen.findByTestId('ft-card-btc_mxn')
    expect(within(mxn).getByRole('heading', { name: 'BTC / MXN' })).toBeInTheDocument()
    expect(within(mxn).getByTestId('signal-pill')).toHaveTextContent(/long/i)
    expect(within(mxn).getByText('vs buy-and-hold')).toBeInTheDocument()
    expect(within(mxn).getByText('after 70 bps/leg')).toBeInTheDocument()
    expect(within(mxn).getByText(/stage 0\.00099999 BTC/)).toBeInTheDocument()
    expect(within(mxn).getByText('stage')).toBeInTheDocument() // ledger badge
    expect(screen.getByTestId('ft-card-btc_usd')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('warns when the executor missed a day', async () => {
    const ft = forwardTestsResponseSchema.parse(structuredClone(forwardTests))
    ft.books[0].run = {
      status: 'missed',
      expected_bar_date: '2026-10-05',
      last_bar_date: '2026-10-03',
      missing_days: ['2026-10-04', '2026-10-05'],
      message: '2 closed days not recorded',
    }
    server.use(http.get('/api/ui/forward-tests', () => HttpResponse.json(ft)))
    renderAt('/')
    expect(await screen.findByRole('alert')).toHaveTextContent(/missed a day/i)
    expect(screen.getByRole('alert')).toHaveTextContent('2026-10-04, 2026-10-05')
  })

  it('shows a readable error when the response breaks the contract', async () => {
    server.use(http.get('/api/ui/forward-tests', () => HttpResponse.json({ generated_at: 'x', books: [{}] })))
    renderAt('/')
    expect(await screen.findByText(/does not match the UI contract/)).toBeInTheDocument()
  })

  it('shows the API error message', async () => {
    server.use(
      http.get('/api/ui/forward-tests', () =>
        HttpResponse.json({ error: 'cannot read the ledger: boom' }, { status: 500 }),
      ),
    )
    renderAt('/')
    expect(await screen.findByText('cannot read the ledger: boom')).toBeInTheDocument()
  })
})

describe('Ledger picker', () => {
  it('lists the ledgers and switches every view to the chosen one', async () => {
    const { container } = renderAt('/')
    const picker = (await screen.findByRole('combobox', { name: /ledger/i })) as HTMLSelectElement
    expect(
      within(picker)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual([expect.stringMatching(/^stage/), expect.stringMatching(/^dry-run · no file yet/)])
    await screen.findByTestId('ft-card-btc_mxn')
    await userEvent.selectOptions(picker, 'dry-run')
    expect(await screen.findAllByText('No records in the dry-run ledger yet')).toHaveLength(2)
    expect(screen.getAllByText('dry-run').length).toBeGreaterThan(0)
    // In-app links keep the selection.
    const risk = container.querySelector('#nav-risk') as HTMLAnchorElement
    expect(risk.getAttribute('href')).toBe('/risk?ledger=dry-run')
  })

  it('reads the ledger from the URL', async () => {
    renderAt('/risk?ledger=dry-run')
    expect(await screen.findByText('Enforced')).toBeInTheDocument()
    expect(screen.getAllByTitle('Ledger: dry-run').length).toBeGreaterThan(0)
  })

  it('shows the API error for an unknown ledger', async () => {
    renderAt('/?ledger=nope')
    expect(await screen.findByText('unknown ledger nope')).toBeInTheDocument()
  })
})

describe('Forward test detail page', () => {
  it('renders stage fills with realized cost vs assumption, and the ledger', async () => {
    renderAt('/forward-tests/btc_mxn')
    const table = await screen.findByText('Fill day')
    const t = table.closest('table')!
    expect(within(t).getByText('fallback')).toBeInTheDocument()
    expect(within(t).getByText('78 bps')).toBeInTheDocument() // fee
    expect(within(t).getByText('+40 bps')).toBeInTheDocument() // slippage vs open
    expect(within(t).getByText('118 bps')).toHaveClass('neg') // above the 70 bps assumption
    expect(await screen.findByText('4 records, newest first')).toBeInTheDocument()
  })
})

describe('Risk page', () => {
  it('shows enforced status, per-book exposure and the policy', async () => {
    renderAt('/risk')
    expect(await screen.findByText('Enforced')).toBeInTheDocument()
    const mxn = screen.getByTestId('risk-card-btc_mxn')
    expect(within(mxn).getByText('118 bps')).toBeInTheDocument()
    expect(within(mxn).getByText(/No order due/)).toBeInTheDocument()
    expect(within(mxn).getByTestId('last-check-btc_mxn')).toHaveTextContent(/No executor check recorded yet/)
    expect(screen.getByText('Max order size')).toBeInTheDocument()
    expect(screen.queryByText('Trading halted by policy')).not.toBeInTheDocument()
  })

  it('shows an order the executor blocked', async () => {
    const r = riskResponseSchema.parse(structuredClone(risk))
    const b = r.books[1]
    b.last_check = {
      bar_date: '2026-10-04',
      fill_date: '2026-10-05',
      policy_version: 'default-2026-10-03',
      order: { book: 'btc_usd', side: 'buy', qty_btc: 0.001, price: 100000, ref_price: 85944 },
      state: { position_btc: 0, orders_today: 0 },
      allowed: false,
      findings: [
        {
          rule: 'max_price_deviation_bps',
          severity: 'block',
          limit: 1500,
          value: 1635,
          message: 'price 100000.00 is 1635 bps from reference 85944.00 (max 1500)',
        },
      ],
    }
    b.blocked_days = ['2026-10-04']
    server.use(http.get('/api/ui/risk', () => HttpResponse.json(r)))
    renderAt('/risk')
    const panel = await screen.findByTestId('last-check-btc_usd')
    expect(within(panel).getByText('blocked, not sent')).toBeInTheDocument()
    expect(within(panel).getByText(/1635 bps from reference/)).toBeInTheDocument()
    expect(within(panel).getByText(/Blocked days:/)).toBeInTheDocument()
  })

  it('surfaces a halt and a blocked next order', async () => {
    const halted = riskResponseSchema.parse(structuredClone(risk))
    halted.halted = true
    halted.halt_source = 'policy'
    halted.halt_reason = 'exchange incident'
    halted.blocks = 1
    const b = halted.books[1]
    const block = {
      rule: 'halted',
      severity: 'block' as const,
      limit: 0,
      value: 1,
      message: 'trading is halted: exchange incident',
    }
    b.next_order = {
      action: 'buy',
      qty_btc: 0.001,
      ref_price: 85944,
      fill_date: '2026-10-02',
      decision: { allowed: false, findings: [block] },
    }
    b.findings = [block, ...b.findings]
    server.use(http.get('/api/ui/risk', () => HttpResponse.json(halted)))
    renderAt('/risk')
    expect(await screen.findByText('Trading halted by policy')).toBeInTheDocument()
    const usd = screen.getByTestId('risk-card-btc_usd')
    expect(within(usd).getByText('would be blocked')).toBeInTheDocument()
    expect(within(usd).getAllByText('trading is halted: exchange incident').length).toBeGreaterThan(0)
  })

  it('shows who halted trading through the halt file, when and why', async () => {
    const r = riskResponseSchema.parse(structuredClone(risk))
    r.halted = true
    r.halt_source = 'file'
    r.halt_reason = 'exchange incident (halt file, by diego at 2026-10-06T01:00:00Z)'
    r.halt_file = {
      ...r.halt_file,
      found: true,
      halted: true,
      reason: 'exchange incident',
      by: 'diego',
      at: '2026-10-06T01:00:00Z',
    }
    server.use(http.get('/api/ui/risk', () => HttpResponse.json(r)))
    renderAt('/risk')
    expect(await screen.findByText('Trading halted by operator')).toBeInTheDocument()
    const who = screen.getByTestId('halt-file')
    expect(who).toHaveTextContent('exchange incident')
    expect(who).toHaveTextContent('by diego')
    expect(who).toHaveTextContent('risk-state.json')
    expect(screen.getByText('source: halt file')).toBeInTheDocument()
  })

  it('warns that an invalid halt file stops the executor', async () => {
    const r = riskResponseSchema.parse(structuredClone(risk))
    r.halt_file = { ...r.halt_file, found: true, error: 'halt file x: halted needs reason, by, at' }
    server.use(http.get('/api/ui/risk', () => HttpResponse.json(r)))
    renderAt('/risk')
    expect(await screen.findByText('Halt file is invalid: the executor will not run')).toBeInTheDocument()
  })
})
