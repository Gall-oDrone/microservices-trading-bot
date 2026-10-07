import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it } from 'vitest'
import { dataHealthSchema, type DataHealth, type HealthCheck } from '../api/schemas'
import { countByStatus, flushTimeline, fmtMinutes, minutesBetween, sortChecks, statusTone } from '../lib/health'
import dryHealth from '../mocks/fixtures/dry-run/health-data.json'
import stageHealth from '../mocks/fixtures/stage/health-data.json'
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

const stage = (): DataHealth => dataHealthSchema.parse(structuredClone(stageHealth))

describe('contract: /api/ui/health/data', () => {
  it.each([
    ['stage', stageHealth],
    ['dry-run', dryHealth],
  ])('%s fixture parses', (_n, data) => {
    const r = dataHealthSchema.safeParse(data)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues[0])).toBe(true)
  })
})

describe('health helpers', () => {
  const c = (id: string, status: HealthCheck['status']): HealthCheck => ({
    id,
    area: 'executor',
    label: id,
    status,
    message: '',
  })

  it('sorts checks worst first, keeping server order for ties', () => {
    const xs = [c('a', 'ok'), c('b', 'warn'), c('c', 'off'), c('d', 'fail'), c('e', 'warn'), c('f', 'unknown')]
    expect(sortChecks(xs).map((x) => x.id)).toEqual(['d', 'b', 'e', 'f', 'c', 'a'])
    expect(countByStatus(xs)).toEqual({ ok: 1, off: 1, unknown: 1, warn: 2, fail: 1 })
    expect(statusTone('fail')).toBe('block')
  })

  it('formats minutes and measures against generated_at', () => {
    expect(fmtMinutes(4.5)).toBe('5 min')
    expect(fmtMinutes(150)).toBe('2.5 h')
    expect(fmtMinutes(180)).toBe('3 h')
    expect(fmtMinutes(6 * 1440 + 10)).toBe('6 days')
    expect(fmtMinutes(NaN)).toBe('—')
    expect(minutesBetween('2026-10-07T20:00:00Z', '2026-10-07T21:30:00Z')).toBe(90)
    expect(minutesBetween('', '2026-10-07T21:30:00Z')).toBeNaN()
  })

  it('lays flushes on a 24 h axis and marks long waits', () => {
    const ref = '2026-10-07T12:00:00Z'
    const hourly = Array.from({ length: 24 }, (_, i) => new Date(Date.parse(ref) - (23 - i) * 3_600_000).toISOString())
    const t = flushTimeline(hourly, ref)
    expect(t.ticks).toHaveLength(24)
    expect(t.ticks[23]).toBe(1)
    expect(t.gaps).toEqual([])
    // Nothing after 05:00: one gap from 05:00 to now.
    const holes = hourly.filter((f) => {
      const h = new Date(f).getUTCHours()
      return h < 6 || h > 12
    })
    const t2 = flushTimeline(holes, ref)
    expect(t2.gaps).toHaveLength(1)
    expect(t2.gaps[0][0]).toBeCloseTo(17 / 24)
    expect(t2.gaps[0][1]).toBe(1)
    // Waits of exactly 2 h are not gaps.
    const t3 = flushTimeline([...holes, '2026-10-07T07:00:00Z', '2026-10-07T09:00:00Z', '2026-10-07T11:00:00Z'], ref)
    expect(t3.gaps).toEqual([])
    // Flushes outside the window are ignored.
    expect(flushTimeline(['2026-10-05T00:00:00Z'], ref).ticks).toEqual([])
  })
})

describe('Data health page', () => {
  it('shows the overall status, worst-first checks, archive cards and executor runs', async () => {
    renderAt('/data-health')
    expect(await screen.findByRole('heading', { level: 1, name: 'Data health' })).toBeInTheDocument()
    const summary = await screen.findByTestId('health-summary')
    const d = stage()
    expect(summary).toHaveClass(`s-${d.status}`)
    // Checks: worst first.
    const rows = within(document.getElementById('health-checks')!).getAllByRole('listitem')
    expect(rows).toHaveLength(d.checks.length)
    expect(rows[0]).toHaveClass(`s-${sortChecks(d.checks)[0].status}`)
    // One archive card per book, each with a flush strip.
    for (const b of d.archive.books) {
      const card = screen.getByTestId(`archive-${b.book}`)
      expect(within(card).getByTestId(`flush-strip-${b.book}`)).toHaveAccessibleName(
        new RegExp(`${b.raw.flushes_24h} flushes in the last 24 hours`),
      )
      expect(within(card).getByText(b.compacted.message, { exact: false })).toBeInTheDocument()
    }
    // Executor: coverage per book and the runs table.
    const ex = screen.getByTestId('executor')
    expect(within(ex).getByTestId('coverage-btc_mxn')).toBeInTheDocument()
    expect(within(ex).getByRole('table')).toBeInTheDocument()
    // The nav badge counts failing checks.
    const fails = d.checks.filter((c) => c.status === 'fail').length
    if (fails > 0) expect(document.getElementById('nav-data-health')).toHaveTextContent(String(fails))
  })

  it('explains how to turn the archive on when it is off', async () => {
    const d = stage()
    d.archive = { status: 'off', source: '', checked_at: d.generated_at, books: [] }
    d.checks = d.checks.filter((c) => c.area === 'executor')
    d.checks.unshift({
      id: 'archive',
      area: 'archive',
      label: 'Trade archive',
      status: 'off',
      message: 'not configured',
    })
    d.collector = { ...d.collector, status: 'off', message: 'archive not configured' }
    server.use(http.get('/api/ui/health/data', () => HttpResponse.json(d)))
    renderAt('/data-health')
    expect(await screen.findByTestId('archive-off')).toHaveTextContent('-archive s3://<bucket>')
    expect(screen.getByTestId('area-collector')).toHaveClass('s-off')
  })

  it('shows a healthy state and the last run errors', async () => {
    const d = stage()
    d.status = 'ok'
    d.checks = d.checks.map((c) => ({ ...c, status: 'ok' as const }))
    if (d.executor.last_run) d.executor.last_run.errors = ['[btc_mxn] error: candles: refusing a revised history']
    server.use(http.get('/api/ui/health/data', () => HttpResponse.json(d)))
    renderAt('/data-health')
    expect(await screen.findByRole('heading', { name: 'Data is flowing' })).toBeInTheDocument()
    if (d.executor.last_run) expect(screen.getByTestId('run-errors')).toHaveTextContent('refusing a revised history')
    expect(document.getElementById('nav-data-health')?.querySelector('.badge')).toBeNull()
  })

  it('shows an error state when ui-api fails', async () => {
    server.use(http.get('/api/ui/health/data', () => HttpResponse.json({ error: 'boom' }, { status: 500 })))
    renderAt('/data-health')
    expect(await screen.findByText('Could not load this view')).toBeInTheDocument()
  })
})
