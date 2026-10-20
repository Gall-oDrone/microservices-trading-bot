/**
 * The attention bar's items (components/AppShell.tsx): what needs the
 * operator on the selected ledger, from its halt state and health checks.
 * Pure, so the ordering and the wording are unit-tested.
 */
import type { HealthCheck } from '../api/schemas'

export interface AttentionItem {
  id: string
  tone: 'block' | 'warn'
  label: string
  message: string
  to: string
}

export interface HaltLike {
  halted: boolean
  reason?: string
  by?: string
}

/**
 * The halt is a block and comes first; a failed check is a block, a warning
 * check a warn; checks that are off or ok never appear. `search` is the
 * ledger query string the links keep.
 */
export function attentionItems(
  halt: HaltLike | null | undefined,
  checks: HealthCheck[] | undefined,
  search: string,
): AttentionItem[] {
  const out: AttentionItem[] = []
  if (halt?.halted) {
    out.push({
      id: 'halt',
      tone: 'block',
      label: 'Trading halted',
      message: [halt.reason, halt.by ? `by ${halt.by}` : ''].filter(Boolean).join(' · ') || 'operator halt file',
      to: `/risk${search}`,
    })
  }
  for (const c of checks ?? []) {
    if (c.status !== 'fail' && c.status !== 'warn') continue
    out.push({
      id: c.id,
      tone: c.status === 'fail' ? 'block' : 'warn',
      label: c.label,
      message: c.message,
      to: `/data-health${search}`,
    })
  }
  out.sort((a, b) => (a.tone === b.tone ? 0 : a.tone === 'block' ? -1 : 1))
  return out
}
