import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import type { z } from 'zod'
import {
  etoroCandlesResponseSchema,
  etoroForwardTestsResponseSchema,
  etoroHealthResponseSchema,
  etoroLedgerResponseSchema,
  etoroReconcileResponseSchema,
  etoroRiskReportSchema,
  etoroRiskResponseSchema,
} from './etoroSchemas'
import {
  candlesResponseSchema,
  capacityResponseSchema,
  preregResponseSchema,
  controlsInfoSchema,
  dataHealthSchema,
  forwardTestsResponseSchema,
  healthSchema,
  ledgerResponseSchema,
  ledgersResponseSchema,
  monteCarloResponseSchema,
  performanceResponseSchema,
  riskResponseSchema,
  runDocSchema,
  runsResponseSchema,
  strategiesInfoSchema,
  studiesResponseSchema,
  studyDocSchema,
  type LedgerInfo,
  type Venue,
} from './schemas'

/** Error from the API or from schema validation, with a message fit for the UI. */
export class ApiError extends Error {
  readonly status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

const BASE = '/api/ui'

export async function fetchJSON<S extends z.ZodTypeAny>(
  path: string,
  schema: S,
  signal?: AbortSignal,
  init?: RequestInit,
): Promise<z.infer<S>> {
  let res: Response
  try {
    res = await fetch(BASE + path, {
      ...init,
      signal,
      headers: { Accept: 'application/json', ...(init?.headers as Record<string, string> | undefined) },
    })
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e
    throw new ApiError('ui-api is not reachable. Is it running on 127.0.0.1:8090?', 0)
  }
  const text = await res.text()
  let body: unknown
  try {
    body = text ? JSON.parse(text) : null
  } catch {
    throw new ApiError(`ui-api returned non-JSON (HTTP ${res.status})`, res.status)
  }
  if (!res.ok) {
    const msg = (body as { error?: string } | null)?.error ?? `HTTP ${res.status}`
    throw new ApiError(msg, res.status)
  }
  const parsed = schema.safeParse(body)
  if (!parsed.success) {
    const issue = parsed.error.issues[0]
    throw new ApiError(
      `Response from ${path} does not match the UI contract at "${issue?.path.join('.')}": ${issue?.message}`,
      res.status,
    )
  }
  return parsed.data
}

/** Dashboards refresh every minute; the ledger changes once a day. */
const REFRESH_MS = 60_000

/**
 * The selected ledger, from the `ledger` URL search param. Empty means the
 * ui-api default (the first configured ledger), so plain URLs keep working.
 */
export function useLedgerName(): string {
  const [params] = useSearchParams()
  return params.get('ledger') ?? ''
}

/** `?ledger=<name>` (or '') to append to in-app links, so the selection survives navigation. */
export function useLedgerSearch(): string {
  const name = useLedgerName()
  return name ? `?ledger=${encodeURIComponent(name)}` : ''
}

export function withLedger(path: string, ledger: string): string {
  if (!ledger) return path
  return `${path}${path.includes('?') ? '&' : '?'}ledger=${encodeURIComponent(ledger)}`
}

/**
 * Per-hook overrides. `ledger` replaces the URL selection (the landing page
 * shows both venues at once); `enabled: false` holds the request back, which
 * the shell uses so a Bitso route is never asked about an eToro ledger.
 */
export interface LedgerOpts {
  ledger?: string
  enabled?: boolean
}

export const queryKeys = {
  forwardTests: (ledger: string) => ['forward-tests', ledger] as const,
  ledger: (ledger: string, book: string) => ['ledger', ledger, book] as const,
  candles: (ledger: string, book: string, days: number) => ['candles', ledger, book, days] as const,
  risk: (ledger: string) => ['risk', ledger] as const,
  health: ['health'] as const,
  ledgers: ['ledgers'] as const,
  studies: ['studies'] as const,
  study: (name: string) => ['study', name] as const,
  runs: ['runs'] as const,
  run: (id: string) => ['run', id] as const,
  dataHealth: (ledger: string) => ['data-health', ledger] as const,
  controls: (ledger: string) => ['controls', ledger] as const,
  strategies: ['strategies'] as const,
  performance: (ledger: string, book: string) => ['performance', ledger, book] as const,
  monteCarlo: (ledger: string, book: string, q: string) => ['montecarlo', ledger, book, q] as const,
  capacity: (ledger: string, book: string) => ['capacity', ledger, book] as const,
  prereg: (book: string) => ['prereg', book] as const,
  // /api/ui/etoro/* (P5a): the eToro ledger's own views.
  etoroForwardTests: (ledger: string) => ['etoro', 'forward-tests', ledger] as const,
  etoroLedger: (ledger: string, book: string) => ['etoro', 'ledger', ledger, book] as const,
  etoroCandles: (ledger: string, book: string, days: number) => ['etoro', 'candles', ledger, book, days] as const,
  etoroRisk: (ledger: string) => ['etoro', 'risk', ledger] as const,
  etoroHealth: (ledger: string) => ['etoro', 'health', ledger] as const,
  etoroReconcile: (ledger: string) => ['etoro', 'reconcile', ledger] as const,
  etoroRiskReport: (ledger: string) => ['etoro', 'risk-report', ledger] as const,
}

// --- Venue ---------------------------------------------------------------

/** The ledger the URL selects (or the default), from the ledgers list; undefined while it loads or when unknown. */
export function useSelectedLedger(): { info: LedgerInfo | undefined; loading: boolean; error: boolean } {
  const name = useLedgerName()
  const q = useLedgers()
  const ledgers = q.data?.ledgers
  let info: LedgerInfo | undefined
  if (ledgers) {
    info = name ? ledgers.find((l) => l.name === name) : (ledgers.find((l) => l.default) ?? ledgers[0])
  }
  return { info, loading: q.isPending, error: q.isError }
}

/**
 * The venue of the selected ledger: 'bitso' or 'etoro'. Undefined only while
 * the ledgers list is loading. An unknown ledger name, or a failed ledgers
 * call, resolves to Bitso so the Bitso routes surface ui-api's own error
 * ("unknown ledger …", "not reachable") instead of a blank page.
 */
export function useVenue(): Venue | undefined {
  const { info, loading, error } = useSelectedLedger()
  if (info) return info.venue
  if (loading && !error) return undefined
  return 'bitso'
}

/** The first configured eToro ledger, if any (the landing page shows it next to Bitso). */
export function useEtoroLedgerName(): string | undefined {
  const q = useLedgers()
  return q.data?.ledgers.find((l) => l.venue === 'etoro')?.name
}

export function useForwardTests(opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.forwardTests(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/forward-tests', ledger), forwardTestsResponseSchema, signal),
    refetchInterval: REFRESH_MS,
    enabled: opts.enabled ?? true,
  })
}

