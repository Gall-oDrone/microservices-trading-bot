import { useQueries } from '@tanstack/react-query'
import type { CSSProperties } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { fetchJSON, queryKeys, useLedgerSearch, useRun, useRuns, useStudies } from '../api/client'
import { runDocSchema, type RunDoc, type RunReport, type RunSummary, type RunWindow } from '../api/schemas'
import { Badge, Banner, CardSkeleton, ErrorState } from '../components/ui'
import { fmtMx, fmtUTC } from '../lib/format'
import { shortName } from '../lib/research'
import {
  bestRule,
  compareOrder,
  costDetail,
  costLabel,
  costSiblings,
  fmtPP,
  fmtRet,
  heat,
  HOLD,
  holdoutIndex,
  ruleNames,
  verdict,
  windowLabel,
  windowOrder,
  windowTag,
  type CompareSort,
} from '../lib/runs'
import { usePageTitle } from '../lib/usePageTitle'

type Names = (rule: string) => string

/** A heat-map cell's colour: teal above holding, coral below, log-scaled. */
function heatStyle(v: number, maxAbs: number): CSSProperties {
  return { '--heat': heat(v, maxAbs).toFixed(3) } as CSSProperties
}

const signClass = (v: number) => (v > 0 ? 'pos' : v < 0 ? 'neg' : '')

