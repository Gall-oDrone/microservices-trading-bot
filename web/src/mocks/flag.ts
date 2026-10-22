/**
 * The mock scenario flag (`?mock=<scenario>` on the page URL, forwarded to
 * every API call by src/api/client.ts in VITE_MOCK builds). Tests cannot set
 * the page URL under a memory router, so they force a scenario here instead.
 *
 * Scenarios: short-ttl, short-cash, market-closed, trading-off (trades.ts)
 * and, since 2026-10-22, `real-canary`: the console as the measurement
 * canary's process sees it (docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md
 * §3.4): the ledger list is the Bitso stage ledger plus `etoro-real-canary`
 * (environment real), its fixtures are the demo captures relabelled, the
 * trading block reports x20 and 50-60 USD, prepare answers REAL phrases and
 * ui-real- refs. No key, no account: all of it is testable offline.
 */
let forced: string | null = null

/** Force a scenario for every request (tests); null clears it. */
export function forceMockFlag(flag: string | null) {
  forced = flag
}

/** The scenario of a request: the URL's `mock` parameter, else the forced one. */
export function mockFlag(request: Request): string {
  return new URL(request.url).searchParams.get('mock') || forced || ''
}

/** The real-canary ledger name the scenario serves. */
export const REAL_CANARY_LEDGER = 'etoro-real-canary'

export function isRealCanary(request: Request): boolean {
  return mockFlag(request) === 'real-canary'
}
