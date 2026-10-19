import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { etoroForwardTestsResponseSchema, tradePlaceResponseSchema, tradePreviewSchema } from '../api/etoroSchemas'
import { controlsInfoSchema } from '../api/schemas'
import { deriveProposals, fmtCountdown, placeTrade, prepareTrade, recommendTrade, secondsLeft } from '../api/trades'
import controls from '../mocks/fixtures/etoro-demo/controls.json'
import forwardTests from '../mocks/fixtures/etoro-demo/etoro-forward-tests.json'
import outcome from '../mocks/fixtures/etoro-demo/etoro-trade-outcome.json'
import preview from '../mocks/fixtures/etoro-demo/etoro-trade-preview.json'
import { resetMockTrades } from '../mocks/trades'
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
  localStorage.clear()
  resetMockTrades()
})
afterEach(() => {
  vi.useRealTimers()
})

describe('contract: the assisted-trade shapes parse', () => {
  it.each([
    ['trade preview (prepare)', tradePreviewSchema, preview],
    ['trade outcome (place)', tradePlaceResponseSchema, outcome],
    ['controls (etoro-demo) with trading', controlsInfoSchema, controls],
  ])('%s', (_n, schema, data) => {
    const r = schema.safeParse(data)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues.slice(0, 3))).toBe(true)
  })

  it('reads the facts the page shows', () => {
    const pv = tradePreviewSchema.parse(preview)
    expect(pv.verdict).toBe('allow')
    expect(pv.confirmation).toBe('PLACE NSDQ100')
    expect(pv.expires_in_s).toBe(120)
    expect(pv.warnings[0]).toMatch(/rule says flat/)
    const o = tradePlaceResponseSchema.parse(outcome)
    expect(o.ledger_recorded).toBe(true)
    expect(o.audit.ref).toBe(o.client_ref)
    const c = controlsInfoSchema.parse(controls)
    // The capture is from a backend without -etoro-trading: off, with the reason.
    expect(c.trading?.enabled).toBe(false)
    expect(c.trading?.disabled_reason).toMatch(/-etoro-trading/)
  })

  it('a console ledger line (signal manual, source ui) parses with the ledger record schema', async () => {
    const { etoroLedgerRecordSchema } = await import('../api/etoroSchemas')
    const line = {
      schema: 'etoro-ledger/v1',
      recorded_at: '2026-10-19T15:00:41Z',
      code_version: 'ui-api dev',
      mode: 'demo',
      venue: 'etoro',
      book: 'nsdq100',
      instrument: { symbol: 'NSDQ100', id: 28 },
      prereg: '',
      decision: {
        bar_date: '2026-10-19',
        fill_date: '2026-10-19',
        close: 29868.7,
        sma50: 29871.96,
        signal: 'manual',
        prev_signal: 'flat',
        action: 'buy',
      },
      paper: {
        forward_start: '',
        started: false,
        days: 0,
        position: '',
        round_trips: 0,
        spread_rt_bps: 0,
        overnight_bps: 0,
        costs_status: '',
        equity_if_closed: 0,
        hold_equity_if_closed: 0,
        max_drawdown: 0,
        financing_pct: 0,
        pending_action: '',
      },
      candles: { source: '', first: '', last: '', bars: 0, missing_trading_days: 0, sha256_prefix: '' },
      demo: {
        env: 'demo',
        target: 'long',
        action: 'open',
        position_before: { state: 'flat' },
        position_after: outcome.position_after,
        order: {
          kind: 'open',
          client_ref: outcome.client_ref,
          request_id: outcome.request_id,
          status: 'executed',
          intent_at: '2026-10-19T15:00:38Z',
          started: '2026-10-19T15:00:38Z',
          finished: '2026-10-19T15:00:41Z',
        },
      },
      source: 'ui',
      operator: { by: 'diego', reason: 'Re-entering NSDQ100 by hand' },
    }
    const r = etoroLedgerRecordSchema.safeParse(line)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
  })
})

