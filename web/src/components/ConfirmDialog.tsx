/**
 * The confirmation dialog every operator control uses (halt/resume a ledger,
 * start/stop a strategy, the halt-all kill switch): reason, who, the target
 * typed again, the operator token, and optionally an acknowledgement box.
 *
 * ui-api does the real checks and audits every attempt; this only collects
 * the input, mirrors the validation for instant feedback, and shows errors.
 * Rendered into document.body (a portal) so no card's overflow or stacking
 * can clip it; focus is trapped and Escape closes it.
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
import { ApiError } from '../api/client'
import { controlProblems, getOperatorToken, type ControlField, type ControlProblem } from '../api/controls'

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

export function Field({
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

export interface ConfirmValues {
  reason: string
  by: string
  confirm: string
  token: string
  ack: boolean
}

export interface ConfirmCallbacks<T> {
  onSuccess: (r: T) => void
  onError: (e: unknown) => void
}

export interface ConfirmDialogProps<T> {
  tone: 'danger' | 'resume'
  icon: ReactNode
  title: string
  sub: ReactNode
  /** Shown above the fields, e.g. who halted and why. */
  current?: ReactNode
  /** Label of the confirm field and the exact text to type in it. */
  confirmLabel: string
  confirmWord: string
  reasonPlaceholder: string
  reasonHint?: string
  submitLabel: string
  pendingLabel: string
  /** A required acknowledgement checkbox (e.g. stopping with an open position). */
  ack?: { label: ReactNode; message: string }
  pending: boolean
  error: unknown
  run: (v: ConfirmValues, cb: ConfirmCallbacks<T>) => void
  onClose: () => void
  onDone: (r: T) => void
}

export function ConfirmDialog<T>({
  tone,
  icon,
  title,
  sub,
  current,
  confirmLabel,
  confirmWord,
  reasonPlaceholder,
  reasonHint = 'one line, kept in the audit log',
  submitLabel,
  pendingLabel,
  ack,
  pending,
  error,
  run,
  onClose,
  onDone,
}: ConfirmDialogProps<T>) {
  const [reason, setReason] = useState('')
  const [by, setBy] = useState(rememberedBy)
  const [confirm, setConfirm] = useState('')
  const [token, setToken] = useState(getOperatorToken)
  const [acked, setAcked] = useState(false)
  const [touched, setTouched] = useState<Partial<Record<ControlField, boolean>>>({})
  const [tried, setTried] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const first = useRef<HTMLInputElement>(null)
  const titleId = useId()

  const problems: ControlProblem[] = controlProblems({ reason, by, confirm, token }, confirmWord)
  if (ack && !acked) problems.push({ field: 'ack', message: ack.message })
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
    if (pending) return
    run(
      { reason, by, confirm, token, ack: acked },
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

  const err = error as Error | null | undefined
  const errTitle =
    err instanceof ApiError
      ? err.status === 401
        ? 'Token rejected'
        : err.status === 409
          ? 'Nothing changed'
          : err.status === 403
            ? 'Controls refused'
            : err.status === 502
              ? 'strategy-executor did not answer'
              : `ui-api said no (HTTP ${err.status || 'unreachable'})`
      : 'Request failed'

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div
        ref={ref}
        className={`modal ${tone}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        id="control-dialog"
        data-testid="control-dialog"
        onKeyDown={onKeyDown}
      >
        <form onSubmit={submit} noValidate>
          <header className="modal-head">
            <span className={`modal-icon ${tone}`} aria-hidden>
              {icon}
            </span>
            <div>
              <h2 id={titleId}>{title}</h2>
              <p className="modal-sub">{sub}</p>
            </div>
          </header>

          {current && (
            <div className="panel modal-current" data-testid="control-current">
              {current}
            </div>
          )}

          <div className="modal-body">
            <Field
              id="control-reason"
              label="Reason"
              error={errorFor('reason')}
              hint={`${reason.trim().length}/500 · ${reasonHint}`}
            >
              <input
                ref={first}
                id="control-reason"
                data-testid="control-reason"
                className="input"
                type="text"
                maxLength={500}
                autoComplete="off"
                placeholder={reasonPlaceholder}
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
                label={confirmLabel}
                error={errorFor('confirm')}
                hint={
                  <>
                    Type <b className="num">{confirmWord}</b> to confirm
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
            {ack && (
              <div className={`field field-check ${errorFor('ack') ? 'invalid' : ''}`}>
                <label htmlFor="control-ack">
                  <input
                    id="control-ack"
                    data-testid="control-ack"
                    type="checkbox"
                    checked={acked}
                    onChange={(e) => setAcked(e.target.checked)}
                    onBlur={touch('ack')}
                    aria-invalid={!!errorFor('ack')}
                    aria-describedby="control-ack-msg"
                  />
                  <span>{ack.label}</span>
                </label>
                {errorFor('ack') && (
                  <span className="field-msg error" id="control-ack-msg">
                    {errorFor('ack')}
                  </span>
                )}
              </div>
            )}
          </div>

          {err && (
            <div className="modal-error" role="alert" data-testid="control-error">
              <b>{errTitle}.</b> {err.message}
            </div>
          )}

          <footer className="modal-actions">
            <button type="button" className="btn btn-ghost" id="control-cancel" onClick={close} disabled={pending}>
              Cancel
            </button>
            <button
              type="submit"
              className={`btn ${tone === 'danger' ? 'btn-danger' : 'btn-ok'}`}
              id="control-submit"
              data-testid="control-submit"
              disabled={pending}
              aria-disabled={tried && problems.length > 0}
              aria-busy={pending}
            >
              {pending ? pendingLabel : submitLabel}
            </button>
          </footer>
        </form>
      </div>
    </div>,
    document.body,
  )
}
