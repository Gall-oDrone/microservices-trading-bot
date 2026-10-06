import { useEffect, useRef, type MouseEvent } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router'
import { useLedgerSearch, useStudies, useStudy } from '../api/client'
import type { StudyDoc } from '../api/schemas'
import { Banner, CardSkeleton, ErrorState } from '../components/ui'
import { fmtDate, fmtUTC } from '../lib/format'
import { shortName } from '../lib/research'
import { usePageTitle } from '../lib/usePageTitle'
import { KindBadge } from './ResearchPage'

function Lineage({
  title,
  names,
  titles,
  id,
}: {
  title: string
  names: string[]
  titles: Map<string, string>
  id: string
}) {
  const search = useLedgerSearch()
  if (names.length === 0) return null
  return (
    <div className="lineage" data-testid={id}>
      <span className="stat-label">{title}</span>
      <ul>
        {names.map((n) => (
          <li key={n}>
            <Link to={{ pathname: `/research/${n}`, search }} title={titles.get(n) ?? n}>
              {shortName(n)}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  )
}

/**
 * The study body. The HTML comes from ui-api's goldmark renderer, which drops
 * raw HTML from the markdown and refuses javascript:/data: links, so it is
 * safe to inject. Links to other studies (data-study) route inside the app;
 * links to other repo files (data-local) are not served and only show a
 * tooltip with their path.
 */
function Prose({ doc }: { doc: StudyDoc }) {
  const ref = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()
  const search = useLedgerSearch()
  const { hash } = useLocation()

  useEffect(() => {
    const el = ref.current
    if (!el) return
    for (const a of el.querySelectorAll<HTMLAnchorElement>('a[data-local]')) {
      a.title = `${a.dataset.local} (repo file; not served by the UI)`
      a.tabIndex = -1
    }
    for (const t of el.querySelectorAll('table')) {
      if (t.parentElement?.classList.contains('table-wrap')) continue
      const wrap = document.createElement('div')
      wrap.className = 'table-wrap'
      t.replaceWith(wrap)
      wrap.appendChild(t)
    }
  }, [doc.html])

  useEffect(() => {
    if (!hash) {
      document.scrollingElement?.scrollTo?.({ top: 0 })
      return
    }
    document.getElementById(decodeURIComponent(hash.slice(1)))?.scrollIntoView?.({ block: 'start' })
  }, [doc.html, hash])

  const onClick = (e: MouseEvent<HTMLDivElement>) => {
    const a = (e.target as HTMLElement).closest('a')
    if (!a || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
    if (a.dataset.local !== undefined) {
      e.preventDefault()
      return
    }
    const study = a.dataset.study
    if (study) {
      e.preventDefault()
      const frag = a.getAttribute('href')?.split('#')[1]
      navigate({ pathname: `/research/${study}`, search, hash: frag ? `#${frag}` : '' })
    }
  }

  return (
    // The click handler only intercepts links; keyboard users follow them natively.
    <div
      ref={ref}
      className="prose"
      data-testid="study-prose"
      onClick={onClick}
      dangerouslySetInnerHTML={{ __html: doc.html }}
    />
  )
}

export function StudyPage() {
  const { name = '' } = useParams()
  const q = useStudy(name)
  const list = useStudies()
  const search = useLedgerSearch()
  const doc = q.data
  usePageTitle(doc ? doc.study.title : shortName(name))
  const titles = new Map((list.data?.studies ?? []).map((s) => [s.name, s.title]))

  return (
    <>
      <div className="page-head">
        <div style={{ minWidth: 0 }}>
          <div className="crumbs">
            <Link to={{ pathname: '/research', search }}>Research</Link> <span>/</span> <span>{shortName(name)}</span>
          </div>
          <h1>{doc?.study.title ?? shortName(name)}</h1>
          {doc && (
            <div className="row" style={{ gap: 10, marginTop: 8 }}>
              <KindBadge kind={doc.study.kind} />
              <span className="muted">{doc.study.date ? fmtDate(doc.study.date) : 'undated'}</span>
              <span className="faint num" title={`modified ${fmtUTC(doc.study.modified)}`}>
                {doc.study.file}
              </span>
            </div>
          )}
        </div>
      </div>

      {q.isLoading && <CardSkeleton lines={8} />}
      {q.isError && (
        <div className="card">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      )}
      {doc && (
        <div className="study-layout">
          <article className="card study-doc" aria-label={doc.study.title}>
            {doc.study.question && (
              <Banner tone="info" title="Question">
                {doc.study.question}
              </Banner>
            )}
            <Prose doc={doc} />
          </article>
          <aside className="study-aside">
            {doc.headings.length > 0 && (
              <nav className="card toc" aria-label="Contents">
                <span className="stat-label">Contents</span>
                <ul>
                  {doc.headings.map((h) => (
                    <li key={h.id} className={h.level === 3 ? 'sub' : ''}>
                      <a href={`#${h.id}`}>{h.text}</a>
                    </li>
                  ))}
                </ul>
              </nav>
            )}
            <div className="card stack" style={{ gap: 14 }}>
              <Lineage title="Builds on" names={doc.study.follows} titles={titles} id="lineage-follows" />
              <Lineage title="Followed by" names={doc.followed_by} titles={titles} id="lineage-followed-by" />
              <Lineage title="Links to" names={doc.study.references} titles={titles} id="lineage-references" />
              <Lineage title="Linked from" names={doc.referenced_by} titles={titles} id="lineage-referenced-by" />
              {doc.study.evidence ? (
                <div className="lineage">
                  <span className="stat-label">Evidence ({doc.study.evidence.dir})</span>
                  <ul className="num faint evidence">
                    {doc.study.evidence.files.map((f) => (
                      <li key={f}>{f}</li>
                    ))}
                  </ul>
                </div>
              ) : (
                <span className="faint">No evidence folder for this date.</span>
              )}
            </div>
          </aside>
        </div>
      )}
    </>
  )
}
