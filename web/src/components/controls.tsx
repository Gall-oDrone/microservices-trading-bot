/**
 * Operator controls (R4) on the Risk page: halt or resume the daily-executor
 * for the selected ledger, behind a confirmation dialog, with the audit log.
 *
 * ui-api does the real checks (token, loopback origin, reason, confirm,
 * state) and writes risk-state.json atomically; this card only collects the
 * input, mirrors the validation for instant feedback, and shows the result.
 */
import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { ApiError, useControls } from '../api/client'
import {
  controlProblems,
  getOperatorToken,
  useControlMutation,
  type ControlAction,
  type ControlField,
} from '../api/controls'
import type { AuditEntry, AuditOutcome, ControlResponse, ControlsInfo } from '../api/schemas'
import { fmtMx, fmtUTC } from '../lib/format'
import { IconBlock, IconCheck, IconShield } from './icons'
import { Badge, Banner, CardSkeleton, ErrorState } from './ui'

const BY_KEY = 'mtb-operator-by'

function rememberedBy(): string {
  try {
    return localStorage.getItem(BY_KEY) ?? ''
  } catch {
    return ''
  }
}

function rememberBy(by: string) {
  try {
    localStorage.setItem(BY_KEY, by)
  } catch {
    // Storage disabled: the name is typed each time.
  }
}

const OUTCOME_TONE: Record<AuditOutcome, 'ok' | 'info' | 'warn' | 'block'> = {
  done: 'ok',
  requested: 'info',
  refused: 'warn',
  denied: 'block',
  failed: 'block',
}

const AUDIT_ROWS = 8

