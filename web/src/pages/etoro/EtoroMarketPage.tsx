/**
 * Market for an eToro ledger. There is no eToro live feed in ui-api yet
 * (porting plan P5c adds the rates poller for NSDQ100 and SPX500), so this
 * shows the session and the last closes the executor decided on, and sends
 * the operator to the stored candles on each book's detail page.
 */
import { Link } from 'react-router'
import { useEtoroForwardTests, useLedgerSearch } from '../../api/client'
import { SessionClock } from '../../components/etoro/parts'
import { IconArrowRight } from '../../components/icons'
import { Banner, CardSkeleton, ErrorState, SignalPill, Stat } from '../../components/ui'
import { bookLabel, fmtDate, fmtPct, fmtPrice } from '../../lib/format'
import { usePageTitle } from '../../lib/usePageTitle'

export function EtoroMarketPage() {
  usePageTitle('Market · eToro', 'The NYSE session and the last closes of the eToro index CFDs. No live quotes yet.')
  const q = useEtoroForwardTests()
  const search = useLedgerSearch()
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Market</h1>
          <p>
            eToro index CFDs trade the NYSE cash session. The executor decides on closed daily candles; the live rates
            poller (bid, ask, spread, provisional flip level) arrives with the next slice of the porting plan.
          </p>
        </div>
        {q.data && <SessionClock s={q.data.session} />}
      </div>

      <Banner tone="info" title="No live eToro quotes yet" id="market-etoro-off">
        ui-api streams Bitso only for now. Each book&apos;s detail page shows the stored candles with the SMA50 and the
        demo orders.
      </Banner>

      {q.isLoading && (
        <div className="grid grid-2">
          <CardSkeleton lines={3} />
          <CardSkeleton lines={3} />
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
            <article key={b.book} className="card venue-etoro interactive" data-testid={`market-${b.book}`}>
              <div className="card-head">
                <div>
                  <h2>{bookLabel(b.book)}</h2>
                  <div className="sub">
                    {b.recorded_at
                      ? `last close ${fmtDate(b.decision.bar_date)} · instrument #${b.instrument.id}`
                      : 'no records yet'}
                  </div>
                </div>
                <SignalPill signal={b.decision.signal} />
              </div>
              {b.recorded_at ? (
                <div className="grid grid-3" style={{ gap: 14 }}>
                  <Stat label="Last close" value={fmtPrice(b.decision.close, b.quote, b.book)} hint="USD" large />
                  <Stat label="SMA50" value={fmtPrice(b.decision.sma50, b.quote, b.book)} hint="50 NYSE closes" large />
                  <Stat
                    label="Distance"
                    value={fmtPct(b.distance_to_sma_pct, 2)}
                    tone={b.distance_to_sma_pct >= 0 ? 'pos' : 'neg'}
                    hint="close vs SMA50"
                    large
                  />
                </div>
              ) : null}
              <div style={{ marginTop: 14 }}>
                <Link
                  to={{ pathname: `/forward-tests/${b.book}`, search }}
                  className="lp-link"
                  id={`market-open-${b.book}`}
                >
                  Candles and demo orders <IconArrowRight />
                </Link>
              </div>
            </article>
          ))}
        </div>
      )}
    </>
  )
}