describe('countdown helpers', () => {
  it('secondsLeft counts whole seconds down to 0 and never below', () => {
    const now = Date.parse('2026-10-19T15:00:04Z')
    expect(secondsLeft('2026-10-19T15:02:04Z', now)).toBe(120)
    expect(secondsLeft('2026-10-19T15:00:04.5Z', now)).toBe(1)
    expect(secondsLeft('2026-10-19T15:00:04Z', now)).toBe(0)
    expect(secondsLeft('2026-10-19T14:59:00Z', now)).toBe(0)
    expect(secondsLeft(undefined, now)).toBe(0)
    expect(secondsLeft('not a time', now)).toBe(0)
  })
  it('fmtCountdown is m:ss', () => {
    expect(fmtCountdown(120)).toBe('2:00')
    expect(fmtCountdown(65)).toBe('1:05')
    expect(fmtCountdown(9)).toBe('0:09')
    expect(fmtCountdown(0)).toBe('0:00')
  })
})

describe('deriveProposals: the rule against the demo position', () => {
  const books = etoroForwardTestsResponseSchema.parse(forwardTests).books
  it('the fixture is in line on both books; the held SPX500 has no stop, so it is a protect proposal', () => {
    const ps = deriveProposals(books, 1100)
    expect(ps.map((p) => [p.book, p.kind])).toEqual([
      ['nsdq100', 'none'],
      ['spx500', 'protect'],
    ])
    expect(ps[0].text).toBe('in line (rule flat, demo flat), nothing to do')
    expect(ps[0].protected).toBeUndefined()
    expect(ps[1].text).toBe('in line (rule long, demo long) but position 3616683295 has no stop loss → protect it')
    expect(ps[1].positionText).toBe('long 0.140735 u')
    expect(ps[1].positionId).toBe('3616683295')
    expect(ps[1].protected).toBe(false)
  })
  it('a held position with a stop is in line and protected', () => {
    const b = { ...books[1], demo_position: { ...books[1].demo_position!, stop_loss_rate: 7550.62 } }
    const [p] = deriveProposals([b], 1100)
    expect(p.kind).toBe('none')
    expect(p.protected).toBe(true)
    expect(p.text).toBe('in line (rule long, demo long), nothing to do')
  })
  it('rule long + demo flat proposes an open of the default amount, with a stop', () => {
    const b = { ...books[0], decision: { ...books[0].decision, signal: 'long' as const } }
    const [p] = deriveProposals([b], 1100)
    expect(p.kind).toBe('open')
    expect(p.amountUsd).toBe(1100)
    expect(p.text).toBe('rule says long, demo is flat → open 1,100 USD x1 (with a stop)')
  })
  it('rule flat + demo long proposes a close of the ledger position', () => {
    const b = { ...books[1], decision: { ...books[1].decision, signal: 'flat' as const } }
    const [p] = deriveProposals([b], 1100)
    expect(p.kind).toBe('close')
    expect(p.positionId).toBe('3616683295')
    expect(p.text).toBe('rule says flat, demo is long → close position 3616683295')
  })
  it('no rule record: nothing to propose', () => {
    const b = { ...books[0], decision: { ...books[0].decision, signal: '' as const } }
    expect(deriveProposals([b], 1100)[0].text).toMatch(/no rule record/)
  })
})

