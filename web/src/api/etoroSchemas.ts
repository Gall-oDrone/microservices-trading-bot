/**
 * Runtime schemas for the eToro index-CFD executor's views in ui-api
 * (services/ui-api/internal/api/etoro.go and etoro_views.go, porting plan
 * P5a). USD, instrument units, New York time. Kept apart from schemas.ts,
 * which stays the Bitso contract; the primitives both share (signal,
 * findings, halt file, health checks, candles) are imported from there.
 */
import { z } from 'zod'
import {
  auditEntrySchema,
  bookLimitsSchema,
  candlePointSchema,
  findingSchema,
  haltFileSchema,
  haltStateSchema,
  healthCheckSchema,
  healthStatus,
  liveSnapshotSchema,
  signal,
} from './schemas'

const orEmpty = <T extends z.ZodTypeAny>(s: T) => s.or(z.literal(''))
/** Go slices come back as null when empty; the UI always wants an array. */
const list = <T extends z.ZodTypeAny>(s: T) =>
  z
    .array(s)
    .nullish()
    .transform((v) => v ?? [])

export const etoroBooks = ['nsdq100', 'spx500'] as const
export type EtoroBook = (typeof etoroBooks)[number]

/** etoroledger.Instrument: the eToro instrument a book trades. */
export const etoroInstrumentSchema = z.object({ symbol: z.string(), id: z.number() })
export type EtoroInstrument = z.infer<typeof etoroInstrumentSchema>

/**
 * etoroledger.Decision: the frozen rule's output for one NYSE trading day.
 * A console trade's line (P5d) carries signal "manual" and the rule's
 * signal as prev_signal ("" without a rule record).
 */
export const etoroDecisionSchema = z.object({
  bar_date: z.string(),
  fill_date: z.string(),
  close: z.number(),
  sma50: z.number(),
  signal: signal.or(z.literal('manual')),
  prev_signal: orEmpty(signal),
  action: z.enum(['buy', 'sell', 'hold']),
})

/** etoroledger.Paper: the pre-registered paper account (cfdsim, frozen costs), 1.0 at the forward start. */
export const etoroPaperSchema = z.object({
  forward_start: z.string(),
  started: z.boolean(),
  days: z.number(),
  position: signal,
  round_trips: z.number(),
  spread_rt_bps: z.number(),
  overnight_bps: z.number(),
  costs_status: z.string(),
  equity_if_closed: z.number(),
  hold_equity_if_closed: z.number(),
  max_drawdown: z.number(),
  financing_pct: z.number(),
  pending_action: z.string(),
})
export type EtoroPaper = z.infer<typeof etoroPaperSchema>

/** etoroledger.CandleInfo: the bars a decision used. */
export const etoroCandleInfoSchema = z.object({
  source: z.string(),
  first: z.string(),
  last: z.string(),
  bars: z.number(),
  missing_trading_days: z.number(),
  sha256_prefix: z.string(),
  revised_closes: z.number().optional(),
  max_revision_bps: z.number().optional(),
})

/** etoroledger.Position: what the executor holds on the demo account for one book. */
export const etoroPositionSchema = z.object({
  state: z.enum(['long', 'flat']),
  position_id: z.string().optional(),
  units: z.number().optional(),
  amount: z.number().optional(),
  open_rate: z.number().optional(),
  opened_at: z.string().optional(),
  // Protection (2026-10-19): the resting rates at eToro after the record; absent = none.
  stop_loss_rate: z.number().optional(),
  take_profit_rate: z.number().optional(),
  stop_type: z.string().optional(),
})
export type EtoroPosition = z.infer<typeof etoroPositionSchema>

/** Whether a ledger position carries a stop loss. */
export function isProtected(p: EtoroPosition | null | undefined): boolean {
  return !!p && p.state === 'long' && (p.stop_loss_rate ?? 0) > 0
}

