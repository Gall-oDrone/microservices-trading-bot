/**
 * Data health for an eToro ledger: the etorohealth checks (the ones
 * ui-alerts runs: ledger, halt file, coverage, run, reconcile, intents) in
 * the Data health shape, and the reconcile reports next to the ledger.
 */
import type { EtoroHealthResponse } from '../../api/etoroSchemas'
import { useEtoroHealth, useEtoroReconcile } from '../../api/client'
import { ReconcileList } from '../../components/etoro/reconcile'
import { Checks, StatusIcon } from '../../components/health'
import { IconPulse } from '../../components/icons'
import { Badge, CardSkeleton, ErrorState, LedgerBadge } from '../../components/ui'
import { fmtDate, fmtNY, fmtUTC } from '../../lib/format'
import { countByStatus, statusLabel, statusTone } from '../../lib/health'
import { usePageTitle } from '../../lib/usePageTitle'
import { PageIntro } from '../../components/PageIntro'

const THRESHOLD_LABEL: Record<string, string> = {
  run_at: 'Run at',
  due_by: 'Due by',
  coverage_days: 'Coverage window',
  intent_grace: 'Intent grace',
}

function Hero({ d }: { d: EtoroHealthResponse }) {
  const n = countByStatus(d.checks)
  const headline =
    d.status === 'ok'
      ? 'The eToro executor is on schedule'
      : d.status === 'fail'
        ? `${n.fail} check${n.fail === 1 ? '' : 's'} failing`
        : d.status === 'warn'
          ? `${n.warn} warning${n.warn === 1 ? '' : 's'}`
          : 'Status unknown'
  const thresholds = Object.entries(d.thresholds).sort(([a], [b]) => a.localeCompare(b))
  return (
    <section
      className={`card health-hero venue-etoro s-${d.status}`}
      aria-labelledby="health-summary-h"
      data-testid="health-summary"
    >
      <div className="health-orb" aria-hidden="true">
        <StatusIcon s={d.status} style={{ width: 30, height: 30 }} />
      </div>
      <div className="health-headline">
        <span className="stat-label">Overall</span>
        <h2 id="health-summary-h">{headline}</h2>
        <div className="row health-counts" style={{ gap: 6, flexWrap: 'wrap' }}>
          {(['fail', 'warn', 'unknown', 'ok', 'off'] as const)
            .filter((s) => n[s] > 0)
            .map((s) => (
              <Badge key={s} tone={statusTone(s)}>
                {n[s]} {statusLabel(s)}
              </Badge>
            ))}
          <span className="faint" style={{ fontSize: 12 }}>
            due day {fmtDate(d.due_date)}
          </span>
        </div>
      </div>
      <dl className="lp-book-rows" style={{ border: 0, padding: 0 }} data-testid="health-thresholds">
        {thresholds.map(([k, v]) => (
          <div key={k}>
            <dt className="stat-label">{THRESHOLD_LABEL[k] ?? k.replace(/_/g, ' ')}</dt>
            <dd className="num" style={{ margin: 0, fontSize: 13 }}>
              {k === 'coverage_days' ? `${v} trading days` : v}
            </dd>
          </div>
        ))}
        <div>
          <dt className="stat-label">Ledger</dt>
          <dd
            className="num faint"
            style={{ margin: 0, fontSize: 11.5, overflowWrap: 'anywhere' }}
            title={d.ledger_path}
          >
            {d.ledger_path}
          </dd>
        </div>
      </dl>
    </section>
  )
}

function ReconcileSection() {
  const q = useEtoroReconcile()
  return (
    <section className="card venue-etoro" aria-labelledby="reconcile-h" data-testid="reconcile-section">
      <div className="card-head">
        <div>
          <h2 id="reconcile-h">Reconcile</h2>
          <div className="sub">
            After every run, cmd/etoro-reconcile compares the ledger with the demo account (positions, orders,
            financing). A drift that the operator&apos;s later re-check clears counts as effective clean, the rule
            ui-alerts applies. Newest first.
          </div>
        </div>
        {q.data && (
          <span className="num faint" title={q.data.dir} style={{ fontSize: 11.5 }}>
            {q.data.reports.length} report{q.data.reports.length === 1 ? '' : 's'}
          </span>
        )}
      </div>
      {q.isLoading && <CardSkeleton lines={3} />}
      {q.isError && <ErrorState error={q.error} onRetry={() => q.refetch()} />}
      {q.data && <ReconcileList reports={q.data.reports} />}
    </section>
  )
}

export function EtoroDataHealthPage() {
  usePageTitle(
    'Data health · eToro',
    'The eToro demo executor: run coverage, halt file, intents and reconcile reports.',
  )
  const q = useEtoroHealth()
  const d = q.data
  return (
    <>
      <div className="page-head">
        <div>
          <div className="row" style={{ gap: 12 }}>
            <h1>Data health</h1>
            {d && <LedgerBadge name={d.ledger} venue="etoro" />}
          </div>
          <PageIntro>
            Is the eToro demo executor doing its 09:35 New York run, and does the demo account still match the ledger?
            The same checks ui-alerts sends, read from the files next to the ledger.
          </PageIntro>
        </div>
        {d && (
          <span className="faint row" style={{ gap: 6 }} title={fmtUTC(d.generated_at)}>
            <IconPulse style={{ width: 14, height: 14 }} /> checked {fmtNY(d.generated_at)} NY
          </span>
        )}
      </div>

      {q.isLoading && (
        <div className="stack" style={{ gap: 16 }}>
          <CardSkeleton lines={3} />
          <CardSkeleton lines={6} />
        </div>
      )}
      {q.isError && (
        <div className="card">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      )}
      {d && (
        <>
          <Hero d={d} />
          <Checks
            checks={d.checks}
            sub={
              <>
                Worst first. Run at {d.thresholds.run_at ?? '09:35 New York'}, due by{' '}
                {d.thresholds.due_by ?? '10:00 New York'}.
              </>
            }
          />
          <ReconcileSection />
        </>
      )}
    </>
  )
}
