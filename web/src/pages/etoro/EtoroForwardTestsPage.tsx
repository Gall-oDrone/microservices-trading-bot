/**
 * Forward tests for an eToro ledger: one card per index-CFD book from
 * GET /api/ui/etoro/forward-tests, the NYSE session clock and the next
 * 09:35 New York run. No live strip yet (eToro quotes arrive with P5c).
 */
import { useEtoroForwardTests } from '../../api/client'
import { EtoroForwardTestCard } from '../../components/etoro/EtoroForwardTestCard'
import { SessionClock } from '../../components/etoro/parts'
import { Banner, CardSkeleton, ErrorState } from '../../components/ui'
import { bookSymbol, fmtNY, fmtUTC } from '../../lib/format'
import { usePageTitle } from '../../lib/usePageTitle'

export function EtoroForwardTestsPage() {
  usePageTitle('Forward tests · eToro')
  const q = useEtoroForwardTests()
  const books = q.data?.books ?? []
  const missed = books.filter((b) => b.run.status === 'missed')
  const pending = books.filter((b) => b.run.status === 'pending')
  return (
    <>
      <div className="page-head">
        <div>
          <h1>SMA50 forward test · eToro index CFDs (demo)</h1>
          <p>
            The frozen SMA50 rule on NSDQ100 and SPX500, decided on each NYSE close and executed at 09:35 New York on a
            demo account with x1 CFDs. Paper numbers sit next to holding the same CFD, after the pre-registered spread
            and overnight financing.
          </p>
        </div>
        {q.data && (
          <div className="stack" style={{ alignItems: 'flex-end', gap: 6 }}>
            <SessionClock s={q.data.session} />
            <span className="faint" title={fmtUTC(q.data.generated_at)}>
              updated {fmtNY(q.data.generated_at)} (New York)
            </span>
          </div>
        )}
      </div>

      {missed.length > 0 && (
        <Banner tone="warn" title="The eToro executor has missed a trading day" id="banner-missed">
          {missed.map((b) => `${bookSymbol(b.book)}: ${b.run.missing_days.join(', ')}`).join(' · ')}. Due{' '}
          {missed[0].run.due_date}. Check the run logs and the reconcile reports next to the ledger.
        </Banner>
      )}
      {pending.length > 0 && missed.length === 0 && (
        <Banner tone="info" title="Today's run is not recorded yet" id="banner-pending">
          {pending[0].run.message}.
        </Banner>
      )}

      {q.isLoading && (
        <div className="grid grid-2">
          <CardSkeleton lines={6} />
          <CardSkeleton lines={6} />
        </div>
      )}
      {q.isError && (
        <div className="card">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      )}
      {q.data && (
        <div className="grid grid-2">
          {q.data.books.map((b) => (
            <EtoroForwardTestCard key={b.book} ft={b} />
          ))}
        </div>
      )}
    </>
  )
}
