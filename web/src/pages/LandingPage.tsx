/**
 * Landing page (`/`): what the bot does, where the frozen rule stands today,
 * how it is judged, the research behind it and the guardrails around it.
 * Every number comes from ui-api (the same contracts as the console); the
 * static copy only explains. The console itself starts at /forward-tests.
 */
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { useCandles, useForwardTests, useHealth, useLedgerSearch, useRisk, useRuns, useStudies } from '../api/client'
import { useLiveStream, type LiveState } from '../api/live'
import type { ForwardTest, RiskResponse, Study } from '../api/schemas'
import { ThemeToggle } from '../components/AppShell'
import { IconArrowRight, IconExternal, IconLogo } from '../components/icons'
import { LiveBadge, TickPrice } from '../components/live'
import { TrendChart } from '../components/TrendChart'
import { Badge, SignalPill, Skeleton } from '../components/ui'
import { bookLabel, fmtBps, fmtDate, fmtFrac, fmtPct, fmtPrice } from '../lib/format'
import { KIND_LABEL, KIND_TONE } from '../lib/research'
import { currentRun, trendGeometry } from '../lib/trend'
import { usePageTitle } from '../lib/usePageTitle'

const REPO = 'https://github.com/Gall-oDrone/microservices-trading-bot'

/** Fades a section in the first time it scrolls into view (immediately where IntersectionObserver is missing). */
function Reveal({ children, className = '', delay = 0 }: { children: ReactNode; className?: string; delay?: number }) {
  const ref = useRef<HTMLDivElement>(null)
  const [shown, setShown] = useState(() => typeof IntersectionObserver === 'undefined')
  useEffect(() => {
    if (shown || !ref.current) return
    const io = new IntersectionObserver(
      (es) => {
        if (es.some((e) => e.isIntersecting)) {
          setShown(true)
          io.disconnect()
        }
      },
      { rootMargin: '0px 0px -8% 0px' },
    )
    io.observe(ref.current)
    return () => io.disconnect()
  }, [shown])
  return (
    <div
      ref={ref}
      className={`lp-reveal ${shown ? 'is-in' : ''} ${className}`}
      style={delay ? { transitionDelay: `${delay}ms` } : undefined}
    >
      {children}
    </div>
  )
}

function SectionHead({
  id,
  kicker,
  title,
  children,
}: {
  id: string
  kicker: string
  title: string
  children?: ReactNode
}) {
  return (
    <header className="lp-section-head">
      <p className="lp-kicker">{kicker}</p>
      <h2 id={id}>{title}</h2>
      {children && <p className="lp-section-lede">{children}</p>}
    </header>
  )
}

function ConsoleLink({ id, children, ghost }: { id: string; children: ReactNode; ghost?: boolean }) {
  const search = useLedgerSearch()
  return (
    <Link to={{ pathname: '/forward-tests', search }} className={`lp-btn ${ghost ? 'ghost' : 'primary'}`} id={id}>
      {children}
      <IconArrowRight />
    </Link>
  )
}

// ---------------------------------------------------------------- nav

function LandingNav() {
  const [scrolled, setScrolled] = useState(false)
  useEffect(() => {
    const on = () => setScrolled(window.scrollY > 8)
    on()
    window.addEventListener('scroll', on, { passive: true })
    return () => window.removeEventListener('scroll', on)
  }, [])
  return (
    <header className={`lp-nav ${scrolled ? 'is-scrolled' : ''}`}>
      <div className="lp-wrap lp-nav-inner">
        <a href="#top" className="lp-brand" id="lp-brand">
          <span className="brand-mark">
            <IconLogo />
          </span>
          <span className="lp-brand-name">Trading Bot</span>
        </a>
        <nav className="lp-nav-links" aria-label="Sections">
          <a href="#today" id="lp-nav-today">
            Today
          </a>
          <a href="#how" id="lp-nav-how">
            How it works
          </a>
          <a href="#research" id="lp-nav-research">
            Research
          </a>
          <a href="#guardrails" id="lp-nav-guardrails">
            Guardrails
          </a>
        </nav>
        <div className="lp-nav-actions">
          <ThemeToggle />
          <ConsoleLink id="nav-open-console">Console</ConsoleLink>
        </div>
      </div>
    </header>
  )
}

