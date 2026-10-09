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
    /** dailyledger.CostBudget: notional-weighted cost vs the pre-registered costs (plan §6.4.10). */
    budget: z.object({
      legs: z.number(),
      legs_without_ref: z.number(),
      notional: z.number(),
      cost_quote: z.number(),
      weighted_bps: z.number(),
      primary_leg_bps: z.number(),
      secondary_leg_bps: z.number(),
      budget_quote: z.number(),
      excess_quote: z.number(),
      budget_used: z.number(),
      prereg: z.string(),
      over_pessimistic: z.boolean(),
    }),
  }),
  last_check: riskCheckSchema.extend({ bar_date: z.string(), fill_date: z.string() }).nullable(),
  blocked_days: z.array(z.string()),
  findings: z.array(findingSchema),
})
export type BookRisk = z.infer<typeof bookRiskSchema>

/** api.HaltFileInfo: the operator halt file next to a ledger (R2; written by the R4 controls). */
export const haltFileSchema = z.object({
  path: z.string(),
  found: z.boolean(),
  halted: z.boolean(),
  reason: z.string(),
  by: z.string(),
  at: z.string(),
  error: z.string().optional(),
})
export type HaltFile = z.infer<typeof haltFileSchema>

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
  halt_file: haltFileSchema,
  books: z.array(bookRiskSchema),
  blocks: z.number(),
  warnings: z.number(),
})
export type RiskResponse = z.infer<typeof riskResponseSchema>

// --- Operator controls (R4, services/ui-api/internal/api/controls.go) ---
// Audit entries carry the halt file before and after (haltStateSchema above).

export const auditOutcome = z.enum(['requested', 'done', 'failed', 'refused', 'denied'])
export type AuditOutcome = z.infer<typeof auditOutcome>

/** audit.Entry: one line of <ledger dir>/ui-audit.jsonl. */
export const auditEntrySchema = z.object({
  id: z.string().optional(),
  at: z.string(),
  action: z.string(),
  outcome: auditOutcome,
  ledger: z.string(),
  by: z.string().optional(),
  reason: z.string().optional(),
  remote: z.string().optional(),
  user_agent: z.string().optional(),
  before: haltStateSchema.optional(),
  after: haltStateSchema.optional(),
  error: z.string().optional(),
  /** Halt-all: ties the per-ledger lines of one kill switch together. */
  group: z.string().optional(),
  /** Strategy start/stop (ledger is "" for those). */
  strategy: z.string().optional(),
  executor: z.string().optional(),
  upstream_status: z.number().optional(),
  detail: z.string().optional(),
})
export type AuditEntry = z.infer<typeof auditEntrySchema>

/** GET /api/ui/controls: whether halt/resume is available for this ledger, and the audit log (newest first). */
export const controlsInfoSchema = z.object({
  ledger: z.string(),
  enabled: z.boolean(),
  disabled_reason: z.string().optional(),
  halt_file: haltFileSchema,
  audit_path: z.string(),
  audit: z.array(auditEntrySchema),
  audit_error: z.string().optional(),
})
export type ControlsInfo = z.infer<typeof controlsInfoSchema>

/** POST /api/ui/risk/halt and /resume. */
export const controlResponseSchema = z.object({
  ledger: z.string(),
  action: z.enum(['halt', 'resume']),
  halt_file: haltFileSchema,
  audit: auditEntrySchema,
  audit_error: z.string().optional(),
})
export type ControlResponse = z.infer<typeof controlResponseSchema>

// --- Strategies page (services/ui-api/internal/api/strategies.go) ---

/** An operator hold on a strategy-executor strategy (persisted stop). */
export const holdSchema = z.object({
  name: z.string().optional(),
  reason: z.string(),
  by: z.string(),
  at: z.string(),
})
export type Hold = z.infer<typeof holdSchema>

export const strategyViewSchema = z.object({
  name: z.string(),
  type: z.string(),
  version: z.string(),
  book: z.string(),
  running: z.boolean(),
  enabled: z.boolean(),
  dry_run: z.boolean().optional(),
  has_position: z.boolean(),
  position_side: z.string().optional(),
  position_size: z.number(),
  entry_price: z.number().optional(),
  unrealized_pnl: z.number(),
  pending_buy: z.boolean(),
  pending_sell: z.boolean(),
  signal_count: z.number(),
  trade_count: z.number(),
  last_signal_at: z.string().optional(),
  total_pnl: z.number(),
  daily_pnl: z.number(),
  win_rate: z.number(),
  hold: holdSchema.optional(),
})
export type StrategyView = z.infer<typeof strategyViewSchema>

export const ledgerControlSchema = z.object({
  name: z.string(),
  remote: z.boolean(),
  controls_enabled: z.boolean(),
  disabled_reason: z.string().optional(),
  halt_file: haltFileSchema,
})
export type LedgerControl = z.infer<typeof ledgerControlSchema>

