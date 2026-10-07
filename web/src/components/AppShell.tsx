import { NavLink, Outlet, useLocation, useSearchParams } from 'react-router'
import { useForwardTests, useHealth, useLedgerName, useLedgerSearch, useLedgers, useRisk } from '../api/client'
import { ago } from '../lib/format'
import { useTheme } from '../lib/theme'
import {
  IconCandles,
  IconFlask,
  IconGrid,
  IconLogo,
  IconMoon,
  IconPulse,
  IconShield,
  IconSun,
  IconTrend,
} from './icons'
import { Badge } from './ui'

/** Light / dark switch. Dark is the default; the choice persists in localStorage. */
function ThemeToggle() {
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
 * Picks which ledger every page reads (stage, dry-run, …). The choice lives in
 * the URL (`?ledger=`), so it is shareable and survives reloads.
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
              {l.name} {l.found ? `· ${l.records} rec` : '· no file yet'}
            </option>
          ))}
        </select>
      )}
    </div>
  )
}

function Nav() {
  const search = useLedgerSearch()
  const ft = useForwardTests()
  const risk = useRisk()
  const missed = ft.data?.books.filter((b) => b.run.status === 'missed').length ?? 0
  const blocks = risk.data?.blocks ?? 0
  const warns = risk.data?.warnings ?? 0
  const inRuns = useLocation().pathname.startsWith('/research/runs')
  return (
    <nav className="nav" aria-label="Main">
      <span className="nav-label">Trading</span>
      <NavLink to={{ pathname: '/', search }} end className="nav-link" id="nav-forward-tests">
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
        Next phases
      </span>
      <span className="nav-link disabled" aria-disabled="true" title="Phase 2">
        <IconPulse /> Data health
        <span className="count faint">P2</span>
      </span>
      <span className="nav-link disabled" aria-disabled="true" title="Phase 3">
        <IconCandles /> Market
        <span className="count faint">P3</span>
      </span>
    </nav>
  )
}

export function AppShell() {
  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">
            <IconLogo />
          </div>
          <div>
            <div className="brand-name">Trading Bot</div>
            <div className="brand-sub">Operator console</div>
          </div>
          <ThemeToggle />
        </div>
        <LedgerPicker />
        <Nav />
        <HealthFoot />
      </aside>
      <main className="main" id="main">
        <Outlet />
      </main>
    </div>
  )
}