// ---------------------------------------------------------------- hero

function HeroCard({ books, live, loading }: { books: ForwardTest[]; live: LiveState; loading: boolean }) {
  const [picked, setPicked] = useState('')
  const options = books.length ? books.map((b) => b.book) : ['btc_mxn', 'btc_usd']
  const book = picked && options.includes(picked) ? picked : options[0]
  const ft = books.find((b) => b.book === book)
  const quote = ft?.quote ?? book.split('_')[1] ?? ''
  const q = useCandles(book, 365)
  const candles = useMemo(() => q.data?.candles ?? [], [q.data])
  const g = useMemo(() => trendGeometry(candles), [candles])
  const run = useMemo(() => currentRun(candles), [candles])
  const snap = live.books[book]
  const hasLive = snap != null && snap.last > 0
  const lastClose = candles.length ? candles[candles.length - 1].close : null
  const signal = ft?.recorded_at ? ft.decision.signal : ''

  return (
    <article className="lp-hero-card" aria-label={`${bookLabel(book)} trend`} data-testid="hero-card">
      <header className="lp-hc-head">
        <div className="seg" role="group" aria-label="Book">
          {options.map((b) => (
            <button key={b} type="button" id={`hero-book-${b}`} aria-pressed={b === book} onClick={() => setPicked(b)}>
              {bookLabel(b)}
            </button>
          ))}
        </div>
        <LiveBadge live={live} />
      </header>

      <div className="lp-hc-price">
        <div className="stat">
          <span className="stat-label">{hasLive ? 'Last trade on Bitso' : 'Last daily close'}</span>
          <span className="lp-price num">
            {hasLive ? (
              <TickPrice value={snap.last} quote={quote} />
            ) : lastClose != null ? (
              fmtPrice(lastClose, quote)
            ) : (
              '—'
            )}
            <span className="lp-quote">{quote.toUpperCase()}</span>
          </span>
        </div>
        <div className="lp-hc-signal">
          {loading ? <Skeleton h={28} w={88} /> : <SignalPill signal={signal} />}
          {run && (
            <span className="faint" data-testid="hero-run">
              {run.state} since {fmtDate(run.since)} · {run.bars} {run.bars === 1 ? 'day' : 'days'}
            </span>
          )}
        </div>
      </div>

      {q.isLoading ? (
        <div className="lp-hc-chart-skel">
          <Skeleton h={250} />
        </div>
      ) : q.isError ? (
        <div className="trend-empty">
          <strong>Chart unavailable</strong>
          <span className="faint">{(q.error as Error).message}</span>
        </div>
      ) : (
        <TrendChart candles={candles} quote={quote} label={`${bookLabel(book)} daily closes`} />
      )}

      <dl className="lp-hc-stats">
        <div>
          <dt>Price change</dt>
          <dd className={`num ${g ? (g.changePct >= 0 ? 'pos' : 'neg') : ''}`}>{g ? fmtPct(g.changePct, 1) : '—'}</dd>
          <dd className="faint">over {candles.length || '—'} daily bars</dd>
        </div>
        <div>
          <dt>Days the rule was long</dt>
          <dd className="num">{g ? fmtFrac(g.longShare, 0) : '—'}</dd>
          <dd className="faint">same period</dd>
        </div>
        <div>
          <dt>Paper vs buy-and-hold</dt>
          <dd className="num">{ft?.recorded_at ? fmtPct(ft.excess_vs_hold_pct) : '—'}</dd>
          <dd className="faint">{ft ? `since ${fmtDate(ft.milestones.forward_start)}` : 'forward window'}</dd>
        </div>
      </dl>
    </article>
  )
}

