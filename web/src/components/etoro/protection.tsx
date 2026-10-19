/**
 * Protection of assisted trades (plan phase B, 2026-10-19): the pieces of
 * the Trade page that show and pick a stop loss / take profit.
 *
 * - ProfilePicker: Conservative / Moderate / Aggressive / Custom.
 * - LevelsRow: one profile's stop and target as rates, distances, money
 *   and R:R, with the guards' clamps.
 * - ProtectionPanel: the form section for an open or a protect, fed by
 *   POST /etoro/trades/recommend (read-only): the three profiles, the ATR
 *   regime, the SMA50 hint, the custom inputs.
 * - ProtectionFacts: the block the preview and the outcome show.
 *
 * ui-api computes every level and clamps it; this file only presents and
 * sends what the operator picked.
 */
import type { ReactNode } from 'react'
import type { StopLevels, StopProfile, TradeProtection, TradeRecommendation, Volatility } from '../../api/etoroSchemas'
import { fmtMoney, fmtPrice } from '../../lib/format'
import { PROFILES, pct, regimeTone } from '../../lib/protection'
import { Field } from '../ConfirmDialog'
import { IconAlert, IconInfo, IconShield } from '../icons'
import { Badge, Stat } from '../ui'

export function VolatilityBadge({ v }: { v: Volatility | null | undefined }) {
  if (!v) {
    return (
      <Badge tone="flat" title="no daily bars for an ATR: the fallback stop applies">
        <span data-testid="protect-regime">no ATR</span>
      </Badge>
    )
  }
  const ratio = v.regime_ratio ? ` · ${v.regime_ratio.toFixed(2)}× its ${v.regime_lookback ?? 252}-day median` : ''
  return (
    <Badge
      tone={regimeTone(v.regime)}
      dot
      title={`ATR(${v.period}) ${v.atr.toFixed(2)} = ${pct(v.atr_pct)} of ${v.close.toFixed(2)}${ratio}`}
    >
      <span data-testid="protect-regime">
        {v.regime} vol · ATR {pct(v.atr_pct)}
      </span>
    </Badge>
  )
}

export function ProfilePicker({
  value,
  onChange,
  disabled,
  idPrefix = 'trade-profile',
}: {
  value: StopProfile
  onChange: (p: StopProfile) => void
  disabled?: boolean
  idPrefix?: string
}) {
  return (
    <div className="seg protect-seg" role="group" aria-label="Risk profile" id={idPrefix}>
      {PROFILES.map((p) => (
        <button
          type="button"
          key={p.id}
          id={`${idPrefix}-${p.id}`}
          data-testid={`${idPrefix}-${p.id}`}
          aria-pressed={value === p.id}
          title={p.hint}
          disabled={disabled}
          onClick={() => onChange(p.id)}
        >
          {p.label}
        </button>
      ))}
    </div>
  )
}

/** One profile's levels, compact: stop · target · max loss · R:R. */
export function LevelsRow({
  lv,
  book,
  selected,
  onPick,
  testId,
}: {
  lv: StopLevels
  book: string
  selected?: boolean
  onPick?: () => void
  testId?: string
}) {
  const body = (
    <>
      <span className="protect-level">
        <span className="muted">stop</span> <b className="num">{fmtPrice(lv.sl_rate, 'usd', book)}</b>{' '}
        <span className="num faint">−{pct(lv.sl_pct)}</span>
        {lv.sl_atr ? <span className="faint"> · {lv.sl_atr.toFixed(1)} ATR</span> : null}
      </span>
      <span className="protect-level">
        <span className="muted">target</span>{' '}
        {lv.tp_rate ? (
          <>
            <b className="num">{fmtPrice(lv.tp_rate, 'usd', book)}</b>{' '}
            <span className="num faint">+{pct(lv.tp_pct)}</span>
          </>
        ) : (
          <span className="faint">none</span>
        )}
      </span>
      <span className="protect-level">
        <span className="muted">max loss</span> <b className="num">{fmtMoney(lv.max_loss_usd, 'usd')}</b>
        {lv.target_usd ? (
          <>
            {' '}
            <span className="muted">· gain</span> <b className="num">{fmtMoney(lv.target_usd, 'usd')}</b>
          </>
        ) : null}
        {lv.rr ? <span className="faint"> · {lv.rr.toFixed(2)}R</span> : null}
      </span>
      {!lv.feasible && (
        <span className="protect-level protect-infeasible">
          <IconBlockInline /> {lv.reason}
        </span>
      )}
    </>
  )
  if (onPick) {
    return (
      <button
        type="button"
        className={`protect-row ${selected ? 'selected' : ''} ${lv.feasible ? '' : 'infeasible'}`}
        onClick={onPick}
        data-testid={testId}
        aria-pressed={selected}
      >
        {body}
      </button>
    )
  }
  return (
    <div className={`protect-row ${lv.feasible ? '' : 'infeasible'}`} data-testid={testId}>
      {body}
    </div>
  )
}