export function useLedger(book: string) {
  const ledger = useLedgerName()
  return useQuery({
    queryKey: queryKeys.ledger(ledger, book),
    queryFn: ({ signal }) =>
      fetchJSON(withLedger(`/forward-tests/${encodeURIComponent(book)}/ledger`, ledger), ledgerResponseSchema, signal),
    refetchInterval: REFRESH_MS,
  })
}

export function useCandles(book: string, days: number, opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.candles(ledger, book, days),
    queryFn: ({ signal }) =>
      fetchJSON(
        withLedger(`/forward-tests/${encodeURIComponent(book)}/candles?days=${days}`, ledger),
        candlesResponseSchema,
        signal,
      ),
    staleTime: 5 * 60_000,
    enabled: opts.enabled ?? true,
  })
}

/** Stage P&L, paper statistics and the rule's trade distribution (plan §6.4.11). */
export function usePerformance(book: string) {
  const ledger = useLedgerName()
  return useQuery({
    queryKey: queryKeys.performance(ledger, book),
    queryFn: ({ signal }) =>
      fetchJSON(
        withLedger(`/forward-tests/${encodeURIComponent(book)}/performance`, ledger),
        performanceResponseSchema,
        signal,
      ),
    refetchInterval: REFRESH_MS,
  })
}

