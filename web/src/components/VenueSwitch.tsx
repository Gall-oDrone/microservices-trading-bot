/**
 * Renders the Bitso or the eToro view of a page from the selected ledger's
 * venue (useVenue). While the ledgers list loads nothing venue-specific is
 * requested, so a Bitso route is never asked about the eToro ledger (ui-api
 * answers 400) or the other way round.
 */
import type { ReactNode } from 'react'
import { useVenue } from '../api/client'
import { CardSkeleton } from './ui'

export function VenueSwitch({ bitso, etoro, skeleton }: { bitso: ReactNode; etoro: ReactNode; skeleton?: ReactNode }) {
  const venue = useVenue()
  if (venue === undefined) {
    return (
      <>
        {skeleton ?? (
          <div className="grid grid-2" aria-busy="true" data-testid="venue-loading">
            <CardSkeleton lines={6} />
            <CardSkeleton lines={6} />
          </div>
        )}
      </>
    )
  }
  return <>{venue === 'etoro' ? etoro : bitso}</>
}