function Hero({ books, live, loading }: { books: ForwardTest[]; live: LiveState; loading: boolean }) {
  const m = books[0]?.milestones
  const total = m ? m.days_elapsed + m.days_to_evaluation : 0
  return (
    <section className="lp-hero" aria-labelledby="lp-title" id="top">
      <div className="lp-wrap lp-hero-grid">
        <div className="lp-hero-copy">
          <p className="lp-kicker">SMA50 forward test · Bitso</p>
          <h1 id="lp-title">
            One frozen trend rule,
            <br />
            tested in the open.
          </h1>
          <p className="lp-lede">
            The bot trades a single pre-registered rule: hold bitcoin while the daily close is above its 50-day average,
            stay in cash while it is below. Every paper and stage result sits next to buy-and-hold, after real fees and
            slippage.
          </p>
          <div className="lp-cta">
            <ConsoleLink id="cta-open-console">Open the console</ConsoleLink>
            <a href="#how" className="lp-btn ghost" id="cta-how">
              How it is judged
            </a>
          </div>
          <dl className="lp-facts">
            <div>
              <dt>Books</dt>
              <dd className="num">{books.length ? books.map((b) => bookLabel(b.book)).join(' · ') : '—'}</dd>
            </div>
            <div>
              <dt>Forward window</dt>
              <dd className="num">{m ? `day ${m.days_elapsed} of ${total}` : '—'}</dd>
            </div>
            <div>
              <dt>Decisions</dt>
              <dd>one a day, on the close</dd>
            </div>
          </dl>
        </div>
        <HeroCard books={books} live={live} loading={loading} />
      </div>
    </section>
  )
}

// ---------------------------------------------------------------- today

function BookTile({ ft }: { ft: ForwardTest }) {
  const search = useLedgerSearch()
  const d = ft.decision
  const p = ft.paper
  const recorded = ft.recorded_at !== ''
  const above = ft.distance_to_sma_pct >= 0
  return (
    <article className="lp-book" data-testid={`lp-book-${ft.book}`} aria-labelledby={`lp-book-${ft.book}-title`}>
      <header className="lp-book-head">
        <div>
          <h3 id={`lp-book-${ft.book}-title`}>{bookLabel(ft.book)}</h3>
          <span className="faint">
            {ft.mode === 'stage' ? 'Paper + Bitso stage' : 'Paper (dry run)'}
            {recorded && ` · bar ${fmtDate(d.bar_date)}`}
          </span>
        </div>
        <SignalPill signal={recorded ? d.signal : ''} />
      </header>
      {recorded ? (
        <>
          <p className="lp-book-big">
            <span className={`num ${above ? 'pos' : 'neg'}`}>{fmtPct(Math.abs(ft.distance_to_sma_pct), 1, false)}</span>{' '}
            {above ? 'above' : 'below'} the 50-day average
          </p>
          <dl className="lp-book-rows">
            <div>
              <dt>Close / SMA50</dt>
              <dd className="num">
                {fmtPrice(d.close, ft.quote)} <span className="faint">/ {fmtPrice(d.sma50, ft.quote)}</span>
              </dd>
            </div>
            <div>
              <dt>Paper equity / hold</dt>
              <dd className="num">
                {p.equity.toFixed(4)} <span className="faint">/ {p.hold_equity.toFixed(4)}</span>
              </dd>
            </div>
            <div>
              <dt>Max drawdown</dt>
              <dd className="num">{fmtFrac(p.max_drawdown)}</dd>
            </div>
            <div>
              <dt>Next open ({fmtDate(d.fill_date)})</dt>
              <dd className="lp-action">{p.pending_action}</dd>
            </div>
          </dl>
        </>
      ) : (
        <p className="muted lp-book-empty">No records in the {ft.ledger} ledger yet.</p>
      )}
      <Link to={{ pathname: `/forward-tests/${ft.book}`, search }} className="lp-link" id={`lp-open-${ft.book}`}>
        Open {bookLabel(ft.book)} <IconArrowRight />
      </Link>
    </article>
  )
}

function Today({ books, loading, error }: { books: ForwardTest[]; loading: boolean; error: Error | null }) {
  return (
    <section className="lp-section" aria-labelledby="today-title" id="today">
      <div className="lp-wrap">
        <Reveal>
          <SectionHead id="today-title" kicker="Today" title="Where the rule stands">
            The last recorded decision for each pre-registered book, from the executor's ledger. Prices update once a
            day, after the Mexico City close.
          </SectionHead>
        </Reveal>
        {loading && (
          <div className="lp-books">
            <Skeleton h={260} />
            <Skeleton h={260} />
          </div>
        )}
        {error && (
          <p className="lp-offline" role="status">
            The console backend is not answering ({error.message}). Start ui-api to see live numbers.
          </p>
        )}
        {books.length > 0 && (
          <div className="lp-books">
            {books.map((b, i) => (
              <Reveal key={b.book} delay={i * 80}>
                <BookTile ft={b} />
              </Reveal>
            ))}
          </div>
        )}
      </div>
    </section>
  )
}

