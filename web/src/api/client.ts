import { useQuery } from '@tanstack/react-query'
import type { z } from 'zod'
import {
  candlesResponseSchema,
  forwardTestsResponseSchema,
  healthSchema,
  ledgerResponseSchema,
  riskResponseSchema,
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
): Promise<z.infer<S>> {
  let res: Response
  try {
    res = await fetch(BASE + path, { signal, headers: { Accept: 'application/json' } })
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

export const queryKeys = {
  forwardTests: ['forward-tests'] as const,
  ledger: (book: string) => ['ledger', book] as const,
  candles: (book: string, days: number) => ['candles', book, days] as const,
  risk: ['risk'] as const,
  health: ['health'] as const,
}

export function useForwardTests() {
  return useQuery({
    queryKey: queryKeys.forwardTests,
    queryFn: ({ signal }) => fetchJSON('/forward-tests', forwardTestsResponseSchema, signal),
    refetchInterval: REFRESH_MS,
  })
}

export function useLedger(book: string) {
  return useQuery({
    queryKey: queryKeys.ledger(book),
    queryFn: ({ signal }) =>
      fetchJSON(`/forward-tests/${encodeURIComponent(book)}/ledger`, ledgerResponseSchema, signal),
    refetchInterval: REFRESH_MS,
  })
}

export function useCandles(book: string, days: number) {
  return useQuery({
    queryKey: queryKeys.candles(book, days),
    queryFn: ({ signal }) =>
      fetchJSON(`/forward-tests/${encodeURIComponent(book)}/candles?days=${days}`, candlesResponseSchema, signal),
    staleTime: 5 * 60_000,
  })
}

export function useRisk() {
  return useQuery({
    queryKey: queryKeys.risk,
    queryFn: ({ signal }) => fetchJSON('/risk', riskResponseSchema, signal),
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
