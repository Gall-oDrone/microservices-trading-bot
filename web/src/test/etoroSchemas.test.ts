import { describe, expect, it } from 'vitest'
import {
  etoroCandlesResponseSchema,
  etoroForwardTestsResponseSchema,
  etoroHealthResponseSchema,
  etoroLedgerResponseSchema,
  etoroLiveSnapshotSchema,
  etoroReconcileResponseSchema,
  etoroRiskReportSchema,
  etoroRiskResponseSchema,
} from '../api/etoroSchemas'
import { controlsInfoSchema, ledgersResponseSchema, strategiesInfoSchema } from '../api/schemas'
import candlesNsdq from '../mocks/fixtures/etoro-demo/etoro-candles-nsdq100.json'
import candlesSpx from '../mocks/fixtures/etoro-demo/etoro-candles-spx500.json'
import controls from '../mocks/fixtures/etoro-demo/controls.json'
import forwardTests from '../mocks/fixtures/etoro-demo/etoro-forward-tests.json'
import health from '../mocks/fixtures/etoro-demo/etoro-health.json'
import ledgerNsdq from '../mocks/fixtures/etoro-demo/etoro-ledger-nsdq100.json'
import ledgerSpx from '../mocks/fixtures/etoro-demo/etoro-ledger-spx500.json'
import liveSnap from '../mocks/fixtures/etoro-demo/etoro-live.json'
import reconcile from '../mocks/fixtures/etoro-demo/etoro-reconcile.json'
import risk from '../mocks/fixtures/etoro-demo/etoro-risk.json'
import riskReport from '../mocks/fixtures/etoro-demo/etoro-risk-report.json'
import ledgers from '../mocks/fixtures/ledgers.json'
import strategies from '../mocks/fixtures/strategies.json'

describe('contract: the eToro ui-api responses parse with the UI schemas', () => {
  it.each([
    ['etoro/forward-tests', etoroForwardTestsResponseSchema, forwardTests],
    ['etoro ledger nsdq100', etoroLedgerResponseSchema, ledgerNsdq],
    ['etoro ledger spx500', etoroLedgerResponseSchema, ledgerSpx],
    ['etoro candles nsdq100', etoroCandlesResponseSchema, candlesNsdq],
    ['etoro candles spx500', etoroCandlesResponseSchema, candlesSpx],
    ['etoro/risk', etoroRiskResponseSchema, risk],
    ['etoro/health', etoroHealthResponseSchema, health],
    ['etoro/reconcile', etoroReconcileResponseSchema, reconcile],
    ['etoro/risk-report', etoroRiskReportSchema, riskReport],
    ['etoro/live', etoroLiveSnapshotSchema, liveSnap],
    ['controls (etoro-demo)', controlsInfoSchema, controls],
    ['ledgers (with venue)', ledgersResponseSchema, ledgers],
    ['strategies (with venue)', strategiesInfoSchema, strategies],
  ])('%s', (_name, schema, data) => {
    const r = schema.safeParse(data)
    expect(r.success, r.success ? '' : JSON.stringify(r.error.issues.slice(0, 3))).toBe(true)
  })

  it('reads the fixture facts the pages show', () => {
    const ft = etoroForwardTestsResponseSchema.parse(forwardTests)
    expect(ft.venue).toBe('etoro')
    expect(ft.session.open).toBe(true)
    const [nsdq, spx] = ft.books
    expect(nsdq.book).toBe('nsdq100')
    expect(nsdq.demo_position?.state).toBe('flat')
    expect(nsdq.last_order?.kind).toBe('close')
    expect(spx.demo_position).toMatchObject({ state: 'long', units: 0.140735 })
    expect(spx.run.next_run_at).toBe('2026-10-19T09:35:00-04:00')

    const r = etoroRiskResponseSchema.parse(risk)
    expect(r.policy.books.nsdq100.max_exposure_pct_equity).toBe(0.05)
    expect(r.policy.books.nsdq100.max_order_notional).toBe(1500)
    expect(r.books[1].utilization.exposure).toBeCloseTo(0.2179, 3)

    const rep = etoroRiskReportSchema.parse(riskReport)
    expect(rep.var.var_99_usd).toBeCloseTo(52.81, 1)
    expect(rep.stress?.worst_episode).toBe('2008-09-lehman')
    expect(rep.fill_quality?.books.map((b) => b.frozen_spread_per_leg_bps)).toEqual([7.75, 7.4])

    const rc = etoroReconcileResponseSchema.parse(reconcile)
    expect(rc.reports[0].date).toBe('2026-10-16')
    expect(rc.reports[0].clean).toBe(false)
    expect(rc.reports[0].recheck?.clean).toBe(true)
    expect(rc.reports[0].effective_clean).toBe(true)

    const h = etoroHealthResponseSchema.parse(health)
    expect(h.checks.map((c) => c.id)).toContain('etoro.reconcile')
    expect(h.thresholds.run_at).toBe('09:35 New York')
  })

  it('old ledger lists without a venue still parse as Bitso', () => {
    const l = ledgersResponseSchema.parse({
      ledgers: [
        { name: 'stage', path: 'x', default: true, found: true, records: 1, modes: ['stage'], last_bar_date: '' },
      ],
    })
    expect(l.ledgers[0].venue).toBe('bitso')
  })

  it('an empty reconcile report (unreadable file) still parses with empty lists', () => {
    const r = etoroReconcileResponseSchema.parse({
      ledger: 'etoro-demo',
      venue: 'etoro',
      dir: 'd',
      reports: [
        {
          date: '2026-10-17',
          path: 'p',
          schema: '',
          generated_at: '',
          clean: false,
          account: null,
          position_reads: 0,
          books: null,
          findings: null,
          drift: 0,
          warnings: 0,
          recheck: null,
          effective_clean: false,
          error: 'unexpected end of JSON input',
        },
      ],
    })
    expect(r.reports[0].books).toEqual([])
    expect(r.reports[0].findings).toEqual([])
  })
})
