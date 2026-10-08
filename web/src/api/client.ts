import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import type { z } from 'zod'
import {
  candlesResponseSchema,
  controlsInfoSchema,
  dataHealthSchema,
  forwardTestsResponseSchema,
  healthSchema,
  ledgerResponseSchema,
  ledgersResponseSchema,
  riskResponseSchema,
  runDocSchema,
  runsResponseSchema,
  strategiesInfoSchema,
  studiesResponseSchema,
  studyDocSchema,
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
}

export function useForwardTests() {
  const ledger = useLedgerName()
  return useQuery({
    queryKey: queryKeys.forwardTests(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/forward-tests', ledger), forwardTestsResponseSchema, signal),
    refetchInterval: REFRESH_MS,
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

export function useCandles(book: string, days: number) {
  const ledger = useLedgerName()
  return useQuery({
    queryKey: queryKeys.candles(ledger, book, days),
    queryFn: ({ signal }) =>
      fetchJSON(
        withLedger(`/forward-tests/${encodeURIComponent(book)}/candles?days=${days}`, ledger),
        candlesResponseSchema,
        signal,
      ),
    staleTime: 5 * 60_000,
  })
}

export function useRisk() {
  const ledger = useLedgerName()
  return useQuery({
    queryKey: queryKeys.risk(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/risk', ledger), riskResponseSchema, signal),
    refetchInterval: REFRESH_MS,
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
export function useDataHealth() {
  const ledger = useLedgerName()
  return useQuery({
    queryKey: queryKeys.dataHealth(ledger),
    queryFn: ({ signal }) => fetchJSON(withLedger('/health/data', ledger), dataHealthSchema, signal),
    refetchInterval: REFRESH_MS,
  })
}
