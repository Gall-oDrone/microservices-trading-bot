/**
 * Runtime schemas for the ui-api JSON (services/ui-api/internal/api).
 * Every response is parsed with these, so a drift between the Go structs and
 * the UI fails loudly instead of rendering wrong numbers.
 */
import { z } from 'zod'

export const signal = z.enum(['long', 'flat'])
export type Signal = z.infer<typeof signal>

export const decisionSchema = z.object({
  bar_date: z.string(),
  fill_date: z.string(),
  close: z.number(),
  sma50: z.number(),
  signal,
  prev_signal: signal,
  action: z.enum(['buy', 'sell', 'hold']),
})

export const paperSchema = z.object({
  forward_start: z.string(),
  days: z.number(),
  position: signal,
  fills: z.number(),
  leg_cost_bps: z.number(),
  equity: z.number(),
  equity_if_closed: z.number(),
  hold_equity: z.number(),
  max_drawdown: z.number(),
  pending_action: z.string(),
})

export const candleInfoSchema = z.object({
  source: z.string(),
  first: z.string(),
  last: z.string(),
  bars: z.number(),
  recent_gaps: z.string().optional(),
  sha256_prefix: z.string(),
})

export const positionSchema = z.object({ state: signal, btc: z.number() })

export const legSchema = z.object({
  side: z.enum(['buy', 'sell']),
  target_btc: z.number(),
  filled_btc: z.number(),
  maker_btc: z.number(),
  taker_btc: z.number(),
  base_delta: z.number(),
  avg_price: z.number(),
  notional: z.number(),
  fees: z.record(z.string(), z.number()).nullable(),
  maker_origin_id: z.string(),
  taker_origin_id: z.string(),
  oids: z.array(z.string()).nullable(),
  maker_placements: z.number(),
  market_fallback: z.boolean(),
  shortfall_btc: z.number(),
  notes: z.array(z.string()).optional(),
  started: z.string(),
  finished: z.string(),
})

export const findingSchema = z.object({
  rule: z.string(),
  severity: z.enum(['block', 'warn']),
  limit: z.number(),
  value: z.number(),
  message: z.string(),
})
export type Finding = z.infer<typeof findingSchema>

/** shared/pkg/risk.HaltState: the operator halt file (risk-state.json next to a ledger). */
export const haltStateSchema = z.object({
  halted: z.boolean(),
  reason: z.string().optional(),
  by: z.string().optional(),
  at: z.string().optional(),
})

/** The daily-executor's pre-trade check (shared/pkg/risk), recorded per planned order. */
export const riskCheckSchema = z.object({
  policy_version: z.string(),
  order: z.object({
    book: z.string(),
    side: z.enum(['buy', 'sell']),
    qty_btc: z.number(),
    price: z.number(),
    ref_price: z.number(),
  }),
  state: z.object({ position_btc: z.number(), orders_today: z.number() }),
  allowed: z.boolean(),
  findings: z.array(findingSchema).optional(),
  /** The operator halt file in force for that run (R2). */
  halt: haltStateSchema.optional(),
})
export type RiskCheck = z.infer<typeof riskCheckSchema>

export const stageSchema = z.object({
  env: z.string(),
  target: signal,
  action: z.enum(['buy', 'sell', 'none', 'blocked']),
  position_before: positionSchema,
  position_after: positionSchema,
  leg: legSchema.optional(),
  risk: riskCheckSchema.optional(),
})

export const ledgerRecordSchema = z.object({
  recorded_at: z.string(),
  code_version: z.string(),
  mode: z.enum(['dry-run', 'stage']),
  book: z.string(),
  prereg: z.string(),
  decision: decisionSchema,
  paper: paperSchema,
  candles: candleInfoSchema,
  stage: stageSchema.optional(),
})
export type LedgerRecord = z.infer<typeof ledgerRecordSchema>

export const runStatusSchema = z.object({
  status: z.enum(['ok', 'pending', 'missed', 'no_data']),
  expected_bar_date: z.string(),
  last_bar_date: z.string(),
  missing_days: z.array(z.string()),
  message: z.string(),
})
export type RunStatus = z.infer<typeof runStatusSchema>

export const milestonesSchema = z.object({
  forward_start: z.string(),
  interim: z.string(),
  evaluation: z.string(),
  days_elapsed: z.number(),
  days_to_interim: z.number(),
  days_to_evaluation: z.number(),
  window_progress: z.number(),
})

/**
 * A book with no records in the selected ledger comes back with zero values:
 * empty strings for the enums. Only the forward-test summary allows that; the
 * ledger records themselves stay strict.
 */
const orEmpty = <T extends z.ZodTypeAny>(s: T) => s.or(z.literal(''))
const summaryDecisionSchema = decisionSchema.extend({
  signal: orEmpty(signal),
  prev_signal: orEmpty(signal),
  action: orEmpty(z.enum(['buy', 'sell', 'hold'])),
})

export const forwardTestSchema = z.object({
  ledger: z.string(),
  book: z.string(),
  base: z.string(),
  quote: z.string(),
  prereg: z.string(),
  mode: z.string(),
  code_version: z.string(),
  recorded_at: z.string(),
  decision: summaryDecisionSchema,
  paper: paperSchema.extend({ position: orEmpty(signal) }),
  candles: candleInfoSchema,
  stage_position: positionSchema.nullable(),
  distance_to_sma_pct: z.number(),
  excess_vs_hold_pct: z.number(),
  milestones: milestonesSchema,
  run: runStatusSchema,
  risk_warnings: z.number(),
  risk_blocks: z.number(),
})
export type ForwardTest = z.infer<typeof forwardTestSchema>