/** Rules × windows: each rule's return minus buy-and-hold's, in percentage points. */
function Matrix({
  doc,
  sel,
  onSelect,
  names,
}: {
  doc: RunDoc
  sel: number
  onSelect: (i: number) => void
  names: Names
}) {
  const ws = doc.report.windows
  const order = windowOrder(ws)
  const rules = doc.run.scores.map((s) => s.rule).filter((r) => r !== HOLD)
  let maxAbs = 0
  for (const w of ws) for (const r of w.results) if (r.rule !== HOLD) maxAbs = Math.max(maxAbs, Math.abs(r.vs_hold_pp))
  const cell = (w: RunWindow, rule: string) => w.results.find((r) => r.rule === rule)

  return (
    <section className="card" aria-labelledby="matrix-h">
      <div className="card-head">
        <h2 id="matrix-h">Rule vs buy-and-hold, by window</h2>
        <span className="faint">return minus holding&apos;s, after costs · pick a window for its detail</span>
      </div>
      <div className="table-wrap">
        <table className="matrix" data-testid="run-matrix">
          <thead>
            <tr>
              <th scope="col">Rule</th>
              {order.map((i) => {
                const tag = windowTag(ws, i)
                return (
                  <th key={i} scope="col" className={i === sel ? 'sel' : ''}>
                    <button
                      type="button"
                      className="matrix-col"
                      aria-pressed={i === sel}
                      onClick={() => onSelect(i)}
                      id={`win-${i}`}
                      title={`${ws[i].from} → ${ws[i].to} · ${ws[i].bars} bars · labelled ${ws[i].label}`}
                    >
                      {windowLabel(ws[i])}
                      {tag && <span className={`matrix-tag ${tag}`}>{tag}</span>}
                    </button>
                  </th>
                )
              })}
            </tr>
          </thead>
          <tbody>
            <tr className="hold">
              <th scope="row">Buy and hold</th>
              {order.map((i) => {
                const h = cell(ws[i], HOLD)
                return (
                  <td key={i} className={`num ${i === sel ? 'sel' : ''}`}>
                    {h ? fmtRet(h.return_pct) : '—'}
                  </td>
                )
              })}
            </tr>
            {rules.map((rule) => (
              <tr key={rule}>
                <th scope="row">{names(rule)}</th>
                {order.map((i) => {
                  const r = cell(ws[i], rule)
                  if (!r)
                    return (
                      <td key={i} className={`num faint ${i === sel ? 'sel' : ''}`}>
                        —
                      </td>
                    )
                  return (
                    <td
                      key={i}
                      className={`num heat ${r.vs_hold_pp > 0 ? 'up' : r.vs_hold_pp < 0 ? 'down' : ''} ${i === sel ? 'sel' : ''}`}
                      style={heatStyle(r.vs_hold_pp, maxAbs)}
                      title={`${names(rule)} ${fmtRet(r.return_pct)} vs hold ${fmtPP(r.vs_hold_pp)} · ${r.round_trips} round trips`}
                    >
                      {fmtPP(r.vs_hold_pp)}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

const SORTS: { k: CompareSort; label: string }[] = [
  { k: 'table', label: 'Tool order' },
  { k: 'vs_hold', label: 'vs hold' },
  { k: 'sharpe', label: 'Sharpe' },
  { k: 'max_dd', label: 'Max DD' },
]

/** "2 / 2" with a tone: all windows, some, none. */
function Score({ n, of, what }: { n: number; of: number; what: string }) {
  const tone = n === of && of > 0 ? 'pos' : n === 0 ? 'neg' : 'warn'
  return (
    <span className={`score-pill ${tone}`} title={`${what} in ${n} of ${of} windows`}>
      {n}/{of}
    </span>
  )
}

/**
 * Every rule, each window side by side: the strategy comparison the plan asks
 * for (development vs holdout for weekly-research). Rankings use the last
 * window, the holdout when there is one, so nothing is chosen on development.
 */
function Comparison({ report, rules, names }: { report: RunReport; rules: string[]; names: Names }) {
  const [params, setParams] = useSearchParams()
  const ws = report.windows
  const hasSharpe = ws.some((w) => w.results.some((r) => r.sharpe !== undefined))
  const hasTrades = ws.some((w) => w.results.some((r) => r.trades !== undefined))
  const raw = params.get('sort') as CompareSort | null
  const sort: CompareSort = SORTS.some((s) => s.k === raw) && (raw !== 'sharpe' || hasSharpe) ? raw! : 'table'
  const setSort = (k: CompareSort) => {
    const next = new URLSearchParams(params)
    if (k === 'table') next.delete('sort')
    else next.set('sort', k)
    setParams(next, { replace: true })
  }
  const hi = holdoutIndex(ws)
  const rankOn = ws[hi >= 0 ? hi : ws.length - 1]
  const cols = 4 + Number(hasSharpe) + Number(hasTrades)

  return (
    <section className="card" aria-labelledby="compare-h" data-testid="run-compare">
      <div className="card-head">
        <h2 id="compare-h">{hi >= 0 ? 'Development vs holdout' : 'Windows side by side'}</h2>
        <div className="seg" role="group" aria-label="Sort rules">
          {SORTS.filter((s) => s.k !== 'sharpe' || hasSharpe).map((s) => (
            <button key={s.k} type="button" aria-pressed={sort === s.k} onClick={() => setSort(s.k)} id={`sort-${s.k}`}>
              {s.label}
            </button>
          ))}
        </div>
      </div>
      <p className="faint compare-note">
        After costs. {sort === 'table' ? 'In the order the tool prints them' : `Ranked on ${windowLabel(rankOn)}`}
        {hi >= 0 ? '; the variants were fixed before the holdout was looked at.' : '.'} Max DD in teal is below
        buy-and-hold&apos;s in that window.
      </p>
      <div className="table-wrap">
        <table className="data run-table compare">
          <thead>
            <tr className="grp">
              <th scope="col" rowSpan={2}>
                Rule
              </th>
              <th scope="colgroup" colSpan={2} className="grp-start">
                Windows
              </th>
              {ws.map((w, i) => (
                <th key={i} scope="colgroup" colSpan={cols} className={`grp-start ${i === hi ? 'holdout' : ''}`}>
                  {windowTag(ws, i) || windowLabel(w)}{' '}
                  <span className="faint num">
                    {w.from} → {w.to}
                  </span>
                </th>
              ))}
            </tr>
            <tr>
              <th scope="col" className="r grp-start" title="Windows where the rule returned more than buy-and-hold">
                Beat hold
              </th>
              <th scope="col" className="r" title="Windows where the rule's max drawdown was below buy-and-hold's">
                Lower DD
              </th>
              {ws.map((_, i) => (
                <Cols key={i} hasSharpe={hasSharpe} hasTrades={hasTrades} />
              ))}
            </tr>
          </thead>
          <tbody>
            {compareOrder(ws, rules, sort).map((rule) => {
              const v = verdict(rule, ws)
              const isHold = rule === HOLD
              return (
                <tr key={rule} className={isHold ? 'hold' : ''} data-testid={`cmp-${rule}`}>
                  <th scope="row">{names(rule)}</th>
                  <td className="num r grp-start">
                    {isHold ? '—' : <Score n={v.beatsHold} of={v.windows} what="Beat buy-and-hold" />}
                  </td>
                  <td className="num r">
                    {isHold ? '—' : <Score n={v.lowerDD} of={v.windows} what="Lower max drawdown than buy-and-hold" />}
                  </td>
                  {ws.map((w, i) => {
                    const r = w.results.find((x) => x.rule === rule)
                    const h = w.results.find((x) => x.rule === HOLD)
                    if (!r)
                      return (
                        <td key={i} colSpan={cols} className="faint grp-start">
                          —
                        </td>
                      )
                    return (
                      <Cells
                        key={i}
                        r={r}
                        holdDD={h?.max_dd_pct}
                        isHold={isHold}
                        hasSharpe={hasSharpe}
                        hasTrades={hasTrades}
                      />
                    )
                  })}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function Cols({ hasSharpe, hasTrades }: { hasSharpe: boolean; hasTrades: boolean }) {
  return (
    <>
      <th scope="col" className="r grp-start">
        Return
      </th>
      <th scope="col" className="r">
        vs hold
      </th>
      <th scope="col" className="r">
        Max DD
      </th>
      {hasSharpe && (
        <th scope="col" className="r" title="Daily returns, annualized with 365 days">
          Sharpe
        </th>
      )}
      {hasTrades && (
        <th scope="col" className="r" title="Every rebalance (round trips in brackets)">
          Trades
        </th>
      )}
      <th scope="col" className="r" title="Commission and slippage paid, % of starting equity">
        Costs
      </th>
    </>
  )
}

function Cells({
  r,
  holdDD,
  isHold,
  hasSharpe,
  hasTrades,
}: {
  r: RunWindow['results'][number]
  holdDD: number | undefined
  isHold: boolean
  hasSharpe: boolean
  hasTrades: boolean
}) {
  const ddBetter = !isHold && holdDD !== undefined && r.max_dd_pct < holdDD
  return (
    <>
      <td
        className="num r grp-start"
        title={
          r.return_zero_cost_pct !== undefined
            ? `${r.return_pct} · ${fmtRet(r.return_zero_cost_pct)} with the same trades and no costs`
            : String(r.return_pct)
        }
      >
        {fmtRet(r.return_pct)}
      </td>
      <td className={`num r ${signClass(r.vs_hold_pp)}`}>{isHold ? '—' : fmtPP(r.vs_hold_pp)}</td>
      <td className={`num r ${ddBetter ? 'pos' : ''}`}>{r.max_dd_pct.toFixed(1)}%</td>
      {hasSharpe && <td className="num r">{r.sharpe !== undefined ? r.sharpe.toFixed(2) : '—'}</td>}
      {hasTrades && (
        <td className="num r">
          {r.trades ?? r.round_trips} <span className="faint">({r.round_trips})</span>
        </td>
      )}
      <td className="num r">{r.cost_pct.toFixed(1)}%</td>
    </>
  )
}

/** Every rule in one window, as in the evidence file, unrounded on hover. */
function WindowDetail({ w, names }: { w: RunWindow; names: Names }) {
  const best = bestRule(w)
  const hasRandom = w.results.some((r) => r.random)
  const hasSharpe = w.results.some((r) => r.sharpe !== undefined)
  const hasTrades = w.results.some((r) => r.trades !== undefined)
  return (
    <section className="card" aria-labelledby="detail-h" data-testid="window-detail">
      <div className="card-head">
        <h2 id="detail-h">
          {windowLabel(w)}{' '}
          <span className="faint num">
            · {w.from} → {w.to} · {w.bars} bars
          </span>
        </h2>
        <Badge tone="flat" mono title="The tool's label; see the note above">
          {w.label}
        </Badge>
      </div>
      {w.gaps && (
        <Banner tone="warn" title="Missing days inside this window">
          {w.gaps}
        </Banner>
      )}
      {w.note ? (
        <p className="muted">{w.note}</p>
      ) : (
        <div className="table-wrap">
          <table className="data run-table">
            <thead>
              <tr>
                <th scope="col">Rule</th>
                <th scope="col" className="r">
                  Return
                </th>
                <th scope="col" className="r">
                  vs hold
                </th>
                {hasSharpe && (
                  <>
                    <th scope="col" className="r">
                      CAGR
                    </th>
                    <th scope="col" className="r">
                      Sharpe
                    </th>
                  </>
                )}
                <th scope="col" className="r">
                  {hasTrades ? 'Trades' : 'Round trips'}
                </th>
                <th scope="col" className="r">
                  Exposure
                </th>
                <th scope="col" className="r">
                  Max DD
                </th>
                <th scope="col" className="r">
                  Costs
                </th>
                {hasTrades && (
                  <th scope="col" className="r" title="Same trades with zero costs">
                    No-cost return
                  </th>
                )}
                {hasRandom && <th scope="col">Random, same trips</th>}
              </tr>
            </thead>
            <tbody>
              {w.results.map((r) => (
                <tr key={r.rule} className={r.rule === HOLD ? 'hold' : r === best ? 'best' : ''}>
                  <th scope="row">
                    {names(r.rule)}
                    {r === best && r.vs_hold_pp > 0 && (
                      <span className="faint" style={{ marginLeft: 6 }}>
                        best
                      </span>
                    )}
                  </th>
                  <td className="num r" title={String(r.return_pct)}>
                    {fmtRet(r.return_pct)}
                  </td>
                  <td className={`num r ${signClass(r.vs_hold_pp)}`}>{r.rule === HOLD ? '—' : fmtPP(r.vs_hold_pp)}</td>
                  {hasSharpe && (
                    <>
                      <td className="num r">{r.cagr_pct !== undefined ? `${r.cagr_pct.toFixed(1)}%` : '—'}</td>
                      <td className="num r">{r.sharpe !== undefined ? r.sharpe.toFixed(2) : '—'}</td>
                    </>
                  )}
                  <td className="num r" title={hasTrades ? `${r.round_trips} round trips from flat` : undefined}>
                    {hasTrades ? (r.trades ?? '—') : r.round_trips}
                  </td>
                  <td className="num r">{r.exposure_pct.toFixed(1)}%</td>
                  <td className="num r">{r.max_dd_pct.toFixed(2)}%</td>
                  <td className="num r" title="Total commission and slippage, % of starting equity">
                    {r.cost_pct.toFixed(2)}%
                  </td>
                  {hasTrades && (
                    <td className="num r faint">
                      {r.return_zero_cost_pct !== undefined ? fmtRet(r.return_zero_cost_pct) : '—'}
                    </td>
                  )}
                  {hasRandom && (
                    <td>
                      {r.random ? (
                        <span
                          className="beat"
                          title={`Beats ${r.random.beat_pct.toFixed(1)}% of ${r.random.sims} random strategies with the same number of round trips`}
                        >
                          <span className="beat-bar" aria-hidden>
                            <span
                              style={{ width: `${r.random.beat_pct}%` }}
                              className={r.random.beat_pct >= 50 ? 'up' : 'down'}
                            />
                          </span>
                          <span className="num">{r.random.beat_pct.toFixed(1)}%</span>
                        </span>
                      ) : (
                        <span className="faint">—</span>
                      )}
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

/** Forward returns after volume-spike days vs every day (weekly-research, base costs). */
function EventStudy({ w, roundTripBps }: { w: RunWindow; roundTripBps: number }) {
  const rows = w.events ?? []
  return (
    <section className="card" aria-labelledby="events-h" data-testid="event-study">
      <div className="card-head">
        <h2 id="events-h">Volume-spike event study</h2>
        <span className="faint">{windowLabel(w)} · enter next open, exit h days later</span>
      </div>
      <div className="table-wrap">
        <table className="data run-table">
          <thead>
            <tr>
              <th scope="col">Condition</th>
              <th scope="col" className="r">
                h
              </th>
              <th scope="col" className="r">
                n
              </th>
              <th scope="col" className="r">
                Mean
              </th>
              <th scope="col" className="r">
                Hit
              </th>
              <th scope="col" className="r" title="mean / (sd / √n); optimistic for h > 1 (overlapping windows)">
                t*
              </th>
              <th scope="col" className="r" title={`Mean minus one round trip (${roundTripBps} bps)`}>
                After costs
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((e, i) => (
              <tr key={i} className={e.condition.startsWith('all days') ? 'hold' : ''}>
                <th scope="row">{e.condition}</th>
                <td className="num r">{e.h}d</td>
                <td className="num r">{e.n}</td>
                <td className={`num r ${signClass(e.mean_pct)}`}>{e.mean_pct.toFixed(2)}%</td>
                <td className="num r">{e.hit_pct.toFixed(1)}%</td>
                <td className={`num r ${Math.abs(e.t) >= 2 ? '' : 'faint'}`}>{e.t.toFixed(2)}</td>
                <td className={`num r ${signClass(e.mean_after_costs_pct)}`}>{e.mean_after_costs_pct.toFixed(2)}%</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

/** The post-hoc volume threshold check: shown, but flagged as not used to choose k. */
function SensitivityCard({ w }: { w: RunWindow }) {
  const s = w.sensitivity!
  const maxRet = Math.max(...s.rows.map((r) => Math.abs(r.return_pct)), 1)
  return (
    <section className="card" aria-labelledby="sens-h" data-testid="sensitivity">
      <div className="card-head">
        <h2 id="sens-h">Volume threshold sensitivity</h2>
        {s.post_hoc && (
          <Badge tone="warn" title="Added after the main tables were seen; k stays at the pre-declared value">
            post hoc
          </Badge>
        )}
      </div>
      <p className="faint compare-note">
        SMA50 whose entries need volume ≥ k × the 20-day mean, {windowLabel(w)}. The pre-declared k is {s.chosen}.
      </p>
      <div className="table-wrap">
        <table className="data run-table">
          <thead>
            <tr>
              <th scope="col">k</th>
              <th scope="col">Return</th>
              <th scope="col" className="r">
                Max DD
              </th>
              <th scope="col" className="r">
                Sharpe
              </th>
              <th scope="col" className="r">
                Trades
              </th>
            </tr>
          </thead>
          <tbody>
            {s.rows.map((r) => (
              <tr key={r.k} className={r.k === s.chosen ? 'best' : r.k === 0 ? 'hold' : ''}>
                <th scope="row" className="num">
                  {r.k === 0 ? 'none (SMA50)' : `${r.k.toFixed(2)}×`}
                  {r.k === s.chosen && (
                    <span className="faint" style={{ marginLeft: 6 }}>
                      chosen
                    </span>
                  )}
                </th>
                <td>
                  <span className="sens-bar">
                    <span className="beat-bar" aria-hidden>
                      <span
                        style={{ width: `${(Math.abs(r.return_pct) / maxRet) * 100}%` }}
                        className={r.return_pct >= 0 ? 'up' : 'down'}
                      />
                    </span>
                    <span className="num">{fmtRet(r.return_pct)}</span>
                  </span>
                </td>
                <td className="num r">{r.max_dd_pct.toFixed(1)}%</td>
                <td className="num r">{r.sharpe.toFixed(2)}</td>
                <td className="num r">{r.trades}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

/**
 * The same data and windows at every cost level in this evidence folder: the
 * question every study in the series ends on (fees × trade count).
 */
function CostSensitivity({
  run,
  siblings,
  window,
  names,
}: {
  run: RunSummary
  siblings: RunSummary[]
  window: RunWindow
  names: Names
}) {
  const search = useLedgerSearch()
  const docs = useQueries({
    queries: siblings.map((s) => ({
      queryKey: queryKeys.run(s.id),
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        fetchJSON(`/research/runs/${encodeURIComponent(s.date)}/${encodeURIComponent(s.name)}`, runDocSchema, signal),
      staleTime: 60_000,
    })),
  })
  const rules = run.scores.map((s) => s.rule).filter((r) => r !== HOLD)
  const rows = siblings.map((s, i) => {
    const w = docs[i].data?.report.windows.find((x) => x.from === window.from && x.to === window.to)
    return { s, w, loading: docs[i].isLoading }
  })
  let maxAbs = 0
  for (const { w } of rows)
    for (const r of w?.results ?? []) if (r.rule !== HOLD) maxAbs = Math.max(maxAbs, Math.abs(r.vs_hold_pp))

  return (
    <section className="card" aria-labelledby="costs-h" data-testid="cost-sensitivity">
      <div className="card-head">
        <h2 id="costs-h">Same data and windows, other costs</h2>
        <span className="faint">{windowLabel(window)} · vs buy-and-hold at the same costs</span>
      </div>
      <div className="table-wrap">
        <table className="matrix">
          <thead>
            <tr>
              <th scope="col">Costs</th>
              <th scope="col">Hold</th>
              {rules.map((r) => (
                <th key={r} scope="col">
                  {names(r)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map(({ s, w, loading }) => (
              <tr key={s.id} className={s.id === run.id ? 'current' : ''}>
                <th scope="row">
                  {s.id === run.id ? (
                    <span title={costDetail(s.costs)}>{costLabel(s.costs)}</span>
                  ) : (
                    <Link to={{ pathname: `/research/runs/${s.id}`, search }} title={costDetail(s.costs)}>
                      {costLabel(s.costs)}
                    </Link>
                  )}
                  {s.costs.level && <span className="matrix-tag cost-level">{s.costs.level}</span>}
                </th>
                <td className="num">
                  {loading ? '…' : w ? fmtRet(w.results.find((r) => r.rule === HOLD)?.return_pct ?? 0) : '—'}
                </td>
                {rules.map((rule) => {
                  const r = w?.results.find((x) => x.rule === rule)
                  if (!r)
                    return (
                      <td key={rule} className="num faint">
                        {loading ? '…' : '—'}
                      </td>
                    )
                  return (
                    <td
                      key={rule}
                      className={`num heat ${r.vs_hold_pp > 0 ? 'up' : r.vs_hold_pp < 0 ? 'down' : ''}`}
                      style={heatStyle(r.vs_hold_pp, maxAbs)}
                      title={`${fmtRet(r.return_pct)} · ${r.round_trips} round trips · costs ${r.cost_pct.toFixed(2)}%`}
                    >
                      {fmtPP(r.vs_hold_pp)}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

/** Parameter lines that apply to this report (daily-research's news and random baseline only when used). */
function paramLines(r: RunReport): string[] {
  const p = r.params
  const out = [`trend: close > SMA(${p.sma})`]
  if (p.news_window > 0) out[0] += ` · news: trailing ${p.news_window}-day score > ${p.news_threshold}`
  if (p.volume_ratio_days) out.push(`volume ratio: day's volume / previous ${p.volume_ratio_days}-day mean`)
  if (p.vol_target) out.push(`vol target: ${(p.vol_target * 100).toFixed(0)}% annualized`)
  if (p.holdout_start) out.push(`holdout: ${p.holdout_start} → ${p.end ?? 'end'}`)
  out.push(costDetail(r.costs))
  if (p.sims > 0) out.push(`random baseline: ${p.sims.toLocaleString('en-US')} strategies, seed ${p.seed}`)
  return out
}

export function RunPage() {
  const { date = '', name = '' } = useParams()
  const q = useRun(date, name)
  const list = useRuns()
  const studies = useStudies()
  const search = useLedgerSearch()
  const [params, setParams] = useSearchParams()
  usePageTitle(`${name} · runs`)
  const doc = q.data
  const titles = new Map((studies.data?.studies ?? []).map((s) => [s.name, s.title]))
  const ws = doc?.report.windows ?? []
  const nWin = ws.length
  const hi = holdoutIndex(ws)
  // A holdout design opens on the holdout: that is the window to judge a variant on.
  const def = hi >= 0 ? hi : 0
  const raw = Number(params.get('w') ?? String(def))
  const sel = Number.isInteger(raw) && raw >= 0 && raw < nWin ? raw : def
  const select = (i: number) => {
    const next = new URLSearchParams(params)
    if (i === def) next.delete('w')
    else next.set('w', String(i))
    setParams(next, { replace: true })
  }
  const siblings = doc ? costSiblings(doc.run, list.data?.runs ?? []) : []
  const names = ruleNames(ws)
  const w = ws[sel]

  return (
    <>
      <div className="page-head">
        <div style={{ minWidth: 0 }}>
          <div className="crumbs">
            <Link to={{ pathname: '/research', search }}>Research</Link> <span>/</span>{' '}
            <Link to={{ pathname: '/research/runs', search }}>Backtest runs</Link> <span>/</span> <span>{date}</span>
          </div>
          <h1 className="mono-title">{name}</h1>
          {doc && (
            <div className="row" style={{ gap: 8, marginTop: 8, flexWrap: 'wrap' }}>
              <Badge tone={doc.run.costs.round_trip_bps === 0 ? 'flat' : 'warn'} title={costDetail(doc.run.costs)}>
                {costLabel(doc.run.costs)}
                {doc.report.costs.level ? ` · ${doc.report.costs.level}` : ''}
              </Badge>
              <span className="muted">
                {doc.run.data.book ? `${doc.run.data.book} · ` : ''}
                {doc.run.data.prices} · {doc.run.data.bars.toLocaleString('en-US')} daily bars {doc.run.data.first} →{' '}
                {doc.run.data.last}
                {doc.run.data.news ? ` · ${doc.run.data.news_days} news days` : ''}
              </span>
            </div>
          )}
        </div>
        {doc && (
          <span className="faint num" title={`generated ${fmtUTC(doc.report.generated_at)}`}>
            {doc.report.tool}
            {doc.report.commit ? ` @ ${doc.report.commit.slice(0, 7)}` : ''} · {fmtMx(doc.report.generated_at)}
          </span>
        )}
      </div>

      {q.isLoading && <CardSkeleton lines={8} />}
      {q.isError && (
        <div className="card">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      )}
      {doc && w && (
        <div className="stack" style={{ gap: 16 }}>
          {hi >= 0 ? (
            <Banner tone="info" title="Development and holdout">
              Every variant and parameter was fixed before the holdout ({ws[hi].from} → {ws[hi].to}) was examined, and
              all of them are reported for both windows. Judge a variant on the holdout; development only shows it was
              not tuned into a loss.
            </Banner>
          ) : (
            nWin > 1 && (
              <Banner tone="info" title="How to read the windows">
                Every window is run independently with the same fixed rules. The tool labels the first window in-sample
                and the rest out-of-sample; a window that spans every year is not out-of-sample. The citing study has
                the design.
              </Banner>
            )
          )}
          {nWin >= 2 && nWin <= 3 && (
            <Comparison report={doc.report} rules={doc.run.scores.map((s) => s.rule)} names={names} />
          )}
          <Matrix doc={doc} sel={sel} onSelect={select} names={names} />
          <WindowDetail w={w} names={names} />
          {Boolean(w.events?.length || w.sensitivity) && (
            <div className="run-extras">
              {w.events && w.events.length > 0 && <EventStudy w={w} roundTripBps={doc.report.costs.round_trip_bps} />}
              {w.sensitivity && <SensitivityCard w={w} />}
            </div>
          )}
          {siblings.length > 1 && <CostSensitivity run={doc.run} siblings={siblings} window={w} names={names} />}

          <section className="card run-facts" aria-label="Provenance">
            <div className="lineage">
              <span className="stat-label">Cited by</span>
              {doc.run.studies.length === 0 ? (
                <span className="faint">No study links to this run yet.</span>
              ) : (
                <ul>
                  {doc.run.studies.map((s) => (
                    <li key={s}>
                      <Link
                        to={{ pathname: `/research/${s}`, search }}
                        title={titles.get(s) ?? s}
                        data-testid={`run-study-${s}`}
                      >
                        {titles.get(s) ?? shortName(s)}
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div className="lineage">
              <span className="stat-label">Files</span>
              <ul className="num faint evidence">
                <li>{doc.run.file}</li>
                {doc.run.text && <li>{doc.run.text}</li>}
              </ul>
            </div>
            <div className="lineage">
              <span className="stat-label">Parameters</span>
              <ul className="num faint evidence">
                {paramLines(doc.report).map((l) => (
                  <li key={l}>{l}</li>
                ))}
              </ul>
            </div>
            <div className="lineage">
              <span className="stat-label">Command-line flags</span>
              <ul className="num faint evidence">
                {Object.entries(doc.report.flags).map(([k, v]) => (
                  <li key={k}>
                    -{k} {v}
                  </li>
                ))}
              </ul>
            </div>
          </section>
        </div>
      )}
    </>
  )
}
