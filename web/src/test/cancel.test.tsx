/**
 * Cancel a prepared trade (2026-10-21): the mock endpoint's contract and
 * the Trade page's Cancel button, Escape shortcut and local Dismiss.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tradeCancelResponseSchema } from '../api/etoroSchemas'
import { cancelTrade, placeTrade, prepareTrade } from '../api/trades'
import { resetMockTrades } from '../mocks/trades'
import { makeQueryClient, routes } from '../router'

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
  localStorage.clear()
  resetMockTrades()
})
afterEach(() => {
  vi.useRealTimers()
})

describe('cancel against the mock endpoint', () => {
  it('burns the token: place is a 409 afterwards, so is a second cancel; the audit line carries the ref', async () => {
    const pv = await prepareTrade(
      { book: 'spx500', side: 'close', positionId: '3616683295', token: TOKEN },
      'etoro-demo',
    )
    expect(pv.token).toBeTruthy()
    const r = await cancelTrade(
      { tradeToken: pv.token!, reason: 'vitest: changed my mind', by: 'vitest', token: TOKEN },
      'etoro-demo',
    )
    expect(tradeCancelResponseSchema.parse(r)).toEqual(r)
    expect(r.status).toBe('cancelled')
    expect(r.client_ref).toBe(pv.client_ref)
    expect(r.audit.action).toBe('trade.cancel')
    expect(r.audit.outcome).toBe('done')
    expect(r.audit.ref).toBe(pv.client_ref)
    expect(r.audit.reason).toBe('vitest: changed my mind')
    expect(r.audit.detail).toMatch(/token burned, nothing sent/)
    await expect(
      placeTrade(
        { tradeToken: pv.token!, confirm: 'CLOSE SPX500', reason: 'vitest: after cancel', by: 'vitest', token: TOKEN },
        'etoro-demo',
      ),
    ).rejects.toMatchObject({ status: 409 })
    await expect(cancelTrade({ tradeToken: pv.token!, token: TOKEN }, 'etoro-demo')).rejects.toMatchObject({
      status: 409,
    })
    // The position is untouched: a fresh close still prepares.
    const again = await prepareTrade(
      { book: 'spx500', side: 'close', positionId: '3616683295', token: TOKEN },
      'etoro-demo',
    )
    expect(again.verdict).toBe('allow')
  })

  it('a placed token cannot be cancelled; a bad token is a 401; reason and by are optional', async () => {
    const pv = await prepareTrade(
      { book: 'spx500', side: 'close', positionId: '3616683295', token: TOKEN },
      'etoro-demo',
    )
    await placeTrade(
      { tradeToken: pv.token!, confirm: 'CLOSE SPX500', reason: 'vitest: closing by hand', by: 'vitest', token: TOKEN },
      'etoro-demo',
    )
    await expect(cancelTrade({ tradeToken: pv.token!, token: TOKEN }, 'etoro-demo')).rejects.toMatchObject({
      status: 409,
    })
    await expect(cancelTrade({ tradeToken: 'nope', token: TOKEN }, 'etoro-demo')).rejects.toMatchObject({ status: 401 })
    const open = await prepareTrade({ book: 'nsdq100', side: 'open', amountUsd: 1100, token: TOKEN }, 'etoro-demo')
    // NSDQ100 had the executor's leg today: blocked, no token to cancel.
    expect(open.verdict).toBe('block')
    expect(open.token).toBeUndefined()
  })
})

describe('Trade page: Cancel on a live preview', () => {
  async function prepareClose(user: ReturnType<typeof userEvent.setup>) {
    renderAt('/trade?ledger=etoro-demo')
    await screen.findByTestId('trade-form')
    await user.selectOptions(screen.getByTestId('trade-book'), 'spx500')
    await user.click(screen.getByTestId('trade-side-close'))
    await user.type(screen.getByTestId('trade-token'), TOKEN)
    await user.click(screen.getByTestId('trade-prepare'))
    const pv = await screen.findByTestId('trade-preview')
    expect(pv).toHaveAttribute('data-verdict', 'allow')
    return pv
  }

  it('the Cancel button drops the preview, shows the cancelled card and the audit line; nothing moved', async () => {
    const user = userEvent.setup()
    const pv = await prepareClose(user)
    const cancelBtn = within(pv).getByTestId('trade-cancel')
    expect(cancelBtn).toHaveTextContent('Cancel · nothing sent')
    expect(cancelBtn).toHaveAttribute('aria-keyshortcuts', 'Escape')
    expect(within(pv).queryByTestId('trade-dismiss')).not.toBeInTheDocument()
    await user.click(cancelBtn)
    const card = await screen.findByTestId('trade-cancelled')
    expect(card).toHaveAttribute('data-status', 'cancelled')
    expect(card).toHaveTextContent('close SPX500 · cancelled')
    expect(card).toHaveTextContent('nothing was sent to eToro')
    expect(card).toHaveTextContent('trade.cancel')
    expect(screen.queryByTestId('trade-preview')).not.toBeInTheDocument()
    expect(screen.queryByTestId('trade-outcome')).not.toBeInTheDocument()
    await waitFor(() => expect(screen.getByTestId('trade-audit')).toHaveTextContent('trade.cancel'))
    // The proposal is unchanged: SPX500 is still held.
    expect(screen.getByTestId('trade-proposal-text-spx500')).toHaveTextContent('has no stop loss → protect it')
    await user.click(screen.getByTestId('trade-cancelled-dismiss'))
    expect(screen.queryByTestId('trade-cancelled')).not.toBeInTheDocument()
  })

  it('Escape cancels too, but not while the place dialog is open', async () => {
    const user = userEvent.setup()
    const pv = await prepareClose(user)
    await user.click(within(pv).getByTestId('trade-place'))
    await screen.findByTestId('control-dialog')
    await user.keyboard('{Escape}')
    // The dialog closed; the preview is still live.
    await waitFor(() => expect(screen.queryByTestId('control-dialog')).not.toBeInTheDocument())
    expect(screen.getByTestId('trade-preview')).toBeInTheDocument()
    expect(within(screen.getByTestId('trade-preview')).getByTestId('trade-cancel')).toBeEnabled()
    await user.keyboard('{Escape}')
    const card = await screen.findByTestId('trade-cancelled')
    expect(card).toHaveAttribute('data-status', 'cancelled')
    expect(screen.queryByTestId('trade-preview')).not.toBeInTheDocument()
  })

  it('a blocked preview offers Dismiss (local) and no Cancel; an expired one offers Re-prepare and Dismiss', async () => {
    const user = userEvent.setup()
    renderAt('/trade?ledger=etoro-demo')
    await screen.findByTestId('trade-form')
    await user.type(screen.getByTestId('trade-token'), TOKEN)
    await user.click(screen.getByTestId('trade-prepare'))
    const blocked = await screen.findByTestId('trade-preview')
    expect(blocked).toHaveAttribute('data-verdict', 'block')
    expect(within(blocked).queryByTestId('trade-cancel')).not.toBeInTheDocument()
    await user.click(within(blocked).getByTestId('trade-dismiss'))
    expect(screen.queryByTestId('trade-preview')).not.toBeInTheDocument()
    expect(screen.queryByTestId('trade-cancelled')).not.toBeInTheDocument()

    await user.selectOptions(screen.getByTestId('trade-book'), 'spx500')
    await user.click(screen.getByTestId('trade-side-close'))
    await user.click(screen.getByTestId('trade-prepare'))
    const pv = await screen.findByTestId('trade-preview')
    expect(within(pv).getByTestId('trade-cancel')).toBeEnabled()
    const spy = vi.spyOn(Date, 'now').mockImplementation(() => new Date().getTime() + 130_000)
    await waitFor(() => expect(pv).toHaveAttribute('data-expired', 'true'), { timeout: 3000 })
    expect(within(pv).queryByTestId('trade-cancel')).not.toBeInTheDocument()
    expect(within(pv).getByTestId('trade-reprepare')).toBeInTheDocument()
    await user.click(within(pv).getByTestId('trade-dismiss'))
    expect(screen.queryByTestId('trade-preview')).not.toBeInTheDocument()
    spy.mockRestore()
  })
})
