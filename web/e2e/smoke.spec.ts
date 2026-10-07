import { expect, test, type Page } from '@playwright/test'

// Every page must render from the captured ui-api fixtures with no console
// error, no uncaught exception, no contract (schema) mismatch and no error
// state, on desktop and on a phone.

function watchConsole(page: Page): string[] {
  const errors: string[] = []
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(`console: ${m.text()}`)
  })
  page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`))
  return errors
}

async function expectHealthy(page: Page, errors: string[]) {
  await expect(page.getByText(/does not match the UI contract/)).toHaveCount(0)
  await expect(page.getByText('Could not load this view')).toHaveCount(0)
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow, 'page must not scroll horizontally').toBeLessThanOrEqual(1)
  expect(errors).toEqual([])
}

test('landing: hero with live price and chart, sections, and the way into the console', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/')
  await expect(page).toHaveTitle(/SMA50 forward tests on Bitso/)
  await expect(page.getByRole('heading', { level: 1, name: /One frozen trend rule/ })).toBeVisible()
  const hero = page.getByTestId('hero-card')
  await expect(hero.getByTestId('live-badge')).toHaveText(/^live$/i)
  await expect(hero.getByTestId('live-price')).toHaveText(/\d/)
  await expect(hero.getByTestId('signal-pill')).toHaveText(/long|flat/)
  // Hovering the chart reads a day.
  const plot = hero.getByTestId('trend-chart').getByRole('img')
  await plot.hover({ position: { x: 40, y: 60 } })
  await expect(hero.getByTestId('trend-tip')).toContainText('SMA50')
  await page.locator('#hero-book-btc_usd').click()
  await expect(hero.getByTestId('trend-chart')).toContainText('Daily close (USD)')
  // Sections render from the fixtures.
  await expect(page.getByTestId('lp-book-btc_mxn')).toBeAttached()
  await expect(page.getByTestId('lp-timeline')).toBeAttached()
  await expect(page.getByTestId('lp-studies').getByRole('link')).toHaveCount(3)
  await expect(page.getByTestId('lp-guards')).toContainText('enforced')
  await expectHealthy(page, errors)
  await page.locator('#cta-open-console').click()
  await expect(page).toHaveURL(/\/forward-tests$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Forward tests' })).toBeVisible()
  await page.locator('#brand-home').click()
  await expect(page).toHaveURL(/\/$/)
})

test('forward tests: one card per book with signal and ledger', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/forward-tests')
  await expect(page).toHaveTitle(/Forward tests/)
  await expect(page.getByRole('heading', { level: 1, name: 'Forward tests' })).toBeVisible()
  for (const book of ['btc_mxn', 'btc_usd']) {
    const card = page.getByTestId(`ft-card-${book}`)
    await expect(card).toBeVisible()
    await expect(card.locator('header').getByTestId('signal-pill')).toHaveText(/long|flat/)
    await expect(card.getByTitle('Ledger: stage')).toBeVisible()
    // Live strip over SSE: provisional label, price, live badge.
    const strip = card.getByTestId(`live-${book}`)
    await expect(strip.getByTestId('live-badge')).toHaveText(/^live$/i)
    await expect(strip.getByText('provisional: if today closed now')).toBeVisible()
    await expect(strip.getByTestId('live-price')).toHaveText(/\d/)
  }
  await expectHealthy(page, errors)
})

test('forward test detail: chart, fills and ledger', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/forward-tests/btc_mxn')
  await expect(page.getByRole('heading', { level: 1, name: 'BTC / MXN' })).toBeVisible()
  await expect(page.locator('canvas').first()).toBeVisible()
  await expect(page.getByRole('columnheader', { name: 'Fill day' })).toBeVisible()
  await expect(page.getByText(/records, newest first/)).toBeVisible()
  await expect(page.getByTestId('live-btc_mxn').getByText('If today closed now', { exact: true })).toBeVisible()
  await expect(page.getByText('flip level (provisional)')).toBeVisible()
  await expectHealthy(page, errors)
})

test('risk: enforced, per-book exposure and policy', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/risk')
  await expect(page.getByRole('heading', { level: 1, name: 'Risk' })).toBeVisible()
  await expect(page.getByText('Enforced', { exact: true })).toBeVisible()
  await expect(page.getByTestId('risk-card-btc_mxn')).toBeVisible()
  await expect(page.getByTestId('risk-card-btc_usd')).toBeVisible()
  await expect(page.getByText('Max order size')).toBeVisible()
  await expectHealthy(page, errors)
})

test('ledger picker switches to the dry-run ledger and links keep it', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/forward-tests')
  await expect(page.getByTestId('ft-card-btc_mxn')).toBeVisible()
  await page.getByRole('combobox', { name: 'Ledger' }).selectOption('dry-run')
  await expect(page).toHaveURL(/\?ledger=dry-run$/)
  await expect(page.getByText('No records in the dry-run ledger yet')).toHaveCount(2)
  await page.locator('#nav-risk').click()
  await expect(page).toHaveURL(/\/risk\?ledger=dry-run$/)
  await expect(page.getByTitle('Ledger: dry-run').first()).toBeVisible()
  await expectHealthy(page, errors)
})

test('research: study list, filter and a study with its lineage', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/forward-tests')
  await page.locator('#nav-research').click()
  await expect(page).toHaveURL(/\/research$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Research' })).toBeVisible()
  await page.locator('#kind-preregistration').click()
  await expect(page.getByTestId(/^study-/)).toHaveCount(2)
  await page.locator('#open-FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27').click()
  await expect(page.getByRole('heading', { level: 1, name: /Pre-Registration/ })).toBeVisible()
  await expect(page.getByTestId('study-prose').getByRole('heading', { name: '1. Rule (frozen)' })).toBeVisible()
  await page.getByTestId('study-prose').locator('a[data-study="BTC-MXN-TREND-CHECK-2026-09-27"]').first().click()
  await expect(page).toHaveURL(/\/research\/BTC-MXN-TREND-CHECK-2026-09-27$/)
  await expect(page.getByRole('heading', { level: 1, name: /Does the BTC-USD Result Transfer/ })).toBeVisible()
  await expectHealthy(page, errors)
})

test('backtest runs: list, matrix, window detail and cost sensitivity', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/research')
  await page.locator('#nav-runs').click()
  await expect(page).toHaveURL(/\/research\/runs$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Backtest runs' })).toBeVisible()
  await page.locator('#cost-frictionless').click()
  await expect(page).toHaveURL(/cost=frictionless/)
  await page.locator('#cost-all').click()
  await page.locator('#open-run-btc-mxn-taker-2026-09-27').click()
  await expect(page).toHaveURL(/\/research\/runs\/2026-09-27\/btc-mxn-taker$/)
  await expect(page.getByRole('heading', { level: 1, name: 'btc-mxn-taker' })).toBeVisible()
  await expect(page.getByTestId('run-matrix')).toBeVisible()
  await page.locator('#win-1').click()
  await expect(page).toHaveURL(/\?w=1$/)
  await expect(page.getByTestId('window-detail').getByRole('heading', { level: 2 })).toContainText('2026-01-01')
  await expect(page.getByTestId('cost-sensitivity').getByRole('row')).toHaveCount(5)
  await expectHealthy(page, errors)
})

test('backtest runs: development vs holdout comparison (weekly-research)', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/research/runs')
  await page.locator('#open-run-weekly-research-btc-mxn-2026-10-03').click()
  await expect(page).toHaveURL(/\/research\/runs\/2026-10-03\/weekly-research-btc-mxn$/)
  const cmp = page.getByTestId('run-compare')
  await expect(cmp.getByRole('heading', { level: 2, name: 'Development vs holdout' })).toBeVisible()
  await expect(cmp.getByTestId('cmp-sma50_volume_1.5x').getByText('2/2')).toHaveCount(2)
  await page.locator('#sort-vs_hold').click()
  await expect(page).toHaveURL(/\?sort=vs_hold$/)
  await expect(cmp.getByTestId(/^cmp-/).nth(1)).toHaveAttribute('data-testid', 'cmp-sma50_volume_1.5x')
  await expect(page.getByTestId('window-detail').getByRole('heading', { level: 2 })).toContainText('2024-10-01')
  await expect(page.getByTestId('event-study')).toBeVisible()
  await expect(page.getByTestId('sensitivity').getByText('post hoc')).toBeVisible()
  await expect(page.getByTestId('cost-sensitivity').getByRole('row')).toHaveCount(3)
  await expectHealthy(page, errors)
})

test('theme: dark by default, switches to light and persists', async ({ page }) => {
  const errors = watchConsole(page)
  await page.goto('/forward-tests/btc_mxn')
  await expect(page.locator('canvas').first()).toBeVisible()
  await expect(page.locator('html')).not.toHaveAttribute('data-theme', 'light')
  const bodyBg = () => page.evaluate(() => getComputedStyle(document.body).backgroundColor)
  const dark = await bodyBg()
  await page.locator('#theme-toggle').click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expect(page.locator('#theme-toggle')).toHaveAttribute('aria-pressed', 'true')
  expect(await bodyBg()).not.toBe(dark)
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expect(page.locator('canvas').first()).toBeVisible()
  await page.goto('/research/runs/2026-09-27/btc-mxn-taker')
  await expect(page.getByTestId('run-matrix')).toBeVisible()
  await expectHealthy(page, errors)
})

test('unknown routes show the not-found page', async ({ page }) => {
  await page.goto('/nope')
  await expect(page.getByRole('link', { name: 'Back to forward tests' })).toBeVisible()
})
