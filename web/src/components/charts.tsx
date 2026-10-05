/**
 * Chart wrappers around TradingView Lightweight Charts (Apache-2.0).
 * Colours come from the CSS tokens so charts match the theme.
 */
import {
  CandlestickSeries,
  ColorType,
  createChart,
  createSeriesMarkers,
  HistogramSeries,
  LineSeries,
  LineStyle,
  type IChartApi,
  type SeriesMarker,
  type Time,
} from 'lightweight-charts'
import { useEffect, useRef } from 'react'
import type { CandlePoint, EquityPoint, Fill } from '../api/schemas'

function token(name: string, fallback: string): string {
  if (typeof window === 'undefined') return fallback
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

function baseOptions() {
  return {
    autoSize: true,
    layout: {
      background: { type: ColorType.Solid, color: 'transparent' },
      textColor: token('--text-3', '#7a8394'),
      fontFamily: 'JetBrains Mono, ui-monospace, monospace',
      fontSize: 11,
      attributionLogo: false,
    },
    grid: {
      vertLines: { visible: false },
      horzLines: { color: 'hsla(222, 18%, 20%, 0.55)' },
    },
    rightPriceScale: { borderVisible: false },
    timeScale: { borderVisible: false, fixLeftEdge: true, fixRightEdge: true },
    crosshair: {
      vertLine: { color: 'hsla(216, 30%, 80%, 0.25)', labelBackgroundColor: token('--surface-3', '#252b38') },
      horzLine: { color: 'hsla(216, 30%, 80%, 0.25)', labelBackgroundColor: token('--surface-3', '#252b38') },
    },
  } as const
}

function useChart(build: (chart: IChartApi) => void, deps: unknown[]) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!ref.current) return
    const chart = createChart(ref.current, baseOptions())
    build(chart)
    chart.timeScale().fitContent()
    return () => chart.remove()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)
  return ref
}

const t = (d: string) => d as Time

/** Daily candles with SMA50, volume, and markers for signal flips and stage fills. */
export function PriceChart({
  candles,
  fills,
  label,
  quote,
}: {
  candles: CandlePoint[]
  fills: Fill[]
  label: string
  quote: string
}) {
  const ref = useChart(
    (chart) => {
      const long = token('--long', '#2ec4a7')
      const loss = token('--loss', '#f0715c')
      const whole = ['mxn', 'usd'].includes(quote.toLowerCase())
      const price = chart.addSeries(CandlestickSeries, {
        upColor: long,
        downColor: loss,
        borderVisible: false,
        wickUpColor: long,
        wickDownColor: loss,
        priceLineVisible: false,
        priceFormat: { type: 'price', precision: whole ? 0 : 2, minMove: whole ? 1 : 0.01 },
      })
      price.setData(candles.map((c) => ({ time: t(c.date), open: c.open, high: c.high, low: c.low, close: c.close })))
      const sma = chart.addSeries(LineSeries, {
        color: token('--warn', '#f5b53d'),
        lineWidth: 2,
        priceLineVisible: false,
        lastValueVisible: false,
        crosshairMarkerVisible: false,
      })
      sma.setData(candles.filter((c) => c.sma50 != null).map((c) => ({ time: t(c.date), value: c.sma50! })))

      const vol = chart.addSeries(HistogramSeries, {
        priceScaleId: 'vol',
        priceFormat: { type: 'volume' },
        lastValueVisible: false,
        priceLineVisible: false,
      })
      chart.priceScale('vol').applyOptions({ scaleMargins: { top: 0.82, bottom: 0 } })
      price.priceScale().applyOptions({ scaleMargins: { top: 0.06, bottom: 0.22 } })
      vol.setData(
        candles.map((c) => ({
          time: t(c.date),
          value: c.volume,
          color: (c.volume_ratio_20d ?? 0) >= 1.5 ? 'hsla(205, 80%, 64%, 0.55)' : 'hsla(220, 12%, 50%, 0.28)',
        })),
      )

      const markers: SeriesMarker<Time>[] = []
      for (let i = 1; i < candles.length; i++) {
        const a = candles[i - 1].long
        const b = candles[i].long
        if (a != null && b != null && a !== b) {
          markers.push({
            time: t(candles[i].date),
            position: b ? 'belowBar' : 'aboveBar',
            color: b ? long : token('--flat', '#8a96aa'),
            shape: b ? 'arrowUp' : 'arrowDown',
          })
        }
      }
      const first = candles[0]?.date ?? ''
      for (const f of fills) {
        if (f.fill_date < first) continue
        markers.push({
          time: t(f.fill_date),
          position: f.side === 'buy' ? 'belowBar' : 'aboveBar',
          color: token('--info', '#5bb4f0'),
          shape: 'circle',
          text: `stage ${f.side}`,
        })
      }
      markers.sort((x, y) => String(x.time).localeCompare(String(y.time)))
      createSeriesMarkers(price, markers)
    },
    [candles, fills, quote],
  )
  return <div ref={ref} className="chart" role="img" aria-label={label} />
}

/** Paper equity vs buy-and-hold since the forward start (both start at 1.0). */
export function EquityChart({ points, label }: { points: EquityPoint[]; label: string }) {
  const ref = useChart(
    (chart) => {
      chart.applyOptions({ timeScale: { fixLeftEdge: false, fixRightEdge: false } })
      const hold = chart.addSeries(LineSeries, {
        color: token('--bench', '#959aa6'),
        lineWidth: 2,
        lineStyle: LineStyle.Dashed,
        priceLineVisible: false,
        lastValueVisible: true,
        title: 'hold',
      })
      hold.setData(points.map((p) => ({ time: t(p.date), value: p.hold_equity })))
      const eq = chart.addSeries(LineSeries, {
        color: token('--long', '#2ec4a7'),
        lineWidth: 2,
        priceLineVisible: false,
        title: 'rule',
        pointMarkersVisible: points.length < 40,
      })
      eq.setData(points.map((p) => ({ time: t(p.date), value: p.equity })))
      eq.createPriceLine({
        price: 1,
        color: 'hsla(220, 12%, 50%, 0.5)',
        lineWidth: 1,
        lineStyle: LineStyle.Dotted,
        axisLabelVisible: false,
        title: 'start',
      })
    },
    [points],
  )
  return <div ref={ref} className="chart sm" role="img" aria-label={label} />
}