/** etoroledger.Account: the demo account before the run's order (USD). */
export const etoroAccountSchema = z.object({
  currency: z.string(),
  cash: z.number(),
  equity: z.number(),
  exposure: z.number(),
})
export type EtoroAccount = z.infer<typeof etoroAccountSchema>

export const etoroQuoteSchema = z.object({
  bid: z.number(),
  ask: z.number(),
  at: z.string(),
  spread_bps: z.number(),
})

export const etoroCostsSchema = z.object({
  spread: z.number(),
  markup: z.number(),
  transaction_fee: z.number(),
  overnight_fee: z.number(),
  weekend_fee: z.number(),
})

/** etoroledger.RiskCheck: the pre-trade check of a planned demo order (shared/pkg/risk). */
export const etoroRiskCheckSchema = z.object({
  policy_version: z.string(),
  order: z.object({
    book: z.string(),
    /** "protect" (2026-10-19) is a recorded check of a stop edit: only the halt applies. */
    side: z.enum(['buy', 'sell', 'protect']),
    /** Base quantity: instrument units on a CFD book (the field name is shared with Bitso). */
    qty_btc: z.number(),
    price: z.number(),
    ref_price: z.number(),
    leverage: z.number().optional(),
    stop_loss: z.number().optional(),
    take_profit: z.number().optional(),
  }),
  state: z.object({
    position_btc: z.number(),
    orders_today: z.number(),
    equity: z.number().optional(),
    exposure: z.number().optional(),
    market_closed: z.boolean().optional(),
  }),
  allowed: z.boolean(),
  findings: z.array(findingSchema).optional(),
  halt: haltStateSchema.optional(),
})
export type EtoroRiskCheck = z.infer<typeof etoroRiskCheckSchema>

/** etoroledger.Order: one demo order (open or close) and its outcome; since 2026-10-19 also a protect or a venue close. */
export const etoroOrderSchema = z.object({
  kind: z.enum(['open', 'close', 'protect', 'venue_close']),
  client_ref: z.string(),
  request_id: z.string(),
  order_id: z.string().optional(),
  status: z.string(),
  position_ids: z.array(z.string()).optional(),
  avg_price: z.number().optional(),
  units: z.number().optional(),
  amount: z.number().optional(),
  fees: z.number().optional(),
  spread_cost: z.number().optional(),
  error: z.string().optional(),
  error_code: z.number().optional(),
  intent_at: z.string(),
  started: z.string(),
  finished: z.string(),
  resumed: z.boolean().optional(),
  /** Fill vs the quote mid the executor saw; negative is better than mid. */
  fill_vs_mid_bps: z.number().optional(),
  /** Fill vs the decision close (the paper account's reference). */
  fill_vs_close_bps: z.number().optional(),
})
export type EtoroOrder = z.infer<typeof etoroOrderSchema>

export const etoroDemoAction = z.enum(['open', 'close', 'none', 'blocked', 'protect', 'venue_close'])
export type EtoroDemoAction = z.infer<typeof etoroDemoAction>

/** etoroledger.VenueClose: eToro closed the position (stop, target, margin). */
export const etoroVenueCloseSchema = z.object({
  kind: z.enum(['stopped_out', 'took_profit', 'venue_close']),
  stop_loss_rate: z.number().optional(),
  take_profit_rate: z.number().optional(),
  close_rate: z.number(),
  gap_bps: z.number().optional(),
  closed_at: z.string(),
  net_profit: z.number(),
  fees: z.number(),
})

/** etoroledger.Demo: the demo-account part of a record. */
export const etoroDemoSchema = z.object({
  env: z.string(),
  target: signal,
  action: etoroDemoAction,
  position_before: etoroPositionSchema,
  position_after: etoroPositionSchema,
  account: etoroAccountSchema.optional(),
  quote: etoroQuoteSchema.optional(),
  costs: etoroCostsSchema.optional(),
  risk: etoroRiskCheckSchema.optional(),
  order: etoroOrderSchema.optional(),
  financing_to_date: z.number().optional(),
  notes: z.array(z.string()).optional(),
  protection: z
    .object({
      profile: z.string(),
      atr: z.number().optional(),
      atr_pct: z.number().optional(),
      stop_loss_before: z.number().optional(),
      take_profit_before: z.number().optional(),
      max_loss_usd: z.number().optional(),
      reason: z.string().optional(),
      operation_id: z.string().optional(),
    })
    .optional(),
  venue_close: etoroVenueCloseSchema.optional(),
})
export type EtoroDemo = z.infer<typeof etoroDemoSchema>