describe('protection against the mock endpoints', () => {
  it('recommend answers three ordered profiles for an open and for the held position', async () => {
    const rec = await recommendTrade({ book: 'nsdq100', side: 'open', amountUsd: 1100, token: TOKEN }, 'etoro-demo')
    expect(rec.default_profile).toBe('moderate')
    expect(Object.keys(rec.profiles).sort()).toEqual(['aggressive', 'conservative', 'moderate'])
    const c = rec.profiles.conservative
    const m = rec.profiles.moderate
    const a = rec.profiles.aggressive
    expect(c.sl_rate).toBeGreaterThan(m.sl_rate)
    expect(m.sl_rate).toBeGreaterThan(a.sl_rate)
    expect(c.tp_rate!).toBeLessThan(m.tp_rate!)
    expect(m.rr).toBeCloseTo(2, 0)
    expect(m.feasible).toBe(true)
    expect(rec.context.bounds.min_sl_pct).toBe(0.01)
    expect(rec.context.volatility?.regime).toBe('normal')
    expect(rec.context.limits.max_sl_pct).toBe(0.1)

    const prot = await recommendTrade({ book: 'spx500', side: 'protect', token: TOKEN }, 'etoro-demo')
    expect(prot.position.position_id).toBe('3616683295')
    expect(prot.context.entry).toBeCloseTo(7816.07, 6)
    expect(prot.context.before?.protected).toBe(false)
    for (const lv of Object.values(prot.profiles)) expect(lv.sl_rate).toBeLessThan(prot.context.mark)

    await expect(
      recommendTrade({ book: 'nsdq100', side: 'protect', token: TOKEN }, 'etoro-demo'),
    ).rejects.toMatchObject({
      status: 409,
    })
  })

  it('an open without a profile gets the default stop and the warning; custom distances are clamped', async () => {
    // SPX500 is held: close it first so an open is eligible (one leg per day is not hit: the fixture has no SPX500 leg today).
    const close = await prepareTrade(
      { book: 'spx500', side: 'close', positionId: '3616683295', token: TOKEN },
      'etoro-demo',
    )
    await placeTrade(
      { tradeToken: close.token!, confirm: 'CLOSE SPX500', reason: 'vitest: make room', by: 'vitest', token: TOKEN },
      'etoro-demo',
    )
    // ... which used the day's leg; the preview still shows the protection on the block verdict.
    const pv = await prepareTrade({ book: 'spx500', side: 'open', amountUsd: 1100, token: TOKEN }, 'etoro-demo')
    expect(pv.protection?.profile).toBe('moderate')
    expect(pv.protection?.levels.feasible).toBe(true)
    expect(pv.protection!.levels.sl_rate).toBeLessThan(pv.quote!.ask)
    expect(pv.warnings.join(' ')).toMatch(/no stop requested: the moderate profile/)
    expect(pv.risk?.order.stop_loss).toBe(pv.protection!.levels.sl_rate)

    const custom = await prepareTrade(
      {
        book: 'nsdq100',
        side: 'open',
        amountUsd: 1100,
        profile: 'custom',
        stopLossPct: 0.005,
        takeProfitPct: 0.03,
        token: TOKEN,
      },
      'etoro-demo',
    )
    expect(custom.protection?.profile).toBe('custom')
    expect(custom.protection!.levels.sl_pct).toBeCloseTo(0.01, 3)
    expect(custom.protection!.levels.clamps[0]).toMatch(/floor/)
    const wide = await prepareTrade(
      { book: 'nsdq100', side: 'open', amountUsd: 1100, profile: 'custom', stopLossPct: 0.15, token: TOKEN },
      'etoro-demo',
    )
    expect(wide.protection!.levels.sl_pct).toBeCloseTo(0.1, 3)
    expect(wide.protection!.levels.tp_rate).toBeUndefined()
    await expect(
      prepareTrade({ book: 'nsdq100', side: 'open', amountUsd: 1100, profile: 'custom', token: TOKEN }, 'etoro-demo'),
    ).rejects.toMatchObject({ status: 400 })
  })

  it('protect the held SPX500: PROTECT phrase, the position gains its stop, not a leg', async () => {
    const pv = await prepareTrade(
      { book: 'spx500', side: 'protect', positionId: '3616683295', token: TOKEN },
      'etoro-demo',
    )
    expect(pv.verdict).toBe('allow')
    expect(pv.confirmation).toBe('PROTECT SPX500')
    expect(pv.protection?.before?.protected).toBe(false)
    const sl = pv.protection!.levels.sl_rate
    const r = await placeTrade(
      {
        tradeToken: pv.token!,
        confirm: 'PROTECT SPX500',
        reason: 'vitest: stop on the open position',
        by: 'vitest',
        token: TOKEN,
      },
      'etoro-demo',
    )
    expect(r.status).toBe('executed')
    expect(r.side).toBe('protect')
    expect(r.position_after.state).toBe('long')
    expect(r.position_after.stop_loss_rate).toBe(sl)
    expect(r.protection?.operation_id).toBe(r.order_id)
    // The position follows and is in line now; a close is still possible today (a protect is not a leg).
    const res = await fetch('/api/ui/etoro/forward-tests?ledger=etoro-demo')
    const ft = etoroForwardTestsResponseSchema.parse(await res.json())
    const spx = ft.books.find((b) => b.book === 'spx500')!
    expect(spx.demo_position?.stop_loss_rate).toBe(sl)
    expect(deriveProposals([spx], 1100)[0].kind).toBe('none')
    const again = await prepareTrade(
      { book: 'spx500', side: 'protect', positionId: '3616683295', profile: 'custom', stopLossPct: 0.09, token: TOKEN },
      'etoro-demo',
    )
    expect(again.protection?.before?.protected).toBe(true)
    expect(again.warnings.join(' ')).toMatch(/loosens the protection/)
    const close = await prepareTrade(
      { book: 'spx500', side: 'close', positionId: '3616683295', token: TOKEN },
      'etoro-demo',
    )
    expect(close.verdict).toBe('allow')
  })
})