/** GET /api/ui/strategies. */
export const strategiesInfoSchema = z.object({
  ledgers: z.array(ledgerControlSchema),
  kill_switch: z.object({
    enabled: z.boolean(),
    disabled_reason: z.string().optional(),
    confirm: z.string(),
    targets: z.array(z.string()),
    already_halted: z.array(z.string()),
  }),
  executor: z.object({
    configured: z.boolean(),
    url: z.string().optional(),
    reachable: z.boolean(),
    error: z.string().optional(),
    controls_enabled: z.boolean(),
    disabled_reason: z.string().optional(),
    holds_supported: z.boolean(),
    fetched_at: z.string().optional(),
  }),
  strategies: z.array(strategyViewSchema),
  /** Holds on names the executor does not have registered now. */
  holds: z.array(holdSchema),
  audit_path: z.string().optional(),
  audit: z.array(auditEntrySchema),
  audit_error: z.string().optional(),
})
export type StrategiesInfo = z.infer<typeof strategiesInfoSchema>

/** POST /api/ui/strategies/{name}/start|stop. */
export const strategyControlResponseSchema = z.object({
  strategy: z.string(),
  action: z.enum(['start', 'stop']),
  upstream: z.record(z.string(), z.unknown()),
  audit: auditEntrySchema,
  audit_error: z.string().optional(),
})
export type StrategyControlResponse = z.infer<typeof strategyControlResponseSchema>

/** POST /api/ui/risk/halt-all (the kill switch). */
export const haltAllResponseSchema = z.object({
  action: z.literal('halt_all'),
  group: z.string(),
  results: z.array(
    z.object({
      ledger: z.string(),
      outcome: z.enum(['halted', 'already_halted', 'failed']),
      error: z.string().optional(),
      halt_file: haltFileSchema,
    }),
  ),
})
export type HaltAllResponse = z.infer<typeof haltAllResponseSchema>

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

// --- Live market data (services/ui-api/internal/live): display only. ---

/** live.Candle: today's forming bar (Mexico City day), seeded from REST then built from trades. */
export const liveCandleSchema = z.object({
  date: z.string(),
  open: z.number(),
  high: z.number(),
  low: z.number(),
  close: z.number(),
  volume: z.number(),
  trade_count: z.number(),
  seeded: z.boolean(),
})
export type LiveCandle = z.infer<typeof liveCandleSchema>

/** live.Provisional: what the frozen rule would say if today closed at the last trade. Never a decision. */
export const provisionalSchema = z.object({
  label: z.string(),
  price: z.number(),
  flip_level: z.number(),
  sma50: z.number(),
  signal,
  distance_to_flip_pct: z.number(),
  based_on: z.string(),
})
export type Provisional = z.infer<typeof provisionalSchema>

export const bookSnapshotSchema = z.object({
  book: z.string(),
  last: z.number(),
  last_side: z.string(),
  last_at: z.string(),
  bid: z.number(),
  ask: z.number(),
  candle: liveCandleSchema.nullable(),
  provisional: provisionalSchema.nullable(),
  provisional_note: z.string().optional(),
  updated_at: z.string(),
})
export type BookSnapshot = z.infer<typeof bookSnapshotSchema>

export const liveStatusSchema = z.object({
  source: z.string(),
  connected: z.boolean(),
  since: z.string(),
  last_message_at: z.string(),
  reconnects: z.number(),
  last_error: z.string().optional(),
})
export type LiveStatus = z.infer<typeof liveStatusSchema>

/** live.Level: one price level of the order book (orders grouped by price), amount in BTC. */
export const levelSchema = z.object({ price: z.number(), amount: z.number() })
export type Level = z.infer<typeof levelSchema>

/** live.TapeTrade; side is the taker's: "buy" lifted the ask, "sell" hit the bid. */
export const tapeTradeSchema = z.object({
  id: z.number(),
  price: z.number(),
  amount: z.number(),
  side: z.enum(['buy', 'sell']),
  at: z.string(),
})
export type TapeTrade = z.infer<typeof tapeTradeSchema>

/** live.Market: the Market page's view of a book (GET /live?market=1, "market" events). */
export const marketSchema = z.object({
  book: z.string(),
  bid: z.number(),
  ask: z.number(),
  mid: z.number(),
  spread: z.number(),
  spread_bps: z.number(),
  bids: z.array(levelSchema),
  asks: z.array(levelSchema),
  depth_at: z.string(),
  trades: z.array(tapeTradeSchema),
  tape_seeded: z.boolean(),
})
export type Market = z.infer<typeof marketSchema>

