/**
 * U1 quick wins (2026-10-20): the attention bar, the chart label rule, the
 * Positions filter and protect link, the Trade page prefill and the
 * persisted outcome, the ledger table keys.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { beforeEach, describe, expect, it } from 'vitest'
import { attentionItems } from '../lib/attention'
import { labelledFills } from '../lib/chartlabels'
import health from '../mocks/fixtures/etoro-demo/etoro-health.json'
import outcome from '../mocks/fixtures/etoro-demo/etoro-trade-outcome.json'
import { resetMockTrades } from '../mocks/trades'
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

beforeEach(() => {
  sessionStorage.clear()
  localStorage.clear()
  resetMockTrades()
})

describe('attentionItems', () => {
  const checks = [
    { id: 'a', area: 'executor' as const, label: 'A', status: 'ok' as const, message: 'fine' },
    { id: 'b', area: 'executor' as const, label: 'B', status: 'warn' as const, message: 'hmm' },
    { id: 'c', area: 'executor' as const, label: 'C', status: 'fail' as const, message: 'bad' },
    { id: 'd', area: 'executor' as const, label: 'D', status: 'off' as const, message: 'not configured' },
  ]
  it('lists the halt first, then failed checks, then warnings; ok and off never', () => {
    const items = attentionItems({ halted: true, reason: 'drill', by: 'diego' }, checks, '?ledger=x')
    expect(items.map((i) => [i.id, i.tone])).toEqual([
      ['halt', 'block'],
      ['c', 'block'],
      ['b', 'warn'],
    ])
    expect(items[0].message).toBe('drill · by diego')
    expect(items[0].to).toBe('/risk?ledger=x')
    expect(items[1].to).toBe('/data-health?ledger=x')
  })
  it('is empty when nothing needs the operator', () => {
    expect(attentionItems({ halted: false }, checks.slice(0, 1), '')).toEqual([])
    expect(attentionItems(null, undefined, '')).toEqual([])
  })
})

describe('labelledFills', () => {
  const dates = Array.from({ length: 30 }, (_, i) => `2026-10-${String(i + 1).padStart(2, '0')}`)
  it('labels a lone fill and the last of a cluster only', () => {
    const fills = [
      { fill_date: '2026-10-02' },
      { fill_date: '2026-10-05' }, // 3 bars after the first: the first loses its label
      { fill_date: '2026-10-15' }, // alone: the next fill is 13 bars away
      { fill_date: '2026-10-28' },
      { fill_date: '2026-10-29' },
    ]
    expect([...labelledFills(fills, dates)].sort()).toEqual([1, 2, 4])
  })
  it('never labels a fill outside the chart', () => {
    expect(labelledFills([{ fill_date: '2025-01-01' }], dates).size).toBe(0)
  })
})

describe('attention bar', () => {
  it('is absent when all is well and shows the halt and the warning checks when not', async () => {
    renderAt('/forward-tests?ledger=etoro-demo')
    await screen.findByText(/SMA50 forward test/)
    expect(screen.queryByTestId('attention-bar')).toBeNull()
  })
  it('renders a failed check as a block with a link to Data health', async () => {
    server.use(
      http.get('/api/ui/etoro/health', () =>
        HttpResponse.json({
          ...health,
          status: 'fail',
          checks: [
            ...health.checks,
            { id: 'etoro.intents', area: 'executor', label: 'Intents', status: 'fail', message: '1 unfinished intent' },
            {
              id: 'etoro.protection',
              area: 'executor',
              label: 'Protection',
              status: 'warn',
              message: 'held without a stop',
            },
          ],
        }),
      ),
    )
    renderAt('/forward-tests?ledger=etoro-demo')
    const bar = await screen.findByTestId('attention-bar')
    expect(bar).toHaveClass('block')
    const items = within(bar).getAllByRole('listitem')
    expect(items[0]).toHaveTextContent('Intents')
    expect(items[0]).toHaveTextContent('1 unfinished intent')
    expect(items[1]).toHaveTextContent('Protection')
    expect(within(items[0]).getByRole('link')).toHaveAttribute('href', '/data-health?ledger=etoro-demo')
  })
})

describe('Positions card (U1)', () => {
  it('filters open / closed and links an unprotected open row to a pre-filled protect form', async () => {
    renderAt('/trade?ledger=etoro-demo')
    const card = await screen.findByTestId('positions-card')
    await within(card).findByTestId('position-3616683295')
    await userEvent.click(within(card).getByTestId('positions-filter-closed'))
    expect(within(card).queryByTestId('position-3616683295')).toBeNull()
    expect(within(card).getByTestId('position-3616683146')).toBeInTheDocument()
    await userEvent.click(within(card).getByTestId('positions-filter-open'))
    const protect = within(card).getByTestId('position-protect-3616683295')
    expect(protect).toHaveAttribute('href', '/trade?ledger=etoro-demo&book=spx500&side=protect&position=3616683295')

    // The row toggles from the keyboard.
    const row = within(card).getByTestId('position-3616683295')
    row.focus()
    await userEvent.keyboard('{Enter}')
    expect(within(card).getByTestId('position-details-3616683295')).toBeInTheDocument()
    await userEvent.keyboard(' ')
    expect(within(card).queryByTestId('position-details-3616683295')).toBeNull()

    // Following the link lands on the Trade form with book, side and position set.
    await userEvent.click(protect)
    await waitFor(() => expect(screen.getByTestId('trade-side-protect')).toHaveAttribute('aria-pressed', 'true'))
    expect(screen.getByTestId('trade-book')).toHaveValue('spx500')
    expect(screen.getByTestId('trade-position')).toHaveValue('3616683295')
  })
})

describe('Trade page outcome persistence', () => {
  it('restores the last outcome of the session and forgets it on Dismiss', async () => {
    sessionStorage.setItem('trade-outcome:etoro-demo', JSON.stringify(outcome))
    renderAt('/trade?ledger=etoro-demo')
    const card = await screen.findByTestId('trade-outcome')
    expect(card).toHaveTextContent(outcome.client_ref)
    await userEvent.click(within(card).getByTestId('trade-outcome-dismiss'))
    expect(screen.queryByTestId('trade-outcome')).toBeNull()
    expect(sessionStorage.getItem('trade-outcome:etoro-demo')).toBeNull()
  })
  it('ignores an unparsable stored outcome', async () => {
    sessionStorage.setItem('trade-outcome:etoro-demo', '{not json')
    renderAt('/trade?ledger=etoro-demo')
    await screen.findByTestId('positions-card')
    expect(screen.queryByTestId('trade-outcome')).toBeNull()
  })
})

describe('ledger table', () => {
  it('keeps a console line and the run line of the same bar as separate rows', async () => {
    renderAt('/forward-tests/nsdq100?ledger=etoro-demo')
    const table = await screen.findByTestId('etoro-records')
    const rows = within(table).getAllByTestId(/^etoro-record-/)
    expect(rows.length).toBeGreaterThan(0)
    expect(within(table).queryByText('Recorded (NY)')).toBeNull()
    expect(within(table).getByText('Bar → fill')).toBeInTheDocument()
  })
})