/** etoroledger.Operator: who placed a console trade and why. */
export const etoroOperatorSchema = z.object({ by: z.string(), reason: z.string() })

/**
 * etoroledger.Record: one ledger line, one instrument, one decision day; or,
 * with source "ui", one assisted trade from the console (P5d).
 */
export const etoroLedgerRecordSchema = z.object({
  schema: z.string(),
  recorded_at: z.string(),
  code_version: z.string(),
  mode: z.enum(['dry-run', 'demo']),
  venue: z.string(),
  book: z.string(),
  instrument: etoroInstrumentSchema,
  prereg: z.string(),
  decision: etoroDecisionSchema,
  paper: etoroPaperSchema.extend({ position: orEmpty(signal) }),
  candles: etoroCandleInfoSchema,
  demo: etoroDemoSchema.optional(),
  source: z.enum(['ui']).optional(),
  operator: etoroOperatorSchema.optional(),
})
export type EtoroLedgerRecord = z.infer<typeof etoroLedgerRecordSchema>

/** api.EtoroSession: the NYSE cash session around now (shared/pkg/mktcal). */
export const etoroSessionSchema = z.object({
  now_new_york: z.string(),
  date: z.string(),
  trading_day: z.boolean(),
  open: z.boolean(),
  opens_at: z.string().optional(),
  closes_at: z.string().optional(),
  early_close: z.boolean().optional(),
  holiday: z.string().optional(),
})
export type EtoroSession = z.infer<typeof etoroSessionSchema>

/** api.EtoroRunStatus: has the 09:35 New York run recorded every due trading day, and when is the next one. */
export const etoroRunStatusSchema = z.object({
  status: z.enum(['ok', 'pending', 'missed', 'no_data']),
  due_date: z.string(),
  last_fill_date: z.string(),
  last_bar_date: z.string(),
  missing_days: z.array(z.string()),
  today_due: z.boolean(),
  today_done: z.boolean(),
  next_run_at: z.string(),
  next_run_date: z.string(),
  message: z.string(),
})
export type EtoroRunStatus = z.infer<typeof etoroRunStatusSchema>

/** api.EtoroOrderView: a demo order with the day it was for; source "ui" and `by` for a console trade. */
export const etoroOrderViewSchema = etoroOrderSchema.extend({
  bar_date: z.string(),
  fill_date: z.string(),
  source: z.enum(['ui']).optional(),
  by: z.string().optional(),
})
export type EtoroOrderView = z.infer<typeof etoroOrderViewSchema>

/**
 * api.EtoroForwardTest: one book's card. A book with no records comes back
 * with zero values (empty enums), like the Bitso summary.
 */
export const etoroForwardTestSchema = z.object({
  ledger: z.string(),
  venue: z.string(),
  book: z.string(),
  instrument: etoroInstrumentSchema,
  quote: z.string(),
  prereg: z.string(),
  mode: z.string(),
  code_version: z.string(),
  recorded_at: z.string(),
  decision: etoroDecisionSchema.extend({
    signal: orEmpty(signal),
    prev_signal: orEmpty(signal),
    action: orEmpty(z.enum(['buy', 'sell', 'hold'])),
  }),
  paper: etoroPaperSchema.extend({ position: orEmpty(signal) }),
  candles: etoroCandleInfoSchema,
  demo_position: etoroPositionSchema.nullable(),
  demo_action: orEmpty(etoroDemoAction),
  account: etoroAccountSchema.nullable(),
  last_order: etoroOrderViewSchema.nullable(),
  distance_to_sma_pct: z.number(),
  excess_vs_hold_pct: z.number(),
  run: etoroRunStatusSchema,
  risk_warnings: z.number(),
  risk_blocks: z.number(),
  risk_findings: list(findingSchema),
})
export type EtoroForwardTest = z.infer<typeof etoroForwardTestSchema>

