/**
 * The operator's clock (U3, 2026-10-20). Venue pages keep their own time
 * (eToro: New York; Bitso bars: Mexico City) because that is what the data
 * means; everything that is a *moment the operator did or saw something*
 * (audit lines, outcomes, positions opened / closed, account reads) follows
 * one preference, chosen in the shell and kept in localStorage.
 */
import { useSyncExternalStore } from 'react'
import { fmtIn } from './format'

export type Tz = 'ny' | 'utc' | 'mx' | 'local'

export const TZ_KEY = 'tz'

const LOCAL_ZONE = typeof Intl !== 'undefined' ? Intl.DateTimeFormat().resolvedOptions().timeZone : 'UTC'

export const TZ_OPTIONS: { id: Tz; label: string; zone: string; title: string }[] = [
  { id: 'ny', label: 'NY', zone: 'America/New_York', title: 'New York (the eToro session clock)' },
  { id: 'utc', label: 'UTC', zone: 'UTC', title: 'UTC (the logs and the ledger timestamps)' },
  { id: 'mx', label: 'MX', zone: 'America/Mexico_City', title: 'Mexico City (the Bitso bars)' },
  { id: 'local', label: 'Local', zone: LOCAL_ZONE, title: `This device (${LOCAL_ZONE})` },
]

const listeners = new Set<() => void>()

function read(): Tz {
  try {
    const v = localStorage.getItem(TZ_KEY)
    return v === 'utc' || v === 'mx' || v === 'local' ? v : 'ny'
  } catch {
    return 'ny'
  }
}

let current: Tz = typeof window === 'undefined' ? 'ny' : read()

export function setTz(tz: Tz) {
  current = tz
  try {
    localStorage.setItem(TZ_KEY, tz)
  } catch {
    // no storage: the choice lasts for the page
  }
  listeners.forEach((l) => l())
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

/** The preference and its setter; every subscriber re-renders on a change. */
export function useTz(): [Tz, (tz: Tz) => void] {
  const tz = useSyncExternalStore(
    subscribe,
    () => current,
    () => 'ny' as Tz,
  )
  return [tz, setTz]
}

export function tzZone(tz: Tz): string {
  return TZ_OPTIONS.find((o) => o.id === tz)?.zone ?? 'America/New_York'
}

export function tzLabel(tz: Tz): string {
  return TZ_OPTIONS.find((o) => o.id === tz)?.label ?? 'NY'
}

/** "2026-10-19T13:35:00Z" in the chosen zone, "19 Oct 2026, 09:35". */
export function fmtTz(iso: string, tz: Tz): string {
  return fmtIn(iso, tzZone(tz))
}

/** A formatter bound to the preference: `fmt(iso)` and the short zone label for the suffix. */
export function useClock(): { fmt: (iso: string) => string; label: string; tz: Tz } {
  const [tz] = useTz()
  return { fmt: (iso) => fmtTz(iso, tz), label: tzLabel(tz), tz }
}

/** Test hook: forget the preference. */
export function resetTz() {
  current = 'ny'
  try {
    localStorage.removeItem(TZ_KEY)
  } catch {
    // ignore
  }
  listeners.forEach((l) => l())
}
