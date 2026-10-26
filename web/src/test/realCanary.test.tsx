import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { controlsInfoSchema, ledgersResponseSchema, tradingInfoSchema } from '../api/schemas'
import { tradePreviewSchema } from '../api/etoroSchemas'
import { accountLabel, amountLabel, faviconFor, sizingLine, titleFor } from '../lib/env'
import { forceMockFlag } from '../mocks/flag'
import { resetMockTrades } from '../mocks/trades'
import { makeQueryClient, routes } from '../router'

/**
 * The real-account canary in the console (2026-10-22, plan C8 / §3.4): under
 * the `real-canary` mock scenario the ledger list carries etoro-real-canary
 * (environment real), and the app must make that unmistakable on every page.
 * The same tests on the demo ledger assert the absence of every REAL mark.
 */

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

const TOKEN = 'c'.repeat(64)

beforeEach(() => {
  sessionStorage.clear()
  localStorage.clear()
  resetMockTrades()
  // index.html's favicon link, which jsdom's empty document lacks.
  if (!document.querySelector('link[rel="icon"]')) {
    const link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }
})
afterEach(() => {
  forceMockFlag(null)
  delete document.documentElement.dataset.env
})

describe('lib/env: the pure helpers', () => {
  it('labels, titles and the favicon follow the environment', () => {
    expect(accountLabel('real')).toBe('REAL')
    expect(accountLabel('demo')).toBe('demo')
    expect(accountLabel(undefined)).toBe('demo')
    expect(titleFor('Trade · eToro', 'real')).toBe('REAL · Trade · eToro · Trading Bot Console')
    expect(titleFor('Trade · eToro', 'demo')).toBe('Trade · eToro · Trading Bot Console')
    expect(faviconFor('real')).toContain('circle')
    expect(faviconFor('demo')).not.toContain('circle')
    expect(amountLabel(20)).toBe('Invested (USD, x20)')
    expect(amountLabel(1)).toBe('Amount (USD, x1)')
  })

  it('the sizing line states invested, leverage, exposure and the max loss', () => {
    expect(sizingLine({ side: 'open', amount_usd: 50, leverage: 20, exposure_usd: 1000.4, max_loss_usd: 24.5 })).toBe(
      '50 USD invested · x20 · exposure 1,000 USD · max loss 24.50 USD',
    )
    // Demo, x1, as before: amount, x1, exposure; no max loss without a stop.
    expect(sizingLine({ side: 'open', amount_usd: 1100, leverage: 1 })).toBe('1,100 USD · x1 · exposure 1,100 USD')
    expect(sizingLine({ side: 'close', amount_usd: 0, position_id: '41', leverage: 20, exposure_usd: 998 })).toBe(
      'position 41 · x20 · exposure 998 USD',
    )
    expect(sizingLine({ side: 'close', amount_usd: 0, position_id: '41' })).toBe('position 41')
  })
})

describe('schemas: environment and leverage are additive', () => {
  it('older captures without the fields parse as demo / x1', () => {
    const t = tradingInfoSchema.parse({
      enabled: true,
      token_ttl_s: 120,
      min_amount_usd: 1000,
      max_amount_usd: 1500,
      default_amount_usd: 1100,
    })
    expect(t.environment).toBe('demo')
    expect(t.leverage_options).toEqual([1])
    expect(t.default_leverage).toBe(1)
  })

  it('the mock ledgers and controls under real-canary say real', async () => {
    forceMockFlag('real-canary')
    const ls = ledgersResponseSchema.parse(await (await fetch('/api/ui/ledgers')).json())
    const canary = ls.ledgers.find((l) => l.name === 'etoro-real-canary')
    expect(canary?.environment).toBe('real')
    expect(canary?.modes).toEqual(['real'])
    expect(ls.ledgers.some((l) => l.name === 'etoro-demo')).toBe(false)
    const c = controlsInfoSchema.parse(await (await fetch('/api/ui/controls?ledger=etoro-real-canary')).json())
    expect(c.environment).toBe('real')
    expect(c.ledger).toBe('etoro-real-canary')
    expect(c.trading?.environment).toBe('real')
    expect(c.trading?.leverage_options).toEqual([20])
    expect(c.trading?.min_amount_usd).toBe(50)
    expect(c.trading?.max_amount_usd).toBe(60)
    expect(c.trading?.default_amount_usd).toBe(50)
    expect(c.trading?.policy_version).toBe('etoro-real-canary-2026-10-22')
    // The demo name is unknown to the canary's ui-api.
    expect((await fetch('/api/ui/controls?ledger=etoro-demo')).status).toBe(400)
    forceMockFlag(null)
    const d = controlsInfoSchema.parse(await (await fetch('/api/ui/controls?ledger=etoro-demo')).json())
    expect(d.environment).toBe('demo')
    expect(d.trading?.leverage_options).toEqual([1])
    expect(d.trading?.min_amount_usd).toBe(1000)
  })
})