// ---------------------------------------------------------------- how it works

const STEPS: { title: string; body: string }[] = [
  {
    title: 'Freeze the rule',
    body: 'The rule, the books, the costs and the pass/fail test were written down before the forward window opened. Nothing is tuned afterwards.',
  },
  {
    title: 'Decide once a day',
    body: 'After each Mexico City day closes, the executor compares the close with its 50-day average and records long or flat. Intraday prices are display only.',
  },
  {
    title: 'Trade small, behind a check',
    body: 'When the signal changes, one small stage order goes to Bitso at the next open, after the risk check. The paper book follows the same decision.',
  },
  {
    title: 'Judge against holding',
    body: 'An interim look half-way and a verdict at the end of the year, both against buy-and-hold after the pre-registered costs.',
  },
]

function Timeline({ books }: { books: ForwardTest[] }) {
  const ref = books[0]?.milestones
  if (!ref) return null
  const total = ref.days_elapsed + ref.days_to_evaluation
  const interimAt = total > 0 ? ((ref.days_elapsed + ref.days_to_interim) / total) * 100 : 50
  return (
    <div className="lp-timeline" data-testid="lp-timeline">
      <div className="lp-tl-head">
        <h3>Forward window</h3>
        <span className="faint num">
          {fmtDate(ref.forward_start)} → {fmtDate(ref.evaluation)}
        </span>
      </div>
      <div className="lp-tl-rows">
        {books.map((b) => {
          const m = b.milestones
          const pct = Math.max(0.8, Math.min(100, m.window_progress * 100))
          return (
            <div className="lp-tl-row" key={b.book}>
              <span className="lp-tl-label">{bookLabel(b.book)}</span>
              <div
                className="lp-tl-track"
                role="progressbar"
                aria-label={`${bookLabel(b.book)} forward window`}
                aria-valuenow={Math.round(m.window_progress * 100)}
                aria-valuemin={0}
                aria-valuemax={100}
              >
                <span className="lp-tl-fill" style={{ width: `${pct}%` }} />
                <span className="lp-tl-now" style={{ left: `${pct}%` }} />
                <span className="lp-tl-tick" style={{ left: `${interimAt}%` }} />
              </div>
              <span className="lp-tl-day num">day {m.days_elapsed}</span>
            </div>
          )
        })}
      </div>
      <div className="lp-tl-legend">
        <span>
          <b>Start</b> {fmtDate(ref.forward_start)}
        </span>
        <span>
          <b>Interim look</b> {fmtDate(ref.interim)}
        </span>
        <span>
          <b>Verdict</b> {fmtDate(ref.evaluation)}
        </span>
      </div>
    </div>
  )
}

function How({ books }: { books: ForwardTest[] }) {
  const search = useLedgerSearch()
  const prereg = books[0]?.prereg.replace(/\.md$/, '')
  return (
    <section className="lp-section" aria-labelledby="how-title" id="how">
      <div className="lp-wrap">
        <Reveal>
          <SectionHead id="how-title" kicker="How it works" title="Decided in advance, checked every day">
            The point is not to find a rule that looked good in the past. It is to find out, honestly, whether one
            frozen rule beats simply holding bitcoin.
          </SectionHead>
        </Reveal>
        <ol className="lp-steps">
          {STEPS.map((s, i) => (
            <li key={s.title}>
              <Reveal delay={i * 70} className="lp-step">
                <span className="lp-step-n num">{String(i + 1).padStart(2, '0')}</span>
                <h3>{s.title}</h3>
                <p>{s.body}</p>
              </Reveal>
            </li>
          ))}
        </ol>
        <Reveal>
          <Timeline books={books} />
        </Reveal>
        {prereg && (
          <p className="lp-note">
            Read the{' '}
            <Link to={{ pathname: `/research/${prereg}`, search }} id="lp-prereg">
              pre-registration
            </Link>{' '}
            for the exact rule, costs and pass/fail test.
          </p>
        )}
      </div>
    </section>
  )
}

