/**
 * Positions & round trips: the schema against the fixture, the P&L picker,
 * the mock endpoint following a console close, and the card on the Trade
 * page and the book page.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { beforeEach, describe, expect, it } from 'vitest'
import { etoroPositionsResponseSchema, tradePnL, type EtoroTrade } from '../api/etoroSchemas'
import { placeTrade, prepareTrade } from '../api/trades'
import positions from '../mocks/fixtures/etoro-demo/etoro-positions.json'
import { mockPositions, resetMockTrades } from '../mocks/trades'
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

beforeEach(() => {
  sessionStorage.clear()
  resetMockTrades()
})

describe('contract: positions & round trips', () => {
  it('the fixture parses and reads as one open rule position and one closed round trip', () => {
    const r = etoroPositionsResponseSchema.safeParse(positions)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues.slice(0, 3))).toBe(true)
    const p = etoroPositionsResponseSchema.parse(positions)
    expect(p.live).toBe(true)
    expect(p.summary).toMatchObject({ open: 1, closed: 1, realized_confirmed: 1 })
    const open = p.trades.find((t) => t.status === 'open')!
    expect(open.position_id).toBe('3616683295')
    expect(open.mark_source).toBe('account')
    const closed = p.trades.find((t) => t.status === 'closed')!
    expect(closed.closed_by).toBe('rule')
    expect(closed.net).toBeCloseTo(-35.34, 6)
  })

  it('tradePnL names its source: eToro net, gross from the fills, unrealised, or nothing', () => {
    const p = etoroPositionsResponseSchema.parse(positions)
    const open = p.trades.find((t) => t.status === 'open')!
    const closed = p.trades.find((t) => t.status === 'closed')!
    expect(tradePnL(open)).toEqual({ value: -23.49, kind: 'unrealized' })
    expect(tradePnL(closed)).toEqual({ value: -35.34, kind: 'net' })
    const grossOnly: EtoroTrade = {
      ...closed,
      net: undefined,
      net_profit: undefined,
      fees: undefined,
      venue_confirmed: false,
    }
    expect(tradePnL(grossOnly)).toEqual({ value: -34.24, kind: 'gross' })
    const ledgerOnly: EtoroTrade = { ...open, mark_source: undefined, unrealized_pnl: undefined }
    expect(tradePnL(ledgerOnly)).toBeNull()
    expect(tradePnL({ ...open, status: 'unknown' })).toBeNull()
  })
})

describe('mock: GET /api/ui/etoro/positions', () => {
  it('answers the rows, filters by book, and follows a console close with the operator and the gross', async () => {
    const all = etoroPositionsResponseSchema.parse(
      await (await fetch('/api/ui/etoro/positions?ledger=etoro-demo')).json(),
    )
    expect(all.trades.map((t) => t.position_id)).toEqual(['3616683295', '3616683146'])
    const one = etoroPositionsResponseSchema.parse(
      await (await fetch('/api/ui/etoro/positions?ledger=etoro-demo&book=spx500')).json(),
    )
    expect(one.trades).toHaveLength(1)
    expect(one.summary.closed).toBe(0)

    const pv = await prepareTrade(
      { book: 'spx500', side: 'close', positionId: '3616683295', token: TOKEN },
      'etoro-demo',
    )
    const out = await placeTrade(
      { tradeToken: pv.token!, confirm: 'CLOSE SPX500', reason: 'vitest: taking the loss', by: 'vitest', token: TOKEN },
      'etoro-demo',
    )
    const after = etoroPositionsResponseSchema.parse(
      await (await fetch('/api/ui/etoro/positions?ledger=etoro-demo')).json(),
    )
    const spx = after.trades.find((t) => t.position_id === '3616683295')!
    expect(spx.status).toBe('closed')
    expect(spx.closed_by).toBe('operator')
    expect(spx.close_reason).toBe('vitest: taking the loss')
    expect(spx.close_rate).toBeCloseTo(out.avg_price, 9)
    expect(spx.gross_pnl).toBeCloseTo(spx.units * (out.avg_price - spx.open_rate), 6)
    expect(spx.net).toBeUndefined() // eToro's history has not listed it yet
    expect(after.summary).toMatchObject({ open: 0, closed: 2, realized_confirmed: 1 })

    const off = etoroPositionsResponseSchema.parse(
      await (await fetch('/api/ui/etoro/positions?ledger=etoro-demo&mock=trading-off')).json(),
    )
    expect(off.live).toBe(false)
    expect(off.live_reason).toMatch(/-etoro-trading/)
    expect(off.account).toBeUndefined()
  })
})

describe('Positions card', () => {
  it('shows the summary and a row per position on the Trade page, with the details on click', async () => {
    renderAt('/trade?ledger=etoro-demo')
    const card = await screen.findByTestId('positions-card')
    expect(await within(card).findByTestId('positions-source')).toHaveTextContent(/ledger \+ account \(live/)
    const summary = within(card).getByTestId('positions-summary')
    expect(summary).toHaveTextContent(/Open/)
    expect(summary).toHaveTextContent(/Realised/)
    expect(summary).toHaveTextContent(/net after fees/)
    const rows = within(card).getAllByTestId(/^position-\d+$/)
    expect(rows).toHaveLength(2)
    expect(within(card).getByTestId('position-status-3616683295')).toHaveTextContent('open')
    expect(within(card).getByTestId('position-status-3616683146')).toHaveTextContent('closed')
    expect(within(card).getByTestId('position-pnl-3616683146')).toHaveTextContent(/35\.34/)
    expect(within(card).getByTestId('position-pnl-3616683146')).toHaveTextContent(/net, eToro/)
    expect(within(card).getByTestId('position-pnl-3616683295')).toHaveTextContent(/unrealised/)

    await userEvent.click(within(card).getByTestId('position-3616683146'))
    const details = await within(card).findByTestId('position-details-3616683146')
    expect(details).toHaveTextContent(/the rule said flat at the 2026-10-15 close/)
    expect(details).toHaveTextContent(/eToro net profit/)
    expect(details).toHaveTextContent(/sma50-nsdq100-2026-10-16-close/)
  })

  it('the book page shows the book’s rows only and says when the account is not read', async () => {
    // The page flag is not forwarded under jsdom: stage trading-off at the handler.
    server.use(
      http.get('/api/ui/etoro/positions', ({ request }) => {
        const u = new URL(request.url)
        u.searchParams.set('mock', 'trading-off')
        return mockPositions(new Request(u))
      }),
    )
    renderAt('/forward-tests/spx500?ledger=etoro-demo')
    const card = await screen.findByTestId('positions-card')
    await waitFor(() => expect(within(card).getAllByTestId(/^position-\d+$/)).toHaveLength(1))
    expect(within(card).getByTestId('positions-source')).toHaveTextContent(/ledger only/)
    expect(within(card).getByTestId('position-3616683295')).toHaveTextContent('SPX500')
    expect(within(card).queryByTestId('position-3616683146')).toBeNull()
  })
})