describe('the REAL bar and the marks', () => {
  it('on the real ledger: the bar on every page, the title prefix, the badges, the document attribute', async () => {
    forceMockFlag('real-canary')
    renderAt('/risk?ledger=etoro-real-canary')
    const bar = await screen.findByTestId('real-bar')
    expect(bar).toHaveAttribute('role', 'alert')
    expect(bar).toHaveAccessibleName('Real account')
    expect(bar).toHaveTextContent('REAL ACCOUNT')
    expect(bar).toHaveTextContent('ledger etoro-real-canary')
    expect(bar).toHaveTextContent('phrases carry REAL')
    await waitFor(() => expect(bar).toHaveTextContent('policy etoro-real-canary-2026-10-22'))
    // Not dismissable: no button inside it.
    expect(within(bar).queryByRole('button')).toBeNull()
    await waitFor(() => expect(document.title).toMatch(/^REAL · /))
    expect(document.documentElement.dataset.env).toBe('real')
    expect(document.querySelector('link[rel="icon"]')?.getAttribute('href')).toContain('circle')
    expect(await screen.findByTestId('ledger-picker-real')).toHaveTextContent('REAL')
    expect(within(document.getElementById('nav-trade')!).getByText('REAL')).toBeInTheDocument()
  })

  it('on the demo ledger: no bar, no prefix, no attribute, the Trade badge says demo', async () => {
    renderAt('/risk?ledger=etoro-demo')
    await waitFor(() => expect(document.getElementById('nav-trade')).toHaveTextContent('demo'))
    expect(screen.queryByTestId('real-bar')).toBeNull()
    expect(screen.queryByTestId('ledger-picker-real')).toBeNull()
    expect(document.title).not.toMatch(/REAL/)
    expect(document.documentElement.dataset.env).toBeUndefined()
  })
})