// ---------------------------------------------------------------- research

function StudyRow({ s, search }: { s: Study; search: string }) {
  return (
    <li>
      <Link to={{ pathname: `/research/${s.name}`, search }} className="lp-study" id={`lp-study-${s.name}`}>
        <span className="lp-study-date num">{fmtDate(s.date)}</span>
        <span className="lp-study-body">
          <span className="lp-study-title">
            {s.title}
            <Badge tone={KIND_TONE[s.kind]}>{KIND_LABEL[s.kind]}</Badge>
          </span>
          <span className="lp-study-summary">{s.question || s.summary}</span>
        </span>
        <IconArrowRight className="lp-study-go" />
      </Link>
    </li>
  )
}

function Research() {
  const search = useLedgerSearch()
  const studies = useStudies()
  const runs = useRuns()
  const list = studies.data?.studies ?? []
  const latest = [...list].sort((a, b) => b.date.localeCompare(a.date)).slice(0, 3)
  const preregs = list.filter((s) => s.kind === 'preregistration').length
  return (
    <section className="lp-section" aria-labelledby="research-title" id="research">
      <div className="lp-wrap lp-split">
        <Reveal>
          <SectionHead id="research-title" kicker="Research" title="Every claim has a write-up">
            Studies, pre-registrations and backtest runs live in the repo and open in the console, with development and
            holdout windows kept apart.
          </SectionHead>
          <dl className="lp-counts">
            <div>
              <dt>studies and reports</dt>
              <dd className="num">{studies.data ? list.length : '—'}</dd>
            </div>
            <div>
              <dt>pre-registrations</dt>
              <dd className="num">{studies.data ? preregs : '—'}</dd>
            </div>
            <div>
              <dt>backtest runs</dt>
              <dd className="num">{runs.data ? runs.data.runs.length : '—'}</dd>
            </div>
          </dl>
          <div className="lp-cta">
            <Link to={{ pathname: '/research', search }} className="lp-btn ghost" id="lp-browse-research">
              Browse research <IconArrowRight />
            </Link>
            <Link to={{ pathname: '/research/runs', search }} className="lp-link" id="lp-browse-runs">
              Backtest runs <IconArrowRight />
            </Link>
          </div>
        </Reveal>
        <Reveal delay={80}>
          <div className="lp-panel">
            <h3 className="lp-panel-title">Latest write-ups</h3>
            {studies.isLoading && (
              <div className="stack" style={{ gap: 10 }}>
                <Skeleton h={58} />
                <Skeleton h={58} />
                <Skeleton h={58} />
              </div>
            )}
            {studies.isError && <p className="faint">Research index unavailable: {studies.error.message}</p>}
            {latest.length > 0 && (
              <ul className="lp-studies" data-testid="lp-studies">
                {latest.map((s) => (
                  <StudyRow key={s.name} s={s} search={search} />
                ))}
              </ul>
            )}
          </div>
        </Reveal>
      </div>
    </section>
  )
}

// ---------------------------------------------------------------- guardrails

