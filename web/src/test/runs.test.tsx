import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { beforeEach, describe, expect, it } from 'vitest'
import { runDocSchema, runsResponseSchema } from '../api/schemas'
import { costSiblings, fmtPP, fmtRet, heat, matchesRun, ruleLabel, windowLabel, windowOrder } from '../lib/runs'
import { resetThemeForTests, THEME_KEY } from '../lib/theme'
import runs from '../mocks/research/runs.json'
import { makeQueryClient, routes } from '../router'

const docs = import.meta.glob<unknown>('../mocks/research/run-*.json', { eager: true, import: 'default' })

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

const TAKER = '2026-09-27/btc-mxn-taker'
const all = runsResponseSchema.parse(runs).runs

describe('runs contract', () => {
  it('the captured run list parses and has a fixture per run', () => {
    const r = runsResponseSchema.safeParse(runs)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
    expect(runs.runs.length).toBeGreaterThan(10)
    for (const run of runs.runs) {
      expect(docs[`../mocks/research/run-${run.id.replace('/', '--')}.json`], run.id).toBeDefined()
    }
  })

  it.each(Object.entries(docs))('%s parses', (_path, data) => {
    const r = runDocSchema.safeParse(data)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
  })
})

describe('runs helpers', () => {
  it('labels rules and windows', () => {
    expect(ruleLabel('trend_sma50')).toBe('Trend SMA50')
    expect(ruleLabel('buy_and_hold')).toBe('Buy and hold')
    expect(windowLabel({ from: '2025-01-01', to: '2025-12-31' })).toBe('2025')
    expect(windowLabel({ from: '2026-01-01', to: '2026-09-26' })).toBe('2026 01-01 → 09-26')
    expect(windowLabel({ from: '2018-01-01', to: '2026-09-26' })).toBe('2018-01 → 2026-09')
  })

  it('orders windows as a time axis, multi-year last', () => {
    const ws = [
      { from: '2025-01-01', to: '2025-12-31' },
      { from: '2026-01-01', to: '2026-09-26' },
      { from: '2018-01-01', to: '2018-12-31' },
      { from: '2018-01-01', to: '2026-09-26' },
    ]
    expect(windowOrder(ws)).toEqual([2, 0, 1, 3])
  })

  it('formats signed numbers', () => {
    expect(fmtPP(10.64)).toBe('+10.6 pp')
    expect(fmtPP(-3.21)).toBe('−3.2 pp')
    expect(fmtPP(0)).toBe('±0.0 pp')
    expect(fmtPP(1234.5)).toBe('+1,235 pp')
    expect(fmtRet(-2.636)).toBe('−2.64%')
    expect(fmtRet(150.04)).toBe('+150.0%')
  })

  it('heat is log-scaled and bounded', () => {
    expect(heat(0, 100)).toBe(0)
    expect(heat(5, 0)).toBe(0)
    expect(heat(100, 100)).toBe(1)
    expect(heat(-100, 100)).toBe(1)
    expect(heat(5, 900)).toBeGreaterThan(5 / 900)
    expect(heat(5, 900)).toBeLessThan(1)
  })

  it('cost siblings share data and windows, cheapest first', () => {
    const taker = all.find((r) => r.id === TAKER)!
    const sib = costSiblings(taker, all)
    expect(sib.map((r) => r.name)).toEqual([
      'btc-mxn-frictionless',
      'btc-mxn-maker',
      'btc-mxn-maker-plus-slippage',
      'btc-mxn-taker',
    ])
    // The 09-28 btc_mxn runs use other windows, so they are not siblings.
    expect(sib.every((r) => r.date === '2026-09-27')).toBe(true)
  })

  it('matchesRun needs every word', () => {
    const taker = all.find((r) => r.id === TAKER)!
    expect(matchesRun(taker, '')).toBe(true)
    expect(matchesRun(taker, 'taker 176')).toBe(true)
    expect(matchesRun(taker, 'taker zzz')).toBe(false)
  })
})