describe('the Trade page on the real account', () => {
  it('bounds 50-60 at x20, REAL badge, the preview first line and the REAL phrase', async () => {
    forceMockFlag('real-canary')
    const user = userEvent.setup()
    renderAt('/trade?ledger=etoro-real-canary')
    expect(await screen.findByTestId('trade-account-badge')).toHaveTextContent('REAL account')
    const manual = await screen.findByTestId('trade-manual')
    expect(manual).toHaveTextContent('An open of 50–60 USD invested at x20')
    const amount = screen.getByTestId('trade-amount') as HTMLInputElement
    expect(amount.min).toBe('50')
    expect(amount.max).toBe('60')
    expect(amount.placeholder).toBe('50')
    expect(screen.getByLabelText('Invested (USD, x20)')).toBe(amount)
    expect((screen.getByTestId('trade-leverage') as HTMLInputElement).value).toBe('x20')
    expect(manual).toHaveTextContent('buys 1,000 USD of exposure')

    // Out of bounds is refused before anything is sent.
    await user.type(screen.getByTestId('trade-token'), TOKEN)
    await user.clear(amount)
    await user.type(amount, '1100')
    await user.click(screen.getByTestId('trade-prepare'))
    expect(await screen.findByText(/Amount: 50–60 USD/)).toBeInTheDocument()
    expect(screen.queryByTestId('trade-preview')).toBeNull()

    // 50 USD at x20: the preview says invested / x20 / exposure / max loss, the phrase carries REAL.
    await user.clear(amount)
    await user.type(amount, '50')
    await user.click(screen.getByTestId('trade-prepare'))
    const preview = await screen.findByTestId('trade-preview')
    expect(preview).toHaveAttribute('data-verdict', 'allow')
    expect(screen.getByTestId('trade-preview-real')).toHaveTextContent('REAL')
    const sizing = screen.getByTestId('trade-sizing').textContent ?? ''
    expect(sizing).toMatch(/^50 USD invested · x20 · exposure 1,0\d\d USD · max loss 2[34]\.\d\d USD$/)
    expect(screen.getByTestId('trade-confirmation')).toHaveTextContent('PLACE REAL NSDQ100')
    expect(preview).toHaveTextContent(/ref ui-real-nsdq100-/)
    expect(screen.getByTestId('trade-place')).toHaveTextContent('Place · PLACE REAL NSDQ100')
    expect(screen.getByTestId('trade-executor-note')).toHaveTextContent('No executor acts on this ledger')

    // The dialog names the REAL account and the phrase.
    await user.click(screen.getByTestId('trade-place'))
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveTextContent(/Place open REAL NSDQ100 on the REAL account/)
    expect(dialog).toHaveTextContent(/to eToro.s REAL account/)
    expect(dialog).toHaveTextContent('50 USD invested · x20')
  })

  it('a preview parsed from the mock carries the new fields; the demo page is unchanged', async () => {
    forceMockFlag('real-canary')
    const res = await fetch('/api/ui/etoro/trades/prepare?ledger=etoro-real-canary', {
      method: 'POST',
      headers: { Authorization: `Bearer ${TOKEN}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ book: 'nsdq100', side: 'open', amount_usd: 50, leverage: 20 }),
    })
    const pv = tradePreviewSchema.parse(await res.json())
    expect(pv.environment).toBe('real')
    expect(pv.leverage).toBe(20)
    expect(pv.exposure_usd).toBeGreaterThan(999)
    expect(pv.max_loss_usd).toBeGreaterThanOrEqual(23)
    expect(pv.max_loss_usd).toBeLessThanOrEqual(24.5)
    expect(pv.confirmation).toBe('PLACE REAL NSDQ100')
    expect(pv.client_ref).toMatch(/^ui-real-nsdq100-/)
    expect(pv.risk?.policy_version).toBe('etoro-real-canary-2026-10-22')
    // x1 on the canary is refused.
    const bad = await fetch('/api/ui/etoro/trades/prepare?ledger=etoro-real-canary', {
      method: 'POST',
      headers: { Authorization: `Bearer ${TOKEN}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ book: 'nsdq100', side: 'open', amount_usd: 50, leverage: 1 }),
    })
    expect(bad.status).toBe(400)
    expect(((await bad.json()) as { error: string }).error).toMatch(/leverage/)

    forceMockFlag(null)
    renderAt('/trade?ledger=etoro-demo')
    expect(await screen.findByTestId('trade-account-badge')).toHaveTextContent('demo account')
    const amount = (await screen.findByTestId('trade-amount')) as HTMLInputElement
    expect(amount.min).toBe('1000')
    expect(amount.max).toBe('1500')
    expect(screen.getByLabelText('Amount (USD, x1)')).toBe(amount)
    expect((screen.getByTestId('trade-leverage') as HTMLInputElement).value).toBe('x1')
    expect(screen.queryByTestId('real-bar')).toBeNull()
    expect(document.title).not.toMatch(/REAL/)
  })
})

describe('lib/protection: the policy caps in words', () => {
  it('demo and canary limits', async () => {
    const { policyCapsSentence } = await import('../lib/protection')
    expect(policyCapsSentence({ min_sl_pct: 0, max_sl_pct: 0.1, max_loss_pct_equity: 0.005 })).toBe(
      'at most 10% from the entry and 0.50% of equity at the stop',
    )
    expect(policyCapsSentence({ min_sl_pct: 0.023, max_sl_pct: 0.0245, max_loss_pct_equity: 0 })).toBe(
      'between 2.30% and 2.45% from the entry and no equity cap: the whole account is the position',
    )
  })
})

describe('the live quote line (2026-10-22)', () => {
  it('quoteAgeSeconds is whole seconds, never negative, null without a time', async () => {
    const { quoteAgeSeconds } = await import('../lib/format')
    const now = Date.parse('2026-10-22T17:44:10Z')
    expect(quoteAgeSeconds('2026-10-22T17:44:08.4Z', now)).toBe(2)
    expect(quoteAgeSeconds('2026-10-22T17:44:12Z', now)).toBe(0)
    expect(quoteAgeSeconds(undefined, now)).toBeNull()
    expect(quoteAgeSeconds('not a time', now)).toBeNull()
  })
})

describe('the live mark of an open position (2026-10-26)', () => {
  it('is units × (bid − open rate), gross, only for open longs the account holds with a quote', async () => {
    const { liveMark } = await import('../lib/liveMark')
    const snap = { bid: 29338, ask: 29339, updated_at: '2026-10-26T14:55:00Z' } as never
    const open = { status: 'open', units: 0.040925, open_rate: 29323.4, account_holds: true } as never
    const m = liveMark(open, snap)
    expect(m?.bid).toBe(29338)
    expect(m?.pnl).toBeCloseTo(0.5975, 3)
    expect(liveMark({ ...(open as object), status: 'closed' } as never, snap)).toBeNull()
    expect(liveMark({ ...(open as object), account_holds: false } as never, snap)).toBeNull()
    expect(liveMark(open, undefined)).toBeNull()
  })
})