/** GET /api/ui/etoro/forward-tests. */
export const etoroForwardTestsResponseSchema = z.object({
  ledger: z.string(),
  venue: z.string(),
  generated_at: z.string(),
  session: etoroSessionSchema,
  books: z.array(etoroForwardTestSchema),
})
export type EtoroForwardTestsResponse = z.infer<typeof etoroForwardTestsResponseSchema>

/** api.EtoroEquityPoint: one decision day, paper (1.0-based) and, when recorded, the demo account (USD). */
export const etoroEquityPointSchema = z.object({
  date: z.string(),
  equity: z.number(),
  hold_equity: z.number(),
  max_drawdown: z.number(),
  signal,
  close: z.number(),
  sma50: z.number(),
  demo_equity: z.number().nullable(),
  demo_exposure: z.number().nullable(),
})
export type EtoroEquityPoint = z.infer<typeof etoroEquityPointSchema>

/** GET /api/ui/etoro/forward-tests/{book}/ledger. */
export const etoroLedgerResponseSchema = z.object({
  ledger: z.string(),
  venue: z.string(),
  book: z.string(),
  mode: z.string(),
  records: list(etoroLedgerRecordSchema),
  equity: list(etoroEquityPointSchema),
})
export type EtoroLedgerResponse = z.infer<typeof etoroLedgerResponseSchema>

/** GET /api/ui/etoro/forward-tests/{book}/candles: the Bitso candle shape (volume is 0 on eToro bars). */
export const etoroCandlesResponseSchema = z.object({
  ledger: z.string(),
  venue: z.string(),
  book: z.string(),
  file: z.string(),
  candles: z.array(candlePointSchema),
})
export type EtoroCandlesResponse = z.infer<typeof etoroCandlesResponseSchema>

/** risk.BookLimits with the CFD fields EtoroDemoPolicy sets. */
export const etoroBookLimitsSchema = bookLimitsSchema.extend({
  max_leverage: z.number().optional(),
  max_exposure_pct_equity: z.number().optional(),
  require_market_open: z.boolean().optional(),
  max_open_orders: z.number().optional(),
  max_orders_per_minute: z.number().optional(),
})
export type EtoroBookLimits = z.infer<typeof etoroBookLimitsSchema>

/** api.EtoroLastCheck: the newest recorded pre-trade check. */
export const etoroLastCheckSchema = etoroRiskCheckSchema.extend({ bar_date: z.string(), fill_date: z.string() })

/** api.EtoroBookRisk: one book's section of the Risk page. */
export const etoroBookRiskSchema = z.object({
  book: z.string(),
  instrument: etoroInstrumentSchema,
  quote: z.string(),
  mode: z.string(),
  limits: etoroBookLimitsSchema,
  state: z.enum(['long', 'flat']),
  position: etoroPositionSchema,
  last_close: z.number(),
  exposure_usd: z.number(),
  exposure_pct_equity: z.number(),
  paper_max_drawdown: z.number(),
  utilization: z.object({ exposure: z.number(), notional: z.number(), drawdown: z.number() }),
  orders_today: z.number(),
  last_order: etoroOrderViewSchema.nullable(),
  last_check: etoroLastCheckSchema.nullable(),
  blocked_days: list(z.string()),
  findings: list(findingSchema),
})
export type EtoroBookRisk = z.infer<typeof etoroBookRiskSchema>