describe('the mock endpoints behave like ui-api', () => {
  it('prepare then place a close of SPX500; a second use of the token is a 409; the position follows', async () => {
    const pv = await prepareTrade(
      { book: 'spx500', side: 'close', positionId: '3616683295', token: TOKEN },
      'etoro-demo',
    )
    expect(pv.verdict).toBe('allow')
    expect(pv.confirmation).toBe('CLOSE SPX500')
    expect(pv.token).toBeTruthy()
    expect(pv.units).toBe(0.140735)
    expect(pv.warnings[0]).toMatch(/rule says long/)
    const r = await placeTrade(
      { tradeToken: pv.token!, confirm: 'CLOSE SPX500', reason: 'vitest: closing by hand', by: 'vitest', token: TOKEN },
      'etoro-demo',
    )
    expect(r.status).toBe('executed')
    expect(r.position_after.state).toBe('flat')
    expect(r.ledger_recorded).toBe(true)
    await expect(
      placeTrade(
        { tradeToken: pv.token!, confirm: 'CLOSE SPX500', reason: 'vitest: twice', by: 'vitest', token: TOKEN },
        'etoro-demo',
      ),
    ).rejects.toMatchObject({ status: 409 })
    // Now the rule (long) disagrees with the demo (flat): the next proposal is an open.
    const res = await fetch('/api/ui/etoro/forward-tests?ledger=etoro-demo')
    const ft = etoroForwardTestsResponseSchema.parse(await res.json())
    const spx = ft.books.find((b) => b.book === 'spx500')!
    expect(spx.demo_position?.state).toBe('flat')
    expect(spx.last_order?.source).toBe('ui')
    expect(deriveProposals([spx], 1100)[0].kind).toBe('open')
    // ... but one leg per day: the open is blocked.
    const again = await prepareTrade({ book: 'spx500', side: 'open', amountUsd: 1100, token: TOKEN }, 'etoro-demo')
    expect(again.verdict).toBe('block')
    expect(again.reasons.join(' ')).toMatch(/max_orders_per_day/)
    expect(again.token).toBeUndefined()
  })

  it('wrong confirm 400, bad amount 400, no bearer 401', async () => {
    const pv = await prepareTrade(
      { book: 'spx500', side: 'close', positionId: '3616683295', token: TOKEN },
      'etoro-demo',
    )
    await expect(
      placeTrade(
        { tradeToken: pv.token!, confirm: 'PLACE SPX500', reason: 'vitest: wrong phrase', by: 'vitest', token: TOKEN },
        'etoro-demo',
      ),
    ).rejects.toMatchObject({ status: 400 })
    await expect(
      prepareTrade({ book: 'nsdq100', side: 'open', amountUsd: 50, token: TOKEN }, 'etoro-demo'),
    ).rejects.toMatchObject({ status: 400 })
    await expect(
      prepareTrade({ book: 'nsdq100', side: 'open', amountUsd: 1100, token: '' }, 'etoro-demo'),
    ).rejects.toMatchObject({ status: 401 })
  })
})