function IconBlockInline() {
  return <IconAlert style={{ color: 'var(--loss)' }} />
}

export function Clamps({ items, testId }: { items: string[]; testId?: string }) {
  if (items.length === 0) return null
  return (
    <ul className="protect-clamps" data-testid={testId}>
      {items.map((c) => (
        <li key={c}>
          <IconInfo /> <span>{c}</span>
        </li>
      ))}
    </ul>
  )
}

/**
 * The form section. The recommendation (when loaded) shows the three
 * profiles live; the picked profile is what prepare sends. Custom shows
 * the distance inputs as percentages of the entry.
 */
export function ProtectionPanel({
  side,
  book,
  profile,
  onProfile,
  slPct,
  tpPct,
  onSlPct,
  onTpPct,
  rec,
  loading,
  error,
  busy,
  errors,
}: {
  side: 'open' | 'protect'
  book: string
  profile: StopProfile
  onProfile: (p: StopProfile) => void
  slPct: string
  tpPct: string
  onSlPct: (v: string) => void
  onTpPct: (v: string) => void
  rec: TradeRecommendation | null
  loading: boolean
  error: string | null
  busy: boolean
  errors?: { sl?: string; tp?: string }
}) {
  const ctx = rec?.context
  const chosen = rec && profile !== 'custom' ? rec.profiles[profile] : undefined
  return (
    <section
      className="protect-panel"
      aria-labelledby="trade-protect-h"
      data-testid="trade-protection"
      data-side={side}
    >
      <div className="protect-head">
        <h3 id="trade-protect-h">
          <IconShield /> Protection
          {side === 'protect' ? (
            <span className="faint"> · the held position</span>
          ) : (
            <span className="faint"> · sent with the order</span>
          )}
        </h3>
        <div className="row" style={{ gap: 8 }}>
          {loading ? <span className="faint">recommending…</span> : <VolatilityBadge v={ctx?.volatility} />}
          {ctx && ctx.bounds_source === 'fallback' && (
            <Badge tone="warn" title="the venue's bounds could not be read; the evidence of 2026-10-10 applies">
              fallback bounds
            </Badge>
          )}
        </div>
      </div>
      <p className="protect-sub">
        A stop loss is required by the risk policy (at most {ctx ? pct(ctx.limits.max_sl_pct, 0) : '10%'} from the entry
        and {ctx ? pct(ctx.limits.max_loss_pct_equity) : '0.50%'} of equity at the stop); the take profit is optional.
        Levels are ATR(14) multiples from today&apos;s price, never inside the spread, rounded against you.
        {ctx?.sma50 ? (
          <>
            {' '}
            The rule&apos;s own exit is the SMA50 at <span className="num">{fmtPrice(ctx.sma50, 'usd', book)}</span>.
          </>
        ) : null}
      </p>
      <ProfilePicker value={profile} onChange={onProfile} disabled={busy} />
      {error && (
        <div className="modal-error" role="alert" data-testid="trade-recommend-error">
          <b>Recommendation unavailable.</b> {error} Prepare still applies the policy&apos;s default.
        </div>
      )}
      {rec && profile !== 'custom' && (
        <div className="protect-rows" data-testid="trade-protect-profiles">
          {PROFILES.filter((p) => p.id !== 'custom').map((p) => {
            const lv = rec.profiles[p.id]
            if (!lv) return null
            return (
              <div key={p.id} className="protect-row-wrap">
                <span className={`protect-row-label ${profile === p.id ? 'selected' : ''}`}>{p.label}</span>
                <LevelsRow
                  lv={lv}
                  book={book}
                  selected={profile === p.id}
                  onPick={() => onProfile(p.id)}
                  testId={`trade-level-${p.id}`}
                />
              </div>
            )
          })}
        </div>
      )}
      {chosen && <Clamps items={chosen.clamps} testId="trade-protect-clamps" />}
      {profile === 'custom' && (
        <div className="field-row protect-custom">
          <Field
            id="trade-sl-pct"
            label="Stop loss (% below entry)"
            error={errors?.sl}
            hint="required · clamped to the policy and the venue"
          >
            <input
              id="trade-sl-pct"
              data-testid="trade-sl-pct"
              className="input num"
              type="number"
              inputMode="decimal"
              min={0.01}
              max={99}
              step={0.1}
              placeholder={rec ? (rec.profiles.moderate.sl_pct * 100).toFixed(2) : '2.00'}
              value={slPct}
              onChange={(e) => onSlPct(e.target.value)}
            />
          </Field>
          <Field
            id="trade-tp-pct"
            label="Take profit (% above entry)"
            error={errors?.tp}
            hint="optional · 0 or empty for none"
          >
            <input
              id="trade-tp-pct"
              data-testid="trade-tp-pct"
              className="input num"
              type="number"
              inputMode="decimal"
              min={0}
              max={1000}
              step={0.1}
              placeholder="none"
              value={tpPct}
              onChange={(e) => onTpPct(e.target.value)}
            />
          </Field>
        </div>
      )}
      {rec && rec.warnings.length > 0 && (
        <ul className="trade-reasons warn" data-testid="trade-recommend-warnings">
          {rec.warnings.map((w) => (
            <li key={w}>
              <IconAlert />
              <span>{w}</span>
            </li>
          ))}
        </ul>
      )}
      {rec?.context.before && (
        <p className="faint" data-testid="trade-protect-before">
          At eToro now:{' '}
          {rec.context.before.protected ? (
            <>
              stop <span className="num">{fmtPrice(rec.context.before.stop_loss_rate ?? 0, 'usd', book)}</span>
              {rec.context.before.take_profit_rate ? (
                <>
                  {' '}
                  · target <span className="num">{fmtPrice(rec.context.before.take_profit_rate, 'usd', book)}</span>
                </>
              ) : (
                ' · no target'
              )}
            </>
          ) : (
            <b style={{ color: 'var(--loss)' }}>no stop loss</b>
          )}
        </p>
      )}
    </section>
  )
}

