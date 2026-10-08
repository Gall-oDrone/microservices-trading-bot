/**
 * Operator controls (R4): halt and resume the daily-executor through ui-api.
 *
 * The operator token is kept in sessionStorage only (gone when the tab
 * closes) and sent as a bearer header; it is not an exchange key and grants
 * nothing but halt/resume on the local ledgers. ui-api writes an audit line
 * for every attempt.
 */
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ApiError, fetchJSON, useLedgerName, withLedger } from './client'
import { controlResponseSchema, type ControlResponse } from './schemas'

const TOKEN_KEY = 'mtb-operator-token'

export function getOperatorToken(): string {
  try {
    return sessionStorage.getItem(TOKEN_KEY) ?? ''
  } catch {
    return ''
  }
}

export function setOperatorToken(t: string) {
  try {
    if (t) sessionStorage.setItem(TOKEN_KEY, t)
    else sessionStorage.removeItem(TOKEN_KEY)
  } catch {
    // Storage disabled: the token is asked for on every action.
  }
}

export type ControlAction = 'halt' | 'resume'

export interface ControlInput {
  action: ControlAction
  reason: string
  by: string
  /** The ledger name typed again; ui-api checks it too. */
  confirm: string
  token: string
}

export type ControlField = 'reason' | 'by' | 'confirm' | 'token'

export interface ControlProblem {
  field: ControlField
  message: string
}

/** Client-side mirror of ui-api's validateControl, for instant feedback (the server decides). */
export function controlProblems(i: Omit<ControlInput, 'action'>, ledger: string): ControlProblem[] {
  const out: ControlProblem[] = []
  const reason = i.reason.trim()
  if (reason.length < 8) out.push({ field: 'reason', message: 'At least 8 characters, so the halt is explained.' })
  if (reason.length > 500) out.push({ field: 'reason', message: 'At most 500 characters.' })
  if (/[\r\n]/.test(i.reason)) out.push({ field: 'reason', message: 'One line.' })
  if (!/^[A-Za-z0-9][A-Za-z0-9 ._@-]{0,63}$/.test(i.by.trim())) {
    out.push({ field: 'by', message: '1–64 letters, digits, spaces or . _ @ -' })
  }
  if (i.confirm !== ledger) out.push({ field: 'confirm', message: `Type “${ledger}” exactly.` })
  if (!i.token.trim()) out.push({ field: 'token', message: 'Required: the token in ui-api’s -operator-token-file.' })
  return out
}

export function sendControl(i: ControlInput, ledger: string): Promise<ControlResponse> {
  return fetchJSON(withLedger(`/risk/${i.action}`, ledger), controlResponseSchema, undefined, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${i.token.trim()}` },
    body: JSON.stringify({ reason: i.reason.trim(), by: i.by.trim(), confirm: i.confirm }),
  })
}

/**
 * Halt or resume the selected ledger. On success the token is remembered
 * for the tab and every view that shows the halt is refreshed; a 401
 * forgets it.
 */
export function useControlMutation() {
  const ledger = useLedgerName()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (i: ControlInput) => sendControl(i, ledger),
    onSuccess: (_r, i) => {
      setOperatorToken(i.token.trim())
    },
    onError: (e) => {
      if (e instanceof ApiError && e.status === 401) setOperatorToken('')
    },
    onSettled: () => {
      // The audit log changes even when the action is refused.
      void qc.invalidateQueries({ queryKey: ['controls'] })
      void qc.invalidateQueries({ queryKey: ['risk'] })
      void qc.invalidateQueries({ queryKey: ['forward-tests'] })
    },
  })
}
