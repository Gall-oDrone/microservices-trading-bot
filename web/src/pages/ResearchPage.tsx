import { Link, useSearchParams } from 'react-router'
import { useLedgerSearch, useStudies } from '../api/client'
import type { Study, StudyKind } from '../api/schemas'
import { Badge, CardSkeleton, Empty, ErrorState } from '../components/ui'
import { fmtDate, fmtMx, fmtUTC } from '../lib/format'
import { KIND_LABEL, KIND_TONE, matchesStudy, shortName } from '../lib/research'
import { usePageTitle } from '../lib/usePageTitle'

const FILTERS: ('all' | StudyKind)[] = ['all', 'study', 'preregistration', 'report', 'assessment']

export function KindBadge({ kind }: { kind: StudyKind }) {
  return <Badge tone={KIND_TONE[kind]}>{KIND_LABEL[kind]}</Badge>
}

function StudyRow({ s, titles }: { s: Study; titles: Map<string, string> }) {
  const search = useLedgerSearch()
  return (
    <article className="card interactive study-row" data-testid={`study-${s.name}`} aria-labelledby={`st-${s.name}`}>
      <div className="study-date">
        <span className="num">{s.date ? fmtDate(s.date) : 'undated'}</span>
        <KindBadge kind={s.kind} />
      </div>
      <div className="study-body">
        <h2 className="study-title" id={`st-${s.name}`}>
          <Link to={{ pathname: `/research/${s.name}`, search }} id={`open-${s.name}`}>
            {s.title}
          </Link>
        </h2>
        {s.question && (
          <p className="study-question">
            <span className="q">Q</span>
            {s.question}
          </p>
        )}
        <p className="study-summary">{s.summary}</p>
        <div className="study-meta">
          {s.follows.map((f) => (
            <Link key={f} to={{ pathname: `/research/${f}`, search }} className="chip" title={titles.get(f) ?? f}>
              follows {shortName(f)}
            </Link>
          ))}
          {s.evidence && (
            <span className="chip" title={`${s.evidence.dir}: ${s.evidence.files.join(', ')}`}>
              evidence · {s.evidence.files.length} files
            </span>
          )}
          <span className="spacer" />
          <span className="faint num" title={`modified ${fmtUTC(s.modified)}`}>
            {s.file}
          </span>
        </div>
      </div>
    </article>
  )
}

export function ResearchPage() {
  usePageTitle('Research')
  const q = useStudies()
  const [params, setParams] = useSearchParams()
  const text = params.get('q') ?? ''
  const kind = (params.get('kind') ?? 'all') as 'all' | StudyKind
  const set = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value && value !== 'all') next.set(key, value)
    else next.delete(key)
    setParams(next, { replace: true })
  }

  const all = q.data?.studies ?? []
  const titles = new Map(all.map((s) => [s.name, s.title]))
  const counts = new Map<string, number>([['all', all.length]])
  for (const s of all) counts.set(s.kind, (counts.get(s.kind) ?? 0) + 1)
  const shown = all.filter((s) => (kind === 'all' || s.kind === kind) && matchesStudy(s, text))

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Research</h1>
          <p>
            Every study write-up, newest first: what was asked, what was found, and which study it builds on. Read only;
            the markdown in the repo is the source.
          </p>
        </div>
        {q.data && (
          <span className="faint" title={fmtUTC(q.data.generated_at)}>
            {q.data.dir} · updated {fmtMx(q.data.generated_at)}
          </span>
        )}
      </div>

      <div className="toolbar">
        <input
          className="input"
          type="search"
          id="study-search"
          placeholder="Search titles, questions and summaries"
          aria-label="Search studies"
          value={text}
          onChange={(e) => set('q', e.target.value)}
        />
        <div className="seg" role="group" aria-label="Kind">
          {FILTERS.map((k) => (
            <button key={k} aria-pressed={kind === k} onClick={() => set('kind', k)} id={`kind-${k}`}>
              {k === 'all' ? 'All' : KIND_LABEL[k]}
              <span className="faint num"> {counts.get(k) ?? 0}</span>
            </button>
          ))}
        </div>
      </div>

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
      {q.data && !q.data.found && (
        <div className="card">
          <Empty title="No studies folder">
            ui-api did not find {q.data.dir || 'a studies folder'}. Start it with -studies-dir pointing at
            docs/backtest-readiness.
          </Empty>
        </div>
      )}
      {q.data?.found && shown.length === 0 && (
        <div className="card">
          <Empty title="No study matches">Clear the search or pick another kind.</Empty>
        </div>
      )}
      {shown.length > 0 && (
        <div className="stack study-list" style={{ gap: 12 }}>
          {shown.map((s) => (
            <StudyRow key={s.name} s={s} titles={titles} />
          ))}
        </div>
      )}
    </>
  )
}