/** GET /api/ui/etoro/risk. */
export const etoroRiskResponseSchema = z.object({
  ledger: z.string(),
  venue: z.string(),
  generated_at: z.string(),
  policy: z.object({
    version: z.string(),
    halted: z.boolean(),
    halt_reason: z.string().optional(),
    books: z.record(z.string(), etoroBookLimitsSchema),
    default: etoroBookLimitsSchema,
  }),
  policy_source: z.string(),
  enforcement: z.string(),
  note: z.string(),
  halted: z.boolean(),
  halt_reason: z.string(),
  halt_source: z.enum(['none', 'policy', 'file', 'both']),
  halt_file: haltFileSchema,
  account: etoroAccountSchema.nullable(),
  account_as_of: z.string(),
  books: z.array(etoroBookRiskSchema),
  blocks: z.number(),
  warnings: z.number(),
})
export type EtoroRiskResponse = z.infer<typeof etoroRiskResponseSchema>

/** GET /api/ui/etoro/health: the etorohealth checks in the Data health shape. */
export const etoroHealthResponseSchema = z.object({
  ledger: z.string(),
  venue: z.string(),
  generated_at: z.string(),
  status: healthStatus,
  due_date: z.string(),
  ledger_path: z.string(),
  checks: list(healthCheckSchema),
  thresholds: z.record(z.string(), z.string()),
})
export type EtoroHealthResponse = z.infer<typeof etoroHealthResponseSchema>

// --- Reconcile reports (cmd/etoro-reconcile, read by ui-api as files) ---

export const etoroReconcileFindingSchema = z.object({
  severity: z.string(),
  book: z.string().optional(),
  code: z.string(),
  message: z.string(),
})
export type EtoroReconcileFinding = z.infer<typeof etoroReconcileFindingSchema>

export const etoroReconcileBookSchema = z.object({
  book: z.string(),
  symbol: z.string(),
  instrument_id: z.number(),
  last_bar_date: z.string(),
  ledger_state: z.string(),
  ledger_position_id: z.string().optional(),
  ledger_units: z.number().optional(),
  account_positions: list(z.string()),
  financing_to_date: z.number().optional(),
  orders_checked: z.number(),
})

/** api.EtoroRecheck: the operator's later read-only re-run of the same day. */
export const etoroRecheckSchema = z.object({
  path: z.string(),
  generated_at: z.string(),
  clean: z.boolean(),
  findings: list(etoroReconcileFindingSchema),
  error: z.string().optional(),
})
export type EtoroRecheck = z.infer<typeof etoroRecheckSchema>

/** api.EtoroReconcileReport: one day's report; zero values when the file could not be read (`error`). */
export const etoroReconcileReportSchema = z.object({
  date: z.string(),
  path: z.string(),
  schema: z.string(),
  generated_at: z.string(),
  clean: z.boolean(),
  account: z
    .object({ currency: z.string(), cash: z.number(), equity: z.number(), unrealized_pnl: z.number() })
    .nullable(),
  position_reads: z.number(),
  history_reads: z.number().optional(),
  settle_waited_s: z.number().optional(),
  books: list(etoroReconcileBookSchema),
  findings: list(etoroReconcileFindingSchema),
  drift: z.number(),
  warnings: z.number(),
  recheck: etoroRecheckSchema.nullable(),
  effective_clean: z.boolean(),
  error: z.string().optional(),
})
export type EtoroReconcileReport = z.infer<typeof etoroReconcileReportSchema>

/** GET /api/ui/etoro/reconcile: the newest reports first. */
export const etoroReconcileResponseSchema = z.object({
  ledger: z.string(),
  venue: z.string(),
  dir: z.string(),
  reports: list(etoroReconcileReportSchema),
})
export type EtoroReconcileResponse = z.infer<typeof etoroReconcileResponseSchema>

// --- Risk report (cmd/etoro-risk-report, schema cfdrisk-report/v1, P4) ---
// ui-api passes the file through and adds `date` and `path`. Only the fields
// the Risk page's card reads are typed; the rest of each section is kept.

const quantilesSchema = z
  .object({ mean: z.number(), p5: z.number(), p25: z.number(), p50: z.number(), p75: z.number(), p95: z.number() })
  .passthrough()