/** GET /api/ui/live, and the "snapshot" event of /api/ui/stream (markets only with ?market=1). */
export const liveSnapshotSchema = z.object({
  generated_at: z.string(),
  upstream: liveStatusSchema,
  books: z.array(bookSnapshotSchema),
  markets: z.array(marketSchema).optional(),
})
export type LiveSnapshot = z.infer<typeof liveSnapshotSchema>

export const heartbeatSchema = z.object({ time: z.string(), upstream: liveStatusSchema })

// --- Research (services/ui-api/internal/research): study write-ups, read-only. ---

export const studyKind = z.enum(['preregistration', 'assessment', 'study', 'report'])
export type StudyKind = z.infer<typeof studyKind>

export const studySchema = z.object({
  name: z.string(),
  file: z.string(),
  title: z.string(),
  date: z.string(),
  kind: studyKind,
  question: z.string().optional(),
  summary: z.string(),
  follows: z.array(z.string()),
  references: z.array(z.string()),
  evidence: z.object({ dir: z.string(), files: z.array(z.string()) }).nullable(),
  bytes: z.number(),
  modified: z.string(),
})
export type Study = z.infer<typeof studySchema>

export const studiesResponseSchema = z.object({
  generated_at: z.string(),
  dir: z.string(),
  found: z.boolean(),
  studies: z.array(studySchema),
})
export type StudiesResponse = z.infer<typeof studiesResponseSchema>

export const studyHeadingSchema = z.object({ level: z.number(), id: z.string(), text: z.string() })

/** One study rendered to HTML by ui-api (goldmark; raw HTML in the markdown is dropped). */
export const studyDocSchema = z.object({
  study: studySchema,
  html: z.string(),
  headings: z.array(studyHeadingSchema),
  followed_by: z.array(z.string()),
  referenced_by: z.array(z.string()),
})
export type StudyDoc = z.infer<typeof studyDocSchema>

// --- Research runs: research-run/v1 JSON reports from the research tools (daily-research and weekly-research -json). ---
// Fields marked optional are additive (weekly-research only); daily-research reports omit them.

const runDataSchema = z.object({
  prices: z.string(),
  book: z.string().optional(),
  bars: z.number(),
  first: z.string(),
  last: z.string(),
  news: z.string().optional(),
  news_days: z.number(),
})
const runCostsSchema = z.object({
  buy_bps: z.number(),
  sell_bps: z.number(),
  slippage_bps: z.number(),
  round_trip_bps: z.number(),
  /** "base" or "stress" (weekly-research). */
  level: z.string().optional(),
  /** How to read the bps, e.g. one per-leg cost that already includes slippage. */
  note: z.string().optional(),
})
const runWindowHeadSchema = z.object({ label: z.string(), from: z.string(), to: z.string(), bars: z.number() })

export const runSummarySchema = z.object({
  id: z.string(),
  date: z.string(),
  name: z.string(),
  file: z.string(),
  text: z.string().optional(),
  schema: z.string(),
  tool: z.string(),
  generated_at: z.string(),
  commit: z.string().optional(),
  data: runDataSchema,
  costs: runCostsSchema,
  windows: z.array(runWindowHeadSchema),
  scores: z.array(z.object({ rule: z.string(), windows: z.number(), beats_hold: z.number() })),
  studies: z.array(z.string()),
})
export type RunSummary = z.infer<typeof runSummarySchema>

export const runsResponseSchema = z.object({
  generated_at: z.string(),
  dir: z.string(),
  found: z.boolean(),
  runs: z.array(runSummarySchema),
  skipped: z.array(z.object({ file: z.string(), error: z.string() })),
})
export type RunsResponse = z.infer<typeof runsResponseSchema>

export const ruleResultSchema = z.object({
  rule: z.string(),
  return_pct: z.number(),
  round_trips: z.number(),
  exposure_pct: z.number(),
  max_dd_pct: z.number(),
  cost_pct: z.number(),
  vs_hold_pp: z.number(),
  random: z.object({ sims: z.number(), beat_pct: z.number() }).nullable(),
  label: z.string().optional(),
  trades: z.number().optional(),
  turnover_x: z.number().optional(),
  return_zero_cost_pct: z.number().optional(),
  cagr_pct: z.number().optional(),
  sharpe: z.number().optional(),
  weeks_up: z.number().optional(),
  weeks_down: z.number().optional(),
  weeks_flat: z.number().optional(),
  median_week_pct: z.number().optional(),
  worst_week_pct: z.number().optional(),
})
export type RuleResult = z.infer<typeof ruleResultSchema>

export const eventRowSchema = z.object({
  condition: z.string(),
  h: z.number(),
  n: z.number(),
  mean_pct: z.number(),
  median_pct: z.number(),
  hit_pct: z.number(),
  t: z.number(),
  mean_after_costs_pct: z.number(),
})
export type EventRow = z.infer<typeof eventRowSchema>

