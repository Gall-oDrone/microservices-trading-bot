import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it } from 'vitest'
import { makeQueryClient, routes } from '../router'

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

describe('eToro ledger pages (?ledger=etoro-demo)', () => {
  it('forward tests: one card per CFD book with the demo position and the next run in New York', async () => {
    renderAt('/forward-tests?ledger=etoro-demo')
    expect(
      await screen.findByRole('heading', { level: 1, name: 'SMA50 forward test · eToro index CFDs (demo)' }),
    ).toBeInTheDocument()
    const nsdq = await screen.findByTestId('ft-card-nsdq100')
    expect(within(nsdq).getByTestId('demo-position-nsdq100')).toHaveTextContent('flat')
    expect(within(nsdq).getByTestId('signal-pill')).toHaveTextContent(/flat/)
    expect(within(nsdq).getByTestId('last-order-nsdq100')).toHaveTextContent(/close/)
    const spx = screen.getByTestId('ft-card-spx500')
    expect(within(spx).getByTestId('demo-position-spx500')).toHaveTextContent('0.140735 u')
    expect(within(spx).getByTestId('next-run')).toHaveTextContent('19 Oct 2026, 09:35 NY')
    expect(within(spx).getByText('7,738.57')).toBeInTheDocument()
    expect(screen.getByTestId('session-clock')).toHaveAttribute('data-open', 'true')
    expect(screen.getAllByTitle('Ledger: etoro-demo').length).toBeGreaterThan(0)
    // No Bitso-only elements: no live strip, no window progress.
    expect(screen.queryByTestId('live-nsdq100')).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('detail: records table with demo actions and the reconcile status of the day', async () => {
    renderAt('/forward-tests/spx500?ledger=etoro-demo')
    expect(await screen.findByRole('heading', { level: 1, name: 'SPX500 · S&P 500 CFD' })).toBeInTheDocument()
    const table = await screen.findByTestId('etoro-records')
    expect(within(table).getAllByRole('row')).toHaveLength(6) // header + 5 records
    const open = within(table).getByTestId('etoro-record-2026-10-09')
    expect(open).toHaveTextContent('open')
    expect(open).toHaveTextContent('executed')
    expect(open).toHaveTextContent('7,816.07')
    expect(open).toHaveTextContent('long 0.140735 u')
    const rec = await screen.findByTestId('detail-reconcile')
    expect(rec).toHaveTextContent('16 Oct 2026')
    expect(within(rec).getByTestId('reconcile-state')).toHaveTextContent('drift 1')
    expect(within(rec).getByTestId('reconcile-recheck')).toHaveTextContent('clean')
    expect(screen.getByTestId('etoro-equity-chart')).toBeInTheDocument()
  })

  it('risk: policy chips, exposure meters, operator controls and the P4 report', async () => {
    renderAt('/risk?ledger=etoro-demo')
    const policy = await screen.findByTestId('policy-summary')
    expect(policy).toHaveTextContent('x1 only')
    expect(policy).toHaveTextContent('1,500 USD')
    expect(policy).toHaveTextContent('5% of equity')
    expect(policy).toHaveTextContent('cash session required')
    const spx = screen.getByTestId('risk-card-spx500')
    expect(within(spx).getByTestId('meter-exposure-spx500')).toHaveTextContent('Exposure vs 5% of equity')
    expect(within(spx).getByTestId('meter-exposure-spx500').querySelector('[role=meter]')).toHaveAttribute(
      'aria-valuenow',
      '22',
    )
    expect(within(spx).getByTestId('meter-notional-spx500')).toHaveTextContent('1,100 USD / 1,500 USD')
    expect(await screen.findByTestId('operator-controls')).toHaveAttribute('data-state', 'running')
    expect(screen.getByText(/Halt or resume the eToro executor/)).toBeInTheDocument()
    const report = await screen.findByTestId('risk-report')
    expect(within(report).getByTestId('report-var')).toHaveTextContent('52.81 USD')
    expect(report).toHaveTextContent('2008-09 Lehman to the November low')
    expect(within(report).getByTestId('fillq-nsdq100')).toHaveTextContent('frozen 7.75 bps')
    expect(within(report).getByTestId('fillq-spx500')).toHaveTextContent('frozen 7.4 bps')
    expect(within(report).getByTestId('report-mc')).toHaveTextContent('P(beats CFD hold)')
  })

  it('data health: the etorohealth checks and the reconcile reports, newest first', async () => {
    renderAt('/data-health?ledger=etoro-demo')
    expect(await screen.findByTestId('check-etoro.reconcile')).toHaveTextContent(
      'drift on the run, clean on the operator',
    )
    const list = await screen.findByTestId('reconcile-list')
    const rows = within(list)
      .getAllByRole('listitem')
      .filter((li) => li.className === 'reconcile-row')
    expect(rows[0]).toHaveAttribute('data-testid', 'reconcile-2026-10-16')
    expect(rows[0]).toHaveAttribute('data-clean', 'true')
    expect(within(rows[0]).getByTestId('reconcile-state')).toHaveTextContent('effective clean')
    expect(within(rows[0]).getByTestId('reconcile-recheck')).toHaveTextContent('clean')
  })

  it('strategies: both venues in the kill switch; start/stop is Bitso-only for the eToro ledger', async () => {
    renderAt('/strategies?ledger=etoro-demo')
    const ks = await screen.findByTestId('kill-switch')
    expect(within(ks).getByTestId('killswitch-target-etoro-demo')).toHaveTextContent('etoro')
    expect(within(ks).getByTestId('killswitch-target-stage')).toHaveTextContent('bitso')
    expect(screen.getByTestId('ledger-card-etoro-demo')).toHaveAttribute('data-venue', 'etoro')
    expect(await screen.findByTestId('strategies-bitso-only')).toBeInTheDocument()
    expect(screen.getByTestId('strategy-stop-lp_btc_demo')).toBeDisabled()
  })

  it('market: without EventSource (jsdom) the feed reads off, the last closes and the session still show', async () => {
    renderAt('/market?ledger=etoro-demo')
    expect(await screen.findByText('Live eToro quotes unavailable')).toBeInTheDocument()
    const spx = await screen.findByTestId('market-spx500')
    expect(within(spx).getByTestId('closes-spx500')).toHaveTextContent('7,738.57')
    expect(within(spx).getByTestId('bid-spx500')).toHaveTextContent('—')
    expect(screen.getByTestId('session-clock')).toHaveAttribute('data-open', 'true')
    expect(screen.getByTestId('live-badge')).toHaveTextContent(/live off/i)
    expect(document.querySelector('#market-open-spx500')).toHaveAttribute(
      'href',
      '/forward-tests/spx500?ledger=etoro-demo',
    )
  })

  it('landing: the eToro row next to the Bitso tiles', async () => {
    renderAt('/')
    const row = await screen.findByTestId('lp-etoro')
    expect(await within(row).findByTestId('lp-etoro-position-spx500')).toHaveTextContent('long 0.140735 u')
    expect(within(row).getByTestId('lp-etoro-position-nsdq100')).toHaveTextContent('flat')
    expect(within(row).getAllByTestId('next-run')[0]).toHaveTextContent('NY')
    expect(document.querySelector('#lp-open-etoro')).toHaveAttribute('href', '/forward-tests?ledger=etoro-demo')
    // The Bitso tiles are still there.
    expect(screen.getByTestId('lp-book-btc_mxn')).toBeInTheDocument()
  })

  it('landing with the eToro ledger selected keeps the Bitso hero on the default ledger', async () => {
    renderAt('/?ledger=etoro-demo')
    expect(await screen.findByTestId('lp-book-btc_mxn')).toHaveTextContent(/above the 50-day average/)
    expect(await screen.findByTestId('lp-etoro')).toBeInTheDocument()
  })
})