const probSchema = z.object({ p: z.number(), lo: z.number(), hi: z.number() })
const bpsStatSchema = z.object({ n: z.number(), mean: z.number(), median: z.number(), worst: z.number() })
const pnlBreakdownSchema = z.object({
  price_pnl: z.number(),
  spread_paid: z.number(),
  markup: z.number(),
  fees: z.number(),
  financing_modelled: z.number(),
  net: z.number(),
})

export const etoroRiskReportSchema = z
  .object({
    schema: z.string(),
    date: z.string(),
    path: z.string(),
    as_of: z.string(),
    generated_at: z.string(),
    params: z
      .object({
        size_usd: z.number(),
        capital_usd: z.number(),
        costs_status: z.string(),
        spread_rt_bps: z.record(z.string(), z.number()),
        overnight_bps_per_night: z.number(),
      })
      .passthrough(),
    positions: list(
      z
        .object({
          book: z.string(),
          symbol: z.string(),
          state: z.string(),
          units: z.number().optional(),
          open_rate: z.number().optional(),
          last_close: z.number(),
          last_date: z.string(),
          exposure_usd: z.number(),
        })
        .passthrough(),
    ),
    var: z
      .object({
        books: list(
          z
            .object({
              book: z.string(),
              exposure_usd: z.number(),
              vol_ewma: z.number(),
              var_99_usd: z.number(),
              backtest: z.object({ zone: z.string() }).passthrough().optional(),
            })
            .passthrough(),
        ),
        net_exposure_usd: z.number(),
        gross_exposure_usd: z.number(),
        var_99_usd: z.number(),
        var_99_undiversified_usd: z.number().optional(),
        es_975_normal_usd: z.number().optional(),
        var_99_historical_usd: z.number().optional(),
        implied_correlation: z.number(),
        var_99_fraction_of_capital: z.number(),
      })
      .passthrough(),
    stress: z
      .object({
        shocks: list(z.object({ move: z.number(), loss_usd: z.number(), margin_close_out: z.boolean() })),
        episodes: list(
          z
            .object({
              id: z.string(),
              name: z.string(),
              base: z.string(),
              end: z.string(),
              loss_usd: z.number(),
              complete: z.boolean(),
            })
            .passthrough(),
        ),
        worst_episode: z.string(),
        worst_episode_loss_usd: z.number(),
        worst_episode_fraction_of_capital: z.number(),
        reverse_stress_move: z.number().optional(),
        margin_close_out_move: z.number().optional(),
      })
      .passthrough()
      .optional(),
    monte_carlo: list(
      z
        .object({
          book: z.string(),
          scenario: z.string(),
          spread_rt_bps: z.number(),
          summary: z
            .object({
              trend_return: quantilesSchema,
              cfd_hold_return: quantilesSchema,
              etf_hold_return: quantilesSchema.optional(),
              trend_max_dd: quantilesSchema,
              p_beats_cfd_hold: probSchema,
              p_beats_etf_hold: probSchema,
              p_shallower_dd: probSchema,
            })
            .passthrough(),
        })
        .passthrough(),
    ),
    attribution: z
      .object({
        as_of: z.string(),
        realized: pnlBreakdownSchema,
        unrealized: pnlBreakdownSchema,
        total: pnlBreakdownSchema,
        books: list(
          z
            .object({
              book: z.string(),
              total: pnlBreakdownSchema,
              financing_charged: z.number(),
              net_with_charged: z.number(),
              paper_equity: z.number(),
              demo_equity: z.number(),
              tracking_bps: z.number(),
            })
            .passthrough(),
        ),
      })
      .passthrough()
      .optional(),
    fill_quality: z
      .object({
        books: list(
          z
            .object({
              book: z.string(),
              n: z.number(),
              quoted_spread_bps: bpsStatSchema,
              fill_vs_mid_bps: bpsStatSchema,
              fill_vs_close_bps: bpsStatSchema,
              latency_s: bpsStatSchema,
              portfolio_lags: z.number(),
              frozen_spread_per_leg_bps: z.number(),
              measured_spread_per_leg_bps: z.number(),
              measured_vs_frozen: z.number(),
            })
            .passthrough(),
        ),
      })
      .passthrough()
      .optional(),
    notes: list(z.string()),
  })
  .passthrough()
