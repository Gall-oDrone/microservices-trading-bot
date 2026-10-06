import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it } from 'vitest'
import { studiesResponseSchema, studyDocSchema } from '../api/schemas'
import { matchesStudy, shortName } from '../lib/research'
import studies from '../mocks/research/studies.json'
import { makeQueryClient, routes } from '../router'

const docs = import.meta.glob<unknown>('../mocks/research/study-*.json', { eager: true, import: 'default' })

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

const PREREG = 'FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27'

describe('research contract', () => {
  it('the captured study list parses', () => {
    const r = studiesResponseSchema.safeParse(studies)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
    expect(studies.studies.length).toBeGreaterThan(5)
  })

  it.each(Object.entries(docs))('%s parses', (_path, data) => {
    const r = studyDocSchema.safeParse(data)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
  })
})

describe('research helpers', () => {
  it('shortName drops the date and humanizes', () => {
    expect(shortName('PATH-TO-PROFITABILITY-2026-09-23')).toBe('Path to profitability')
  })
  it('matchesStudy needs every word', () => {
    const s = studiesResponseSchema.parse(studies).studies[0]
    expect(matchesStudy(s, '')).toBe(true)
    expect(matchesStudy(s, s.title.split(' ')[0])).toBe(true)
    expect(matchesStudy(s, `${s.title.split(' ')[0]} zzzz-not-there`)).toBe(false)
  })
})

describe('Research page', () => {
  it('lists every study newest first with kind, summary and lineage', async () => {
    renderAt('/research')
    expect(await screen.findByRole('heading', { level: 1, name: 'Research' })).toBeInTheDocument()
    const rows = await screen.findAllByTestId(/^study-/)
    expect(rows).toHaveLength(studies.studies.length)
    expect(rows[0]).toHaveAttribute('data-testid', `study-${studies.studies[0].name}`)
    const prereg = screen.getByTestId(`study-${PREREG}`)
    expect(within(prereg).getByText('pre-registration')).toBeInTheDocument()
    expect(within(prereg).getByRole('link', { name: /follows btc mxn trend check/i })).toBeInTheDocument()
    expect(within(prereg).getByText(/evidence · \d+ files/)).toBeInTheDocument()
  })

  it('filters by kind and by search text, keeping both in the URL', async () => {
    const user = userEvent.setup()
    const router = renderAt('/research?ledger=dry-run')
    await screen.findAllByTestId(/^study-/)
    await user.click(screen.getByRole('button', { name: /pre-registration/ }))
    await waitFor(() => expect(screen.getAllByTestId(/^study-/)).toHaveLength(2))
    expect(router.state.location.search).toContain('kind=preregistration')
    expect(router.state.location.search).toContain('ledger=dry-run')

    await user.type(screen.getByRole('searchbox', { name: 'Search studies' }), 'btc_usd')
    await waitFor(() => expect(screen.getAllByTestId(/^study-/)).toHaveLength(1))
    await user.type(screen.getByRole('searchbox', { name: 'Search studies' }), ' nothing-matches')
    expect(await screen.findByText('No study matches')).toBeInTheDocument()
  })
})

describe('Study page', () => {
  it('renders the document with contents, lineage and evidence', async () => {
    renderAt(`/research/${PREREG}`)
    expect(await screen.findByRole('heading', { level: 1, name: /Pre-Registration/ })).toBeInTheDocument()
    const prose = await screen.findByTestId('study-prose')
    expect(within(prose).getByRole('heading', { level: 2, name: '1. Rule (frozen)' })).toBeInTheDocument()
    expect(within(prose).getByText(/important/i, { selector: '.alert-title' })).toBeInTheDocument()
    expect(prose.querySelector('script')).toBeNull()
    const toc = screen.getByRole('navigation', { name: 'Contents' })
    expect(within(toc).getByRole('link', { name: '1. Rule (frozen)' })).toHaveAttribute('href', '#1-rule-frozen')
    expect(within(screen.getByTestId('lineage-follows')).getByRole('link')).toHaveTextContent('Btc mxn trend check')
    expect(screen.getByText(/Evidence \(docs\/backtest-readiness\/evidence-2026-09-27\)/)).toBeInTheDocument()
  })

  it('routes links to other studies inside the app and leaves repo files inert', async () => {
    const user = userEvent.setup()
    const router = renderAt(`/research/${PREREG}`)
    const prose = await screen.findByTestId('study-prose')
    const local = prose.querySelector<HTMLAnchorElement>('a[data-local]')!
    await waitFor(() => expect(local.title).toMatch(/not served by the UI/))
    await user.click(local)
    expect(router.state.location.pathname).toBe(`/research/${PREREG}`)

    const study = prose.querySelector<HTMLAnchorElement>('a[data-study="BTC-MXN-TREND-CHECK-2026-09-27"]')!
    await user.click(study)
    await waitFor(() => expect(router.state.location.pathname).toBe('/research/BTC-MXN-TREND-CHECK-2026-09-27'))
    expect(await screen.findByRole('heading', { level: 1, name: /BTC\/MXN|btc_mxn|Trend/i })).toBeInTheDocument()
  })

  it('shows an error for an unknown study', async () => {
    renderAt('/research/NOPE-2026-01-01')
    expect(await screen.findByText('no study NOPE-2026-01-01')).toBeInTheDocument()
  })
})
