import type { EtoroTrade } from '../api/etoroSchemas'
import type { BookSnapshot } from '../api/schemas'

/**
 * The live mark of an open long (2026-10-26): what the position is worth at
 * the current bid, gross of fees, from the 2 s quote stream. eToro's own
 * figure (`unrealized_pnl`, net of fees) comes from the portfolio read that
 * ui-api caches 45 s and the card refetches every 60 s; this is the number
 * in between. Null when the row is not an open long the account holds, or
 * the stream has no quote for its book.
 */
export function liveMark(
  t: Pick<EtoroTrade, 'status' | 'units' | 'open_rate' | 'account_holds'>,
  snap: BookSnapshot | undefined,
) {
  if (t.status !== 'open' || t.account_holds === false || !snap || !(snap.bid > 0) || !(t.units > 0)) return null
  return { bid: snap.bid, pnl: t.units * (snap.bid - t.open_rate), at: snap.updated_at }
}
