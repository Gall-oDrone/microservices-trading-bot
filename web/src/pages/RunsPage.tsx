import { Link, useSearchParams } from 'react-router'
import { useLedgerSearch, useRuns, useStudies } from '../api/client'
import type { RunSummary } from '../api/schemas'
import { Banner, CardSkeleton, Empty, ErrorState } from '../components/ui'
import { fmtDate, fmtMx, fmtUTC } from '../lib/format'
import { shortName } from '../lib/research'
import { costLabel, HOLD, matchesRun, ruleLabel } from '../lib/runs'
import { usePageTitle } from '../lib/usePageTitle'

type CostFilter = 'all' | 'costs' | 'frictionless'
const COST_FILTERS: { k: CostFilter; label: string }[] = [
  { k: 'all', label: 'All' },
  { k: 'costs', label: 'With costs' },
  { k: 'frictionless', label: 'Frictionless' },
]

/** One bar per rule: in how many windows it beat buy-and-hold. */
function Scores({ run }: { run: RunSummary }) {
  return (
    <div className="run-scores">
      {run.scores
        .filter((s) => s.rule !== HOLD)
        .map((s) => {
          const frac = s.windows ? s.beats_hold / s.windows : 0
          return (
            <div
              key={s.rule}
              className="run-score"
              title={`${ruleLabel(s.rule)} beat holding in ${s.beats_hold} of ${s.windows} windows`}
            >
              <span className="run-score-name">{ruleLabel(s.rule)}</span>
              <span className="run-score-bar" aria-hidden>
                <span style={{ width: `${Math.round(frac * 100)}%` }} className={frac >= 0.5 ? 'up' : 'down'} />
              </span>
              <span className="num faint">
                {s.beats_hold}/{s.windows}
              </span>
            </div>
          )
        })}
    </div>
  )
}

function RunRow({ run }: { run: RunSummary }) {
  const search = useLedgerSearch()
  const d = run.data
  return (
    <article className="card interactive run-row" data-testid={`run-${run.id}`} aria-labelledby={`rn-${run.id}`}>
      <div className="run-main">
        <h3 className="run-name" id={`rn-${run.id}`}>
          <Link to={{ pathname: `/research/runs/${run.id}`, search }} id={`open-run-${run.name}-${run.date}`}>
            {run.name}
          </Link>
        </h3>
        <div className="study-meta">
          <span className={`chip ${run.costs.round_trip_bps === 0 ? 'chip-flat' : 'chip-warn'}`}>
            {costLabel(run.costs)}
          </span>
          <span className="chip" title={`${d.first} → ${d.last}${d.news ? ` · news: ${d.news_days} days` : ''}`}>
            {d.book ? `${d.book} · ` : ''}
            {d.prices} · {d.bars.toLocaleString('en-US')} bars
          </span>
          <span className="chip">
            {run.windows.length} window{run.windows.length === 1 ? '' : 's'}
          </span>
        </div>
      </div>
      <Scores run={run} />
    </article>
  )
}

export function RunsPage() {
  usePageTitle('Backtest runs')
  const q = useRuns()
  const studies = useStudies()
  const search = useLedgerSearch()
  const [params, setParams] = useSearchParams()
  const text = params.get('q') ?? ''
  const cost = (params.get('cost') ?? 'all') as CostFilter
  const set = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value && value !== 'all') next.set(key, value)
    else next.delete(key)
    setParams(next, { replace: true })
  }

  const titles = new Map((studies.data?.studies ?? []).map((s) => [s.name, s.title]))
  const all = q.data?.runs ?? []
  const shown = all.filter(
    (r) => (cost === 'all' || (cost === 'frictionless') === (r.costs.round_trip_bps === 0)) && matchesRun(r, text),
  )
  const groups = new Map<string, RunSummary[]>()
  for (const r of shown) groups.set(r.date, [...(groups.get(r.date) ?? []), r])

  return (
    <>
      <div className="page-head">
        <div>
          <div className="crumbs">
            <Link to={{ pathname: '/research', search }}>Research</Link> <span>/</span> <span>Backtest runs</span>
          </div>
          <h1>Backtest runs</h1>
          <p>
            The research tools' machine-readable results (<code>-json</code>), grouped by evidence folder. Every number
            is the one in the committed evidence file, after the costs shown.
          </p>
        </div>
        {q.data && (
          <span className="faint" title={fmtUTC(q.data.generated_at)}>
            {all.length} runs · updated {fmtMx(q.data.generated_at)}
          </span>
        )}
      </div>

      <div className="toolbar">
        <input
          className="input"
          type="search"
          id="run-search"
          placeholder="Search runs, books and studies"
          aria-label="Search runs"
          value={text}
          onChange={(e) => set('q', e.target.value)}
        />
        <div className="seg" role="group" aria-label="Costs">
          {COST_FILTERS.map(({ k, label }) => (
            <button key={k} aria-pressed={cost === k} onClick={() => set('cost', k)} id={`cost-${k}`}>
              {label}
            </button>
          ))}
        </div>
      </div>

      {q.data && q.data.skipped.length > 0 && (
        <Banner tone="warn" title={`${q.data.skipped.length} report file(s) could not be read`}>
          {q.data.skipped.map((s) => `${s.file}: ${s.error}`).join(' · ')}
        </Banner>
      )}
      {q.isLoading && (
        <div className="stack">
          <CardSkeleton lines={3} />
          <CardSkeleton lines={3} />
        </div>
      )}
      {q.isError && (
        <div className="card">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      )}
      {q.data && all.length === 0 && (
        <div className="card">
          <Empty title="No runs yet">
            Run a research tool with <code>-json evidence-&lt;date&gt;/&lt;name&gt;.json</code> next to its text output.
          </Empty>
        </div>
      )}
      {all.length > 0 && shown.length === 0 && (
        <div className="card">
          <Empty title="No run matches">Clear the search or pick another cost filter.</Empty>
        </div>
      )}
      {[...groups].map(([date, runs]) => {
        const cited = [...new Set(runs.flatMap((r) => r.studies))].sort()
        return (
          <section key={date} className="run-group" aria-labelledby={`grp-${date}`}>
            <div className="run-group-head">
              <h2 id={`grp-${date}`}>
                Evidence {fmtDate(date)} <span className="faint num">· {runs.length}</span>
              </h2>
              <div className="study-meta">
                {cited.map((s) => (
                  <Link key={s} to={{ pathname: `/research/${s}`, search }} className="chip" title={titles.get(s) ?? s}>
                    {shortName(s)}
                  </Link>
                ))}
              </div>
            </div>
            <div className="run-grid">
              {runs.map((r) => (
                <RunRow key={r.id} run={r} />
              ))}
            </div>
          </section>
        )
      })}
    </>
  )
}
