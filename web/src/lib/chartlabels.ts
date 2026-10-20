/**
 * Marker labelling for the price chart (components/charts.tsx). Pure, so the
 * clustering rule is unit-tested.
 */

/**
 * Which fills get a text label: markers overlap when fills land within a few
 * bars of each other, so only the last fill of a cluster (no other fill in
 * the next `gap` bars) keeps its text; the others keep the dot. `dates` are
 * the chart's bar dates in order; a fill outside them is never labelled.
 */
export function labelledFills(fills: { fill_date: string }[], dates: string[], gap = 8): Set<number> {
  const idx = new Map(dates.map((d, i) => [d, i]))
  const pos = fills.map((f) => idx.get(f.fill_date) ?? -1)
  const keep = new Set<number>()
  fills.forEach((_, i) => {
    const p = pos[i]
    if (p < 0) return
    const crowded = pos.some((q, j) => j !== i && q > p && q - p <= gap)
    if (!crowded) keep.add(i)
  })
  return keep
}
