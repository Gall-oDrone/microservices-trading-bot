import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it } from 'vitest'
import { capacityResponseSchema, monteCarloResponseSchema, performanceResponseSchema } from '../api/schemas'
import capDry from '../mocks/fixtures/dry-run/capacity-btc_mxn.json'
import dryPerfMxn from '../mocks/fixtures/dry-run/performance-btc_mxn.json'
import capMxn from '../mocks/fixtures/stage/capacity-btc_mxn.json'
import capUsd from '../mocks/fixtures/stage/capacity-btc_usd.json'
import mcMxn from '../mocks/fixtures/stage/montecarlo-btc_mxn.json'
import mcUsd from '../mocks/fixtures/stage/montecarlo-btc_usd.json'
import perfMxn from '../mocks/fixtures/stage/performance-btc_mxn.json'
import perfUsd from '../mocks/fixtures/stage/performance-btc_usd.json'
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

describe('contract: performance and Monte Carlo fixtures', () => {
  it.each([
    ['stage btc_mxn', perfMxn],
    ['stage btc_usd', perfUsd],
    ['dry-run btc_mxn', dryPerfMxn],
  ])('%s performance parses', (_n, data) => {
    expect(performanceResponseSchema.safeParse(data).success).toBe(true)
  })
  it.each([
    ['btc_mxn', mcMxn],
    ['btc_usd', mcUsd],
  ])('%s Monte Carlo parses', (_n, data) => {
    const m = monteCarloResponseSchema.parse(data)
    expect(m.summary.histogram.reduce((s, b) => s + b.trend, 0)).toBe(m.summary.config.paths)
  })
})

describe('Forward test detail: P&L, trades and Monte Carlo', () => {
  it('shows the stage P&L with its attribution adding up', async () => {
    renderAt('/forward-tests/btc_mxn')
    const stats = await screen.findByTestId('pnl-stats')
    const p = performanceResponseSchema.parse(perfMxn).pnl!
    // +39.30 MXN on 1,522.86 invested as of the 2026-10-04 capture.
    expect(stats).toHaveTextContent(`+${p.total.toFixed(2)} MXN`)
    expect(stats).toHaveTextContent('1,522.86 MXN invested')
    expect(stats).toHaveTextContent(`${Math.round(p.shortfall_bps)} bps`)
    expect(stats).toHaveTextContent('1,533,610')
    const a = screen.getByTestId('pnl-attribution')
    expect(within(a).getByText(`−${p.fees.toFixed(2)} MXN`)).toBeInTheDocument()
    expect(within(a).getByText(`+${p.market_pnl.toFixed(2)} MXN`)).toBeInTheDocument()
    expect(p.market_pnl - p.fees - p.slippage).toBeCloseTo(p.total, 9)
  })

  it('shows the open trade, the skewed trade distribution and the calendar base rates', async () => {
    renderAt('/forward-tests/btc_mxn')
    expect(await screen.findByTestId('open-trade')).toHaveTextContent('20 Aug 2026')
    const d = screen.getByTestId('trade-dist')
    expect(d).toHaveTextContent('103')
    expect(d).toHaveTextContent('17%')
    expect(d).toHaveTextContent('-42%') // without the best trade
    const cal = document.getElementById('calendar-years')!
    expect(within(cal).getAllByRole('row')).toHaveLength(1 + 9 + 1)
    expect(cal).toHaveTextContent('5 / 9')
    expect(cal).toHaveTextContent('6 / 9')
    expect(screen.getByTestId('stats-caveat')).toHaveTextContent('need about 90 days')
  })

  it('renders the Monte Carlo and re-queries when the cost or block changes', async () => {
    const seen: string[] = []
    server.use(
      http.get('/api/ui/forward-tests/:book/montecarlo', ({ request }) => {
        const q = new URL(request.url).searchParams
        seen.push(`${q.get('cost')}/${q.get('block')}/${q.get('paths')}`)
        const m = structuredClone(mcMxn)
        return HttpResponse.json({ ...m, cost: q.get('cost'), leg_bps: q.get('cost') === 'pessimistic' ? 88 : 70 })
      }),
    )
    renderAt('/forward-tests/btc_mxn')
    const probs = await screen.findByTestId('mc-probs')
    const m = monteCarloResponseSchema.parse(mcMxn)
    expect(probs).toHaveTextContent(`${Math.round(m.summary.p_beats_hold.p * 100)}%`)
    expect(probs).toHaveTextContent('5/9 yrs')
    expect(probs).toHaveTextContent('6/9 yrs')
    expect(screen.getByTestId('mc-histogram')).toHaveAccessibleName(/2000 paths/)
    expect(document.getElementById('mc-quantiles')).toHaveTextContent('Rule minus hold')
    expect(seen).toEqual(['primary/20/2000'])

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'taker' }))
    expect(await screen.findByText(/88 bps per leg \(pessimistic\)/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '60d blocks' }))
    await screen.findByText(/88 bps per leg/)
    expect(seen).toEqual(['primary/20/2000', 'pessimistic/20/2000', 'pessimistic/60/2000'])
  })

  it('explains an empty P&L for a book without stage fills', async () => {
    const p = performanceResponseSchema.parse(structuredClone(perfMxn))
    server.use(http.get('/api/ui/forward-tests/:book/performance', () => HttpResponse.json({ ...p, pnl: null })))
    renderAt('/forward-tests/btc_mxn')
    expect(await screen.findByText('No stage fills yet', { selector: '*' }, { timeout: 3000 })).toBeInTheDocument()
  })
})