export type MonteCarloParams = { cost: 'primary' | 'pessimistic' | 'realized'; block: number; paths: number }

/** The Monte Carlo of the rule vs buy-and-hold; ui-api caches runs per candle file. */
export function useMonteCarlo(book: string, p: MonteCarloParams) {
  const ledger = useLedgerName()
  const q = `cost=${p.cost}&block=${p.block}&paths=${p.paths}`
  return useQuery({
    queryKey: queryKeys.monteCarlo(ledger, book, q),
    queryFn: ({ signal }) =>
      fetchJSON(
        withLedger(`/forward-tests/${encodeURIComponent(book)}/montecarlo?${q}`, ledger),
        monteCarloResponseSchema,
        signal,
      ),
    staleTime: 10 * 60_000,
  })
}

/** Order sizes against the slippage budget: the live book walk and the square-root law (plan §6.4.12). */
export function useCapacity(book: string) {
  const ledger = useLedgerName()
  return useQuery({
    queryKey: queryKeys.capacity(ledger, book),
    queryFn: ({ signal }) =>
      fetchJSON(
        withLedger(`/forward-tests/${encodeURIComponent(book)}/capacity`, ledger),
        capacityResponseSchema,
        signal,
      ),
    refetchInterval: REFRESH_MS,
  })
}

/** The latest pre-registered verdict report for a book (plan §6.4.14); not ledger-specific. */
export function usePrereg(book: string) {
  return useQuery({
    queryKey: queryKeys.prereg(book),
    queryFn: ({ signal }) =>
      fetchJSON(`/forward-tests/${encodeURIComponent(book)}/prereg`, preregResponseSchema, signal),
    staleTime: 10 * 60_000,
  })
}

export function useRisk(opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.risk(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/risk', ledger), riskResponseSchema, signal),
    refetchInterval: REFRESH_MS,
    enabled: opts.enabled ?? true,
  })
}

/** Halt/resume availability for the selected ledger and its audit log (R4). */
export function useControls() {
  const ledger = useLedgerName()
  return useQuery({
    queryKey: queryKeys.controls(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/controls', ledger), controlsInfoSchema, signal),
    refetchInterval: REFRESH_MS,
  })
}

/**
 * Every ledger's halt state, the kill switch, and the intraday
 * strategy-executor's strategies. Intraday strategies change faster than the
 * daily ledger, so this polls every 10 s.
 */
export function useStrategies() {
  return useQuery({
    queryKey: queryKeys.strategies,
    queryFn: ({ signal }) => fetchJSON('/strategies', strategiesInfoSchema, signal),
    refetchInterval: 10_000,
  })
}

export function useLedgers() {
  return useQuery({
    queryKey: queryKeys.ledgers,
    queryFn: ({ signal }) => fetchJSON('/ledgers', ledgersResponseSchema, signal),
    refetchInterval: REFRESH_MS,
  })
}

export function useHealth() {
  return useQuery({
    queryKey: queryKeys.health,
    queryFn: ({ signal }) => fetchJSON('/healthz', healthSchema, signal),
    refetchInterval: 30_000,
    retry: false,
  })
}

/** Study write-ups change rarely; refetch on focus (the default) is enough. */
export function useStudies() {
  return useQuery({
    queryKey: queryKeys.studies,
    queryFn: ({ signal }) => fetchJSON('/research/studies', studiesResponseSchema, signal),
    staleTime: 60_000,
  })
}

export function useStudy(name: string) {
  return useQuery({
    queryKey: queryKeys.study(name),
    queryFn: ({ signal }) => fetchJSON(`/research/studies/${encodeURIComponent(name)}`, studyDocSchema, signal),
    staleTime: 60_000,
    enabled: name !== '',
  })
}

/** research-run reports (daily-research -json) found in the evidence folders. */
export function useRuns() {
  return useQuery({
    queryKey: queryKeys.runs,
    queryFn: ({ signal }) => fetchJSON('/research/runs', runsResponseSchema, signal),
    staleTime: 60_000,
  })
}

/** One report; `id` is `<evidence date>/<name>`. */
export function useRun(date: string, name: string) {
  const id = `${date}/${name}`
  return useQuery({
    queryKey: queryKeys.run(id),
    queryFn: ({ signal }) =>
      fetchJSON(`/research/runs/${encodeURIComponent(date)}/${encodeURIComponent(name)}`, runDocSchema, signal),
    staleTime: 60_000,
    enabled: date !== '' && name !== '',
  })
}

/**
 * Collector flushes, archive compaction and executor runs for the selected
 * ledger. ui-api caches the S3 listing for a minute, so polling faster gains
 * nothing.
 */
export function useDataHealth(opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.dataHealth(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/health/data', ledger), dataHealthSchema, signal),
    refetchInterval: REFRESH_MS,
    enabled: opts.enabled ?? true,
  })
}

