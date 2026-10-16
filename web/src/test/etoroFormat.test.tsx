import { QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { createMemoryRouter, RouterProvider } from 'react-router'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useVenue } from '../api/client'
import { quoteOf } from '../lib/market'
import { bookLabel, bookSymbol, CFD_BOOKS, fmtNY, fmtNYTime, fmtPrice, fmtUnits, quoteDigits } from '../lib/format'
import { makeQueryClient } from '../router'
import { server } from './setup'

describe('eToro formatting', () => {
  it('shows New York time on both sides of daylight saving', () => {
    // 13:35 UTC in October is 09:35 EDT (UTC-4): the executor's run.
    expect(fmtNY('2026-10-19T13:35:00Z')).toMatch(/19 Oct 2026, 09:35/)
    // 14:35 UTC in January is 09:35 EST (UTC-5).
    expect(fmtNY('2027-01-19T14:35:00Z')).toMatch(/19 Jan 2027, 09:35/)
    // An offset-carrying stamp (api.EtoroRunStatus.next_run_at) reads the same.
    expect(fmtNY('2026-10-19T09:35:00-04:00')).toMatch(/19 Oct 2026, 09:35/)
    expect(fmtNYTime('2026-10-16T16:00:00-04:00')).toBe('16:00')
    expect(fmtNY('not a date')).toBe('not a date')
  })

  it('formats CFD units to six decimals', () => {
    expect(fmtUnits(0.140735)).toBe('0.140735 u')
    expect(fmtUnits(0.035799)).toBe('0.035799 u')
    expect(fmtUnits(1)).toBe('1.000000 u')
  })

  it('labels the CFD books and keeps the Bitso labels', () => {
    expect(bookLabel('nsdq100')).toBe('NSDQ100 · Nasdaq-100 CFD')
    expect(bookLabel('spx500')).toBe('SPX500 · S&P 500 CFD')
    expect(bookSymbol('nsdq100')).toBe('NSDQ100')
    expect(bookLabel('btc_mxn')).toBe('BTC / MXN')
    expect(bookSymbol('btc_mxn')).toBe('BTC / MXN')
    expect(Object.keys(CFD_BOOKS)).toEqual(['nsdq100', 'spx500'])
  })

  it('quotes the CFD books in USD with cents; Bitso prices stay whole', () => {
    expect(quoteOf('nsdq100')).toBe('usd')
    expect(quoteOf('spx500')).toBe('usd')
    expect(quoteOf('btc_mxn')).toBe('mxn')
    expect(quoteDigits('usd', 'spx500')).toBe(2)
    expect(quoteDigits('usd')).toBe(0)
    expect(quoteDigits('mxn', 'btc_mxn')).toBe(0)
    expect(fmtPrice(7738.57, 'USD', 'spx500')).toBe('7,738.57')
    expect(fmtPrice(29868.7, 'usd', 'nsdq100')).toBe('29,868.70')
    expect(fmtPrice(29868.7, 'usd')).toBe('29,869')
  })
})

function wrapper(path: string) {
  return function Wrapper({ children }: { children: ReactNode }) {
    const router = createMemoryRouter([{ path: '*', element: <>{children}</> }], { initialEntries: [path] })
    const qc = makeQueryClient()
    qc.setDefaultOptions({ queries: { retry: false } })
    return (
      <QueryClientProvider client={qc}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
  }
}

describe('useVenue', () => {
  it('is undefined while the ledgers list loads, then follows the selected ledger', async () => {
    const { result } = renderHook(() => useVenue(), { wrapper: wrapper('/risk?ledger=etoro-demo') })
    expect(result.current).toBeUndefined()
    await waitFor(() => expect(result.current).toBe('etoro'))
  })

  it('defaults to the default ledger (Bitso) without ?ledger=', async () => {
    const { result } = renderHook(() => useVenue(), { wrapper: wrapper('/risk') })
    await waitFor(() => expect(result.current).toBe('bitso'))
  })

  it('treats an unknown ledger as Bitso so ui-api can report it', async () => {
    const { result } = renderHook(() => useVenue(), { wrapper: wrapper('/risk?ledger=nope') })
    await waitFor(() => expect(result.current).toBe('bitso'))
  })

  it('falls back to Bitso when the ledgers list cannot be read', async () => {
    server.use(http.get('/api/ui/ledgers', () => HttpResponse.json({ error: 'boom' }, { status: 500 })))
    const { result } = renderHook(() => useVenue(), { wrapper: wrapper('/risk?ledger=etoro-demo') })
    await waitFor(() => expect(result.current).toBe('bitso'))
  })

  it('reads a venue-less ledgers list as Bitso', async () => {
    server.use(
      http.get('/api/ui/ledgers', () =>
        HttpResponse.json({
          ledgers: [{ name: 'old', path: 'p', default: true, found: true, records: 1, modes: [], last_bar_date: '' }],
        }),
      ),
    )
    const { result } = renderHook(() => useVenue(), { wrapper: wrapper('/risk?ledger=old') })
    await waitFor(() => expect(result.current).toBe('bitso'))
  })
})
