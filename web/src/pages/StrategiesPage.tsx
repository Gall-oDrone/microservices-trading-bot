/**
 * Strategies: every daily-executor ledger with its halt state, the halt-all
 * kill switch, and the intraday strategy-executor's strategies with operator
 * start/stop.
 *
 * A stop here is a *hold*: strategy-executor persists it (STRATEGY_HOLD_FILE),
 * so neither strategy-router nor scripts/start-organic-trading.sh can start
 * the strategy again, even after a restart, until an operator starts it on
 * this page. ui-api checks the token, the confirmation and the state, and
 * audits every attempt.
 */
import { useRef, useState } from 'react'
import { Link } from 'react-router'
import { useStrategies } from '../api/client'
import { useHaltAllMutation, useStrategyControlMutation, type StrategyAction } from '../api/controls'
import type {
  HaltAllResponse,
  LedgerControl,
  StrategiesInfo,
  StrategyControlResponse,
  StrategyView,
} from '../api/schemas'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { AuditLog } from '../components/controls'
import { IconArrowRight, IconBlock, IconCheck, IconGrid, IconShield } from '../components/icons'
import { Badge, Banner, CardSkeleton, ErrorState, LedgerBadge, Stat } from '../components/ui'
import { bookLabel, fmtFrac, fmtMoney, fmtMx, fmtUTC } from '../lib/format'
import { usePageTitle } from '../lib/usePageTitle'

function quoteOf(book: string): string {
  return book.split('_')[1] ?? ''
}

function busy(s: StrategyView): boolean {
  return s.has_position || s.pending_buy || s.pending_sell
}

function positionText(s: StrategyView): string {
  const parts: string[] = []
  if (s.has_position) parts.push(`${s.position_side || 'open'} ${s.position_size}`)
  if (s.pending_buy) parts.push('pending buy')
  if (s.pending_sell) parts.push('pending sell')
  return parts.join(' · ')
}

type LedgerState = 'remote' | 'invalid' | 'halted' | 'running'

function ledgerState(l: LedgerControl): LedgerState {
  if (l.remote) return 'remote'
  if (l.halt_file.error) return 'invalid'
  return l.halt_file.halted ? 'halted' : 'running'
}

function LedgerCard({ l }: { l: LedgerControl }) {
  const st = ledgerState(l)
  const hf = l.halt_file
  return (
    <article className={`card ledger-card is-${st}`} data-testid={`ledger-card-${l.name}`} data-state={st}>
      <div className="ledger-card-head">
        <span className={`controls-orb ${st === 'running' ? '' : st}`} aria-hidden />
        <LedgerBadge name={l.name} />
        <span className="spacer" />
        <Badge tone={st === 'running' ? 'ok' : st === 'remote' ? 'flat' : st === 'invalid' ? 'warn' : 'block'} dot>
          {st === 'running'
            ? 'trading allowed'
            : st === 'remote'
              ? 'S3 copy'
              : st === 'invalid'
                ? 'invalid file'
                : 'halted'}
        </Badge>
      </div>
      <div className="ledger-card-body">
        {st === 'remote' && <span className="faint">Read from S3: halt it where the executor runs.</span>}
        {st === 'invalid' && <span className="warn-text">{hf.error}: the executor fails closed.</span>}
        {st === 'halted' && (
          <>
            <span>{hf.reason}</span>
            <span className="faint">
              by <b>{hf.by}</b> · <span title={fmtUTC(hf.at)}>{fmtMx(hf.at)}</span>
            </span>
          </>
        )}
        {st === 'running' && <span className="faint">No operator halt. The policy limits still apply.</span>}
      </div>
      <Link
        to={{ pathname: '/risk', search: `?ledger=${encodeURIComponent(l.name)}` }}
        className="ledger-card-link"
        id={`ledger-risk-${l.name}`}
      >
        {st === 'halted' ? 'Resume on the Risk page' : 'Open the Risk page'} <IconArrowRight />
      </Link>
    </article>
  )
}

