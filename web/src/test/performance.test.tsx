import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it } from 'vitest'
import { monteCarloResponseSchema, performanceResponseSchema } from '../api/schemas'
import dryPerfMxn from '../mocks/fixtures/dry-run/performance-btc_mxn.json'
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