export type EtoroRiskReport = z.infer<typeof etoroRiskReportSchema>

// --- Live quotes (services/ui-api/internal/etorolive, P5c): display only. ---

/**
 * api.EtoroLiveSnapshot: GET /api/ui/etoro/live and the "snapshot" event of
 * /api/ui/etoro/stream. The Bitso live shape (books with bid/ask, the
 * forming bar and the provisional flip level) plus the NYSE session. The
 * bar is a UTC day (eToro's OneDay bucket), `last` is the mid of the newest
 * 5 s rates poll, and `trade_count` counts quote updates: the rates route
 * carries no trades.
 */
export const etoroLiveSnapshotSchema = liveSnapshotSchema.extend({ session: etoroSessionSchema })
export type EtoroLiveSnapshot = z.infer<typeof etoroLiveSnapshotSchema>

/** The "session" event that follows every heartbeat on /api/ui/etoro/stream. */
export const etoroSessionEventSchema = etoroSessionSchema

// --- Assisted trades (services/ui-api/internal/api/trades.go, P5d) ---------

export const tradeSide = z.enum(['open', 'close', 'protect'])
export type TradeSide = z.infer<typeof tradeSide>

export const tradeVerdict = z.enum(['allow', 'block'])

// --- Protection (services/ui-api/internal/api/protection.go, 2026-10-19) ---

export const stopProfile = z.enum(['conservative', 'moderate', 'aggressive', 'custom'])
export type StopProfile = z.infer<typeof stopProfile>

/** stops.Levels: a stop and target after the guards, as rates and fractions of the entry. */
export const stopLevelsSchema = z.object({
  profile: stopProfile,
  sl_rate: z.number(),
  sl_pct: z.number(),
  tp_rate: z.number().optional(),
  tp_pct: z.number().optional(),
  sl_atr: z.number().optional(),
  tp_atr: z.number().optional(),
  max_loss_usd: z.number(),
  target_usd: z.number().optional(),
  rr: z.number().optional(),
  clamps: list(z.string()),
  feasible: z.boolean(),
  reason: z.string().optional(),
})
export type StopLevels = z.infer<typeof stopLevelsSchema>

/** stops.Volatility: ATR(14) and the regime against its own history. */
export const volatilitySchema = z.object({
  period: z.number(),
  atr: z.number(),
  atr_pct: z.number(),
  close: z.number(),
  bars: z.number(),
  regime: z.enum(['calm', 'normal', 'elevated', 'extreme', 'unknown']),
  regime_ratio: z.number().optional(),
  median_atr_pct: z.number().optional(),
  regime_lookback: z.number().optional(),
})
export type Volatility = z.infer<typeof volatilitySchema>

export const stopBoundsSchema = z.object({
  min_sl_pct: z.number(),
  max_sl_pct: z.number(),
  max_tp_pct: z.number(),
  tick: z.number(),
})
export const stopLimitsSchema = z.object({
  min_sl_pct: z.number(),
  max_sl_pct: z.number(),
  max_loss_pct_equity: z.number(),
})

/** api.ProtectionBefore: the position's protection as the account reports it. */
export const protectionBeforeSchema = z.object({
  stop_loss_rate: z.number().optional(),
  take_profit_rate: z.number().optional(),
  stop_type: z.string().optional(),
  protected: z.boolean(),
})