describe('Runs page', () => {
  it('groups runs by evidence folder with score bars', async () => {
    renderAt('/research/runs')
    expect(await screen.findByRole('heading', { level: 1, name: 'Backtest runs' })).toBeInTheDocument()
    const rows = await screen.findAllByTestId(/^run-2026-/)
    expect(rows).toHaveLength(runs.runs.length)
    expect(screen.getByRole('heading', { level: 2, name: /Evidence .*27.*·/ })).toBeInTheDocument()
    const taker = screen.getByTestId(`run-${TAKER}`)
    expect(within(taker).getByText('176 bps round trip')).toBeInTheDocument()
    expect(within(taker).getByText('3/10')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Backtest runs' })).toHaveClass('active')
    expect(screen.getByRole('link', { name: 'Studies' })).not.toHaveClass('active')
  })

  it('filters by cost and text through the URL', async () => {
    const router = renderAt('/research/runs?cost=frictionless')
    const rows = await screen.findAllByTestId(/^run-2026-/)
    expect(rows.length).toBe(runs.runs.filter((r) => r.costs.round_trip_bps === 0).length)
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search runs' }), 'usd')
    await waitFor(() => expect(router.state.location.search).toContain('q=usd'))
    expect(screen.getAllByTestId(/^run-2026-/).map((r) => r.dataset.testid)).toEqual([
      'run-2026-09-28/btc-usd-frictionless',
      'run-2026-09-27/by-year-frictionless',
      'run-2026-09-27/full-span-frictionless',
    ])
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search runs' }), ' nothing-like-this')
    expect(await screen.findByText('No run matches')).toBeInTheDocument()
  })
})

describe('Run page', () => {
  it('shows the matrix, the selected window and cost sensitivity', async () => {
    const router = renderAt(`/research/runs/${TAKER}`)
    expect(await screen.findByRole('heading', { level: 1, name: 'btc-mxn-taker' })).toBeInTheDocument()
    const matrix = await screen.findByTestId('run-matrix')
    // 2018…2026 then the full span; the in-sample 2025 window is selected by default.
    const cols = within(matrix).getAllByRole('button')
    expect(cols.map((c) => c.id)).toEqual([
      'win-2',
      'win-3',
      'win-4',
      'win-5',
      'win-6',
      'win-7',
      'win-8',
      'win-0',
      'win-1',
      'win-9',
    ])
    expect(within(matrix).getByRole('button', { name: /2025/ })).toHaveAttribute('aria-pressed', 'true')
    expect(within(screen.getByTestId('window-detail')).getByRole('heading', { level: 2 })).toHaveTextContent('2025')

    await userEvent.click(within(matrix).getByRole('button', { name: /2026 01-01/ }))
    await waitFor(() => expect(router.state.location.search).toBe('?w=1'))
    const detail = screen.getByTestId('window-detail')
    expect(within(detail).getByRole('heading', { level: 2 })).toHaveTextContent('2026-01-01 → 2026-09-26')
    const sma = within(detail).getByRole('row', { name: /Trend SMA50/ })
    expect(within(sma).getByText('+4.4 pp')).toBeInTheDocument()
    expect(within(sma).getByText('82.5%')).toBeInTheDocument()

    const costs = await screen.findByTestId('cost-sensitivity')
    await waitFor(() => expect(within(costs).queryByText('…')).not.toBeInTheDocument())
    expect(within(costs).getAllByRole('row')).toHaveLength(5)
    expect(within(costs).getByRole('link', { name: 'frictionless' })).toHaveAttribute(
      'href',
      '/research/runs/2026-09-27/btc-mxn-frictionless',
    )
    expect(screen.getByTestId('run-study-FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27')).toBeInTheDocument()
  })

  it('an out-of-range ?w falls back to the first window', async () => {
    renderAt(`/research/runs/${TAKER}?w=99`)
    const matrix = await screen.findByTestId('run-matrix')
    expect(within(matrix).getByRole('button', { name: /2025/ })).toHaveAttribute('aria-pressed', 'true')
  })

  it('an unknown run is an error, not a crash', async () => {
    renderAt('/research/runs/2026-09-27/no-such-run')
    expect(await screen.findByText(/no run 2026-09-27\/no-such-run/)).toBeInTheDocument()
  })
})

describe('theme', () => {
  beforeEach(() => {
    localStorage.clear()
    resetThemeForTests()
  })

  it('defaults to dark and persists the switch to light', async () => {
    renderAt('/research/runs')
    const btn = await screen.findByRole('button', { name: 'Switch to light theme' })
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(btn).toHaveAttribute('aria-pressed', 'false')
    await userEvent.click(btn)
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(localStorage.getItem(THEME_KEY)).toBe('light')
    const back = screen.getByRole('button', { name: 'Switch to dark theme' })
    expect(back).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(back)
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(localStorage.getItem(THEME_KEY)).toBe('dark')
  })

  it('reads a saved light theme', async () => {
    localStorage.setItem(THEME_KEY, 'light')
    resetThemeForTests()
    renderAt('/research/runs')
    expect(await screen.findByRole('button', { name: 'Switch to dark theme' })).toBeInTheDocument()
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})
