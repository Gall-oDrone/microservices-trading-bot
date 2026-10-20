/**
 * U2 (Today page, stop history) and U3 (phone navigation, collapsible
 * intros, operator clock) — 2026-10-20.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { beforeEach, describe, expect, it } from 'vitest'
import { resetMockTrades } from '../mocks/trades'
import { makeQueryClient, routes } from '../router'
import { atr, distanceTo, openRisk, stopHistory } from '../lib/today'
import { fmtTz, resetTz, tzLabel } from '../lib/tz'
import positions from '../mocks/fixtures/etoro-demo/etoro-positions.json'
import { etoroPositionsResponseSchema } from '../api/etoroSchemas'

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
  resetTz()
  resetMockTrades()
})

describe('lib/today', () => {
  it('atr: Wilder over 14 bars, 0 without enough bars', () => {
    const flat = Array.from({ length: 20 }, (_, i) => ({ high: 102, low: 98, close: 100 + (i % 2) }))
    // Every true range is max(4, |102-prev|, |98-prev|) = 4 or 3: a steady 4ish.
    const a = atr(flat)
    expect(a).toBeGreaterThan(3.5)
    expect(a).toBeLessThanOrEqual(4)
    expect(atr(flat.slice(0, 10))).toBe(0)
  })
  it('distanceTo: stop below the mark, target above, in points, % and ATRs', () => {
    expect(distanceTo(100, 95, 2, 'stop')).toEqual({ points: 5, pct: 0.05, atrs: 2.5 })
    expect(distanceTo(100, 110, 0, 'target')).toEqual({ points: 10, pct: 0.1, atrs: 0 })
    expect(distanceTo(0, 95, 2, 'stop')).toBeNull()
  })
  it('openRisk: max loss at stops over protected rows, unprotected counted apart', () => {
    const p = etoroPositionsResponseSchema.parse(positions)
    const base = p.trades.find((t) => t.status === 'open')!
    const r = openRisk([
      { ...base, stop_loss_rate: base.open_rate * 0.95 },
      { ...base, position_id: 'x', stop_loss_rate: undefined, amount: 500 },
      { ...base, position_id: 'c', status: 'closed' },
    ])
    expect(r.open).toBe(2)
    expect(r.exposure).toBeCloseTo(base.amount + 500, 6)
    expect(r.maxLossAtStops).toBeCloseTo(base.units * base.open_rate * 0.05, 6)
    expect(r.unprotected).toBe(1)
    expect(r.unprotectedExposure).toBe(500)
  })
  it('stopHistory: one point per fill date, the later line on a date wins, none without a stop', () => {
    const rec = (bar: string, fill: string, at: string, stop?: number) => ({
      decision: { bar_date: bar, fill_date: fill },
      recorded_at: at,
      demo: { position_after: stop ? { stop_loss_rate: stop } : {} },
    })
    const pts = stopHistory([
      rec('2026-10-14', '2026-10-15', '2026-10-15T13:35:00Z', 100),
      rec('2026-10-15', '2026-10-16', '2026-10-16T13:35:00Z', 101),
      rec('2026-10-16', '2026-10-16', '2026-10-16T15:00:00Z', 103), // console protect the same day
      rec('2026-10-16', '2026-10-19', '2026-10-19T13:35:00Z'), // closed: no stop
    ])
    expect(pts).toEqual([
      { date: '2026-10-15', value: 100 },
      { date: '2026-10-16', value: 103 },
    ])
  })
})

describe('lib/tz', () => {
  it('formats a moment in the chosen zone and labels it', () => {
    const iso = '2026-10-19T13:35:00Z'
    expect(fmtTz(iso, 'ny')).toMatch(/19 Oct 2026, 09:35/)
    expect(fmtTz(iso, 'utc')).toMatch(/19 Oct 2026, 13:35/)
    expect(fmtTz(iso, 'mx')).toMatch(/19 Oct 2026, 07:35/)
    expect(tzLabel('mx')).toBe('MX')
  })
})

describe('Today page', () => {
  it('shows open risk, the open position against its stop, the last run per book and nothing waiting', async () => {
    renderAt('/today?ledger=etoro-demo')
    const risk = await screen.findByTestId('today-risk')
    const strip = within(risk).getByTestId('today-strip')
    expect(strip).toHaveTextContent(/Equity/)
    expect(strip).toHaveTextContent(/Open exposure/)
    // The fixture's open SPX500 position carries no stop: the loss is unbounded.
    expect(within(risk).getByTestId('today-max-loss')).toHaveTextContent('unbounded')
    const row = within(risk).getByTestId('today-open-3616683295')
    expect(row).toHaveTextContent('SPX500')
    expect(within(row).getByTestId('today-protect-3616683295')).toHaveAttribute(
      'href',
      '/trade?ledger=etoro-demo&book=spx500&side=protect&position=3616683295',
    )
    const runs = screen.getByTestId('today-runs')
    expect(within(runs).getByTestId('today-run-nsdq100')).toHaveTextContent(/demo close/)
    expect(within(runs).getByTestId('today-run-spx500')).toHaveTextContent(/demo none/)
    const needs = screen.getByTestId('today-needs')
    expect(needs).toHaveTextContent(/Nothing waiting on you/)
    // The nav shows Today for an eToro ledger.
    expect(document.getElementById('nav-today')).not.toBeNull()
  })
  it('a Bitso ledger gets the note and the link to the eToro Today', async () => {
    renderAt('/today?ledger=stage')
    await screen.findByTestId('today-bitso-note')
    expect(document.getElementById('today-switch-etoro')).toHaveAttribute('href', '/today?ledger=etoro-demo')
    expect(document.getElementById('nav-today')).toBeNull()
  })
})

describe('U3 shell', () => {
  it('renders the phone tab bar with the primary pages and a More sheet', async () => {
    renderAt('/trade?ledger=etoro-demo')
    const bar = await screen.findByTestId('tabbar')
    // The venue resolves once the ledgers load: the first tab becomes Today.
    await waitFor(() => expect(document.getElementById('tab-today')).not.toBeNull())
    expect(document.getElementById('tab-today')).toHaveAttribute('href', '/today?ledger=etoro-demo')
    expect(document.getElementById('tab-risk')).toHaveAttribute('href', '/risk?ledger=etoro-demo')
    expect(document.getElementById('tab-trade')).toHaveClass('active')
    expect(within(bar).getByText('Backtest runs')).toBeInTheDocument()
  })
  it('the clock toggle changes the audit log column and the positions times', async () => {
    renderAt('/trade?ledger=etoro-demo')
    const card = await screen.findByTestId('positions-card')
    await within(card).findByTestId('position-3616683295')
    expect(within(card).getByTestId('positions-opened-h')).toHaveTextContent('Opened (NY)')
    await userEvent.click(screen.getAllByTestId('tz-utc')[0])
    expect(within(card).getByTestId('positions-opened-h')).toHaveTextContent('Opened (UTC)')
    expect(within(card).getByTestId('position-3616683295')).toHaveTextContent('12 Oct 2026, 13:35')
    expect(localStorage.getItem('tz')).toBe('utc')
  })
  it('the page intro has a more / less toggle', async () => {
    renderAt('/market?ledger=etoro-demo')
    const toggle = await screen.findByTestId('page-intro-toggle')
    expect(toggle).toHaveTextContent('more')
    await userEvent.click(toggle)
    expect(toggle).toHaveTextContent('less')
    expect(screen.getByTestId('page-intro')).toHaveClass('open')
  })
})
