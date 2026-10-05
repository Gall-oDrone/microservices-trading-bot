import type { ReactNode } from 'react'
import { ApiError } from '../api/client'
import type { Finding, Signal } from '../api/schemas'
import { IconAlert, IconArrowUp, IconBlock, IconInfo, IconMinus } from './icons'

export function Stat({
  label,
  value,
  hint,
  tone,
  large,
  title,
  id,
}: {
  label: ReactNode
  value: ReactNode
  hint?: ReactNode
  tone?: 'pos' | 'neg' | 'warn'
  large?: boolean
  title?: string
  id?: string
}) {
  const toneClass = tone === 'warn' ? 'warn-text' : (tone ?? '')
  return (
    <div className="stat" id={id}>
      <span className="stat-label">{label}</span>
      <span className={`stat-value ${large ? 'lg' : ''} ${toneClass}`} title={title}>
        {value}
      </span>
      {hint != null && <span className="stat-hint">{hint}</span>}
    </div>
  )
}

export function SignalPill({ signal }: { signal: Signal }) {
  return (
    <span className={`signal-pill ${signal}`} data-testid="signal-pill">
      {signal === 'long' ? <IconArrowUp /> : <IconMinus />}
      {signal}
    </span>
  )
}

export function Badge({
  tone,
  children,
  dot,
  mono,
  title,
}: {
  tone?: 'long' | 'flat' | 'loss' | 'warn' | 'info' | 'ok' | 'block'
  children: ReactNode
  dot?: boolean
  mono?: boolean
  title?: string
}) {
  return (
    <span className={`badge ${tone ?? ''} ${mono ? 'mono' : ''}`} title={title}>
      {dot && <span className="dot" />}
      {children}
    </span>
  )
}

export function Banner({
  tone,
  title,
  children,
  id,
}: {
  tone: 'warn' | 'block' | 'info'
  title: ReactNode
  children?: ReactNode
  id?: string
}) {
  const Icon = tone === 'block' ? IconBlock : tone === 'warn' ? IconAlert : IconInfo
  return (
    <div className={`banner ${tone}`} role={tone === 'info' ? 'status' : 'alert'} id={id}>
      <Icon />
      <div>
        <strong>{title}</strong>
        {children && <p>{children}</p>}
      </div>
    </div>
  )
}

/** A limit's usage. value/limit; limit 0 means disabled. */
export function Meter({
  label,
  value,
  limitLabel,
  ratio,
  disabled,
}: {
  label: ReactNode
  value: ReactNode
  limitLabel: ReactNode
  ratio: number
  disabled?: boolean
}) {
  const r = Math.max(0, ratio)
  const tone = disabled ? 'off' : r >= 1 ? 'block' : r >= 0.8 ? 'warn' : ''
  return (
    <div className="meter">
      <div className="meter-top">
        <span className="muted">{label}</span>
        <span className="num">
          {value} <span className="faint">/ {disabled ? 'off' : limitLabel}</span>
        </span>
      </div>
      <div
        className="meter-track"
        role="meter"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(Math.min(r, 1) * 100)}
        aria-label={typeof label === 'string' ? label : undefined}
      >
        <div className={`meter-fill ${tone}`} style={{ width: `${Math.min(r, 1) * 100}%` }} />
      </div>
    </div>
  )
}

export function FindingRow({ f }: { f: Finding }) {
  return (
    <li className="row" style={{ alignItems: 'flex-start', gap: 10 }}>
      <Badge tone={f.severity === 'block' ? 'block' : 'warn'}>{f.severity}</Badge>
      <div style={{ minWidth: 0 }}>
        <div>{f.message}</div>
        <div className="faint num" style={{ fontSize: 11.5 }}>
          {f.rule}
        </div>
      </div>
    </li>
  )
}

export function Skeleton({ h = 16, w = '100%' }: { h?: number; w?: number | string }) {
  return <div className="skeleton" style={{ height: h, width: w }} />
}

export function CardSkeleton({ lines = 4 }: { lines?: number }) {
  return (
    <div className="card" aria-busy="true">
      <div className="stack" style={{ gap: 12 }}>
        <Skeleton h={20} w="40%" />
        {Array.from({ length: lines }, (_, i) => (
          <Skeleton key={i} h={14} w={`${90 - i * 12}%`} />
        ))}
      </div>
    </div>
  )
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const msg = error instanceof ApiError || error instanceof Error ? error.message : String(error)
  return (
    <div className="empty" role="alert">
      <IconAlert />
      <strong>Could not load this view</strong>
      <span className="muted" style={{ maxWidth: '60ch' }}>
        {msg}
      </span>
      {onRetry && (
        <button className="btn" onClick={onRetry} style={{ marginTop: 8 }}>
          Retry
        </button>
      )}
    </div>
  )
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <IconInfo />
      <strong>{title}</strong>
      {children && <span className="muted">{children}</span>}
    </div>
  )
}
