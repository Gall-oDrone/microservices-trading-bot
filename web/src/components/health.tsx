/**
 * Health visuals shared by the Data health page of both venues: the status
 * icon and badge, and the "Checks" list (worst first). Pure presentation over
 * the HealthCheck shape ui-api uses for Bitso (/health/data) and eToro
 * (/etoro/health).
 */
import type { CSSProperties, ReactNode } from 'react'
import type { HealthCheck, HealthStatus } from '../api/schemas'
import { sortChecks, statusLabel, statusTone } from '../lib/health'
import { IconAlert, IconBlock, IconCheck, IconInfo, IconMinus } from './icons'
import { Badge } from './ui'

export function StatusIcon({ s, style }: { s: HealthStatus; style?: CSSProperties }) {
  const p = { style: { width: 16, height: 16, ...style }, 'aria-hidden': true } as const
  switch (s) {
    case 'ok':
      return <IconCheck {...p} />
    case 'warn':
      return <IconAlert {...p} />
    case 'fail':
      return <IconBlock {...p} />
    case 'off':
      return <IconMinus {...p} />
    default:
      return <IconInfo {...p} />
  }
}

export function StatusBadge({ s, children }: { s: HealthStatus; children?: ReactNode }) {
  return (
    <Badge tone={statusTone(s)} dot>
      {children ?? statusLabel(s)}
    </Badge>
  )
}

export function Checks({ checks, sub }: { checks: HealthCheck[]; sub?: ReactNode }) {
  return (
    <section className="card" aria-labelledby="checks-h" style={{ marginBottom: 16 }}>
      <div className="card-head">
        <div>
          <h2 id="checks-h">Checks</h2>
          <div className="sub">
            {sub ?? <>Worst first. &ldquo;Not configured&rdquo; does not count against the overall status.</>}
          </div>
        </div>
      </div>
      <ul className="check-list" id="health-checks">
        {sortChecks(checks).map((c) => (
          <li key={c.id} className={`check-row s-${c.status}`} data-testid={`check-${c.id}`}>
            <span className="check-icon">
              <StatusIcon s={c.status} />
            </span>
            <span className="check-label">{c.label}</span>
            <span className="check-msg muted">{c.message}</span>
            <StatusBadge s={c.status} />
          </li>
        ))}
      </ul>
    </section>
  )
}