export const forwardTestsResponseSchema = z.object({
  ledger: z.string(),
  generated_at: z.string(),
  books: z.array(forwardTestSchema),
})
export type ForwardTestsResponse = z.infer<typeof forwardTestsResponseSchema>

export const fillSchema = z.object({
  bar_date: z.string(),
  fill_date: z.string(),
  side: z.enum(['buy', 'sell']),
  target_btc: z.number(),
  filled_btc: z.number(),
  maker_btc: z.number(),
  taker_btc: z.number(),
  net_btc: z.number(),
  avg_price: z.number(),
  notional: z.number(),
  fee_quote: z.number(),
  fee_bps: z.number(),
  ref_open: z.number().nullable(),
  slippage_bps: z.number().nullable(),
  total_cost_bps: z.number(),
  assumed_leg_bps: z.number(),
  market_fallback: z.boolean(),
  started: z.string(),
  finished: z.string(),
  notes: z.array(z.string()),
})
export type Fill = z.infer<typeof fillSchema>

export const equityPointSchema = z.object({
  date: z.string(),
  equity: z.number(),
  hold_equity: z.number(),
  max_drawdown: z.number(),
  signal,
  close: z.number(),
  sma50: z.number(),
})
export type EquityPoint = z.infer<typeof equityPointSchema>

export const ledgerResponseSchema = z.object({
  ledger: z.string(),
  book: z.string(),
  mode: z.string(),
  records: z.array(ledgerRecordSchema),
  equity: z.array(equityPointSchema),
  fills: z.array(fillSchema),
})
export type LedgerResponse = z.infer<typeof ledgerResponseSchema>

export const candlePointSchema = z.object({
  date: z.string(),
  open: z.number(),
  high: z.number(),
  low: z.number(),
  close: z.number(),
  volume: z.number(),
  trade_count: z.number(),
  sma50: z.number().nullable(),
  volume_ratio_20d: z.number().nullable(),
  long: z.boolean().nullable(),
})
export type CandlePoint = z.infer<typeof candlePointSchema>

export const candlesResponseSchema = z.object({
  ledger: z.string(),
  book: z.string(),
  file: z.string(),
  candles: z.array(candlePointSchema),
})
export type CandlesResponse = z.infer<typeof candlesResponseSchema>

export const bookLimitsSchema = z.object({
  max_order_btc: z.number(),
  max_position_btc: z.number(),
  max_order_notional: z.number(),
  max_orders_per_day: z.number(),
  max_price_deviation_bps: z.number(),
  drawdown_warn: z.number(),
  cost_warn_bps: z.number(),
})
export type BookLimits = z.infer<typeof bookLimitsSchema>

export const riskDecisionSchema = z.object({
  allowed: z.boolean(),
  findings: z.array(findingSchema).nullable(),
})

export const bookRiskSchema = z.object({
  book: z.string(),
  quote: z.string(),
  mode: z.string(),
  limits: bookLimitsSchema,
  position_btc: z.number(),
  position_notional: z.number(),
  last_close: z.number(),
  paper_max_drawdown: z.number(),
  utilization: z.object({
    position: z.number(),
    order_size: z.number(),
    notional: z.number(),
    drawdown: z.number(),
  }),
  next_order: z
    .object({
      action: z.enum(['buy', 'sell', 'none']),
      qty_btc: z.number(),
      ref_price: z.number(),
      fill_date: z.string(),
      decision: riskDecisionSchema,
    })
    .nullable(),
  realized_cost: z.object({
    legs: z.number(),
    avg_fee_bps: z.number(),
    avg_slippage_bps: z.number(),
    avg_total_bps: z.number(),
    assumed_leg_bps: z.number(),
    fallback_legs: z.number(),
  }),
  last_check: riskCheckSchema.extend({ bar_date: z.string(), fill_date: z.string() }).nullable(),
  blocked_days: z.array(z.string()),
  findings: z.array(findingSchema),
})
export type BookRisk = z.infer<typeof bookRiskSchema>

export const riskResponseSchema = z.object({
  ledger: z.string(),
  generated_at: z.string(),
  policy: z.object({
    version: z.string(),
    halted: z.boolean(),
    halt_reason: z.string().optional(),
    books: z.record(z.string(), bookLimitsSchema),
    default: bookLimitsSchema,
  }),
  policy_source: z.string(),
  stage_size_btc: z.number(),
  enforcement: z.string(),
  note: z.string(),
  halted: z.boolean(),
  halt_reason: z.string(),
  halt_source: z.enum(['none', 'policy', 'file', 'both']),
  halt_file: z.object({
    path: z.string(),
    found: z.boolean(),
    halted: z.boolean(),
    reason: z.string(),
    by: z.string(),
    at: z.string(),
    error: z.string().optional(),
  }),
  books: z.array(bookRiskSchema),
  blocks: z.number(),
  warnings: z.number(),
})
export type RiskResponse = z.infer<typeof riskResponseSchema>

export const healthSchema = z.object({
  status: z.string(),
  version: z.string(),
  ledger: z.string(),
  ledger_found: z.boolean(),
  records: z.number(),
  ledgers: z.number(),
  policy: z.string(),
  time: z.string(),
})
export type Health = z.infer<typeof healthSchema>

/** One configured ledger (ui-api -ledgers). The first is the default. */
export const ledgerInfoSchema = z.object({
  name: z.string(),
  path: z.string(),
  default: z.boolean(),
  found: z.boolean(),
  records: z.number(),
  modes: z.array(z.string()),
  last_bar_date: z.string(),
  error: z.string().optional(),
})
export type LedgerInfo = z.infer<typeof ledgerInfoSchema>

export const ledgersResponseSchema = z.object({ ledgers: z.array(ledgerInfoSchema) })
export type LedgersResponse = z.infer<typeof ledgersResponseSchema>
