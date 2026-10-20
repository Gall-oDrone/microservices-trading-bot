import { Link, NavLink, Outlet, useLocation, useSearchParams } from 'react-router'
import {
  useDataHealth,
  useEtoroForwardTests,
  useEtoroHealth,
  useEtoroRisk,
  useForwardTests,
  useHealth,
  useLedgerName,
  useLedgerSearch,
  useLedgers,
  useRisk,
  useVenue,
} from '../api/client'
import { ago } from '../lib/format'
import { useTheme } from '../lib/theme'
import {
  IconAlert,
  IconBlock,
  IconCandles,
  IconFlask,
  IconGrid,
  IconLogo,
  IconMoon,
  IconPower,
  IconPulse,
  IconShield,
  IconSun,
  IconTrade,
  IconTrend,
} from './icons'
import { Badge, VenueTag } from './ui'
import { attentionItems } from '../lib/attention'

/**
 * The attention bar: one line per problem that needs the operator (halt,
 * failed or warning checks of the selected ledger), each linking to the page
 * that owns it. Rendered above every console page; absent when all is well,
 * so its presence alone is the signal.
 */
function AttentionBar() {
  const search = useLedgerSearch()
  const venue = useVenue()
  const bitso = venue === 'bitso'
  const etoro = venue === 'etoro'
  const risk = useRisk({ enabled: bitso })
  const erisk = useEtoroRisk({ enabled: etoro })
  const health = useDataHealth({ enabled: bitso })
  const ehealth = useEtoroHealth({ enabled: etoro })
  const halt = etoro
    ? erisk.data
      ? {
          halted: erisk.data.halted,
          reason: erisk.data.halt_reason || erisk.data.halt_file.reason,
          by: erisk.data.halt_file.by,
        }
      : null
    : risk.data
      ? { halted: risk.data.halt_file.halted, reason: risk.data.halt_file.reason, by: risk.data.halt_file.by }
      : null
  const items = attentionItems(halt, etoro ? ehealth.data?.checks : health.data?.checks, search)
  if (items.length === 0) return null
  const blocks = items.filter((i) => i.tone === 'block').length
  const shown = items.slice(0, 3)
  const more = items.length - shown.length
  return (
    <div
      className={`attention ${blocks > 0 ? 'block' : 'warn'}`}
      role="status"
      aria-live="polite"
      data-testid="attention-bar"
    >
      <span className="attention-icon" aria-hidden="true">
        {blocks > 0 ? <IconBlock /> : <IconAlert />}
      </span>
      <ul className="attention-list">
        {shown.map((i) => (
          <li key={i.id} data-testid={`attention-${i.id}`}>
            <Link to={i.to} className="attention-link">
              <Badge tone={i.tone} dot>
                {i.label}
              </Badge>
              <span className="attention-msg">{i.message}</span>
            </Link>
          </li>
        ))}
      </ul>
      {more > 0 && (
        <Link to={`/data-health${search}`} className="attention-more">
          +{more} more
        </Link>
      )}
    </div>
  )
}

/** Light / dark switch. Dark is the default; the choice persists in localStorage. */
export function ThemeToggle() {
  const [theme, setTheme] = useTheme()
  const light = theme === 'light'
  const next = light ? 'dark' : 'light'
  return (
    <button
      type="button"
      id="theme-toggle"
      className="icon-btn theme-toggle"
      aria-label={`Switch to ${next} theme`}
      aria-pressed={light}
      title={`Switch to ${next} theme`}
      onClick={() => setTheme(next)}
    >
      {light ? <IconMoon /> : <IconSun />}
    </button>
  )
}

function HealthFoot() {
  const h = useHealth()
  const up = h.isSuccess && h.data.status === 'ok'
  return (
    <div className="sidebar-foot" id="api-health">
      <div className="row" style={{ gap: 8 }}>
        <span className={`live-dot ${up ? '' : 'down'}`} />
        <span className="muted">{up ? 'ui-api connected' : h.isLoading ? 'connecting…' : 'ui-api offline'}</span>
      </div>
      {h.data && (
        <span className="faint num" title={h.data.ledger}>
          {h.data.records} ledger records · {h.data.version}
        </span>
      )}
      {h.dataUpdatedAt > 0 && <span className="faint">checked {ago(new Date(h.dataUpdatedAt).toISOString())}</span>}
    </div>
  )
}

/**
 * Picks which ledger every page reads (stage, dry-run, etoro-demo, …). The
 * choice lives in the URL (`?ledger=`), so it is shareable and survives
 * reloads. Each ledger shows its venue: the pages, units and clock follow it.
 */
