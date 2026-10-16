/**
 * Trade: the assisted prepare → place page (porting plan P5d). It exists
 * for the eToro demo ledger; a Bitso ledger gets a short note and a link to
 * the eToro ledger when one is configured. The Bitso stage executor keeps
 * its own, unassisted path (post-only maker legs at the open).
 */
import { Link } from 'react-router'
import { useEtoroLedgerName, useLedgerName } from '../api/client'
import { IconArrowRight } from '../components/icons'
import { Empty, LedgerBadge } from '../components/ui'
import { VenueSwitch } from '../components/VenueSwitch'
import { usePageTitle } from '../lib/usePageTitle'
import { EtoroTradePage } from './etoro/EtoroTradePage'

function BitsoTradeNote() {
  usePageTitle('Trade', 'Assisted trading exists for the eToro demo ledger only.')
  const ledger = useLedgerName()
  const etoro = useEtoroLedgerName()
  return (
    <>
      <div className="page-head">
        <div>
          <div className="row" style={{ gap: 12 }}>
            <h1>Trade</h1>
            {ledger && <LedgerBadge name={ledger} />}
          </div>
          <p>Assisted prepare → place exists for the eToro demo ledger only.</p>
        </div>
      </div>
      <div className="card" data-testid="trade-bitso-note">
        <Empty title="No assisted trading on a Bitso ledger">
          The Bitso stage executor places its own post-only legs at the Mexico City open and has no operator order path;
          the halt controls on the Risk page are the only way to act on it. Assisted trades (proposal from the SMA50
          rule or by hand, with a two-minute token and the confirmation phrase) exist for the eToro demo account.
          {etoro ? (
            <>
              {' '}
              <Link to={{ pathname: '/trade', search: `?ledger=${encodeURIComponent(etoro)}` }} id="trade-switch-etoro">
                Open the Trade page for {etoro} <IconArrowRight />
              </Link>
            </>
          ) : (
            <> No eToro ledger is configured on this ui-api (-etoro-ledger).</>
          )}
        </Empty>
      </div>
    </>
  )
}

export function TradePage() {
  return <VenueSwitch bitso={<BitsoTradeNote />} etoro={<EtoroTradePage />} />
}
