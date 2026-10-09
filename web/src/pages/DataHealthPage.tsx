import type { CSSProperties, ReactNode } from 'react'
import { Link } from 'react-router'
import { useDataHealth, useLedgerSearch } from '../api/client'
import type { ArchiveBook, DataHealth, HealthCheck, HealthStatus, RunLog } from '../api/schemas'
import { IconAlert, IconBlock, IconCheck, IconInfo, IconMinus, IconPulse } from '../components/icons'
import { Badge, CardSkeleton, ErrorState, LedgerBadge, Stat } from '../components/ui'
import { bookLabel, fmtDate, fmtMx, fmtUTC } from '../lib/format'
import {
  areaStatus,
  countByStatus,
  flushTimeline,
  fmtBytes,
  fmtMinutes,
  minutesBetween,
  sortChecks,
  statusLabel,
  statusTone,
} from '../lib/health'
import { usePageTitle } from '../lib/usePageTitle'

function StatusIcon({ s, style }: { s: HealthStatus; style?: CSSProperties }) {
  const p = { style: { width: 16, height: 16, ...style }, 'aria-hidden': true } as const
  switch (s) {
    case 'ok':
      return <IconCheck {...p} />
    case 'warn':
      return <IconAlert {...p} />
    case 'fail':
      return <IconBlock {...p} />
    case 'off':
      return <IconMinus {...p} />
    default:
      return <IconInfo {...p} />
  }
}

function StatusBadge({ s, children }: { s: HealthStatus; children?: ReactNode }) {
  return (
    <Badge tone={statusTone(s)} dot>
      {children ?? statusLabel(s)}
    </Badge>
  )
}

const AREAS: { area: HealthCheck['area']; title: string; href: string }[] = [
  { area: 'collector', title: 'Collector', href: '#archive-h' },
  { area: 'archive', title: 'Archive', href: '#archive-h' },
  { area: 'executor', title: 'Daily-executor', href: '#executor-h' },
]

function Summary({ d }: { d: DataHealth }) {
  const n = countByStatus(d.checks)
  const headline =
    d.status === 'ok'
      ? 'Data is flowing'
      : d.status === 'fail'
        ? `${n.fail} check${n.fail === 1 ? '' : 's'} failing`
        : d.status === 'warn'
          ? `${n.warn} warning${n.warn === 1 ? '' : 's'}`
          : 'Status unknown'
  const areaMsg: Record<HealthCheck['area'], string> = {
    collector: d.collector.message,
    archive:
      d.archive.status === 'off'
        ? 'not configured'
        : (d.archive.books.find((b) => b.compacted.status !== 'ok')?.compacted.message ??
          (d.archive.books[0]?.compacted.message || d.archive.error || '')),
    executor: d.executor.message,
  }
  return (
    <section
      className={`card health-hero s-${d.status}`}
      aria-labelledby="health-summary-h"
      data-testid="health-summary"
    >
      <div className="health-orb" aria-hidden="true">
        <StatusIcon s={d.status} style={{ width: 30, height: 30 }} />
      </div>
      <div className="health-headline">
        <span className="stat-label">Overall</span>
        <h2 id="health-summary-h">{headline}</h2>
        <div className="row health-counts" style={{ gap: 6, flexWrap: 'wrap' }}>
          {(['fail', 'warn', 'unknown', 'ok', 'off'] as const)
            .filter((s) => n[s] > 0)
            .map((s) => (
              <Badge key={s} tone={statusTone(s)}>
                {n[s]} {statusLabel(s)}
              </Badge>
            ))}
        </div>
      </div>
      <div className="health-areas">
        {AREAS.map(({ area, title, href }) => {
          const s =
            area === 'collector'
              ? d.collector.status
              : area === 'executor'
                ? d.executor.status
                : (areaStatus(d.checks, area) ?? d.archive.status)
          return (
            <a key={area} href={href} className={`health-area s-${s}`} data-testid={`area-${area}`}>
              <span className="row" style={{ gap: 8 }}>
                <StatusIcon s={s} style={{ width: 14, height: 14 }} />
                <strong>{title}</strong>
                <span className="spacer" />
                <span className="faint">{statusLabel(s)}</span>
              </span>
              <span className="muted health-area-msg">{areaMsg[area]}</span>
            </a>
          )
        })}
      </div>
    </section>
  )
}