function KillSwitch({ info, onResult }: { info: StrategiesInfo; onResult: (r: HaltAllResponse) => void }) {
  const ks = info.kill_switch
  const m = useHaltAllMutation()
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const toHalt = ks.targets.filter((t) => !ks.already_halted.includes(t))
  const allHalted = ks.targets.length > 0 && toHalt.length === 0
  const close = () => {
    setOpen(false)
    m.reset()
    requestAnimationFrame(() => trigger.current?.focus())
  }
  return (
    <section
      className={`card killswitch ${allHalted ? 'is-halted' : ''}`}
      aria-labelledby="killswitch-h"
      id="kill-switch"
      data-testid="kill-switch"
    >
      <div className="killswitch-icon" aria-hidden>
        <IconBlock />
      </div>
      <div className="killswitch-text">
        <h2 id="killswitch-h">Kill switch</h2>
        <p>
          Halt every local ledger at once: ui-api writes <span className="num">risk-state.json</span> next to{' '}
          {ks.targets.length === 0 ? (
            'each local ledger'
          ) : (
            <>
              {ks.targets.map((t, i) => (
                <span key={t}>
                  {i > 0 && (i === ks.targets.length - 1 ? ' and ' : ', ')}
                  <b className="num">{t}</b>
                </span>
              ))}
            </>
          )}
          . Ledgers already halted stay as they are. Resume them one by one on the Risk page.
        </p>
        {!ks.enabled && (
          <div className="panel controls-off" data-testid="killswitch-disabled">
            <b>Off:</b> {ks.disabled_reason}
          </div>
        )}
      </div>
      <div className="killswitch-side">
        <div className="killswitch-count num" data-testid="killswitch-count">
          {allHalted ? 'all halted' : `${toHalt.length} of ${ks.targets.length} to halt`}
        </div>
        <button
          ref={trigger}
          type="button"
          className="btn btn-danger killswitch-btn"
          id="kill-switch-open"
          data-testid="kill-switch-open"
          disabled={!ks.enabled || allHalted}
          onClick={() => setOpen(true)}
        >
          <IconBlock /> Halt all ledgers
        </button>
      </div>
      {open && (
        <ConfirmDialog<HaltAllResponse>
          tone="danger"
          icon={<IconBlock />}
          title="Halt all ledgers"
          sub={
            <>
              Writes an operator halt to the halt file of{' '}
              {toHalt.map((t, i) => (
                <span key={t}>
                  {i > 0 && ', '}
                  <b className="num">{t}</b>
                </span>
              ))}
              . The daily-executor then blocks and records every new stage order on each until it is resumed.
            </>
          }
          current={
            ks.already_halted.length > 0 ? (
              <>
                Already halted, left as is: <b className="num">{ks.already_halted.join(', ')}</b>
              </>
            ) : undefined
          }
          confirmLabel="Phrase"
          confirmWord={ks.confirm}
          reasonPlaceholder="e.g. Exchange incident, stopping everything"
          reasonHint="one line, kept in every halt file and audit log"
          submitLabel={`Halt ${toHalt.length} ledger${toHalt.length === 1 ? '' : 's'}`}
          pendingLabel="Halting…"
          pending={m.isPending}
          error={m.error}
          run={(v, cb) => m.mutate({ reason: v.reason, by: v.by, confirm: v.confirm, token: v.token }, cb)}
          onClose={close}
          onDone={(r) => {
            onResult(r)
            close()
          }}
        />
      )}
    </section>
  )
}

interface Pending {
  s: StrategyView
  action: StrategyAction
}