describe('Forward test detail: daily P&L, MXN terms and capacity (plan §6.4.12)', () => {
  it('parses the capacity fixtures', () => {
    for (const d of [capMxn, capUsd, capDry]) expect(capacityResponseSchema.safeParse(d).success).toBe(true)
    expect(capacityResponseSchema.parse(capMxn).book_status).toBe('live')
    expect(capacityResponseSchema.parse(capDry).book_status).toBe('none')
  })

  it('charts the daily P&L with NAV against capital', async () => {
    renderAt('/forward-tests/btc_mxn')
    expect(await screen.findByTestId('pnl-history-chart')).toBeInTheDocument()
    const p = performanceResponseSchema.parse(perfMxn)
    const last = p.pnl_history[p.pnl_history.length - 1]
    const nav = screen.getByTestId('nav-stats')
    expect(nav).toHaveTextContent(
      `${last.nav.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} MXN`,
    )
    expect(nav).toHaveTextContent('capital 25,000 MXN')
    expect(screen.queryByTestId('mxn-terms')).not.toBeInTheDocument() // only for USD books
  })

  it('shows btc_usd in pesos against holding btc_mxn', async () => {
    renderAt('/forward-tests/btc_usd')
    const m = await screen.findByTestId('mxn-terms')
    const t = performanceResponseSchema.parse(perfUsd).mxn_terms!
    expect(m).toHaveTextContent('60 bps each way')
    expect(m).toHaveTextContent(t.h2_so_far ? 'ahead' : 'behind')
    expect(m).toHaveTextContent(t.fx_end.toFixed(4))
  })

  it('lists order sizes against the 10 bps slippage budget', async () => {
    renderAt('/forward-tests/btc_mxn')
    const cap = await screen.findByTestId('capacity')
    const c = capacityResponseSchema.parse(capMxn)
    const rows = within(document.getElementById('capacity-table')!).getAllByRole('row').slice(1)
    expect(rows).toHaveLength(c.rows.length)
    expect(rows[0]).toHaveClass('current') // today's 0.001 BTC leg
    expect(within(rows[0]).getByText('yes')).toBeInTheDocument()
    expect(within(rows[rows.length - 1]).getByText('no')).toBeInTheDocument()
    expect(cap).toHaveTextContent('Capacity at 10 bps slippage')
    expect(cap).toHaveTextContent('0.001 / 0.01 BTC')
  })
})

describe('Capacity over time: hourly book samples (plan §6.4.13)', () => {
  it('parses the history, absent in older responses', () => {
    expect(capacityResponseSchema.parse(capMxn).history?.samples).toBeGreaterThan(0)
    expect(capacityResponseSchema.parse(capDry).history ?? null).toBeNull()
  })

  it('shows spread, depth, capacity and walk costs as percentiles', async () => {
    renderAt('/forward-tests/btc_mxn')
    const hist = await screen.findByTestId('book-history')
    const h = capacityResponseSchema.parse(capMxn).history!
    expect(hist).toHaveTextContent(`${h.samples} hourly samples`)
    const rows = within(document.getElementById('book-history-table')!).getAllByRole('row').slice(1)
    expect(rows).toHaveLength(3 + h.sizes.length)
    expect(rows[0]).toHaveTextContent(h.spread_bps.p50.toFixed(1))
    expect(rows[2]).toHaveTextContent(`Capacity at ${h.budget_bps} bps`)
  })
})