function Checks({ checks }: { checks: HealthCheck[] }) {
  return (
    <section className="card" aria-labelledby="checks-h" style={{ marginBottom: 16 }}>
      <div className="card-head">
        <div>
          <h2 id="checks-h">Checks</h2>
          <div className="sub">
            Worst first. &ldquo;Not configured&rdquo; does not count against the overall status.
          </div>
        </div>
      </div>
      <ul className="check-list" id="health-checks">
        {sortChecks(checks).map((c) => (
          <li key={c.id} className={`check-row s-${c.status}`} data-testid={`check-${c.id}`}>
            <span className="check-icon">
              <StatusIcon s={c.status} />
            </span>
            <span className="check-label">{c.label}</span>
            <span className="check-msg muted">{c.message}</span>
            <StatusBadge s={c.status} />
          </li>
        ))}
      </ul>
    </section>
  )
}

function FlushStrip({ b, refIso }: { b: ArchiveBook; refIso: string }) {
  const t = flushTimeline(b.raw.flushes, refIso)
  return (
    <div className="flush">
      <div
        className="flush-strip"
        role="img"
        aria-label={`${b.raw.flushes_24h} flushes in the last 24 hours, ${t.gaps.length} wait${t.gaps.length === 1 ? '' : 's'} over 2 hours`}
        data-testid={`flush-strip-${b.book}`}
      >
        {t.gaps.map(([a, z], i) => (
          <span key={`g${i}`} className="flush-gap" style={{ left: `${a * 100}%`, width: `${(z - a) * 100}%` }} />
        ))}
        {t.ticks.map((x, i) => (
          <span key={i} className="flush-tick" style={{ left: `${x * 100}%` }} />
        ))}
      </div>
      <div className="flush-axis faint">
        <span>24 h ago</span>
        <span>12 h</span>
        <span>now</span>
      </div>
    </div>
  )
}

function LagMeter({ days, failDays }: { days: number; failDays: number }) {
  const r = Math.min(1, days / failDays)
  const tone = days >= failDays ? 'block' : days >= 2 ? 'warn' : ''
  return (
    <div
      className="meter-track"
      role="meter"
      aria-label="Compaction lag"
      aria-valuemin={0}
      aria-valuemax={failDays}
      aria-valuenow={Math.min(days, failDays)}
    >
      <div className={`meter-fill ${tone}`} style={{ width: `${Math.max(r, 0.03) * 100}%` }} />
    </div>
  )
}

function ArchiveCard({ b, d }: { b: ArchiveBook; d: DataHealth }) {
  const r = b.raw
  const c = b.compacted
  const failDays = Number(d.thresholds.compaction_fail_days ?? 4)
  const lastRun = c.created_at ? minutesBetween(c.created_at, d.generated_at) : NaN
  return (
    <article className="card" aria-labelledby={`arch-${b.book}`} data-testid={`archive-${b.book}`}>
      <header className="card-head">
        <div>
          <h3 id={`arch-${b.book}`}>{bookLabel(b.book)}</h3>
          <div className="sub num">trades/book={b.book}</div>
        </div>
        <StatusBadge s={b.status} />
      </header>

      <div className="health-sub">
        <span className="stat-label">Raw flushes</span>
        <StatusBadge s={r.status} />
      </div>
      <div className="health-stats">
        <Stat
          label="Last flush"
          value={r.latest_at ? `${fmtMinutes(r.age_minutes)} ago` : '—'}
          tone={r.status === 'fail' ? 'neg' : r.status === 'warn' ? 'warn' : undefined}
          hint={r.latest_at ? fmtMx(r.latest_at) : 'none found'}
          title={r.latest_key}
        />
        <Stat
          label="Flushes · 24 h"
          value={r.flushes_24h}
          tone={r.flush_gaps_24h > 0 ? 'warn' : undefined}
          hint={`longest wait ${fmtMinutes(r.max_flush_gap_minutes)}`}
        />
        <Stat label="Today (UTC)" value={r.objects_today} hint={fmtBytes(r.bytes_today)} />
      </div>
      <FlushStrip b={b} refIso={d.generated_at} />
      <p className="health-msg muted">{r.message}</p>

      <div className="health-sub" style={{ marginTop: 18 }}>
        <span className="stat-label">Daily compaction</span>
        <StatusBadge s={c.status} />
      </div>
      <div className="health-stats">
        <Stat
          label="Compacted through"
          value={c.latest_partition ? fmtDate(c.latest_partition) : '—'}
          hint={`expected ${fmtDate(c.expected_through)}`}
        />
        <Stat
          label="Days behind"
          value={c.latest_partition ? c.days_behind : '—'}
          tone={c.status === 'fail' ? 'neg' : c.status === 'warn' ? 'warn' : undefined}
          hint={Number.isFinite(lastRun) ? `last run ${fmtMinutes(lastRun)} ago` : 'no manifest'}
        />
        <Stat
          label="Partitions"
          value={c.partitions}
          hint={c.first_partition ? `since ${fmtDate(c.first_partition)}` : '—'}
        />
      </div>
      <LagMeter days={c.days_behind} failDays={failDays} />
      <p className="health-msg muted">
        {c.message}
        {c.latest_partition && (
          <span className="faint num">
            {' '}
            · last day {c.source_rows.toLocaleString()} rows → {c.compacted_rows.toLocaleString()}
            {c.duplicate_tids > 0 && `, ${c.duplicate_tids} duplicate tids`}
          </span>
        )}
      </p>
    </article>
  )
}