function StrategyDialog({
  p,
  executorURL,
  onClose,
  onDone,
}: {
  p: Pending
  executorURL: string
  onClose: () => void
  onDone: (r: StrategyControlResponse) => void
}) {
  const m = useStrategyControlMutation()
  const { s, action } = p
  const stop = action === 'stop'
  const isBusy = stop && busy(s)
  return (
    <ConfirmDialog<StrategyControlResponse>
      tone={stop ? 'danger' : 'resume'}
      icon={stop ? <IconBlock /> : <IconCheck />}
      title={stop ? 'Stop and hold strategy' : 'Start strategy'}
      sub={
        stop ? (
          <>
            Stops <b>{s.name}</b> on <span className="num">{executorURL}</span> and holds it: strategy-router and the
            organic-trading script cannot start it again, even after a restart, until an operator starts it here.
          </>
        ) : (
          <>
            {s.hold ? 'Releases the operator hold and starts' : 'Starts'} <b>{s.name}</b> on{' '}
            <span className="num">{executorURL}</span>.{' '}
            {s.dry_run ? 'It is in dry-run mode: it logs signals only.' : 'It emits signals for the trading engine.'}
          </>
        )
      }
      current={
        isBusy ? (
          <span className="warn-text">
            Open exposure: <b>{positionText(s)}</b>. A stopped strategy places no exit orders.
          </span>
        ) : !stop && s.hold ? (
          <>
            Held by <b>{s.hold.by}</b> · <span title={fmtUTC(s.hold.at)}>{fmtMx(s.hold.at)}</span> · {s.hold.reason}
          </>
        ) : undefined
      }
      confirmLabel="Strategy name"
      confirmWord={s.name}
      reasonPlaceholder={stop ? 'e.g. Fee drift above 2x, pausing to investigate' : 'e.g. Fees back to normal'}
      submitLabel={stop ? `Stop ${s.name}` : `Start ${s.name}`}
      pendingLabel={stop ? 'Stopping…' : 'Starting…'}
      ack={
        isBusy
          ? {
              label: (
                <>
                  I understand <b>{s.name}</b> keeps its open position or order unmanaged after the stop.
                </>
              ),
              message: 'Tick this to stop a strategy with open exposure.',
            }
          : undefined
      }
      pending={m.isPending}
      error={m.error}
      run={(v, cb) =>
        m.mutate(
          {
            name: s.name,
            action,
            reason: v.reason,
            by: v.by,
            confirm: v.confirm,
            token: v.token,
            ackPosition: isBusy && v.ack,
          },
          cb,
        )
      }
      onClose={onClose}
      onDone={onDone}
    />
  )
}

function StrategyStatus({ s }: { s: StrategyView }) {
  if (s.running) {
    return (
      <Badge tone="ok" dot>
        running
      </Badge>
    )
  }
  if (s.hold) {
    return (
      <div className="strategy-held">
        <Badge tone="block" dot>
          held
        </Badge>
        <span className="faint" title={`${s.hold.by} · ${fmtUTC(s.hold.at)}`}>
          {s.hold.reason || 'no reason'} · {s.hold.by}
        </span>
      </div>
    )
  }
  return <Badge tone="flat">stopped</Badge>
}