function Guardrails() {
  const q = useRisk()
  const search = useLedgerSearch()
  const r: RiskResponse | undefined = q.data
  const limits = r ? (r.books[0]?.limits ?? r.policy.default) : null
  const rows: { k: string; v: ReactNode; sub?: ReactNode }[] = r
    ? [
        {
          k: 'Pre-trade check',
          v: <span className={r.enforcement === 'enforced' ? 'pos' : 'warn-text'}>{r.enforcement}</span>,
          sub: 'before every stage order; a blocked order is skipped and recorded',
        },
        {
          k: 'Halt',
          v: r.halted ? <span className="neg">halted</span> : <span className="pos">not halted</span>,
          sub: r.halted ? r.halt_reason : 'policy and halt file both clear',
        },
        {
          k: 'Stage order size',
          v: <span className="num">{r.stage_size_btc} BTC</span>,
          sub: 'fixed per signal change',
        },
        {
          k: 'Position cap',
          v: <span className="num">{limits?.max_position_btc} BTC</span>,
          sub: `${limits?.max_orders_per_day} order a day at most`,
        },
        {
          k: 'Price guard',
          v: <span className="num">{fmtBps(limits?.max_price_deviation_bps ?? 0)}</span>,
          sub: 'largest allowed gap from the reference price',
        },
        ...r.books.map((b) => {
          const c = b.realized_cost
          const over = c.legs > 0 && c.avg_total_bps > c.assumed_leg_bps
          return {
            k: `Cost per leg, ${bookLabel(b.book)}`,
            v:
              c.legs > 0 ? (
                <span className={`num ${over ? 'warn-text' : ''}`}>{fmtBps(c.avg_total_bps)}</span>
              ) : (
                <span className="faint">no fills yet</span>
              ),
            sub: `${c.legs} ${c.legs === 1 ? 'leg' : 'legs'} realized · ${fmtBps(c.assumed_leg_bps)} assumed`,
          }
        }),
      ]
    : []
  return (
    <section className="lp-section" aria-labelledby="guardrails-title" id="guardrails">
      <div className="lp-wrap lp-split reverse">
        <Reveal>
          <SectionHead id="guardrails-title" kicker="Guardrails" title="Small, capped and stoppable">
            Real orders are tiny on purpose. A shared risk library checks every stage order against the policy, and
            realized costs are compared with what the pre-registration assumed.
          </SectionHead>
          <Link to={{ pathname: '/risk', search }} className="lp-btn ghost" id="lp-open-risk">
            Open the risk view <IconArrowRight />
          </Link>
        </Reveal>
        <Reveal delay={80}>
          <div className="lp-panel">
            <h3 className="lp-panel-title">
              Risk policy <span className="faint num">{r?.policy.version}</span>
            </h3>
            {q.isLoading && <Skeleton h={240} />}
            {q.isError && <p className="faint">Risk policy unavailable: {q.error.message}</p>}
            {rows.length > 0 && (
              <dl className="lp-guards" data-testid="lp-guards">
                {rows.map((row) => (
                  <div key={row.k}>
                    <dt>{row.k}</dt>
                    <dd>{row.v}</dd>
                    {row.sub && <dd className="faint lp-guard-sub">{row.sub}</dd>}
                  </div>
                ))}
              </dl>
            )}
          </div>
        </Reveal>
      </div>
    </section>
  )
}

// ---------------------------------------------------------------- closing

function Closing() {
  const h = useHealth()
  const up = h.isSuccess && h.data.status === 'ok'
  return (
    <>
      <section className="lp-section lp-closing" aria-labelledby="closing-title">
        <div className="lp-wrap">
          <Reveal className="lp-closing-card">
            <h2 id="closing-title">See today's decision.</h2>
            <p>Forward tests, fills, risk limits and the research, in one read-only console.</p>
            <div className="lp-cta" style={{ justifyContent: 'center' }}>
              <ConsoleLink id="closing-open-console">Open the console</ConsoleLink>
            </div>
          </Reveal>
        </div>
      </section>
      <footer className="lp-footer">
        <div className="lp-wrap lp-footer-inner">
          <span className="row" style={{ gap: 8 }} id="lp-health">
            <span className={`live-dot ${up ? '' : 'down'}`} />
            <span className="muted">
              {up
                ? `ui-api connected · ${h.data.records} ledger records`
                : h.isLoading
                  ? 'connecting…'
                  : 'ui-api offline'}
            </span>
          </span>
          <span className="faint lp-disclaimer">
            Display only: live prices never feed the executor. Paper and stage results are research, not investment
            advice.
          </span>
          <a href={REPO} target="_blank" rel="noreferrer" className="lp-link" id="lp-repo">
            Source <IconExternal />
          </a>
        </div>
      </footer>
    </>
  )
}

// ---------------------------------------------------------------- page

export function LandingPage() {
  usePageTitle('SMA50 forward tests on Bitso')
  const q = useForwardTests()
  const books = useMemo(() => q.data?.books ?? [], [q.data])
  const live = useLiveStream(books.map((b) => b.book))
  return (
    <div className="lp">
      <LandingNav />
      <main id="main">
        <Hero books={books} live={live} loading={q.isLoading} />
        <Today books={books} loading={q.isLoading} error={q.error} />
        <How books={books} />
        <Research />
        <Guardrails />
        <Closing />
      </main>
    </div>
  )
}
