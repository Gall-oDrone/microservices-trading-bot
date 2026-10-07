import { QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it } from 'vitest'
import type { CandlePoint } from '../api/schemas'
import { currentRun, nearestIndex, TREND_W, trendGeometry } from '../lib/trend'
import { makeQueryClient, routes } from '../router'
import { server } from './setup'

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  const qc = makeQueryClient()
  qc.setDefaultOptions({ queries: { retry: false } })
  render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return router
}

const candle = (date: string, close: number, sma50: number | null, long: boolean | null): CandlePoint => ({
  date,
  open: close,
  high: close,
  low: close,
  close,
  volume: 1,
  trade_count: 1,
  sma50,
  volume_ratio_20d: null,
  long,
})

describe('trendGeometry', () => {
  it('needs at least two closes', () => {
    expect(trendGeometry([])).toBeNull()
    expect(trendGeometry([candle('2026-01-01', 100, null, null)])).toBeNull()
  })

  it('spans the width, shades long runs and summarises the period', () => {
    const cs = [
      candle('2026-01-01', 100, null, null),
      candle('2026-01-02', 110, 100, true),
      candle('2026-01-03', 120, 105, true),
      candle('2026-01-04', 90, 105, false),
      candle('2026-01-05', 125, 110, true),
    ]
    const g = trendGeometry(cs)!
    expect(g.points[0].x).toBe(0)
    expect(g.points[4].x).toBe(TREND_W)
    // Highest close is drawn nearest the top.
    expect(Math.min(...g.points.map((p) => p.y))).toBe(g.points[4].y)
    // Two long runs: days 2-3 and day 5 (clamped to the right edge).
    expect(g.longBands).toEqual([
      { x: 125, w: 500 },
      { x: 875, w: 125 },
    ])
    expect(g.sma.startsWith('M250 ')).toBe(true) // no SMA on day 1
    expect(g.longShare).toBe(0.75)
    expect(g.changePct).toBeCloseTo(25)
    expect(g.min).toBe(90)
    expect(g.max).toBe(125)
  })

  it('maps a pointer position to the nearest day', () => {
    expect(nearestIndex(0, 5)).toBe(0)
    expect(nearestIndex(0.49, 5)).toBe(2)
    expect(nearestIndex(2, 5)).toBe(4)
    expect(nearestIndex(0.5, 0)).toBe(-1)
  })
})

describe('currentRun', () => {
  it('reports the state of the last bar and when it began', () => {
    const cs = [
      candle('2026-01-01', 1, 1, true),
      candle('2026-01-02', 1, 1, false),
      candle('2026-01-03', 1, 1, true),
      candle('2026-01-04', 1, 1, true),
    ]
    expect(currentRun(cs)).toEqual({ state: 'long', since: '2026-01-03', bars: 2 })
    expect(currentRun(cs.slice(0, 2))).toEqual({ state: 'flat', since: '2026-01-02', bars: 1 })
    expect(currentRun([candle('2026-01-01', 1, null, null)])).toBeNull()
  })
})

describe('Landing page', () => {
  it('renders outside the console with the hero, today, research and guardrails', async () => {
    renderAt('/')
    expect(screen.getByRole('heading', { level: 1, name: /One frozen trend rule/ })).toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: 'Main' })).not.toBeInTheDocument() // no console sidebar

    const hero = screen.getByTestId('hero-card')
    expect(await within(hero).findByTestId('trend-chart')).toBeInTheDocument()
    expect(within(hero).getByTestId('signal-pill')).toHaveTextContent(/long/i)
    expect(within(hero).getByTestId('hero-run')).toHaveTextContent(/^long since .+ · \d+ days$/)

    const mxn = await screen.findByTestId('lp-book-btc_mxn')
    expect(within(mxn).getByText(/above the 50-day average/)).toBeInTheDocument()
    expect(within(mxn).getByRole('link', { name: /Open BTC \/ MXN/ })).toHaveAttribute('href', '/forward-tests/btc_mxn')
    expect(screen.getByTestId('lp-book-btc_usd')).toBeInTheDocument()

    expect(screen.getByTestId('lp-timeline')).toHaveTextContent('Interim look')
    expect(within(await screen.findByTestId('lp-studies')).getAllByRole('link')).toHaveLength(3)
    const guards = await screen.findByTestId('lp-guards')
    expect(within(guards).getByText('enforced')).toBeInTheDocument()
    expect(within(guards).getByText('not halted')).toBeInTheDocument()
    expect(within(guards).getByText('Cost per leg, BTC / MXN')).toBeInTheDocument()

    for (const a of screen.getAllByRole('link', { name: /Open the console|Console/ })) {
      expect(a).toHaveAttribute('href', '/forward-tests')
    }
  })

  it('switches the hero chart between books', async () => {
    renderAt('/')
    const usd = await screen.findByRole('button', { name: 'BTC / USD' })
    expect(screen.getByRole('button', { name: 'BTC / MXN' })).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(usd)
    expect(usd).toHaveAttribute('aria-pressed', 'true')
    await waitFor(() => expect(screen.getByTestId('trend-chart')).toHaveTextContent(/Daily close \(USD\)/))
  })

  it('reads a day from the chart with the keyboard', async () => {
    renderAt('/')
    const plot = within(await screen.findByTestId('trend-chart')).getByRole('img')
    fireEvent.keyDown(plot, { key: 'ArrowLeft' })
    const tip = screen.getByTestId('trend-tip')
    expect(tip).toHaveTextContent(/Close/)
    expect(tip).toHaveTextContent(/SMA50/)
    fireEvent.keyDown(plot, { key: 'Escape' })
    expect(screen.queryByTestId('trend-tip')).not.toBeInTheDocument()
  })

  it('keeps the selected ledger on its console links', async () => {
    renderAt('/?ledger=dry-run')
    await screen.findByTestId('lp-book-btc_mxn')
    expect(screen.getAllByText('No records in the dry-run ledger yet.')).toHaveLength(2)
    expect(document.querySelector('#cta-open-console')).toHaveAttribute('href', '/forward-tests?ledger=dry-run')
    expect(document.querySelector('#lp-open-risk')).toHaveAttribute('href', '/risk?ledger=dry-run')
  })

  it('still explains the project when ui-api is down', async () => {
    server.use(http.get('/api/ui/forward-tests', () => HttpResponse.json({ error: 'boom' }, { status: 500 })))
    renderAt('/')
    expect(await screen.findByText(/The console backend is not answering \(boom\)/)).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Decided in advance, checked every day' })).toBeInTheDocument()
  })

  it('the console brand links back to the landing page', async () => {
    const router = renderAt('/risk')
    await userEvent.click(await screen.findByRole('link', { name: /Trading Bot/ }))
    expect(router.state.location.pathname).toBe('/')
  })
})
