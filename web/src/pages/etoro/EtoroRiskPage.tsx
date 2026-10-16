/**
 * Risk for an eToro ledger: EtoroDemoPolicy, the halt file with the shared
 * operator controls (halt / resume, confirm with the ledger name), the demo
 * account, per-book exposure against the caps, and the latest P4 risk report.
 */
import { ApiError, useEtoroRisk, useEtoroRiskReport } from '../../api/client'
import { OperatorControls } from '../../components/controls'
import { EtoroBookRiskCard, NoRiskReport, PolicySummary, RiskReportCard } from '../../components/etoro/risk'
import { HaltBanner } from '../../components/halt'
import { IconShield } from '../../components/icons'
import { Banner, CardSkeleton, ErrorState, LedgerBadge, Stat } from '../../components/ui'
import { fmtMoney, fmtNY, fmtUTC } from '../../lib/format'
import { usePageTitle } from '../../lib/usePageTitle'

function RiskReportSection() {
  const q = useEtoroRiskReport()
  if (q.isLoading) return <CardSkeleton lines={6} />
  if (q.isError) {
    const e = q.error
    if (e instanceof ApiError && e.status === 404) return <NoRiskReport message={e.message} />
    return (
      <section className="card venue-etoro" aria-label="Risk report">
        <ErrorState error={e} onRetry={() => q.refetch()} />
      </section>
    )
  }
  return q.data ? <RiskReportCard r={q.data} /> : null
}

export function EtoroRiskPage() {
  usePageTitle('Risk · eToro', 'EtoroDemoPolicy, halt file, demo exposure and the latest risk report.')
  const q = useEtoroRisk()
  const r = q.data
  const firstBook = r ? Object.keys(r.policy.books).sort()[0] : undefined
  const limits = r ? (firstBook ? r.policy.books[firstBook] : r.policy.default) : undefined
  return (
    <>
      <div className="page-head">
        <div>
          <div className="row" style={{ gap: 12 }}>
            <h1>Risk</h1>
            {r && <LedgerBadge name={r.ledger} venue="etoro" />}
          </div>
          <p>
            What the eToro demo executor may send: x1 CFDs, one small leg per instrument per day, inside the NYSE cash
            session, under an exposure cap. Checks apply to orders, never to the frozen signal.
          </p>
        </div>
        {r && (
          <span className="faint" title={fmtUTC(r.generated_at)}>
            evaluated {fmtNY(r.generated_at)} NY
          </span>
        )}
      </div>

      {q.isLoading && (
        <div className="grid grid-2">
          <CardSkeleton lines={8} />
          <CardSkeleton lines={8} />
        </div>
      )}
      {q.isError && (
        <div className="card">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      )}

      {r && (
        <>
          {r.halt_file.error ? (
            <Banner tone="block" title="Halt file is invalid: the executor will not run" id="banner-halt-file-error">
              {r.halt_file.error}. Fix or remove <span className="num">{r.halt_file.path}</span>.
            </Banner>
          ) : null}
          {r.halted ? <HaltBanner r={r} orderNoun="demo order" clock="ny" /> : null}
          <Banner tone="info" title="Enforced" id="banner-enforcement">
            {r.note}
          </Banner>

          <div className="grid grid-4" style={{ marginBottom: 16 }}>
            <div className="card venue-etoro">
              <Stat
                label={
                  <>
                    <IconShield style={{ width: 13, height: 13 }} /> Halt
                  </>
                }
                value={r.halted ? 'HALTED' : 'off'}
                tone={r.halted ? 'neg' : 'pos'}
                hint={
                  r.halted
                    ? `source: ${r.halt_source === 'both' ? 'policy + halt file' : r.halt_source === 'file' ? 'halt file' : 'policy'}`
                    : r.halt_file.found
                      ? 'halt file present, not halted'
                      : 'no halt file'
                }
              />
            </div>
            <div className="card venue-etoro">
              <Stat
                label="Blocks"
                value={r.blocks}
                tone={r.blocks > 0 ? 'neg' : undefined}
                hint="orders the policy would stop"
              />
            </div>
            <div className="card venue-etoro">
              <Stat
                label="Warnings"
                value={r.warnings}
                tone={r.warnings > 0 ? 'warn' : undefined}
                hint="review, never blocking"
              />
            </div>
            <div className="card venue-etoro">
              <Stat
                label="Demo account"
                value={r.account ? fmtMoney(r.account.equity, r.account.currency, 2) : '—'}
                hint={
                  r.account ? (
                    <>
                      cash {fmtMoney(r.account.cash, r.account.currency, 2)} · exposure{' '}
                      {fmtMoney(r.account.exposure, r.account.currency, 2)} ·{' '}
                      <span title={fmtUTC(r.account_as_of)}>{fmtNY(r.account_as_of)} NY</span>
                    </>
                  ) : (
                    'no demo record yet'
                  )
                }
              />
            </div>
          </div>

          {limits && (
            <div style={{ marginBottom: 16 }}>
              <PolicySummary l={limits} version={r.policy.version} />
            </div>
          )}

          <div style={{ marginBottom: 16 }}>
            <OperatorControls policyHalted={r.policy.halted} venue="etoro" />
          </div>

          <div className="grid grid-2" style={{ marginBottom: 16 }}>
            {r.books.map((b) => (
              <EtoroBookRiskCard key={b.book} b={b} equity={r.account?.equity ?? null} />
            ))}
          </div>

          <RiskReportSection />
        </>
      )}
    </>
  )
}
