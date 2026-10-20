/**
 * Today: the eToro operator's first screen (U2, 2026-10-20). A Bitso ledger
 * has no equivalent yet (its executor has no account layer); it gets a note
 * and the links that answer the same questions there.
 */
import { Link } from 'react-router'
import { useEtoroLedgerName, useLedgerName, useLedgerSearch } from '../api/client'
import { IconArrowRight } from '../components/icons'
import { Empty, LedgerBadge } from '../components/ui'
import { VenueSwitch } from '../components/VenueSwitch'
import { usePageTitle } from '../lib/usePageTitle'
import { EtoroTodayPage } from './etoro/EtoroTodayPage'

function BitsoTodayNote() {
  usePageTitle('Today', 'The Today view exists for the eToro demo ledger.')
  const ledger = useLedgerName()
  const search = useLedgerSearch()
  const etoro = useEtoroLedgerName()
  return (
    <>
      <div className="page-head">
        <div>
          <div className="row" style={{ gap: 12 }}>
            <h1>Today</h1>
            {ledger && <LedgerBadge name={ledger} />}
          </div>
          <p>The Today view reads the eToro demo account; a Bitso ledger is read from its files.</p>
        </div>
      </div>
      <div className="card" data-testid="today-bitso-note">
        <Empty title="No Today view on a Bitso ledger yet">
          The Bitso stage executor records its ledger and nothing else: the{' '}
          <Link to={`/forward-tests${search}`}>forward tests</Link> show the last run and the position,{' '}
          <Link to={`/risk${search}`}>Risk</Link> the limits and the halt,{' '}
          <Link to={`/data-health${search}`}>Data health</Link> the checks.
          {etoro ? (
            <>
              {' '}
              <Link to={{ pathname: '/today', search: `?ledger=${encodeURIComponent(etoro)}` }} id="today-switch-etoro">
                Open Today for {etoro} <IconArrowRight />
              </Link>
            </>
          ) : null}
        </Empty>
      </div>
    </>
  )
}

export function TodayPage() {
  return <VenueSwitch bitso={<BitsoTodayNote />} etoro={<EtoroTodayPage />} />
}
