/**
 * Operator controls (R4) on the Risk page: halt or resume the daily-executor
 * for the selected ledger, behind a confirmation dialog, with the audit log.
 *
 * ui-api does the real checks (token, loopback origin, reason, confirm,
 * state) and writes risk-state.json atomically; this card only collects the
 * input, mirrors the validation for instant feedback, and shows the result.
 */
import { useRef, useState } from 'react'
import { useControls } from '../api/client'
import { useControlMutation, type ControlAction } from '../api/controls'
import type { AuditEntry, AuditOutcome, ControlResponse, ControlsInfo } from '../api/schemas'
import { fmtMx, fmtUTC } from '../lib/format'
import { ConfirmDialog } from './ConfirmDialog'
import { IconBlock, IconCheck, IconShield } from './icons'
import { Badge, Banner, CardSkeleton, ErrorState } from './ui'

const OUTCOME_TONE: Record<AuditOutcome, 'ok' | 'info' | 'warn' | 'block'> = {
  done: 'ok',
  requested: 'info',
  refused: 'warn',
  denied: 'block',
  failed: 'block',
}

const AUDIT_ROWS = 8

/** What an audit log view needs (GET /controls and GET /strategies both carry it). */
export interface AuditSource {
  audit: AuditEntry[]
  audit_path?: string
  audit_error?: string
}

/**
 * An audit log, newest first. `target` adds a column with the strategy or
 * ledger each line is about (the Strategies page mixes them).
 */