function LedgerPicker() {
  const q = useLedgers()
  const selected = useLedgerName()
  const [params, setParams] = useSearchParams()
  const ledgers = q.data?.ledgers ?? []
  if (ledgers.length === 0) return null
  const current = ledgers.find((l) => l.name === selected) ?? ledgers.find((l) => l.default) ?? ledgers[0]
  const unknown = selected !== '' && !ledgers.some((l) => l.name === selected)
  return (
    <div className="ledger-picker">
      <label className="nav-label" htmlFor="ledger-picker">
        Ledger
        {!unknown && <VenueTag venue={current.venue} />}
      </label>
      {ledgers.length === 1 ? (
        <div className="ledger-single" id="ledger-picker" title={current.path}>
          <span className="ledger-name">{current.name}</span>
          <span className="faint num">{current.records} rec</span>
        </div>
      ) : (
        <select
          id="ledger-picker"
          className="select"
          aria-label="Ledger"
          value={unknown ? '' : current.name}
          onChange={(e) => {
            const next = new URLSearchParams(params)
            const l = ledgers.find((x) => x.name === e.target.value)
            if (!l || l.default) next.delete('ledger')
            else next.set('ledger', l.name)
            setParams(next)
          }}
        >
          {unknown && <option value="">unknown: {selected}</option>}
          {ledgers.map((l) => (
            <option key={l.name} value={l.name}>
              {l.name} {l.found ? `· ${l.records} rec` : '· no file yet'} · {l.venue}
            </option>
          ))}
        </select>
      )}
    </div>
  )
}

function Nav() {
  const search = useLedgerSearch()
  const venue = useVenue()
  const bitso = venue === 'bitso'
  const etoro = venue === 'etoro'
  // Each venue's own endpoints feed the counts; the other venue's stay idle.
  const ft = useForwardTests({ enabled: bitso })
  const eft = useEtoroForwardTests({ enabled: etoro })
  const risk = useRisk({ enabled: bitso })
  const erisk = useEtoroRisk({ enabled: etoro })
  const health = useDataHealth({ enabled: bitso })
  const ehealth = useEtoroHealth({ enabled: etoro })
  const books = (etoro ? eft.data?.books : ft.data?.books) ?? []
  const missed = books.filter((b) => b.run.status === 'missed').length
  const blocks = (etoro ? erisk.data?.blocks : risk.data?.blocks) ?? 0
  const warns = (etoro ? erisk.data?.warnings : risk.data?.warnings) ?? 0
  const checks = (etoro ? ehealth.data?.checks : health.data?.checks) ?? []
  const healthFails = checks.filter((c) => c.status === 'fail').length
  const healthWarns = checks.filter((c) => c.status === 'warn').length
  const inRuns = useLocation().pathname.startsWith('/research/runs')
  return (
    <nav className="nav" aria-label="Main">
      <span className="nav-label">Trading</span>
      <NavLink to={{ pathname: '/forward-tests', search }} className="nav-link" id="nav-forward-tests">
        <IconTrend /> Forward tests
        {missed > 0 && (
          <span className="count">
            <Badge tone="warn">{missed}</Badge>
          </span>
        )}
      </NavLink>
      <NavLink to={{ pathname: '/risk', search }} className="nav-link" id="nav-risk">
        <IconShield /> Risk
        {(blocks > 0 || warns > 0) && (
          <span className="count">
            <Badge tone={blocks > 0 ? 'block' : 'warn'}>{blocks > 0 ? blocks : warns}</Badge>
          </span>
        )}
      </NavLink>
      <NavLink to={{ pathname: '/strategies', search }} className="nav-link" id="nav-strategies">
        <IconPower /> Strategies
      </NavLink>
      <NavLink to={{ pathname: '/trade', search }} className="nav-link" id="nav-trade">
        <IconTrade /> Trade
        {etoro && (
          <span className="count" aria-hidden="true">
            <Badge tone="etoro">demo</Badge>
          </span>
        )}
      </NavLink>
      <NavLink to={{ pathname: '/market', search }} className="nav-link" id="nav-market">
        <IconCandles /> Market
        <span className="count nav-live" aria-hidden="true" title="Live" />
      </NavLink>
      <span className="nav-label" style={{ marginTop: 14 }}>
        Research
      </span>
      <NavLink
        to={{ pathname: '/research', search }}
        className={({ isActive }) => `nav-link${isActive && !inRuns ? ' active' : ''}`}
        id="nav-research"
      >
        <IconFlask /> Studies
      </NavLink>
      <NavLink to={{ pathname: '/research/runs', search }} className="nav-link" id="nav-runs">
        <IconGrid /> Backtest runs
      </NavLink>
      <span className="nav-label" style={{ marginTop: 14 }}>
        Operations
      </span>
      <NavLink to={{ pathname: '/data-health', search }} className="nav-link" id="nav-data-health">
        <IconPulse /> Data health
        {(healthFails > 0 || healthWarns > 0) && (
          <span className="count">
            <Badge tone={healthFails > 0 ? 'block' : 'warn'}>{healthFails > 0 ? healthFails : healthWarns}</Badge>
          </span>
        )}
      </NavLink>
    </nav>
  )
}

export function AppShell() {
  const search = useLedgerSearch()
  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <Link to={{ pathname: '/', search }} className="brand-link" id="brand-home" title="Back to the landing page">
            <div className="brand-mark">
              <IconLogo />
            </div>
            <div>
              <div className="brand-name">Trading Bot</div>
              <div className="brand-sub">Operator console</div>
            </div>
          </Link>
          <ThemeToggle />
        </div>
        <LedgerPicker />
        <Nav />
        <HealthFoot />
      </aside>
      <main className="main" id="main">
        <AttentionBar />
        <Outlet />
      </main>
    </div>
  )
}