function StrategiesCard({ info }: { info: StrategiesInfo }) {
  const ex = info.executor
  const [pending, setPending] = useState<Pending | null>(null)
  const [flash, setFlash] = useState<StrategyControlResponse | null>(null)
  const triggers = useRef(new Map<string, HTMLButtonElement>())
  const running = info.strategies.filter((s) => s.running).length
  const held = info.strategies.filter((s) => s.hold).length

  const close = (name: string) => {
    setPending(null)
    requestAnimationFrame(() => triggers.current.get(name)?.focus())
  }

  return (
    <section
      className="card strategies-card"
      aria-labelledby="strategies-h"
      id="intraday"
      data-testid="strategies-card"
    >
      <div className="card-head">
        <div>
          <h2 id="strategies-h">
            <IconGrid className="controls-h-icon" /> Intraday strategies
          </h2>
          <div className="sub">
            {ex.configured ? (
              <>
                strategy-executor at <span className="num">{ex.url}</span>
                {ex.fetched_at && (
                  <>
                    {' '}
                    · read <span title={fmtUTC(ex.fetched_at)}>{fmtMx(ex.fetched_at)}</span>
                  </>
                )}
              </>
            ) : (
              'No strategy-executor connected'
            )}
          </div>
        </div>
        <Badge tone={!ex.configured ? 'flat' : !ex.reachable ? 'block' : ex.controls_enabled ? 'ok' : 'warn'} dot>
          {!ex.configured
            ? 'not connected'
            : !ex.reachable
              ? 'unreachable'
              : ex.controls_enabled
                ? 'armed · local token'
                : 'read-only'}
        </Badge>
      </div>

      {!ex.configured && (
        <div className="panel controls-off" data-testid="executor-off">
          Start ui-api with <code>-strategy-executor-url http://127.0.0.1:&lt;port&gt;</code> (loopback only) to list
          the intraday strategies here. Start and stop also need <code>-operator-token-file</code>.
        </div>
      )}
      {ex.configured && !ex.reachable && (
        <Banner tone="block" title="strategy-executor did not answer" id="executor-error">
          {ex.error}
        </Banner>
      )}
      {ex.reachable && !ex.controls_enabled && ex.disabled_reason && (
        <div className="panel controls-off" data-testid="executor-readonly">
          <b>Read-only:</b> {ex.disabled_reason}
        </div>
      )}
      {ex.reachable && !ex.holds_supported && (
        <Banner tone="warn" title="This strategy-executor has no hold list" id="executor-no-holds">
          It predates STRATEGY_HOLD_FILE: a stop is not persisted and a restart or the router can start the strategy
          again. Upgrade it before relying on stops.
        </Banner>
      )}

      {ex.reachable && (
        <>
          <div className="strategy-stats">
            <Stat label="Registered" value={info.strategies.length} />
            <Stat label="Running" value={running} tone={running > 0 ? 'pos' : undefined} />
            <Stat
              label="Held"
              value={held + info.holds.length}
              tone={held + info.holds.length > 0 ? 'neg' : undefined}
            />
            <Stat
              label="Open exposure"
              value={info.strategies.filter(busy).length}
              tone={info.strategies.some(busy) ? 'warn' : undefined}
              hint="position or pending order"
            />
          </div>

          {flash && (
            <div
              className={`controls-flash ${flash.action === 'stop' ? 'halt' : 'resume'}`}
              role="status"
              data-testid="strategy-flash"
            >
              {flash.action === 'stop' ? <IconBlock /> : <IconCheck />}
              <span>
                {flash.action === 'stop' ? 'Stopped and held' : 'Started'} <b>{flash.strategy}</b> at{' '}
                <span title={fmtUTC(flash.audit.at)}>{fmtMx(flash.audit.at)}</span>. Audited as{' '}
                <span className="num">{flash.audit.id || flash.audit.outcome}</span>.
                {flash.audit_error && <span className="neg"> Audit warning: {flash.audit_error}</span>}
              </span>
            </div>
          )}

          {info.strategies.length === 0 ? (
            <div className="panel muted controls-empty" data-testid="strategies-empty">
              strategy-executor has no strategies registered. Register them with{' '}
              <span className="num">scripts/start-organic-trading.sh</span> or POST /api/v1/strategies.
            </div>
          ) : (
            <div className="table-wrap">
              <table className="data strategies-table" id="strategies-table" data-testid="strategies-table">
                <thead>
                  <tr>
                    <th scope="col">Strategy</th>
                    <th scope="col">Book</th>
                    <th scope="col">Status</th>
                    <th scope="col">Exposure</th>
                    <th scope="col" className="r">
                      Signals / trades
                    </th>
                    <th scope="col" className="r">
                      P&amp;L (today)
                    </th>
                    <th scope="col">
                      <span className="sr-only">Action</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {info.strategies.map((s) => {
                    const q = quoteOf(s.book)
                    return (
                      <tr key={s.name} data-testid={`strategy-row-${s.name}`} className={s.hold ? 'is-held' : ''}>
                        <td>
                          <div className="strategy-name">
                            <b className="num">{s.name}</b>
                            <span className="faint">
                              {s.type} · v{s.version}
                              {s.dry_run && (
                                <>
                                  {' '}
                                  · <Badge tone="info">dry-run</Badge>
                                </>
                              )}
                            </span>
                          </div>
                        </td>
                        <td className="nowrap" data-label="Book">
                          {bookLabel(s.book)}
                        </td>
                        <td data-label="Status">
                          <StrategyStatus s={s} />
                        </td>
                        <td data-label="Exposure">
                          {busy(s) ? (
                            <span className="warn-text">{positionText(s)}</span>
                          ) : (
                            <span className="faint">flat</span>
                          )}
                        </td>
                        <td className="r num" data-label="Signals / trades">
                          {s.signal_count} / {s.trade_count}
                          <div className="faint">
                            {s.trade_count > 0 ? `${fmtFrac(s.win_rate, 0)} won` : 'no trades'}
                          </div>
                        </td>
                        <td
                          className={`r num ${s.total_pnl > 0 ? 'pos' : s.total_pnl < 0 ? 'neg' : ''}`}
                          data-label="P&L (today)"
                        >
                          {fmtMoney(s.total_pnl, q)}
                          <div className="faint">{fmtMoney(s.daily_pnl, q)}</div>
                        </td>
                        <td className="r">
                          {s.running ? (
                            <button
                              ref={(el) => {
                                if (el) triggers.current.set(s.name, el)
                              }}
                              type="button"
                              className="btn btn-danger btn-sm"
                              id={`strategy-stop-${s.name}`}
                              data-testid={`strategy-stop-${s.name}`}
                              disabled={!ex.controls_enabled}
                              onClick={() => {
                                setFlash(null)
                                setPending({ s, action: 'stop' })
                              }}
                            >
                              <IconBlock /> Stop
                            </button>
                          ) : (
                            <button
                              ref={(el) => {
                                if (el) triggers.current.set(s.name, el)
                              }}
                              type="button"
                              className="btn btn-ok btn-sm"
                              id={`strategy-start-${s.name}`}
                              data-testid={`strategy-start-${s.name}`}
                              disabled={!ex.controls_enabled}
                              onClick={() => {
                                setFlash(null)
                                setPending({ s, action: 'start' })
                              }}
                            >
                              <IconCheck /> Start
                            </button>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}

          {info.holds.length > 0 && (
            <div className="panel strategy-orphans" data-testid="orphan-holds">
              <b>Held but not registered:</b>{' '}
              {info.holds.map((h, i) => (
                <span key={h.name ?? i}>
                  {i > 0 && ' · '}
                  <span className="num">{h.name}</span>{' '}
                  <span className="faint">
                    ({h.reason || 'no reason'}, {h.by})
                  </span>
                </span>
              ))}
              . The hold applies again as soon as the strategy is registered.
            </div>
          )}

          <AuditLog
            info={info}
            target
            empty="No strategy start or stop attempts yet. Every attempt, allowed or not, is appended here."
          />
        </>
      )}

      {pending && ex.url && (
        <StrategyDialog
          p={pending}
          executorURL={ex.url}
          onClose={() => close(pending.s.name)}
          onDone={(r) => {
            setFlash(r)
            close(pending.s.name)
          }}
        />
      )}
    </section>
  )
}

function HaltAllFlash({ r }: { r: HaltAllResponse }) {
  const failed = r.results.filter((x) => x.outcome === 'failed')
  return (
    <div className={`controls-flash halt`} role="status" data-testid="halt-all-flash">
      <IconBlock />
      <span>
        Kill switch <span className="num">{r.group}</span>:{' '}
        {r.results.map((x, i) => (
          <span key={x.ledger}>
            {i > 0 && ' · '}
            <b className="num">{x.ledger}</b> {x.outcome.replace('_', ' ')}
            {x.error && <span className="neg"> ({x.error})</span>}
          </span>
        ))}
        {failed.length > 0 && <span className="neg"> · {failed.length} failed, check the audit logs</span>}
      </span>
    </div>
  )
}

export function StrategiesPage() {
  usePageTitle('Strategies', 'Ledger halts, the kill switch and intraday strategy start/stop, all audited.')
  const q = useStrategies()
  const info = q.data
  const [haltAll, setHaltAll] = useState<HaltAllResponse | null>(null)
  const halted = info?.ledgers.filter((l) => ledgerState(l) === 'halted').length ?? 0
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Strategies</h1>
          <p>
            Every ledger and intraday strategy in one place. A stop holds the strategy across restarts until an operator
            starts it again; every attempt is audited.
          </p>
        </div>
        {info?.executor.fetched_at && (
          <span className="faint" title={fmtUTC(info.executor.fetched_at)}>
            updated {fmtMx(info.executor.fetched_at)}
          </span>
        )}
      </div>

      {q.isLoading && (
        <div className="stack" style={{ gap: 16 }}>
          <CardSkeleton lines={3} />
          <CardSkeleton lines={6} />
        </div>
      )}
      {q.isError && (
        <div className="card">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      )}

      {info && (
        <>
          <KillSwitch info={info} onResult={setHaltAll} />
          {haltAll && <HaltAllFlash r={haltAll} />}

          <div className="section-head">
            <h2>
              <IconShield className="controls-h-icon" /> Daily-executor ledgers
            </h2>
            <span className="faint">
              {info.ledgers.length} ledger{info.ledgers.length === 1 ? '' : 's'}
              {halted > 0 && ` · ${halted} halted`}
            </span>
          </div>
          <div className="ledger-grid" data-testid="ledger-grid">
            {info.ledgers.map((l) => (
              <LedgerCard key={l.name} l={l} />
            ))}
          </div>

          <StrategiesCard info={info} />
        </>
      )}
    </>
  )
}