describe('Trade page (?ledger=etoro-demo)', () => {
  it('shows the proposals, prepares a manual close, counts down and places it through the dialog', async () => {
    const user = userEvent.setup()
    renderAt('/trade?ledger=etoro-demo')
    expect(await screen.findByRole('heading', { level: 1, name: 'Trade' })).toBeInTheDocument()
    const proposals = await screen.findByTestId('trade-proposals')
    expect(within(proposals).getByTestId('trade-proposal-text-nsdq100')).toHaveTextContent(
      'in line (rule flat, demo flat)',
    )
    // SPX500 is held without a stop: the proposal is to protect it.
    expect(within(proposals).getByTestId('trade-proposal-text-spx500')).toHaveTextContent(
      'has no stop loss → protect it',
    )
    expect(within(proposals).getByTestId('trade-protected-spx500')).toHaveTextContent('no stop')
    expect(screen.getByTestId('trade-propose-spx500')).toHaveTextContent('Prepare protection')

    // Manual: close SPX500.
    await user.selectOptions(screen.getByTestId('trade-book'), 'spx500')
    await user.click(screen.getByTestId('trade-side-close'))
    expect(screen.getByTestId('trade-position')).toHaveValue('3616683295')
    await user.type(screen.getByTestId('trade-token'), TOKEN)
    await user.click(screen.getByTestId('trade-prepare'))
    const pv = await screen.findByTestId('trade-preview')
    expect(pv).toHaveAttribute('data-verdict', 'allow')
    expect(within(pv).getByTestId('trade-confirmation')).toHaveTextContent('CLOSE SPX500')
    expect(within(pv).getByTestId('trade-countdown')).toHaveTextContent(/expires in [12]:\d\d/)
    expect(within(pv).getByTestId('trade-warnings')).toHaveTextContent('the rule says long')
    expect(within(pv).getByTestId('trade-executor-note')).toHaveTextContent("brought back to the rule's target")
    expect(getOperatorTokenFromStorage()).toBe(TOKEN)

    await user.click(within(pv).getByTestId('trade-place'))
    const dialog = await screen.findByTestId('control-dialog')
    expect(dialog).toHaveTextContent('CLOSE SPX500')
    expect(screen.getByTestId('control-token')).toHaveValue(TOKEN)
    await user.type(screen.getByTestId('control-reason'), 'vitest: closing SPX500 by hand')
    await user.type(screen.getByTestId('control-by'), 'vitest')
    await user.type(screen.getByTestId('control-confirm'), 'CLOSE SPX500')
    await user.click(screen.getByTestId('control-submit'))
    const out = await screen.findByTestId('trade-outcome')
    expect(out).toHaveAttribute('data-status', 'executed')
    expect(out).toHaveTextContent('ledger recorded')
    expect(out).toHaveTextContent('3616683295')
    expect(within(out).getByTestId('trade-fill-quality')).toBeInTheDocument()
    expect(screen.queryByTestId('trade-preview')).not.toBeInTheDocument()
    // The audit tail and the proposals follow.
    await waitFor(() => expect(screen.getByTestId('trade-audit')).toHaveTextContent('trade.place'))
    expect(screen.getByTestId('trade-audit')).toHaveTextContent('done')
    await waitFor(() =>
      expect(screen.getByTestId('trade-proposal-text-spx500')).toHaveTextContent(
        'rule says long, demo is flat → open 1,100 USD',
      ),
    )
    expect(screen.getByTestId('trade-propose-spx500')).toBeInTheDocument()
  })

  it('a block verdict shows the reasons and no Place button; the form validates the amount', async () => {
    const user = userEvent.setup()
    renderAt('/trade?ledger=etoro-demo')
    await screen.findByTestId('trade-form')
    await user.type(screen.getByTestId('trade-amount'), '99')
    await user.type(screen.getByTestId('trade-token'), TOKEN)
    await user.click(screen.getByTestId('trade-prepare'))
    expect(await screen.findByText(/Amount: 1,000–1,500 USD/)).toBeInTheDocument()
    await user.clear(screen.getByTestId('trade-amount'))
    await user.type(screen.getByTestId('trade-amount'), '1100')
    // NSDQ100 had the executor's leg today: one leg per day blocks the open.
    await user.click(screen.getByTestId('trade-prepare'))
    const pv = await screen.findByTestId('trade-preview')
    expect(pv).toHaveAttribute('data-verdict', 'block')
    expect(within(pv).getByTestId('trade-reasons')).toHaveTextContent('max_orders_per_day')
    expect(within(pv).queryByTestId('trade-place')).not.toBeInTheDocument()
    expect(within(pv).getByTestId('trade-reprepare')).toHaveTextContent('Prepare again')
  })

  it('an expired token disables Place and offers Re-prepare', async () => {
    const user = userEvent.setup()
    renderAt('/trade?ledger=etoro-demo')
    await screen.findByTestId('trade-form')
    await user.selectOptions(screen.getByTestId('trade-book'), 'spx500')
    await user.click(screen.getByTestId('trade-side-close'))
    await user.type(screen.getByTestId('trade-token'), TOKEN)
    await user.click(screen.getByTestId('trade-prepare'))
    const pv = await screen.findByTestId('trade-preview')
    expect(within(pv).getByTestId('trade-place')).toBeEnabled()
    // Move the clock past the expiry: the countdown reads it on its next tick.
    const spy = vi.spyOn(Date, 'now').mockImplementation(() => new Date().getTime() + 130_000)
    await waitFor(() => expect(pv).toHaveAttribute('data-expired', 'true'), { timeout: 3000 })
    expect(within(pv).getByTestId('trade-countdown')).toHaveTextContent('token expired')
    expect(within(pv).getByTestId('trade-place')).toBeDisabled()
    expect(within(pv).getByTestId('trade-reprepare')).toBeInTheDocument()
    spy.mockRestore()
  })

  it('trading disabled: the reason, how to enable it, proposals read-only, no form', async () => {
    // The capture itself: a dev backend without -etoro-trading (the mock
    // overlays "enabled" otherwise; in the browser `?mock=trading-off` does this).
    server.use(http.get('/api/ui/controls', () => HttpResponse.json(controls)))
    renderAt('/trade?ledger=etoro-demo')
    const banner = await screen.findByTestId('trade-disabled-reason')
    expect(banner).toHaveTextContent('-etoro-trading')
    expect(document.getElementById('trade-disabled')).toHaveTextContent('-operator-token-file')
    await screen.findByTestId('trade-proposals')
    expect(screen.queryByTestId('trade-form')).not.toBeInTheDocument()
    expect(screen.queryByTestId('trade-propose-spx500')).not.toBeInTheDocument()
  })

  it('a Bitso ledger gets the note and the link to the eToro ledger', async () => {
    renderAt('/trade')
    expect(await screen.findByTestId('trade-bitso-note')).toHaveTextContent('eToro demo account')
    expect(screen.getByRole('link', { name: /Open the Trade page for etoro-demo/ })).toHaveAttribute(
      'href',
      '/trade?ledger=etoro-demo',
    )
  })
})

function getOperatorTokenFromStorage() {
  return sessionStorage.getItem('mtb-operator-token')
}