// --- eToro venue (/api/ui/etoro/*, P5a) -----------------------------------
// Same cadence as the Bitso peers: the ledger changes once a day (09:35 New
// York), so a minute is plenty; candles are stable for longer.

/** Per book: decision, paper, demo position, account, last order, run status, and the NYSE session. */
export function useEtoroForwardTests(opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.etoroForwardTests(ledger),
    queryFn: ({ signal }) =>
      fetchJSON(withLedger('/etoro/forward-tests', ledger), etoroForwardTestsResponseSchema, signal),
    refetchInterval: REFRESH_MS,
    enabled: opts.enabled ?? true,
  })
}

/** The book's ledger records and the paper-vs-demo equity series. */
export function useEtoroLedger(book: string, opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.etoroLedger(ledger, book),
    queryFn: ({ signal }) =>
      fetchJSON(
        withLedger(`/etoro/forward-tests/${encodeURIComponent(book)}/ledger`, ledger),
        etoroLedgerResponseSchema,
        signal,
      ),
    refetchInterval: REFRESH_MS,
    enabled: opts.enabled ?? true,
  })
}

/** The executor's daily candles with SMA50 (the Bitso candle shape). */
export function useEtoroCandles(book: string, days: number, opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.etoroCandles(ledger, book, days),
    queryFn: ({ signal }) =>
      fetchJSON(
        withLedger(`/etoro/forward-tests/${encodeURIComponent(book)}/candles?days=${days}`, ledger),
        etoroCandlesResponseSchema,
        signal,
      ),
    staleTime: 5 * 60_000,
    enabled: opts.enabled ?? true,
  })
}

/** EtoroDemoPolicy, the halt file, the demo account and per-book exposure. */
export function useEtoroRisk(opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.etoroRisk(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/etoro/risk', ledger), etoroRiskResponseSchema, signal),
    refetchInterval: REFRESH_MS,
    enabled: opts.enabled ?? true,
  })
}

/** The etorohealth checks (ledger, halt, coverage, run, reconcile, intents). */
export function useEtoroHealth(opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.etoroHealth(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/etoro/health', ledger), etoroHealthResponseSchema, signal),
    refetchInterval: REFRESH_MS,
    enabled: opts.enabled ?? true,
  })
}

/** The newest reconcile reports next to the ledger, with the operator's re-checks. */
export function useEtoroReconcile(opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.etoroReconcile(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/etoro/reconcile', ledger), etoroReconcileResponseSchema, signal),
    refetchInterval: REFRESH_MS,
    enabled: opts.enabled ?? true,
  })
}

/** The latest P4 risk report (cfdrisk-report/v1); 404 until the first one is written. */
export function useEtoroRiskReport(opts: LedgerOpts = {}) {
  const selected = useLedgerName()
  const ledger = opts.ledger ?? selected
  return useQuery({
    queryKey: queryKeys.etoroRiskReport(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/etoro/risk-report', ledger), etoroRiskReportSchema, signal),
    staleTime: 10 * 60_000,
    enabled: opts.enabled ?? true,
  })
}