function ArchiveSection({ d }: { d: DataHealth }) {
  const a = d.archive
  return (
    <section aria-labelledby="archive-h" style={{ marginBottom: 16 }}>
      <div className="section-head">
        <h2 id="archive-h">Collector and trade archive</h2>
        {a.source && (
          <Badge mono title={`checked ${fmtUTC(a.checked_at)}`}>
            {a.source}
          </Badge>
        )}
      </div>
      {a.status === 'off' ? (
        <div className="card health-off" data-testid="archive-off">
          <IconInfo style={{ width: 18, height: 18 }} />
          <div>
            <strong>Archive not configured.</strong>{' '}
            <span className="muted">
              Start ui-api with <code>-archive s3://&lt;bucket&gt;</code> (read-only listing with the default AWS
              credentials) to see the collector&rsquo;s flushes and the compaction progress.
            </span>
          </div>
        </div>
      ) : a.books.length === 0 || (a.status === 'unknown' && a.error) ? (
        <div className="card health-off" role="alert" data-testid="archive-error">
          <IconAlert style={{ width: 18, height: 18, color: 'var(--warn)' }} />
          <div>
            <strong>Cannot list the archive.</strong> <span className="muted">{a.error}</span>
          </div>
        </div>
      ) : (
        <div className="grid grid-2">
          {a.books.map((b) => (
            <ArchiveCard key={b.book} b={b} d={d} />
          ))}
        </div>
      )}
      <p className="faint" style={{ fontSize: 12, marginTop: 10 }}>
        {d.collector.note}
      </p>
    </section>
  )
}

function ExitBadge({ r }: { r: RunLog }) {
  if (r.exit_code === null)
    return <StatusBadge s={r.status}>{r.status === 'unknown' ? 'running?' : 'no exit code'}</StatusBadge>
  return <StatusBadge s={r.status}>exit {r.exit_code}</StatusBadge>
}