export function AuditLog({ info, empty, target = false }: { info: AuditSource; empty: string; target?: boolean }) {
  const [all, setAll] = useState(false)
  const rows = all ? info.audit : info.audit.slice(0, AUDIT_ROWS)
  return (
    <>
      <div className="controls-audit-head">
        <h3>Audit log</h3>
        <span className="num faint controls-path" title={info.audit_path}>
          {info.audit_path}
        </span>
      </div>
      {info.audit_error && (
        <Banner tone="warn" title="Some audit lines could not be read" id="controls-audit-error">
          {info.audit_error}
        </Banner>
      )}
      {info.audit.length === 0 ? (
        <div className="panel muted controls-empty" data-testid="audit-empty">
          {empty}
        </div>
      ) : (
        <div className="table-wrap">
          <table className="data controls-audit" id="audit-table" data-testid="audit-table">
            <thead>
              <tr>
                <th scope="col">When (Mexico City)</th>
                <th scope="col">Action</th>
                {target && <th scope="col">Target</th>}
                <th scope="col">Outcome</th>
                <th scope="col">By</th>
                <th scope="col">Reason / error</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((e: AuditEntry, i) => (
                <tr key={e.id ? `${e.id}-${e.outcome}` : i} data-testid="audit-row">
                  <td className="num nowrap" title={fmtUTC(e.at)}>
                    {fmtMx(e.at)}
                  </td>
                  <td>{e.action}</td>
                  {target && <td className="num">{e.strategy || e.ledger || <span className="faint">—</span>}</td>}
                  <td>
                    <Badge tone={OUTCOME_TONE[e.outcome]} dot>
                      {e.outcome}
                    </Badge>
                  </td>
                  <td>{e.by || <span className="faint">—</span>}</td>
                  <td className="controls-why">
                    {e.reason && <span>{e.reason}</span>}
                    {e.detail && <span className="faint">{e.detail}</span>}
                    {e.error && <span className={e.outcome === 'done' ? 'faint' : 'neg'}>{e.error}</span>}
                    {!e.reason && !e.error && !e.detail && <span className="faint">—</span>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {info.audit.length > AUDIT_ROWS && (
        <button type="button" className="btn btn-ghost controls-more" onClick={() => setAll((v) => !v)}>
          {all ? 'Show fewer' : `Show all ${info.audit.length}`}
        </button>
      )}
    </>
  )
}

function ControlDialog({
  action,
  info,
  onClose,
  onDone,
}: {
  action: ControlAction
  info: ControlsInfo
  onClose: () => void
  onDone: (r: ControlResponse) => void
}) {
  const ledger = info.ledger
  const m = useControlMutation()
  const halt = action === 'halt'
  return (
    <ConfirmDialog<ControlResponse>
      tone={halt ? 'danger' : 'resume'}
      icon={halt ? <IconBlock /> : <IconCheck />}
      title={halt ? 'Halt trading' : 'Resume trading'}
      sub={
        halt ? (
          <>
            Writes a halt to <span className="num">{info.halt_file.path}</span>. The daily-executor blocks and records
            every new stage order on <b>{ledger}</b> until someone resumes.
          </>
        ) : (
          <>
            Clears the operator halt in <span className="num">{info.halt_file.path}</span>. The next run on{' '}
            <b>{ledger}</b> trades again if the policy allows it.
          </>
        )
      }
      current={
        !halt && info.halt_file.halted ? (
          <>
            Halted by <b>{info.halt_file.by}</b> ·{' '}
            <span title={fmtUTC(info.halt_file.at)}>{fmtMx(info.halt_file.at)}</span> · {info.halt_file.reason}
          </>
        ) : undefined
      }
      confirmLabel="Ledger name"
      confirmWord={ledger}
      reasonPlaceholder={
        halt ? 'e.g. Bitso API incident, pausing until resolved' : 'e.g. Incident resolved, checks green'
      }
      reasonHint="one line, kept in the halt file and the audit log"
      submitLabel={halt ? `Halt ${ledger}` : `Resume ${ledger}`}
      pendingLabel={halt ? 'Halting…' : 'Resuming…'}
      pending={m.isPending}
      error={m.error}
      run={(v, cb) => m.mutate({ action, reason: v.reason, by: v.by, confirm: v.confirm, token: v.token }, cb)}
      onClose={onClose}
      onDone={onDone}
    />
  )
}

/** The halt/resume card. `policyHalted` notes a policy halt, which only the policy file can lift. */
export function OperatorControls({ policyHalted = false }: { policyHalted?: boolean }) {
  const q = useControls()
  const [open, setOpen] = useState<ControlAction | null>(null)
  const [flash, setFlash] = useState<ControlResponse | null>(null)
  const trigger = useRef<HTMLButtonElement>(null)

  if (q.isLoading) return <CardSkeleton lines={4} />
  if (q.isError) {
    return (
      <section className="card" aria-label="Operator controls">
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      </section>
    )
  }
  const info = q.data
  if (!info) return null
  const hf = info.halt_file
  const halted = hf.halted
  const invalid = !!hf.error

  const closeDialog = () => {
    setOpen(null)
    // Back to the button that opened it (it may have flipped halt <-> resume).
    requestAnimationFrame(() => trigger.current?.focus())
  }

  return (
    <section
      className={`card controls-card ${halted ? 'halted' : ''}`}
      aria-labelledby="controls-h"
      id="operator-controls"
      data-testid="operator-controls"
      data-state={invalid ? 'invalid' : halted ? 'halted' : 'running'}
    >
      <div className="card-head">
        <div>
          <h2 id="controls-h">
            <IconShield className="controls-h-icon" /> Operator controls
          </h2>
          <div className="sub">
            Halt or resume the daily-executor for this ledger. ui-api writes{' '}
            <span className="num">risk-state.json</span> and audits every attempt.
          </div>
        </div>
        <Badge tone={info.enabled ? 'ok' : 'flat'} dot>
          {info.enabled ? 'armed · local token' : 'off'}
        </Badge>
      </div>

      {!info.enabled && (
        <div className="panel controls-off" data-testid="controls-disabled">
          <b>Controls are off:</b> {info.disabled_reason || 'not enabled on this ui-api'}. The page stays read-only;
          halt by editing <span className="num">{hf.path}</span> by hand, or start ui-api on 127.0.0.1 with{' '}
          <code>-operator-token-file</code> (a 0600 file).
        </div>
      )}

      <div className="controls-state">
        <span className={`controls-orb ${invalid ? 'invalid' : halted ? 'halted' : 'running'}`} aria-hidden />
        <div className="controls-state-text">
          <div className="controls-state-title" data-testid="controls-state">
            {invalid ? 'Halt file is invalid' : halted ? 'Halted by operator' : 'No operator halt'}
          </div>
          <div className="faint">
            {invalid ? (
              <>
                The executor fails closed and will not run: {hf.error}. Halting replaces the file; resuming needs a hand
                fix.
              </>
            ) : halted ? (
              <>
                {hf.reason} · by <b>{hf.by}</b> · <span title={fmtUTC(hf.at)}>{fmtMx(hf.at)}</span>
              </>
            ) : (
              <>The next executor run may trade, within the policy limits.</>
            )}
            {policyHalted && <> The policy halt stays in force until the policy lifts it.</>}
          </div>
        </div>
        {halted && !invalid ? (
          <button
            ref={trigger}
            type="button"
            className="btn btn-ok controls-action"
            id="control-resume"
            data-testid="control-resume"
            disabled={!info.enabled}
            onClick={() => {
              setFlash(null)
              setOpen('resume')
            }}
          >
            <IconCheck /> Resume trading
          </button>
        ) : (
          <button
            ref={trigger}
            type="button"
            className="btn btn-danger controls-action"
            id="control-halt"
            data-testid="control-halt"
            disabled={!info.enabled}
            onClick={() => {
              setFlash(null)
              setOpen('halt')
            }}
          >
            <IconBlock /> Halt trading
          </button>
        )}
      </div>

      {flash && (
        <div className={`controls-flash ${flash.action}`} role="status" data-testid="controls-flash">
          {flash.action === 'halt' ? <IconBlock /> : <IconCheck />}
          <span>
            {flash.action === 'halt' ? 'Halted' : 'Resumed'} <b>{flash.ledger}</b> at{' '}
            <span title={fmtUTC(flash.audit.at)}>{fmtMx(flash.audit.at)}</span>. Audited as{' '}
            <span className="num">{flash.audit.id || flash.audit.outcome}</span>.
            {flash.audit_error && <span className="neg"> Audit warning: {flash.audit_error}</span>}
          </span>
        </div>
      )}

      <AuditLog
        info={info}
        empty="No halt or resume attempts on this ledger yet. Every attempt, allowed or not, is appended here."
      />

      {open && (
        <ControlDialog
          action={open}
          info={info}
          onClose={closeDialog}
          onDone={(r) => {
            setFlash(r)
            closeDialog()
          }}
        />
      )}
    </section>
  )
}
