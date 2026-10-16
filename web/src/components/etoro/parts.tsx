/**
 * Building blocks of the eToro forward-test views: the NYSE session clock,
 * the run badge with its next 09:35 New York start, the demo position and
 * account strip, and the last demo order on one line. Money is USD, sizes
 * are instrument units, times are New York.
 */
import type { ReactNode } from 'react'
import type { EtoroAccount, EtoroOrderView, EtoroPosition, EtoroRunStatus, EtoroSession } from '../../api/etoroSchemas'
import { fmtBps, fmtDate, fmtMoney, fmtNY, fmtNYTime, fmtPrice, fmtUnits, fmtUTC } from '../../lib/format'
import { Badge, Stat } from '../ui'

/** NYSE open / closed, from api.EtoroSession; the executor only acts inside the cash session. */
export function SessionClock({ s }: { s: EtoroSession }) {
  let text: ReactNode
  if (s.open) {
    text = (
      <>
        NYSE <b>open</b>
        {s.closes_at && (
          <>
            {' '}
            · {s.early_close ? 'early close' : 'closes'} {fmtNYTime(s.closes_at)} NY
          </>
        )}
      </>
    )
  } else if (!s.trading_day) {
    text = (
      <>
        NYSE <b>closed</b> · {s.holiday || 'weekend'}
      </>
    )
  } else if (s.opens_at && s.now_new_york < s.opens_at) {
    text = (
      <>
        NYSE <b>pre-open</b> · opens {fmtNYTime(s.opens_at)} NY
      </>
    )
  } else {
    text = (
      <>
        NYSE <b>closed</b>
        {s.closes_at && <> · since {fmtNYTime(s.closes_at)} NY</>}
      </>
    )
  }
  return (
    <span
      className={`session-clock ${s.open ? 'open' : 'closed'}`}
      data-testid="session-clock"
      data-open={s.open}
      title={`New York ${fmtNY(s.now_new_york)} · ${fmtDate(s.date)}`}
    >
      <span className="live-dot" aria-hidden="true" />
      <span>{text}</span>
    </span>
  )
}

/** "up to date" / "run pending" / "run missed" with the next 09:35 New York start. */
export function RunBadge({ run, withNext = true }: { run: EtoroRunStatus; withNext?: boolean }) {
  const s = run.status
  const tone = s === 'ok' ? 'ok' : s === 'pending' ? 'info' : 'warn'
  const text = s === 'ok' ? 'up to date' : s === 'pending' ? 'run pending' : s === 'missed' ? 'run missed' : 'no data'
  return (
    <span className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
      <Badge tone={tone} dot title={run.message}>
        {text}
      </Badge>
      {withNext && <NextRun run={run} />}
    </span>
  )
}

export function NextRun({ run }: { run: EtoroRunStatus }) {
  return (
    <span className="faint num" data-testid="next-run" title={fmtUTC(run.next_run_at)}>
      next run {fmtNY(run.next_run_at)} NY
    </span>
  )
}

/** The paper costs the pre-registration froze, or the provisional ones until it did. */
export function CostsBadge({ status }: { status: string }) {
  const provisional = /provisional/i.test(status)
  return (
    <Badge tone={provisional ? 'warn' : 'ok'} title={status}>
      costs {provisional ? 'provisional' : 'frozen'}
    </Badge>
  )
}

/** Demo position, account and exposure as the newest run saw them (before its order). */
export function DemoStrip({
  position,
  account,
  book,
  compact = false,
}: {
  position: EtoroPosition | null
  account: EtoroAccount | null
  book: string
  compact?: boolean
}) {
  const long = position?.state === 'long' && !!position.units
  const quote = account?.currency ?? 'USD'
  const exposurePct = account && account.equity > 0 ? (account.exposure / account.equity) * 100 : null
  return (
    <div className={`demo-strip ${long ? 'long' : ''}`} data-testid={`demo-strip-${book}`}>
      <Stat
        label="Demo position"
        value={
          long ? (
            <span className="num" data-testid={`demo-position-${book}`}>
              {fmtUnits(position.units!)}
            </span>
          ) : (
            <span data-testid={`demo-position-${book}`}>flat</span>
          )
        }
        tone={long ? 'pos' : undefined}
        hint={
          long ? (
            <>
              open {fmtPrice(position.open_rate ?? 0, quote, book)} ·{' '}
              {position.opened_at ? <span title={fmtUTC(position.opened_at)}>{fmtNY(position.opened_at)} NY</span> : ''}
              {position.amount ? ` · ${fmtMoney(position.amount, quote, 0)}` : ''}
            </>
          ) : (
            'no open CFD on the demo account'
          )
        }
      />
      <Stat
        label="Demo account"
        value={account ? <span className="num">{fmtMoney(account.equity, quote, 2)}</span> : '—'}
        hint={account ? `equity · cash ${fmtMoney(account.cash, quote, 2)}` : 'no demo record yet'}
      />
      <Stat
        label="Exposure"
        value={account ? <span className="num">{fmtMoney(account.exposure, quote, 2)}</span> : '—'}
        hint={
          exposurePct != null
            ? `${exposurePct.toFixed(2)}% of equity, all open positions`
            : compact
              ? ''
              : 'all open positions, account currency'
        }
      />
    </div>
  )
}

/** The newest demo order: kind, status, size and price, fill quality vs mid and vs the decision close. */
export function OrderLine({ o, book, quote }: { o: EtoroOrderView; book: string; quote: string }) {
  const ok = o.status === 'executed'
  return (
    <div className="order-line" data-testid={`last-order-${book}`}>
      <span className="muted">Last order</span>
      <Badge tone={o.kind === 'open' ? 'long' : 'flat'}>{o.kind}</Badge>
      <Badge tone={ok ? 'ok' : o.error ? 'block' : 'warn'} dot>
        {o.status}
      </Badge>
      {o.units != null && o.avg_price != null && (
        <span className="num">
          {fmtUnits(o.units)} @ {fmtPrice(o.avg_price, quote, book)}
        </span>
      )}
      {o.fill_vs_mid_bps != null && (
        <span className="faint num" title="fill vs the quote mid the executor saw; positive is worse than mid">
          vs mid {fmtBps(o.fill_vs_mid_bps, true)}
        </span>
      )}
      {o.fill_vs_close_bps != null && (
        <span className="faint num" title="fill vs the decision close (the paper account's reference)">
          vs close {fmtBps(o.fill_vs_close_bps, true)}
        </span>
      )}
      <span className="faint" title={fmtUTC(o.finished)}>
        {fmtNY(o.finished)} NY
      </span>
      {o.order_id && <span className="faint num">#{o.order_id}</span>}
      {o.error && <span className="neg">{o.error}</span>}
    </div>
  )
}