/** The protection block of a preview or an outcome. */
export function ProtectionFacts({ p, book, extra }: { p: TradeProtection; book: string; extra?: ReactNode }) {
  const lv = p.levels
  return (
    <div
      className="protect-facts"
      data-testid="trade-preview-protection"
      data-profile={p.profile}
      data-feasible={lv.feasible}
    >
      <div className="trade-facts">
        <Stat
          label="Stop loss"
          value={<span className="num">{fmtPrice(lv.sl_rate, 'usd', book)}</span>}
          tone={lv.feasible ? undefined : 'neg'}
          hint={`−${pct(lv.sl_pct)} from ${fmtPrice(p.entry, 'usd', book)}${lv.sl_atr ? ` · ${lv.sl_atr.toFixed(1)} ATR below ${fmtPrice(p.mark, 'usd', book)}` : ''}`}
        />
        <Stat
          label="Take profit"
          value={lv.tp_rate ? <span className="num">{fmtPrice(lv.tp_rate, 'usd', book)}</span> : 'none'}
          hint={
            lv.tp_rate
              ? `+${pct(lv.tp_pct)}${lv.tp_atr ? ` · ${lv.tp_atr.toFixed(1)} ATR` : ''}`
              : 'the rule or the operator exits'
          }
        />
        <Stat
          label="Max loss at the stop"
          value={<span className="num">{fmtMoney(lv.max_loss_usd, 'usd')}</span>}
          tone="warn"
          hint={
            lv.target_usd
              ? `gain at the target ${fmtMoney(lv.target_usd, 'usd')} · ${(lv.rr ?? 0).toFixed(2)}R`
              : 'no target'
          }
        />
        <Stat
          label="Profile"
          value={<span style={{ textTransform: 'capitalize' }}>{p.profile}</span>}
          hint={
            p.volatility
              ? `${p.volatility.regime} volatility · ATR(${p.volatility.period}) ${pct(p.volatility.atr_pct)}`
              : 'no ATR: fallback distance'
          }
        />
      </div>
      {p.before && (
        <p className="faint" data-testid="trade-preview-before">
          Before: {p.before.protected ? `stop ${fmtPrice(p.before.stop_loss_rate ?? 0, 'usd', book)}` : 'no stop loss'}
          {p.before.take_profit_rate ? ` · target ${fmtPrice(p.before.take_profit_rate, 'usd', book)}` : ''}
        </p>
      )}
      <Clamps items={lv.clamps} testId="trade-preview-clamps" />
      {extra}
    </div>
  )
}