export const sensitivitySchema = z.object({
  post_hoc: z.boolean(),
  chosen: z.number(),
  rows: z.array(
    z.object({ k: z.number(), return_pct: z.number(), max_dd_pct: z.number(), sharpe: z.number(), trades: z.number() }),
  ),
})
export type Sensitivity = z.infer<typeof sensitivitySchema>

export const runWindowSchema = runWindowHeadSchema.extend({
  gaps: z.string().optional(),
  note: z.string().optional(),
  results: z.array(ruleResultSchema),
  events: z.array(eventRowSchema).optional(),
  sensitivity: sensitivitySchema.optional(),
})
export type RunWindow = z.infer<typeof runWindowSchema>

/** The report as the tool wrote it (schema research-run/v1; fields are only ever added). */
export const runReportSchema = z.object({
  schema: z.string().startsWith('research-run/'),
  tool: z.string(),
  generated_at: z.string(),
  commit: z.string().optional(),
  flags: z.record(z.string(), z.string()),
  data: runDataSchema,
  costs: runCostsSchema,
  params: z.object({
    sma: z.number(),
    news_window: z.number(),
    news_threshold: z.number(),
    sims: z.number(),
    seed: z.number(),
    holdout_start: z.string().optional(),
    end: z.string().optional(),
    volume_ratio_days: z.number().optional(),
    vol_target: z.number().optional(),
  }),
  windows: z.array(runWindowSchema),
})
export type RunReport = z.infer<typeof runReportSchema>

export const runDocSchema = z.object({ run: runSummarySchema, report: runReportSchema })
export type RunDoc = z.infer<typeof runDocSchema>

// ---- Data health (GET /api/ui/health/data) ----

/** ok < off (not configured) < unknown < warn < fail */
export const healthStatus = z.enum(['ok', 'off', 'unknown', 'warn', 'fail'])
export type HealthStatus = z.infer<typeof healthStatus>

export const healthCheckSchema = z.object({
  id: z.string(),
  area: z.enum(['collector', 'archive', 'executor']),
  label: z.string(),
  status: healthStatus,
  message: z.string(),
})
export type HealthCheck = z.infer<typeof healthCheckSchema>

export const rawHealthSchema = z.object({
  status: healthStatus,
  message: z.string(),
  latest_partition: z.string(),
  latest_key: z.string(),
  latest_at: z.string(),
  age_minutes: z.number(),
  objects_today: z.number(),
  bytes_today: z.number(),
  flushes_24h: z.number(),
  max_flush_gap_minutes: z.number(),
  flush_gaps_24h: z.number(),
  flushes: z.array(z.string()),
})

export const compactionHealthSchema = z.object({
  status: healthStatus,
  message: z.string(),
  latest_partition: z.string(),
  expected_through: z.string(),
  days_behind: z.number(),
  partitions: z.number(),
  first_partition: z.string(),
  created_at: z.string(),
  source_rows: z.number(),
  compacted_rows: z.number(),
  duplicate_tids: z.number(),
  other_day_rows: z.number(),
  manifest_error: z.string().optional(),
})

export const archiveBookSchema = z.object({
  book: z.string(),
  status: healthStatus,
  raw: rawHealthSchema,
  compacted: compactionHealthSchema,
})
export type ArchiveBook = z.infer<typeof archiveBookSchema>

export const runLogSchema = z.object({
  file: z.string(),
  started_at: z.string(),
  modified_at: z.string(),
  version: z.string(),
  mode: z.string(),
  exit_code: z.number().nullable(),
  status: healthStatus,
  message: z.string(),
  upload: z.enum(['ok', 'failed', '']),
  upload_target: z.string(),
  books: z.array(z.object({ book: z.string(), stage: z.string(), ledger: z.string() })),
  errors: z.array(z.string()),
})
export type RunLog = z.infer<typeof runLogSchema>

export const dataHealthSchema = z.object({
  ledger: z.string(),
  generated_at: z.string(),
  status: healthStatus,
  checks: z.array(healthCheckSchema),
  collector: z.object({ status: healthStatus, message: z.string(), note: z.string() }),
  archive: z.object({
    status: healthStatus,
    source: z.string(),
    checked_at: z.string(),
    error: z.string().optional(),
    books: z.array(archiveBookSchema),
  }),
  executor: z.object({
    status: healthStatus,
    message: z.string(),
    ledger_path: z.string(),
    ledger_found: z.boolean(),
    ledger_modified_at: z.string(),
    records: z.number(),
    last_recorded_at: z.string(),
    books: z.array(z.object({ book: z.string(), run: runStatusSchema })),
    last_run: runLogSchema.nullable(),
    runs: z.array(runLogSchema),
    upload: z.object({ status: healthStatus, message: z.string(), target: z.string(), at: z.string() }),
  }),
  thresholds: z.record(z.string(), z.string()),
})
export type DataHealth = z.infer<typeof dataHealthSchema>
