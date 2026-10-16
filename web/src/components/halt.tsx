/**
 * Who halted trading, when and why: from the policy, the ledger's halt file
 * (R2), or both. Shared by the Risk page of both venues; `orderNoun` names
 * what the executor blocks ("stage order" on Bitso, "demo order" on eToro).
 */
import type { HaltFile } from '../api/schemas'
import { fmtMx, fmtNY, fmtUTC } from '../lib/format'
import { Banner } from './ui'

export interface HaltView {
  halt_file: HaltFile
  halt_source: 'none' | 'policy' | 'file' | 'both'
  halt_reason: string
  policy: { halt_reason?: string }
}

export function HaltBanner({
  r,
  orderNoun = 'stage order',
  clock = 'mx',
}: {
  r: HaltView
  orderNoun?: string
  clock?: 'mx' | 'ny'
}) {
  const f = r.halt_file
  const fromFile = r.halt_source === 'file' || r.halt_source === 'both'
  const fromPolicy = r.halt_source === 'policy' || r.halt_source === 'both'
  const title = fromFile && !fromPolicy ? 'Trading halted by operator' : 'Trading halted by policy'
  const when = clock === 'ny' ? `${fmtNY(f.at)} New York` : `${fmtMx(f.at)} Mexico City`
  return (
    <Banner tone="block" title={title} id="banner-halt">
      {fromFile ? (
        <span data-testid="halt-file">
          <b>{f.reason}</b> · by <b>{f.by}</b> · <span title={fmtUTC(f.at)}>{when}</span> ·{' '}
          <span className="num faint">{f.path}</span>
          {fromPolicy && <> · policy: {r.policy.halt_reason || 'no reason given'}</>}
        </span>
      ) : (
        <>{r.halt_reason || 'No reason given.'}</>
      )}{' '}
      Every new {orderNoun} is blocked and recorded.{' '}
      {fromFile ? (
        <>
          Resume from <a href="#operator-controls">Operator controls</a> below (or remove the file by hand).
        </>
      ) : (
        <>Lift it in the policy to resume.</>
      )}
    </Banner>
  )
}
