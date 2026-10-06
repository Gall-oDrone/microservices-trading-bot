/**
 * Live (display-only) market data: connection badge and the provisional
 * "if today closed now" strip. Nothing here is a decision; the executor uses
 * closed production candles.
 */
import { useState } from 'react'
import type { LivePhase, LiveState } from '../api/live'
import type { BookSnapshot, ForwardTest } from '../api/schemas'
import { ago, fmtMoney, fmtPct, fmtPrice, fmtUTC } from '../lib/format'
import { Badge, SignalPill, Stat } from './ui'

const PHASE_LABEL: Record<LivePhase, string> = {
  live: 'Live',
  stale: 'Stale',
  reconnecting: 'Reconnecting',
  connecting: 'Connecting',
  off: 'Live off',
  error: 'Live error',
}

function badgeTitle(live: LiveState): string {
  const u = live.upstream
  const lines = [
    'Display only: live prices never feed the executor.',
    u ? `Source ${u.source} · ${u.connected ? 'connected' : 'disconnected'} since ${fmtUTC(u.since)}` : '',
    u && u.reconnects > 0 ? `${u.reconnects} reconnects` : '',
    u?.last_error ? `Last error: ${u.last_error}` : '',
    live.error ?? '',
  ]
  return lines.filter(Boolean).join('\n')
}

/** Connection state of the live stream, with the age of the last event when stale. */
export function LiveBadge({ live }: { live: LiveState }) {
  const age = live.lastEventAt != null ? Math.round((live.now - live.lastEventAt) / 1000) : null
  return (
    <span className={`live-badge ${live.phase}`} data-testid="live-badge" title={badgeTitle(live)}>
      <span className="pulse" aria-hidden="true" />
      {PHASE_LABEL[live.phase]}
      {live.phase === 'stale' && age != null && <span className="num"> · {age}s</span>}
    </span>
  )
}

/**
 * Up/down since the previous value, kept in state (React's "store the
 * previous prop" pattern) so the flash restarts on every tick.
 */
function useTick(v: number | undefined): 'up' | 'down' | '' {
  const [prev, setPrev] = useState(v)
  const [dir, setDir] = useState<'up' | 'down' | ''>('')
  if (v !== prev) {
    setPrev(v)
    if (v != null && prev != null && v !== prev) setDir(v > prev ? 'up' : 'down')
  }
  return dir
}

function TickPrice({ value, quote }: { value: number; quote: string }) {
  const dir = useTick(value)
  return (
    <span key={value} className={`tick ${dir}`} data-testid="live-price">
      {fmtPrice(value, quote)}
    </span>
  )
}

/**
 * The live strip on a forward-test card (and, `large`, on the detail page):
 * last trade, the provisional flip level, and what the frozen rule would say
 * if today closed now. Greyed out when the stream is stale.
 */
export function LiveStrip({
  ft,
  snap,
  live,
  large,
}: {
  ft: ForwardTest
  snap: BookSnapshot | undefined
  live: LiveState
  large?: boolean
}) {
  const p = snap?.provisional ?? null
  const fresh = live.phase === 'live'
  const recorded = ft.recorded_at ? ft.decision.signal : ''
  const wouldFlip = p != null && recorded !== '' && p.signal !== recorded
  const stage = ft.stage_position
  const hasPrice = snap != null && snap.last > 0

  return (
    <section
      className={`live-strip ${fresh ? '' : 'is-stale'} ${large ? 'lg' : ''}`}
      aria-label={`Live ${ft.book}`}
      data-testid={`live-${ft.book}`}
    >
      <div className="live-head">
        <LiveBadge live={live} />
        <span className="live-label">{p?.label ?? 'provisional: if today closed now'}</span>
        <span className="spacer" />
        {hasPrice && snap.last_at && (
          <span className="faint num" title={fmtUTC(snap.last_at)}>
            last trade {ago(snap.last_at, live.now)}
          </span>
        )}
      </div>

      {!hasPrice ? (
        <div className="faint live-empty">
          {live.phase === 'connecting' ? 'Connecting to live prices…' : (live.error ?? 'No live trades yet.')}
        </div>
      ) : (
        <div className={`grid ${large ? 'grid-4' : 'grid-4 live-grid'}`}>
          <Stat
            label="Last trade"
            value={<TickPrice value={snap.last} quote={ft.quote} />}
            hint={
              snap.bid > 0 && snap.ask > 0 ? (
                <span className="num">
                  {fmtPrice(snap.bid, ft.quote)} / {fmtPrice(snap.ask, ft.quote)}
                </span>
              ) : undefined
            }
            large={large}
          />
          <Stat
            label="Flip level"
            value={p ? fmtPrice(p.flip_level, ft.quote) : '—'}
            hint={p ? <span className="num">SMA50 now {fmtPrice(p.sma50, ft.quote)}</span> : snap.provisional_note}
            title="Mean of the last 49 closed closes: today's close above it means long"
            large={large}
          />
          <Stat
            label="Distance to flip"
            value={p ? fmtPct(p.distance_to_flip_pct, 1) : '—'}
            tone={p ? (p.distance_to_flip_pct > 0 ? 'pos' : 'neg') : undefined}
            hint={p ? `based on bars to ${p.based_on}` : undefined}
            large={large}
          />
          <Stat
            label="If today closed now"
            value={
              p ? (
                <span className="row" style={{ gap: 6 }}>
                  <SignalPill signal={p.signal} />
                  {wouldFlip && (
                    <Badge tone="warn" title="Provisional: the recorded signal would change if today closed now">
                      would flip
                    </Badge>
                  )}
                </span>
              ) : (
                '—'
              )
            }
            hint={
              stage && stage.btc > 0 ? (
                <span className="num">stage {fmtMoney(stage.btc * snap.last, ft.quote, 0)}</span>
              ) : (
                `recorded: ${recorded || 'none'}`
              )
            }
            large={large}
          />
        </div>
      )}
    </section>
  )
}