/** api.TradeProtection: the levels a trade will carry and where they came from. */
export const tradeProtectionSchema = z.object({
  profile: z.string(),
  levels: stopLevelsSchema,
  volatility: volatilitySchema.nullable().optional(),
  entry: z.number(),
  mark: z.number(),
  bounds: stopBoundsSchema,
  limits: stopLimitsSchema,
  bounds_source: z.enum(['venue', 'fallback']),
  sma50: z.number().optional(),
  before: protectionBeforeSchema.nullable().optional(),
})
export type TradeProtection = z.infer<typeof tradeProtectionSchema>

/** api.TradeRecommendation: POST /api/ui/etoro/trades/recommend. */
export const tradeRecommendationSchema = z.object({
  ledger: z.string(),
  book: z.string(),
  side: z.enum(['open', 'protect']),
  instrument: etoroInstrumentSchema,
  quote: etoroQuoteSchema.nullable(),
  account: etoroAccountSchema.nullable(),
  position: etoroPositionSchema,
  rule_signal: orEmpty(signal),
  amount_usd: z.number().optional(),
  units: z.number(),
  profiles: z.record(z.string(), stopLevelsSchema),
  default_profile: z.string(),
  context: tradeProtectionSchema,
  policy_version: z.string(),
  warnings: list(z.string()),
  prepared_at: z.string(),
})
export type TradeRecommendation = z.infer<typeof tradeRecommendationSchema>

/** etoroledger.Protection: the protect block of a ledger line / place response. */
export const ledgerProtectionSchema = z.object({
  profile: z.string(),
  atr: z.number().optional(),
  atr_pct: z.number().optional(),
  stop_loss_before: z.number().optional(),
  take_profit_before: z.number().optional(),
  max_loss_usd: z.number().optional(),
  reason: z.string().optional(),
  operation_id: z.string().optional(),
})

/**
 * api.TradePreview: POST /api/ui/etoro/trades/prepare. The checks' outcome
 * in order (halt, risk.Check, eligibility, quote, costs, cash) as reasons,
 * the preview itself, and on an allow verdict the token to place it within
 * expires_in_s seconds.
 */
export const tradePreviewSchema = z.object({
  verdict: tradeVerdict,
  reasons: list(z.string()),
  warnings: list(z.string()),
  ledger: z.string(),
  book: z.string(),
  side: tradeSide,
  instrument: etoroInstrumentSchema,
  position: etoroPositionSchema,
  rule_signal: orEmpty(signal),
  quote: etoroQuoteSchema.nullable(),
  costs: etoroCostsSchema.nullable(),
  account: etoroAccountSchema.nullable(),
  risk: etoroRiskCheckSchema.nullable(),
  units: z.number(),
  amount_usd: z.number(),
  position_id: z.string().optional(),
  ref_price: z.number(),
  confirmation: z.string(),
  client_ref: z.string(),
  request_id: z.string(),
  prepared_at: z.string(),
  protection: tradeProtectionSchema.nullable().optional(),
  token: z.string().optional(),
  expires_at: z.string().optional(),
  expires_in_s: z.number().optional(),
})
export type TradePreview = z.infer<typeof tradePreviewSchema>

/** api.TradePlaceResponse: POST /api/ui/etoro/trades/place (200; a 502 carries the same shape with `error`). */
export const tradePlaceResponseSchema = z.object({
  status: z.string(),
  book: z.string(),
  side: tradeSide,
  client_ref: z.string(),
  request_id: z.string(),
  order_id: z.string().optional(),
  position_ids: list(z.string()),
  avg_price: z.number(),
  units: z.number(),
  amount_usd: z.number(),
  fill_vs_mid_bps: z.number(),
  fill_vs_close_bps: z.number(),
  position_after: etoroPositionSchema,
  ledger_recorded: z.boolean(),
  ledger_error: z.string().optional(),
  audit_path: z.string(),
  audit: auditEntrySchema,
  audit_error: z.string().optional(),
  error: z.string().optional(),
  protection: ledgerProtectionSchema.nullable().optional(),
  warnings: list(z.string()).optional(),
})
export type TradePlaceResponse = z.infer<typeof tradePlaceResponseSchema>
