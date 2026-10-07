/**
 * The landing page's trend chart: a year of daily closes against the SMA50,
 * with the days the frozen rule was long shaded. Plain SVG (no chart library)
 * so it draws instantly and animates in; hover, touch or arrow keys read a day.
 */
import { useId, useMemo, useState, type KeyboardEvent, type PointerEvent } from 'react'
import type { CandlePoint } from '../api/schemas'
import { fmtDate, fmtPrice } from '../lib/format'
import { nearestIndex, TREND_H, TREND_W, trendGeometry } from '../lib/trend'

export function TrendChart({ candles, quote, label }: { candles: CandlePoint[]; quote: string; label: string }) {
  const g = useMemo(() => trendGeometry(candles), [candles])
  const [hover, setHover] = useState<number | null>(null)
  const gid = useId().replace(/:/g, '')

  if (!g) {
    return <div className="trend-empty faint">Not enough daily candles to draw yet.</div>
  }

  const n = g.points.length
  const last = g.points[n - 1]
  const p = hover != null ? g.points[hover] : null
  const pct = (x: number) => `${(x / TREND_W) * 100}%`
  const top = (y: number) => `${(y / TREND_H) * 100}%`

  const onMove = (e: PointerEvent<HTMLDivElement>) => {
    const r = e.currentTarget.getBoundingClientRect()
    if (r.width > 0) setHover(nearestIndex((e.clientX - r.left) / r.width, n))
  }
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = e.shiftKey ? 10 : 1
    if (e.key === 'ArrowLeft') setHover((h) => Math.max(0, (h ?? n) - step))
    else if (e.key === 'ArrowRight') setHover((h) => Math.min(n - 1, (h ?? n - 1) + step))
    else if (e.key === 'Escape') setHover(null)
    else return
    e.preventDefault()
  }

  const right = p != null && p.x > TREND_W / 2
  return (
    <figure className="trend" data-testid="trend-chart">
      <div
        className="trend-plot"
        tabIndex={0}
        role="img"
        aria-label={`${label}: ${n} daily closes from ${g.points[0].date} to ${last.date}, with the 50-day average. Shaded days: the rule was long. Arrow keys read a day.`}
        onPointerMove={onMove}
        onPointerDown={onMove}
        onPointerLeave={() => setHover(null)}
        onKeyDown={onKey}
        onBlur={() => setHover(null)}
      >
        <svg viewBox={`0 0 ${TREND_W} ${TREND_H}`} preserveAspectRatio="none" aria-hidden="true">
          <defs>
            <linearGradient id={`${gid}-area`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" className="trend-area-top" />
              <stop offset="100%" className="trend-area-bottom" />
            </linearGradient>
          </defs>
          <g className="trend-bands">
            {g.longBands.map((b) => (
              <rect key={b.x} x={b.x} y={0} width={b.w} height={TREND_H} />
            ))}
          </g>
          <path className="trend-area" d={g.area} fill={`url(#${gid}-area)`} />
          <path className="trend-sma" d={g.sma} vectorEffect="non-scaling-stroke" />
          <path className="trend-price" d={g.price} vectorEffect="non-scaling-stroke" />
        </svg>

        <span className="trend-dot" style={{ left: pct(last.x), top: top(last.y) }} aria-hidden="true" />

        {p && (
          <>
            <span className="trend-cross" style={{ left: pct(p.x) }} aria-hidden="true" />
            <span className="trend-hover-dot" style={{ left: pct(p.x), top: top(p.y) }} aria-hidden="true" />
            <div
              className={`trend-tip ${right ? 'left' : ''}`}
              style={
                right ? { right: `calc(${100 - (p.x / TREND_W) * 100}% + 12px)` } : { left: `calc(${pct(p.x)} + 12px)` }
              }
              data-testid="trend-tip"
              aria-live="polite"
            >
              <div className="trend-tip-date">{fmtDate(p.date)}</div>
              <div className="trend-tip-row">
                <span>Close</span>
                <span className="num">{fmtPrice(p.close, quote)}</span>
              </div>
              <div className="trend-tip-row">
                <span>SMA50</span>
                <span className="num">{p.sma50 != null ? fmtPrice(p.sma50, quote) : '—'}</span>
              </div>
              <div className="trend-tip-row">
                <span>Rule</span>
                <span className={p.long ? 'pos' : 'muted'}>
                  {p.long == null ? 'warming up' : p.long ? 'long' : 'flat'}
                </span>
              </div>
            </div>
          </>
        )}
      </div>
      <figcaption className="legend trend-legend">
        <span className="key" style={{ color: 'var(--text)' }}>
          <span className="swatch" /> Daily close ({quote.toUpperCase()})
        </span>
        <span className="key" style={{ color: 'var(--warn)' }}>
          <span className="swatch dashed" /> SMA50
        </span>
        <span className="key" style={{ color: 'var(--long)' }}>
          <span className="trend-band-key" /> Rule long
        </span>
        <span className="spacer" />
        <span className="faint num">
          {fmtPrice(g.min, quote)} – {fmtPrice(g.max, quote)}
        </span>
      </figcaption>
    </figure>
  )
}
