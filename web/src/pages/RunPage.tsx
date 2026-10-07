import { useQueries } from '@tanstack/react-query'
import type { CSSProperties } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { fetchJSON, queryKeys, useLedgerSearch, useRun, useRuns, useStudies } from '../api/client'
import { runDocSchema, type RunDoc, type RunSummary, type RunWindow } from '../api/schemas'
import { Badge, Banner, CardSkeleton, ErrorState } from '../components/ui'
import { fmtMx, fmtUTC } from '../lib/format'
import { shortName } from '../lib/research'
import {
  bestRule,
  costDetail,
  costLabel,
  costSiblings,
  fmtPP,
  fmtRet,
  heat,
  HOLD,
  ruleLabel,
  windowLabel,
  windowOrder,
} from '../lib/runs'
import { usePageTitle } from '../lib/usePageTitle'

/** A heat-map cell's colour: teal above holding, coral below, log-scaled. */
function heatStyle(v: number, maxAbs: number): CSSProperties {
  return { '--heat': heat(v, maxAbs).toFixed(3) } as CSSProperties
}

/** Rules × windows: each rule's return minus buy-and-hold's, in percentage points. */
function Matrix({ doc, sel, onSelect }: { doc: RunDoc; sel: number; onSelect: (i: number) => void }) {
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
              {order.map((i) => (
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
                    {i === 0 && ws.length > 1 && <span className="matrix-tag">in-sample</span>}
                  </button>
                </th>
              ))}
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
                <th scope="row">{ruleLabel(rule)}</th>
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
                      title={`${ruleLabel(rule)} ${fmtRet(r.return_pct)} vs hold ${fmtPP(r.vs_hold_pp)} · ${r.round_trips} round trips`}
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

/** Every rule in one window, as in the evidence file, unrounded on hover. */
function WindowDetail({ w }: { w: RunWindow }) {
  const best = bestRule(w)
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
                <th scope="col" className="r">
                  Round trips
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
                <th scope="col">Random, same trips</th>
              </tr>
            </thead>
            <tbody>
              {w.results.map((r) => (
                <tr key={r.rule} className={r.rule === HOLD ? 'hold' : r === best ? 'best' : ''}>
                  <th scope="row">
                    {ruleLabel(r.rule)}
                    {r === best && r.vs_hold_pp > 0 && (
                      <span className="faint" style={{ marginLeft: 6 }}>
                        best
                      </span>
                    )}
                  </th>
                  <td className="num r" title={String(r.return_pct)}>
                    {fmtRet(r.return_pct)}
                  </td>
                  <td className={`num r ${r.vs_hold_pp > 0 ? 'pos' : r.vs_hold_pp < 0 ? 'neg' : ''}`}>
                    {r.rule === HOLD ? '—' : fmtPP(r.vs_hold_pp)}
                  </td>
                  <td className="num r">{r.round_trips}</td>
                  <td className="num r">{r.exposure_pct.toFixed(1)}%</td>
                  <td className="num r">{r.max_dd_pct.toFixed(2)}%</td>
                  <td className="num r" title="Total commission and slippage, % of starting equity">
                    {r.cost_pct.toFixed(2)}%
                  </td>
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
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

/**
 * The same data and windows at every cost level in this evidence folder: the
 * question every study in the series ends on (fees × trade count).
 */
function CostSensitivity({ run, siblings, window }: { run: RunSummary; siblings: RunSummary[]; window: RunWindow }) {
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
                  {ruleLabel(r)}
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
  const nWin = doc?.report.windows.length ?? 0
  const raw = Number(params.get('w') ?? '0')
  const sel = Number.isInteger(raw) && raw >= 0 && raw < nWin ? raw : 0
  const select = (i: number) => {
    const next = new URLSearchParams(params)
    if (i === 0) next.delete('w')
    else next.set('w', String(i))
    setParams(next, { replace: true })
  }
  const siblings = doc ? costSiblings(doc.run, list.data?.runs ?? []) : []

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
      {doc && (
        <div className="stack" style={{ gap: 16 }}>
          {nWin > 1 && (
            <Banner tone="info" title="How to read the windows">
              Every window is run independently with the same fixed rules. The tool labels the first window in-sample
              and the rest out-of-sample; a window that spans every year is not out-of-sample. The citing study has the
              design.
            </Banner>
          )}
          <Matrix doc={doc} sel={sel} onSelect={select} />
          <WindowDetail w={doc.report.windows[sel]} />
          {siblings.length > 1 && (
            <CostSensitivity run={doc.run} siblings={siblings} window={doc.report.windows[sel]} />
          )}

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
                <li>
                  trend: close &gt; SMA({doc.report.params.sma}) · news: trailing {doc.report.params.news_window}-day
                  score &gt; {doc.report.params.news_threshold}
                </li>
                <li>{costDetail(doc.report.costs)}</li>
                <li>
                  random baseline: {doc.report.params.sims.toLocaleString('en-US')} strategies, seed{' '}
                  {doc.report.params.seed}
                </li>
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