function RunsTable({ runs }: { runs: RunLog[] }) {
  const books = Array.from(new Set(runs.flatMap((r) => r.books.map((b) => b.book)))).sort()
  return (
    <div className="table-wrap">
      <table className="data" id="runs-table">
        <thead>
          <tr>
            <th>Started (Mexico City)</th>
            <th>Exit</th>
            <th>Version</th>
            {books.map((b) => (
              <th key={b}>{bookLabel(b)}</th>
            ))}
            <th>Reconcile</th>
            <th>S3 copy</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => (
            <tr key={r.file} title={r.message}>
              <td className="num" title={r.file}>
                {r.started_at ? fmtMx(r.started_at) : r.file}
              </td>
              <td>
                <ExitBadge r={r} />
              </td>
              <td className="num faint">{r.version || '—'}</td>
              {books.map((b) => {
                const x = r.books.find((y) => y.book === b)
                return (
                  <td key={b} className="run-book" title={x?.stage}>
                    {x?.ledger || <span className="faint">—</span>}
                  </td>
                )
              })}
              <td title={r.reconcile_detail || undefined}>
                {r.reconcile === 'ok' ? (
                  <Badge tone="ok">matched</Badge>
                ) : r.reconcile === 'breaks' ? (
                  <Badge tone="block">breaks</Badge>
                ) : r.reconcile === 'error' ? (
                  <Badge tone="warn">error</Badge>
                ) : (
                  <span className="faint">—</span>
                )}
              </td>
              <td>
                {r.upload === 'ok' ? (
                  <Badge tone="ok">uploaded</Badge>
                ) : r.upload === 'failed' ? (
                  <Badge tone="block">failed</Badge>
                ) : (
                  <span className="faint">—</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function ExecutorSection({ d }: { d: DataHealth }) {
  const e = d.executor
  const search = useLedgerSearch()
  const lr = e.last_run
  return (
    <section className="card" aria-labelledby="executor-h" style={{ marginBottom: 16 }} data-testid="executor">
      <div className="card-head">
        <div>
          <h2 id="executor-h">Daily-executor</h2>
          <div className="sub">{e.message}</div>
        </div>
        <StatusBadge s={e.status} />
      </div>

      <div className="grid grid-4" style={{ gap: 14, marginBottom: 16 }}>
        <Stat
          label="Ledger"
          value={e.ledger_found ? `${e.records} records` : 'no file'}
          hint={e.ledger_path}
          title={e.ledger_path}
        />
        <Stat
          label="Last recorded"
          value={e.last_recorded_at ? `${fmtMinutes(minutesBetween(e.last_recorded_at, d.generated_at))} ago` : '—'}
          hint={e.last_recorded_at ? fmtMx(e.last_recorded_at) : 'nothing yet'}
        />
        <Stat
          label="Last run"
          value={lr ? <ExitBadge r={lr} /> : '—'}
          hint={
            lr ? `${lr.started_at ? fmtMx(lr.started_at) : lr.file}${lr.mode ? ` · ${lr.mode}` : ''}` : 'no run logs'
          }
        />
        <Stat
          label="Ledger copy in S3"
          value={
            <StatusBadge s={e.upload.status}>
              {e.upload.status === 'off' ? 'off' : statusLabel(e.upload.status)}
            </StatusBadge>
          }
          hint={e.upload.target || e.upload.message}
          title={e.upload.message}
        />
      </div>

      <div className="stack" style={{ gap: 8, marginBottom: 16 }}>
        {e.books.map((b) => (
          <div key={b.book} className="panel row coverage-row" data-testid={`coverage-${b.book}`}>
            <Link to={{ pathname: `/forward-tests/${b.book}`, search }} className="coverage-book">
              {bookLabel(b.book)}
            </Link>
            <span className="faint num">
              last bar {b.run.last_bar_date ? fmtDate(b.run.last_bar_date) : '—'} · expected{' '}
              {fmtDate(b.run.expected_bar_date)}
            </span>
            <span className="spacer" />
            {b.run.missing_days.slice(0, 6).map((m) => (
              <span key={m} className="chip chip-warn num">
                {m}
              </span>
            ))}
            {b.run.missing_days.length > 6 && <span className="faint">+{b.run.missing_days.length - 6}</span>}
            <Badge
              tone={
                b.run.status === 'ok'
                  ? 'ok'
                  : b.run.status === 'pending'
                    ? 'info'
                    : b.run.status === 'missed'
                      ? 'block'
                      : 'flat'
              }
              dot
            >
              {b.run.status === 'ok' ? 'up to date' : b.run.status === 'no_data' ? 'no data' : b.run.status}
            </Badge>
          </div>
        ))}
      </div>

      {lr && lr.errors.length > 0 && (
        <div className="panel stack" style={{ gap: 4, marginBottom: 16 }} data-testid="run-errors">
          <span className="stat-label">From the last run&rsquo;s log</span>
          {lr.errors.map((x, i) => (
            <code key={i} className="run-err">
              {x}
            </code>
          ))}
        </div>
      )}

      {e.runs.length > 0 ? (
        <RunsTable runs={e.runs} />
      ) : (
        <p className="muted" style={{ margin: 0 }}>
          No run logs next to the ledger. Run the executor with <code>scripts/daily-executor-run.sh</code> so each run
          leaves <code>run-&lt;UTC&gt;.log</code> with its exit code.
        </p>
      )}
    </section>
  )
}

export function DataHealthPage() {
  usePageTitle(
    'Data health',
    'Collector flushes, trade-archive compaction and daily-executor runs behind the forward tests.',
  )
  const q = useDataHealth()
  const d = q.data
  return (
    <>
      <div className="page-head">
        <div>
          <div className="row" style={{ gap: 12 }}>
            <h1>Data health</h1>
            {d && <LedgerBadge name={d.ledger} />}
          </div>
          <p>
            Is the data behind the forward tests arriving? The collector&rsquo;s trade archive in S3 and the
            daily-executor&rsquo;s runs, checked read-only.
          </p>
        </div>
        {d && (
          <span className="faint row" style={{ gap: 6 }} title={fmtUTC(d.generated_at)}>
            <IconPulse style={{ width: 14, height: 14 }} /> checked {fmtMx(d.generated_at)}
          </span>
        )}
      </div>

      {q.isLoading && (
        <div className="stack" style={{ gap: 16 }}>
          <CardSkeleton lines={3} />
          <div className="grid grid-2">
            <CardSkeleton lines={8} />
            <CardSkeleton lines={8} />
          </div>
        </div>
      )}
      {q.isError && (
        <div className="card">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      )}
      {d && (
        <>
          <Summary d={d} />
          <Checks checks={d.checks} />
          <ArchiveSection d={d} />
          <ExecutorSection d={d} />
        </>
      )}
    </>
  )
}