function AuditLog({ info }: { info: ControlsInfo }) {
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
          No halt or resume attempts on this ledger yet. Every attempt, allowed or not, is appended here.
        </div>
      ) : (
        <div className="table-wrap">
          <table className="data controls-audit" id="audit-table" data-testid="audit-table">
            <thead>
              <tr>
                <th scope="col">When (Mexico City)</th>
                <th scope="col">Action</th>
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
                  <td>
                    <Badge tone={OUTCOME_TONE[e.outcome]} dot>
                      {e.outcome}
                    </Badge>
                  </td>
                  <td>{e.by || <span className="faint">—</span>}</td>
                  <td className="controls-why">
                    {e.reason && <span>{e.reason}</span>}
                    {e.error && <span className={e.outcome === 'done' ? 'faint' : 'neg'}>{e.error}</span>}
                    {!e.reason && !e.error && <span className="faint">—</span>}
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

function Field({
  id,
  label,
  hint,
  error,
  children,
}: {
  id: string
  label: string
  hint?: ReactNode
  error?: string
  children: ReactNode
}) {
  return (
    <div className={`field ${error ? 'invalid' : ''}`}>
      <label htmlFor={id}>{label}</label>
      {children}
      {error ? (
        <span className="field-msg error" id={`${id}-msg`}>
          {error}
        </span>
      ) : hint ? (
        <span className="field-msg" id={`${id}-msg`}>
          {hint}
        </span>
      ) : null}
    </div>
  )
}

const FOCUSABLE = 'button:not([disabled]), input:not([disabled]), [href], [tabindex]:not([tabindex="-1"])'

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
  const [reason, setReason] = useState('')
  const [by, setBy] = useState(rememberedBy)
  const [confirm, setConfirm] = useState('')
  const [token, setToken] = useState(getOperatorToken)
  const [touched, setTouched] = useState<Partial<Record<ControlField, boolean>>>({})
  const [tried, setTried] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const first = useRef<HTMLInputElement>(null)
  const titleId = useId()
  const halt = action === 'halt'

  const problems = controlProblems({ reason, by, confirm, token }, ledger)
  const errorFor = (f: ControlField) => (tried || touched[f] ? problems.find((p) => p.field === f)?.message : undefined)
  const touch = (f: ControlField) => () => setTouched((t) => ({ ...t, [f]: true }))

  useEffect(() => {
    first.current?.focus()
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [])

  const pending = m.isPending
  const close = useCallback(() => {
    if (!pending) onClose()
  }, [pending, onClose])

  // Escape closes from anywhere (focus may sit on the backdrop or the body).
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [close])

  // Tab stays inside the dialog.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Tab' || !ref.current) return
    const els = Array.from(ref.current.querySelectorAll<HTMLElement>(FOCUSABLE))
    if (els.length === 0) return
    const [a, z] = [els[0], els[els.length - 1]]
    if (e.shiftKey && document.activeElement === a) {
      e.preventDefault()
      z.focus()
    } else if (!e.shiftKey && document.activeElement === z) {
      e.preventDefault()
      a.focus()
    }
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTried(true)
    if (problems.length > 0) {
      document.getElementById(`control-${problems[0].field}`)?.focus()
      return
    }
    if (m.isPending) return
    m.mutate(
      { action, reason, by, confirm, token },
      {
        onSuccess: (r) => {
          rememberBy(by.trim())
          onDone(r)
        },
        onError: (err) => {
          if (err instanceof ApiError && err.status === 401) setToken('')
        },
      },
    )
  }

  const err = m.error
  const errTitle =
    err instanceof ApiError
      ? err.status === 401
        ? 'Token rejected'
        : err.status === 409
          ? 'Nothing changed'
          : err.status === 403
            ? 'Controls refused'
            : `ui-api said no (HTTP ${err.status || 'unreachable'})`
      : 'Request failed'

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div
        ref={ref}
        className={`modal ${halt ? 'danger' : 'resume'}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        id="control-dialog"
        data-testid="control-dialog"
        onKeyDown={onKeyDown}
      >
        <form onSubmit={submit} noValidate>
          <header className="modal-head">
            <span className={`modal-icon ${halt ? 'danger' : 'resume'}`} aria-hidden>
              {halt ? <IconBlock /> : <IconCheck />}
            </span>
            <div>
              <h2 id={titleId}>{halt ? 'Halt trading' : 'Resume trading'}</h2>
              <p className="modal-sub">
                {halt ? (
                  <>
                    Writes a halt to <span className="num">{info.halt_file.path}</span>. The daily-executor blocks and
                    records every new stage order on <b>{ledger}</b> until someone resumes.
                  </>
                ) : (
                  <>
                    Clears the operator halt in <span className="num">{info.halt_file.path}</span>. The next run on{' '}
                    <b>{ledger}</b> trades again if the policy allows it.
                  </>
                )}
              </p>
            </div>
          </header>

          {!halt && info.halt_file.halted && (
            <div className="panel modal-current" data-testid="control-current">
              Halted by <b>{info.halt_file.by}</b> ·{' '}
              <span title={fmtUTC(info.halt_file.at)}>{fmtMx(info.halt_file.at)}</span> · {info.halt_file.reason}
            </div>
          )}

          <div className="modal-body">
            <Field
              id="control-reason"
              label="Reason"
              error={errorFor('reason')}
              hint={`${reason.trim().length}/500 · one line, kept in the halt file and the audit log`}
            >
              <input
                ref={first}
                id="control-reason"
                data-testid="control-reason"
                className="input"
                type="text"
                maxLength={500}
                autoComplete="off"
                placeholder={
                  halt ? 'e.g. Bitso API incident, pausing until resolved' : 'e.g. Incident resolved, checks green'
                }
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                onBlur={touch('reason')}
                aria-invalid={!!errorFor('reason')}
                aria-describedby="control-reason-msg"
              />
            </Field>
            <div className="field-row">
              <Field id="control-by" label="By" error={errorFor('by')} hint="Your name or handle">
                <input
                  id="control-by"
                  data-testid="control-by"
                  className="input"
                  type="text"
                  maxLength={64}
                  autoComplete="nickname"
                  value={by}
                  onChange={(e) => setBy(e.target.value)}
                  onBlur={touch('by')}
                  aria-invalid={!!errorFor('by')}
                  aria-describedby="control-by-msg"
                />
              </Field>
              <Field
                id="control-confirm"
                label="Ledger name"
                error={errorFor('confirm')}
                hint={
                  <>
                    Type <b className="num">{ledger}</b> to confirm
                  </>
                }
              >
                <input
                  id="control-confirm"
                  data-testid="control-confirm"
                  className="input mono"
                  type="text"
                  autoComplete="off"
                  spellCheck={false}
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                  onBlur={touch('confirm')}
                  aria-invalid={!!errorFor('confirm')}
                  aria-describedby="control-confirm-msg"
                />
              </Field>
            </div>
            <Field
              id="control-token"
              label="Operator token"
              error={errorFor('token')}
              hint="Kept for this tab only (sessionStorage); never an exchange key"
            >
              <input
                id="control-token"
                data-testid="control-token"
                className="input mono"
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={token}
                onChange={(e) => setToken(e.target.value)}
                onBlur={touch('token')}
                aria-invalid={!!errorFor('token')}
                aria-describedby="control-token-msg"
              />
            </Field>
          </div>

          {err && (
            <div className="modal-error" role="alert" data-testid="control-error">
              <b>{errTitle}.</b> {err.message}
            </div>
          )}

          <footer className="modal-actions">
            <button type="button" className="btn btn-ghost" id="control-cancel" onClick={close} disabled={m.isPending}>
              Cancel
            </button>
            <button
              type="submit"
              className={`btn ${halt ? 'btn-danger' : 'btn-ok'}`}
              id="control-submit"
              data-testid="control-submit"
              disabled={m.isPending}
              aria-disabled={tried && problems.length > 0}
              aria-busy={m.isPending}
            >
              {m.isPending ? (halt ? 'Halting…' : 'Resuming…') : halt ? `Halt ${ledger}` : `Resume ${ledger}`}
            </button>
          </footer>
        </form>
      </div>
    </div>,
    document.body,
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

      <AuditLog info={info} />

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
