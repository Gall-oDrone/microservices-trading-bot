import { expect, test, type Page } from '@playwright/test'

// The eToro venue (porting plan P5b): the same production build and MSW
// fixtures as smoke.spec.ts, with the ledger picker on etoro-demo. Every page
// renders from fixtures/etoro-demo with no console error, no contract
// mismatch and no error state, on desktop and on a phone. Times are New York.

const ETORO = '?ledger=etoro-demo'

function watchConsole(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(`console: ${m.text()}`)
  })
  page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`))
  return errors
}

async function expectHealthy(page: Page, errors: string[], allow?: RegExp) {
  await expect(page.getByText(/does not match the UI contract/)).toHaveCount(0)
  await expect(page.getByText('Could not load this view')).toHaveCount(0)
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow, 'page must not scroll horizontally').toBeLessThanOrEqual(1)
  expect(allow ? errors.filter((e) => !allow.test(e)) : errors).toEqual([])
}

test('ledger picker: lists etoro-demo with its venue and switches every view to it', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/forward-tests')
  const picker = page.locator('#ledger-picker')
  await expect(picker.locator('option')).toHaveText([
    /^stage · 8 rec · bitso$/,
    /^dry-run · no file yet · bitso$/,
    /^etoro-demo · 10 rec · etoro$/,
  ])
  await expect(page.locator('.ledger-picker').getByTestId('venue-tag')).toHaveText('bitso')
  await picker.selectOption('etoro-demo')
  await expect(page).toHaveURL(/\/forward-tests\?ledger=etoro-demo$/)
  await expect(page.locator('.ledger-picker').getByTestId('venue-tag')).toHaveText('etoro')
  await expect(
    page.getByRole('heading', { level: 1, name: 'SMA50 forward test · eToro index CFDs (demo)' }),
  ).toBeVisible()
  // Every nav link carries the selection.
  await expect(page.locator('#nav-risk')).toHaveAttribute('href', '/risk?ledger=etoro-demo')
  await expect(page.locator('#nav-data-health')).toHaveAttribute('href', '/data-health?ledger=etoro-demo')
  await expectHealthy(page, errors)
})

test('forward tests: NSDQ100 flat, SPX500 long 0.140735 u, next run in New York', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto(`/forward-tests${ETORO}`)
  await expect(page).toHaveTitle(/Forward tests · eToro/)
  const clock = page.getByTestId('session-clock')
  await expect(clock).toHaveAttribute('data-open', 'true')
  await expect(clock).toContainText('NYSE open')

  const nsdq = page.getByTestId('ft-card-nsdq100')
  await expect(nsdq).toBeVisible()
  await expect(nsdq.locator('header').getByTestId('signal-pill')).toHaveText(/flat/)
  await expect(nsdq.getByTestId('demo-position-nsdq100')).toHaveText('flat')
  await expect(nsdq.getByTestId('last-order-nsdq100')).toContainText('close')
  await expect(nsdq.getByTestId('last-order-nsdq100')).toContainText('executed')
  await expect(nsdq.getByTitle('Ledger: etoro-demo')).toBeVisible()

  const spx = page.getByTestId('ft-card-spx500')
  await expect(spx.locator('header').getByTestId('signal-pill')).toHaveText(/long/)
  await expect(spx.getByTestId('demo-position-spx500')).toHaveText('0.140735 u')
  await expect(spx).toContainText('7,738.57')
  await expect(spx.getByTestId('next-run')).toHaveText('next run 19 Oct 2026, 09:35 NY')
  await expect(spx).toContainText('99,949.90 USD')
  // No Bitso live strip on the eToro cards (P5c).
  await expect(page.getByTestId('live-badge')).toHaveCount(0)
  await expectHealthy(page, errors)

  await spx.locator('#open-spx500').click()
  await expect(page).toHaveURL(/\/forward-tests\/spx500\?ledger=etoro-demo$/)
})

test('forward test detail (spx500): candles, paper vs demo equity, reconcile and the records', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto(`/forward-tests/spx500${ETORO}`)
  await expect(page.getByRole('heading', { level: 1, name: 'SPX500 · S&P 500 CFD' })).toBeVisible()
  await expect(page.locator('canvas').first()).toBeVisible()
  await expect(page.getByTestId('etoro-equity-chart').locator('canvas').first()).toBeVisible()
  await expect(page.getByText('demo account, USD (left axis)')).toBeVisible()
  await expect(page.getByTestId('demo-position-spx500')).toHaveText('0.140735 u')
  const rec = page.getByTestId('detail-reconcile')
  await expect(rec).toContainText('16 Oct 2026')
  await expect(rec.getByTestId('reconcile-state')).toContainText('drift 1')
  await expect(rec.getByTestId('reconcile-recheck')).toContainText('clean')
  const table = page.getByTestId('etoro-records')
  await expect(table.getByRole('columnheader', { name: 'Demo' })).toBeVisible()
  await expect(table.getByRole('row')).toHaveCount(6)
  await expect(table.getByTestId('etoro-record-2026-10-09')).toContainText('executed')
  await expect(table.getByTestId('etoro-record-2026-10-09')).toContainText('7,816.07')
  await page.locator('#range-365').click()
  await expect(page.locator('#range-365')).toHaveAttribute('aria-pressed', 'true')
  await expectHealthy(page, errors)
})

test('risk: the 5 % exposure meter, the policy chips and the P4 risk report', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto(`/risk${ETORO}`)
  await expect(page.getByRole('heading', { level: 1, name: 'Risk' })).toBeVisible()
  await expect(page.getByText('Enforced', { exact: true })).toBeVisible()
  const policy = page.getByTestId('policy-summary')
  await expect(policy).toContainText('x1 only')
  await expect(policy).toContainText('1,500 USD')
  await expect(policy).toContainText('5% of equity')
  await expect(policy).toContainText('cash session required')
  const spx = page.getByTestId('risk-card-spx500')
  const meter = spx.getByTestId('meter-exposure-spx500')
  await expect(meter).toContainText('Exposure vs 5% of equity')
  await expect(meter.getByRole('meter')).toHaveAttribute('aria-valuenow', '22')
  await expect(spx.getByTestId('meter-notional-spx500')).toContainText('1,100 USD / 1,500 USD')
  await expect(page.getByTestId('risk-card-nsdq100').getByTestId('last-check-nsdq100')).toContainText('sent')
  // Operator controls are the shared, venue-neutral card (confirm word is the ledger name).
  const controls = page.getByTestId('operator-controls')
  await expect(controls).toHaveAttribute('data-state', 'running')
  await expect(controls).toContainText('Halt or resume the eToro executor')
  await controls.locator('#control-halt').click()
  const dialog = page.getByRole('dialog', { name: 'Halt trading' })
  await expect(dialog).toContainText('every new demo order on etoro-demo')
  await page.locator('#control-reason').fill('E2E drill: pausing the eToro executor')
  await page.locator('#control-by').fill('e2e')
  await page.locator('#control-confirm').fill('etoro-demo')
  await page.locator('#control-token').fill('mock-token')
  await page.locator('#control-submit').click()
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('controls-flash')).toContainText('Halted etoro-demo')
  await expect(controls).toHaveAttribute('data-state', 'halted')
  await expect(page.locator('#banner-halt')).toContainText('E2E drill: pausing the eToro executor')
  await expect(page.locator('#banner-halt')).toContainText('New York')
  // The P4 report card.
  const report = page.getByTestId('risk-report')
  await expect(report).toBeVisible()
  await expect(report.getByTestId('report-var')).toHaveText('52.81 USD')
  await expect(report).toContainText('0.878')
  await expect(report).toContainText('2008-09 Lehman to the November low')
  await expect(report.getByTestId('fillq-nsdq100')).toContainText('frozen 7.75 bps')
  await expect(report.getByTestId('fillq-spx500')).toContainText('frozen 7.4 bps')
  await expect(report.getByTestId('report-mc')).toContainText('P(beats CFD hold)')
  await page.locator('#mc-scenario-measured').click()
  await expect(page.locator('#mc-scenario-measured')).toHaveAttribute('aria-pressed', 'true')
  await expectHealthy(page, errors)
})

test('data health: etorohealth checks and the 10-16 reconcile as drift with a clean re-check', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto(`/data-health${ETORO}`)
  await expect(page.getByRole('heading', { level: 1, name: 'Data health' })).toBeVisible()
  await expect(page.getByTestId('health-summary')).toContainText('on schedule')
  await expect(page.getByTestId('health-thresholds')).toContainText('09:35 New York')
  await expect(page.getByTestId('check-etoro.reconcile')).toContainText(
    "drift on the run, clean on the operator's re-check",
  )
  await expect(page.getByTestId('check-etoro.run')).toContainText('healthy')
  const row = page.getByTestId('reconcile-2026-10-16')
  await expect(row).toHaveAttribute('data-clean', 'true')
  await expect(row.getByTestId('reconcile-state')).toContainText('drift 1')
  await expect(row.getByTestId('reconcile-state')).toContainText('effective clean')
  await expect(row.getByTestId('reconcile-recheck')).toContainText('clean')
  await expect(row).toContainText('close_not_in_history')
  // Newest first.
  const rows = page.getByTestId('reconcile-list').locator('.reconcile-row')
  await expect(rows).toHaveCount(5)
  await expect(rows.first()).toHaveAttribute('data-testid', 'reconcile-2026-10-16')
  await expectHealthy(page, errors)
})

test('strategies: the kill switch names both venues and halt-all still works in the mock', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto(`/strategies${ETORO}`)
  await expect(page.getByRole('heading', { level: 1, name: 'Strategies' })).toBeVisible()
  const targets = page.getByTestId('killswitch-targets')
  await expect(targets.getByTestId('killswitch-target-stage')).toContainText('bitso')
  await expect(targets.getByTestId('killswitch-target-etoro-demo')).toContainText('etoro')
  await expect(page.getByTestId('ledger-card-etoro-demo')).toHaveAttribute('data-venue', 'etoro')
  await expect(page.getByTestId('ledger-card-stage')).toHaveAttribute('data-venue', 'bitso')
  await expect(page.getByTestId('killswitch-count')).toHaveText('3 of 3 to halt')
  // Intraday strategies are Bitso's: start/stop is held back for the eToro ledger.
  await expect(page.getByTestId('strategies-bitso-only')).toBeVisible()
  await expect(page.locator('#strategy-stop-lp_btc_demo')).toBeDisabled()
  await expectHealthy(page, errors)

  await page.locator('#kill-switch-open').click()
  const ks = page.getByRole('dialog', { name: 'Halt all ledgers' })
  await expect(ks).toContainText('the eToro executor records its demo orders as blocked')
  await page.locator('#control-reason').fill('E2E: kill switch drill, both venues')
  await page.locator('#control-by').fill('e2e')
  await page.locator('#control-confirm').fill('HALT ALL')
  await page.locator('#control-token').fill('mock-token')
  await page.locator('#control-submit').click()
  await expect(ks).toBeHidden()
  const flash = page.getByTestId('halt-all-flash')
  await expect(flash).toContainText('stage halted')
  await expect(flash).toContainText('etoro-demo halted')
  await expect(page.getByTestId('ledger-card-etoro-demo')).toHaveAttribute('data-state', 'halted')
  await expect(page.getByTestId('killswitch-count')).toHaveText('all halted')

  await page.locator('#ledger-risk-etoro-demo').click()
  await expect(page).toHaveURL(/\/risk\?ledger=etoro-demo$/)
  await expect(page.getByTestId('operator-controls')).toHaveAttribute('data-state', 'halted')
  await expect(page.locator('#banner-halt')).toContainText('E2E: kill switch drill, both venues')
  await expectHealthy(page, errors)
})

test('landing: the eToro row next to the Bitso tiles, linking to the eToro forward tests', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/')
  await expect(page.getByTestId('lp-book-btc_mxn')).toBeAttached()
  const row = page.getByTestId('lp-etoro')
  await expect(row.getByTestId('lp-etoro-position-spx500')).toContainText('long 0.140735 u')
  await expect(row.getByTestId('lp-etoro-position-nsdq100')).toHaveText('flat')
  await expect(row.getByTestId('next-run').first()).toContainText('19 Oct 2026, 09:35 NY')
  await expect(row.getByTestId('session-clock')).toContainText('NYSE')
  await expectHealthy(page, errors)
  await page.locator('#lp-open-etoro').scrollIntoViewIfNeeded()
  await page.locator('#lp-open-etoro').click()
  await expect(page).toHaveURL(/\/forward-tests\?ledger=etoro-demo$/)
  await expect(page.getByTestId('ft-card-nsdq100')).toBeVisible()
})

test('market: live NSDQ100 and SPX500 quotes, the NYSE session badge and the provisional flip level', async ({
  page,
}) => {
  const errors = watchConsole(page)
  await page.goto(`/market${ETORO}`)
  await expect(page).toHaveTitle(/Market · eToro/)
  // The mock stream (/api/ui/etoro/stream) connects and the snapshot paints both books.
  await expect(page.getByTestId('live-badge')).toHaveText(/^live$/i)
  const nsdq = page.getByTestId('market-nsdq100')
  await expect(nsdq.getByTestId('bid-nsdq100')).toHaveText('29,189.40')
  await expect(nsdq.getByTestId('ask-nsdq100')).toHaveText('29,190.10')
  await expect(nsdq.getByTestId('spread-nsdq100')).toContainText('0.24 bps')
  await expect(nsdq.getByTestId('flip-nsdq100')).toHaveText('goes long above 29,882.02')
  await expect(nsdq.getByTestId('forming-nsdq100')).toContainText('UTC day 16 Oct 2026')
  await expect(nsdq.getByTestId('closes-nsdq100')).toContainText('29,868.70')
  const spx = page.getByTestId('market-spx500')
  await expect(spx.getByTestId('bid-spx500')).toHaveText('7,649.08')
  await expect(spx.getByTestId('ask-spx500')).toHaveText('7,649.38')
  await expect(spx.getByTestId('flip-spx500')).toHaveText('goes long above 7,707.38')
  await expect(spx.getByText('would flip')).toBeVisible() // recorded long, mid below the flip level
  await expect(spx.getByTestId('closes-spx500')).toContainText('7,738.57')
  const clock = page.getByTestId('session-clock')
  await expect(clock).toHaveAttribute('data-open', 'true')
  await expect(clock).toContainText('NYSE open · closes 16:00 NY')
  await expect(page.locator('#market-etoro-off')).toHaveCount(0)
  await expectHealthy(page, errors)
  await spx.locator('#market-open-spx500').click()
  await expect(page).toHaveURL(/\/forward-tests\/spx500\?ledger=etoro-demo$/)
})

// --- Trade page (P5d): assisted prepare -> place against the mock. ---------
// The mock keeps tokens (real 120 s expiry, single use), the halt from the
// controls mock and the positions in memory per page; `?mock=<scenario>` on
// the page URL stages a scenario (forwarded to the API in mock builds only).

const TOKEN = 'e2e-operator-token-0123456789abcdef0123456789'

async function placeThroughDialog(page: Page, phrase: string, reason: string) {
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText(phrase)
  await page.locator('#control-reason').fill(reason)
  await page.locator('#control-by').fill('e2e')
  await page.locator('#control-confirm').fill(phrase)
  if (
    await page
      .locator('#control-token')
      .inputValue()
      .then((v) => v === '')
  ) {
    await page.locator('#control-token').fill(TOKEN)
  }
  await page.locator('#control-submit').click()
}

test('trade: proposal → prepare → countdown → place with the phrase → outcome, audit tail and the ledger follow', async ({
  page,
}) => {
  const errors = watchConsole(page)
  await page.goto(`/trade${ETORO}`)
  await expect(page).toHaveTitle(/Trade · eToro/)
  await expect(page.locator('#nav-trade')).toHaveAttribute('href', '/trade?ledger=etoro-demo')
  // Both books are in line with the rule in the capture: nothing to propose yet.
  await expect(page.getByTestId('trade-proposal-text-nsdq100')).toContainText('in line (rule flat, demo flat)')
  await expect(page.getByTestId('trade-proposal-text-spx500')).toContainText('in line (rule long, demo long)')
  await expect(page.getByTestId('trade-propose-spx500')).toHaveCount(0)

  // Manual: close SPX500 by hand.
  await page.getByTestId('trade-book').selectOption('spx500')
  await page.getByTestId('trade-side-close').click()
  await expect(page.getByTestId('trade-position')).toHaveValue('3616683295')
  await page.getByTestId('trade-token').fill(TOKEN)
  await page.getByTestId('trade-prepare').click()
  const pv = page.getByTestId('trade-preview')
  await expect(pv).toHaveAttribute('data-verdict', 'allow')
  await expect(pv.getByTestId('trade-confirmation')).toHaveText('CLOSE SPX500')
  await expect(pv.getByTestId('trade-countdown')).toContainText(/expires in [12]:\d\d/)
  await expect(pv.getByTestId('trade-warnings')).toContainText('the rule says long')
  await expect(pv).toContainText('0.140735 u')
  await expect(pv.getByTestId('trade-executor-note')).toContainText("brought back to the rule's target")
  await pv.getByTestId('trade-place').click()
  await placeThroughDialog(page, 'CLOSE SPX500', 'E2E: closing SPX500 by hand')
  const out = page.getByTestId('trade-outcome')
  await expect(out).toHaveAttribute('data-status', 'executed')
  await expect(out).toContainText('ledger recorded')
  await expect(out).toContainText('3616683295')
  await expect(out.getByTestId('trade-fill-quality')).toContainText('bps')
  await expect(page.getByTestId('trade-preview')).toHaveCount(0)

  // The audit tail shows prepare done, place requested, place done.
  const audit = page.getByTestId('trade-audit')
  await expect(audit.getByTestId('audit-row').first()).toContainText('trade.place')
  await expect(audit.getByTestId('audit-row').first()).toContainText('done')
  await expect(audit).toContainText('trade.prepare')

  // The rule (long) now disagrees with the demo (flat): the proposal is an
  // open; prepared from the proposal it is blocked by the one-leg-per-day
  // rule (the close was today's leg), with the reason and no Place button.
  await expect(page.getByTestId('trade-proposal-text-spx500')).toContainText(
    'rule says long, demo is flat → open 1,100 USD',
  )
  await page.getByTestId('trade-propose-spx500').click()
  await expect(pv).toHaveAttribute('data-verdict', 'block')
  await expect(pv.getByTestId('trade-reasons')).toContainText('max_orders_per_day')
  await expect(pv.getByTestId('trade-place')).toHaveCount(0)
  await expect(pv.getByTestId('trade-reprepare')).toContainText('Prepare again')

  // The forward-tests view follows the console trade.
  await page.locator('#nav-forward-tests').click()
  await expect(page.getByTestId('ft-card-spx500').getByTestId('demo-position-spx500')).toHaveText('flat')
  await expect(page.getByTestId('ft-card-spx500').getByTestId('last-order-spx500')).toContainText('close')
  await expectHealthy(page, errors)
})

test('trade: a proposal from the rule fills the form and prepares with the rule’s amount', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto(`/trade${ETORO}`)
  // The capture is in line on both books, so a proposal appears only after a
  // console trade: close SPX500 by hand, then the rule (long) proposes the
  // re-open and the button prepares it (the preview itself blocks on the
  // one-leg-per-day rule, which is the policy).
  await page.getByTestId('trade-book').selectOption('spx500')
  await page.getByTestId('trade-side-close').click()
  await page.getByTestId('trade-token').fill(TOKEN)
  await page.getByTestId('trade-prepare').click()
  await page.getByTestId('trade-place').click()
  await placeThroughDialog(page, 'CLOSE SPX500', 'E2E: closing SPX500 before the proposal')
  await expect(page.getByTestId('trade-outcome')).toHaveAttribute('data-status', 'executed')
  await page.getByTestId('trade-propose-spx500').click()
  await expect(page.getByTestId('trade-side-open')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByTestId('trade-amount')).toHaveValue('1100')
  const pv = page.getByTestId('trade-preview')
  await expect(pv).toContainText('Preview · open SPX500')
  await expect(pv).toContainText('1,100 USD x1')
  await expect(pv.getByTestId('trade-confirmation')).toHaveText('PLACE SPX500')
  await expectHealthy(page, errors)
})

test('trade: an expired token disables Place and offers Re-prepare', async ({ page }) => {
  const errors = watchConsole(page)
  // `?mock=short-ttl`: the mock issues 3 s tokens.
  await page.goto(`/trade${ETORO}&mock=short-ttl`)
  await page.getByTestId('trade-book').selectOption('spx500')
  await page.getByTestId('trade-side-close').click()
  await page.getByTestId('trade-token').fill(TOKEN)
  await page.getByTestId('trade-prepare').click()
  const pv = page.getByTestId('trade-preview')
  await expect(pv).toHaveAttribute('data-verdict', 'allow')
  await expect(pv.getByTestId('trade-place')).toBeEnabled()
  await expect(pv).toHaveAttribute('data-expired', 'true', { timeout: 8000 })
  await expect(pv.getByTestId('trade-countdown')).toContainText('token expired')
  await expect(pv.getByTestId('trade-place')).toBeDisabled()
  await pv.getByTestId('trade-reprepare').click()
  await expect(pv).toHaveAttribute('data-expired', 'false')
  await expect(pv.getByTestId('trade-place')).toBeEnabled()
  await expectHealthy(page, errors)
})

test('trade: a halt between prepare and place is refused with 422 and the reason', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto(`/trade${ETORO}`)
  await page.getByTestId('trade-book').selectOption('spx500')
  await page.getByTestId('trade-side-close').click()
  await page.getByTestId('trade-token').fill(TOKEN)
  await page.getByTestId('trade-prepare').click()
  const pv = page.getByTestId('trade-preview')
  await expect(pv).toHaveAttribute('data-verdict', 'allow')
  // Someone halts the ledger meanwhile (the mock's halt control, same page context).
  const halted = await page.evaluate(async () => {
    const r = await fetch('/api/ui/risk/halt?ledger=etoro-demo', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: 'Bearer mock-token' },
      body: JSON.stringify({ reason: 'E2E: halt during prepare', by: 'e2e', confirm: 'etoro-demo' }),
    })
    return r.status
  })
  expect(halted).toBe(200)
  await pv.getByTestId('trade-place').click()
  await placeThroughDialog(page, 'CLOSE SPX500', 'E2E: placing after a halt')
  const err = page.getByTestId('control-error')
  await expect(err).toContainText('Refused at the re-check')
  await expect(err).toContainText('trading is halted: E2E: halt during prepare')
  await page.locator('#control-cancel').click()
  await expect(pv.getByTestId('trade-refused')).toContainText('trading is halted')
  await expect(pv.getByTestId('trade-place')).toBeDisabled()
  await expect(pv.getByTestId('trade-reprepare')).toBeVisible()
  // Prepared again while halted: a block verdict with the halt as the first reason.
  await pv.getByTestId('trade-reprepare').click()
  await expect(pv).toHaveAttribute('data-verdict', 'block')
  await expect(pv.getByTestId('trade-reasons').locator('li').first()).toContainText(
    'halted: trading is halted: E2E: halt during prepare',
  )
  await expect(page.locator('#trade-halted')).toContainText('E2E: halt during prepare')
  // The refused place is a 422 the browser logs as a resource error: expected here.
  await expectHealthy(page, errors, /status of 422/)
})

test('trade: the kill switch (HALT ALL) makes prepare answer block', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto(`/strategies${ETORO}`)
  await page.locator('#kill-switch-open').click()
  await page.locator('#control-reason').fill('E2E: kill switch before trading')
  await page.locator('#control-by').fill('e2e')
  await page.locator('#control-confirm').fill('HALT ALL')
  await page.locator('#control-token').fill(TOKEN)
  await page.locator('#control-submit').click()
  await expect(page.getByTestId('halt-all-flash')).toContainText('etoro-demo halted')
  await page.locator('#nav-trade').click()
  await expect(page).toHaveURL(/\/trade\?ledger=etoro-demo$/)
  await expect(page.locator('#trade-halted')).toContainText('E2E: kill switch before trading')
  await page.getByTestId('trade-book').selectOption('spx500')
  await page.getByTestId('trade-side-close').click()
  await page.getByTestId('trade-prepare').click()
  const pv = page.getByTestId('trade-preview')
  await expect(pv).toHaveAttribute('data-verdict', 'block')
  await expect(pv.getByTestId('trade-reasons').locator('li').first()).toContainText(
    'halted: trading is halted: E2E: kill switch before trading',
  )
  await expect(pv.getByTestId('trade-place')).toHaveCount(0)
  await expectHealthy(page, errors)
})

test('trade: with trading off the page says why and how to enable it, and offers no preview', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto(`/trade${ETORO}&mock=trading-off`)
  const banner = page.locator('#trade-disabled')
  await expect(banner).toContainText('Assisted trading is off')
  await expect(page.getByTestId('trade-disabled-reason')).toContainText('-etoro-trading')
  await expect(banner).toContainText('-operator-token-file')
  await expect(banner).toContainText('ETORO_ENV=demo')
  await expect(page.getByTestId('trade-proposals')).toBeVisible()
  await expect(page.getByTestId('trade-form')).toHaveCount(0)
  await expect(page.getByTestId('trade-propose-spx500')).toHaveCount(0)
  await expectHealthy(page, errors)
})

test('trade: a Bitso ledger gets the note and the link to the eToro ledger', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/trade')
  await expect(page.getByTestId('trade-bitso-note')).toContainText('eToro demo account')
  await page.locator('#trade-switch-etoro').click()
  await expect(page).toHaveURL(/\/trade\?ledger=etoro-demo$/)
  await expect(page.getByTestId('trade-proposals')).toBeVisible()
  await expectHealthy(page, errors)
})
