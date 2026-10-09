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
  type IPriceLine,
  type ISeriesApi,
  type SeriesMarker,
  type Time,
} from 'lightweight-charts'
import { useEffect, useRef } from 'react'
import type { CandlePoint, EquityPoint, Fill, LiveCandle, PnLDay } from '../api/schemas'
import { useTheme } from '../lib/theme'

function token(name: string, fallback: string): string {
  if (typeof window === 'undefined') return fallback
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

/**
 * A token colour at the given opacity, as plain `rgba()`. Lightweight Charts
 * only parses rgb()/rgba() computed values, so color-mix() (which serializes
 * as `color(srgb …)`) cannot be used here.
 */
function alpha(name: string, fallback: string, a: number): string {
  const probe = document.createElement('span')
  probe.style.display = 'none'
  probe.style.color = token(name, fallback)
  document.body.appendChild(probe)
  const rgb = getComputedStyle(probe).color
  probe.remove()
  const m = rgb.match(/^rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)$/)
  if (!m) return fallback
  const base = m[4] ? parseFloat(m[4]) : 1
  return `rgba(${m[1]}, ${m[2]}, ${m[3]}, ${+(base * a).toFixed(3)})`
}

function baseOptions() {
  const cross = token('--chart-cross', 'hsla(216, 30%, 80%, 0.25)')
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
      horzLines: { color: token('--chart-grid', 'hsla(222, 18%, 20%, 0.55)') },
    },
    rightPriceScale: { borderVisible: false },
    timeScale: { borderVisible: false, fixLeftEdge: true, fixRightEdge: true },
    crosshair: {
      vertLine: { color: cross, labelBackgroundColor: token('--surface-3', '#252b38') },
      horzLine: { color: cross, labelBackgroundColor: token('--surface-3', '#252b38') },
    },
  } as const
}

/**
 * Builds the chart when `deps` or the theme change (colours are read from the
 * CSS tokens at build time); `build` may return a cleanup run before the chart
 * is removed.
 */
function useChart(build: (chart: IChartApi) => void | (() => void), deps: unknown[]) {
  const ref = useRef<HTMLDivElement>(null)
  const [theme] = useTheme()
  useEffect(() => {
    if (!ref.current) return
    const chart = createChart(ref.current, baseOptions())
    const done = build(chart)
    chart.timeScale().fitContent()
    return () => {
      done?.()
      chart.remove()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, theme])
  return ref
}

const t = (d: string) => d as Time

/** Live (display-only) overlay for PriceChart: today's forming bar and the provisional flip level. */
export interface LiveOverlay {
  candle: LiveCandle | null
  flip: number | null
  fresh: boolean
}

/** Daily candles with SMA50, volume, and markers for signal flips and stage fills. */
export function PriceChart({
  candles,
  fills,
  label,
  quote,
  live,
}: {
  candles: CandlePoint[]
  fills: Fill[]
  label: string
  quote: string
  live?: LiveOverlay
}) {
  const priceRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const volRef = useRef<ISeriesApi<'Histogram'> | null>(null)
  const flipRef = useRef<IPriceLine | null>(null)
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
      const volHi = token('--chart-vol-hi', 'hsla(205, 80%, 64%, 0.55)')
      const volLo = token('--chart-vol', 'hsla(220, 12%, 50%, 0.28)')
      vol.setData(
        candles.map((c) => ({
          time: t(c.date),
          value: c.volume,
          color: (c.volume_ratio_20d ?? 0) >= 1.5 ? volHi : volLo,
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
      priceRef.current = price
      volRef.current = vol
      return () => {
        priceRef.current = null
        volRef.current = null
        flipRef.current = null
      }
    },
    [candles, fills, quote],
  )
  const [theme] = useTheme()

  // Live overlay, applied without rebuilding the chart. It must re-run after
  // every rebuild (same deps as the build, plus theme) because the rebuilt
  // series start without the forming bar and flip line; effects run in
  // declaration order, so this sees the new series.
  const c = live?.candle ?? null
  const flip = live?.flip ?? null
  const fresh = live?.fresh ?? false
  useEffect(() => {
    const price = priceRef.current
    if (!price) return
    const lastClosed = candles[candles.length - 1]?.date ?? ''
    if (c && c.date > lastClosed && c.open > 0) {
      // Translucent: a forming bar, not a closed one.
      const up = c.close >= c.open
      const a = fresh ? 0.5 : 0.25
      const color = up ? alpha('--long', '#2ec4a7', a) : alpha('--loss', '#f0715c', a)
      price.update({
        time: t(c.date),
        open: c.open,
        high: c.high,
        low: c.low,
        close: c.close,
        color,
        wickColor: color,
      })
      volRef.current?.update({ time: t(c.date), value: c.volume, color: alpha('--info', '#5bb4f0', 0.25) })
    }
    if (flip != null && flip > 0) {
      const opts = {
        price: flip,
        color: fresh ? token('--info', '#5bb4f0') : token('--text-3', '#7a8394'),
        lineWidth: 1 as const,
        lineStyle: LineStyle.Dashed,
        axisLabelVisible: true,
        title: 'flip (provisional)',
      }
      if (flipRef.current) flipRef.current.applyOptions(opts)
      else flipRef.current = price.createPriceLine(opts)
    } else if (flipRef.current) {
      price.removePriceLine(flipRef.current)
      flipRef.current = null
    }
  }, [candles, fills, quote, c, flip, fresh, theme])
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
        color: alpha('--text-3', '#7a8394', 0.6),
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

/** Plan §6.4.12: the stage position's P&L by day (bars) and cumulative (line), and the paper account's P&L on the same money. */
export function PnLHistoryChart({ days, label }: { days: PnLDay[]; label: string }) {
  const ref = useChart(
    (chart) => {
      chart.applyOptions({ timeScale: { fixLeftEdge: false, fixRightEdge: false } })
      const up = alpha('--long', '#2ec4a7', 0.55)
      const down = alpha('--loss', '#e8735a', 0.55)
      const daily = chart.addSeries(HistogramSeries, {
        priceLineVisible: false,
        lastValueVisible: false,
        title: 'daily',
      })
      daily.setData(days.map((d) => ({ time: t(d.date), value: d.daily, color: d.daily >= 0 ? up : down })))
      const paper = chart.addSeries(LineSeries, {
        color: token('--bench', '#959aa6'),
        lineWidth: 2,
        lineStyle: LineStyle.Dashed,
        priceLineVisible: false,
        title: 'paper',
      })
      paper.setData(days.map((d) => ({ time: t(d.date), value: d.paper_pnl })))
      const total = chart.addSeries(LineSeries, {
        color: token('--info', '#5fb3f0'),
        lineWidth: 2,
        priceLineVisible: false,
        title: 'stage',
        pointMarkersVisible: days.length < 40,
      })
      total.setData(days.map((d) => ({ time: t(d.date), value: d.total })))
      total.createPriceLine({
        price: 0,
        color: alpha('--text-3', '#7a8394', 0.6),
        lineWidth: 1,
        lineStyle: LineStyle.Dotted,
        axisLabelVisible: false,
        title: '',
      })
    },
    [days],
  )
  return <div ref={ref} className="chart sm" role="img" aria-label={label} data-testid="pnl-history-chart" />
}
