/**
 * Reconcile reports of the eToro executor (cmd/etoro-reconcile, read by
 * ui-api from <ledger dir>/reconcile/<date>.json and .recheck.json): the
 * badge for one day, its findings with the operator's re-check, and the
 * list on the Data health page (newest first).
 */
import type { EtoroReconcileFinding, EtoroReconcileReport, EtoroRecheck } from '../../api/etoroSchemas'
import { fmtDate, fmtMoney, fmtNY, fmtUTC } from '../../lib/format'
import { Badge, Empty } from '../ui'

type Recheck = EtoroRecheck | null | undefined

/** clean / drift / warnings for one report, with "effective clean" when a re-check cleared the drift. */
export function ReconcileBadge({ r }: { r: EtoroReconcileReport }) {
  if (r.error) {
    return (
      <Badge tone="warn" dot title={r.error}>
        unreadable
      </Badge>
    )
  }
  if (r.clean) {
    return (
      <Badge tone="ok" dot>
        clean
      </Badge>
    )
  }
  return (
    <span className="row" style={{ gap: 6 }} data-testid="reconcile-state">
      <Badge tone={r.drift > 0 ? 'block' : 'warn'} dot>
        {r.drift > 0 ? `drift ${r.drift}` : `${r.warnings} warning${r.warnings === 1 ? '' : 's'}`}
      </Badge>
      {r.effective_clean && (
        <Badge tone="ok" title="The operator's later re-check found the ledger and the account in agreement">
          effective clean
        </Badge>
      )}
    </span>
  )
}

export function ReconcileFindings({
  findings,
  recheck,
  quiet = false,
}: {
  findings: EtoroReconcileFinding[]
  recheck: Recheck
  /** Skip the "agree" line (the caller already explained an empty, filtered list). */
  quiet?: boolean
}) {
  return (
    <>
      {findings.length === 0 ? (
        quiet ? null : (
          <div className="faint" style={{ fontSize: 12.5 }}>
            Ledger and account agree.
          </div>
        )
      ) : (
        <ul className="reconcile-findings">
          {findings.map((f, i) => (
            <li key={i}>
              <Badge tone={f.severity === 'drift' ? 'block' : 'warn'}>{f.severity}</Badge>{' '}
              {f.book && <span className="num">{f.book.toUpperCase()} · </span>}
              {f.message} <span className="faint num">({f.code})</span>
            </li>
          ))}
        </ul>
      )}
      {recheck && (
        <div className="reconcile-recheck" data-testid="reconcile-recheck">
          Re-check <span title={fmtUTC(recheck.generated_at)}>{fmtNY(recheck.generated_at)} NY</span>:{' '}
          {recheck.error ? (
            <span className="warn-text">{recheck.error}</span>
          ) : recheck.clean ? (
            <Badge tone="ok" dot>
              clean
            </Badge>
          ) : (
            <>
              <Badge tone="block" dot>
                {recheck.findings.length} finding{recheck.findings.length === 1 ? '' : 's'}
              </Badge>{' '}
              {recheck.findings.map((f) => f.message).join(' · ')}
            </>
          )}
        </div>
      )}
    </>
  )
}

/** The newest reports first, one row per day. */
export function ReconcileList({ reports }: { reports: EtoroReconcileReport[] }) {
  if (reports.length === 0) {
    return (
      <Empty title="No reconcile reports yet">
        The wrapper writes one after every run; run cmd/etoro-reconcile by hand to create one now.
      </Empty>
    )
  }
  return (
    <ul className="reconcile-list" data-testid="reconcile-list">
      {reports.map((r) => (
        <li key={r.date} className="reconcile-row" data-testid={`reconcile-${r.date}`} data-clean={r.effective_clean}>
          <span>
            <b>{fmtDate(r.date)}</b>
            <div className="faint num" style={{ fontSize: 11 }} title={fmtUTC(r.generated_at)}>
              {r.generated_at ? `${fmtNY(r.generated_at)} NY` : '—'}
            </div>
          </span>
          <ReconcileBadge r={r} />
          <span style={{ minWidth: 0 }}>
            {r.error ? (
              <span className="warn-text">{r.error}</span>
            ) : (
              <ReconcileFindings findings={r.findings} recheck={r.recheck} />
            )}
          </span>
          <span className="faint num" style={{ fontSize: 12, textAlign: 'right' }}>
            {r.account ? (
              <>
                equity {fmtMoney(r.account.equity, r.account.currency, 0)}
                <br />
                {r.books.map((b) => `${b.symbol} ${b.ledger_state}`).join(' · ')}
              </>
            ) : (
              '—'
            )}
          </span>
        </li>
      ))}
    </ul>
  )
}
