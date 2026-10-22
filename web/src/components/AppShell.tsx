import { Link, NavLink, Outlet, useLocation, useSearchParams } from 'react-router'
import { useEffect } from 'react'
import {
  useControls,
  useDataHealth,
  useEnvironment,
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
import { accountLabel } from '../lib/env'
import { TZ_OPTIONS, useTz } from '../lib/tz'

/**
 * The REAL account bar (2026-10-22, plan §3.4): rendered above every page
 * while the selected ledger is the measurement canary's, never dismissable.
 * It names the account, the ledger, the policy the trades are checked with
 * and the ui-api the page talks to, so a tab on the canary console can
 * never be mistaken for the demo one. Absent on every demo ledger.
 */
function RealBar() {
  const env = useEnvironment()
  const ledger = useLedgerName()
  const controls = useControls()
  const real = env === 'real'
  useEffect(() => {
    if (real) document.documentElement.dataset.env = 'real'
    else delete document.documentElement.dataset.env
    return () => {
      delete document.documentElement.dataset.env
    }
  }, [real])
  if (!real) return null
  const policy = controls.data?.trading?.policy_version
  const api = typeof __UI_API__ === 'string' ? __UI_API__.replace(/^https?:\/\//, '') : ''
  return (
    <div className="real-bar" role="alert" aria-label="Real account" data-testid="real-bar">
      <span className="real-bar-mark" aria-hidden="true">
        <IconAlert />
      </span>
      <b className="real-bar-title">REAL ACCOUNT</b>
      <span className="real-bar-sep">·</span>
      <span>measurement canary</span>
      <span className="real-bar-sep">·</span>
      <span className="num">ledger {ledger || 'etoro-real-canary'}</span>
      {policy && (
        <>
          <span className="real-bar-sep">·</span>
          <span className="num">policy {policy}</span>
        </>
      )}
      {api && (
        <>
          <span className="real-bar-sep">·</span>
          <span className="num">ui-api {api}</span>
        </>
      )}
      <span className="real-bar-sep">·</span>
      <span className="real-bar-note">every order placed here is real money; phrases carry REAL</span>
    </div>
  )
}

/**
 * The operator's clock (U3): which zone the audit lines, outcomes and
 * position times are shown in. Venue clocks (the NYSE session, the Bitso
 * bars) are not affected.
 */
export function TzToggle() {
  const [tz, setTz] = useTz()
  return (
    <div className="seg tz-toggle" role="group" aria-label="Clock" data-testid="tz-toggle">
      {TZ_OPTIONS.map((o) => (
        <button
          key={o.id}
          type="button"
          aria-pressed={tz === o.id}
          onClick={() => setTz(o.id)}
          title={o.title}
          data-testid={`tz-${o.id}`}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

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
        {!unknown && current.venue === 'etoro' && current.environment === 'real' && (
          <Badge tone="real" title="This ledger is written against the REAL eToro account (the measurement canary)">
            <span data-testid="ledger-picker-real">REAL</span>
          </Badge>
        )}
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
              {l.venue === 'etoro' && l.environment === 'real' ? ' · REAL' : ''}
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
  const env = useEnvironment()
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
      {etoro && (
        <NavLink to={{ pathname: '/today', search }} className="nav-link" id="nav-today">
          <IconSun /> Today
        </NavLink>
      )}
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
            <Badge tone={env === 'real' ? 'real' : 'etoro'}>{accountLabel(env)}</Badge>
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

/**
 * The phone navigation (U3): a fixed bottom bar with the five pages an
 * operator opens on the move and a "More" sheet with the rest. Hidden above
 * 720 px by CSS; the sidebar nav is hidden below it.
 */
function TabBar() {
  const search = useLedgerSearch()
  const venue = useVenue()
  const etoro = venue === 'etoro'
  const ehealth = useEtoroHealth({ enabled: etoro })
  const health = useDataHealth({ enabled: venue === 'bitso' })
  const checks = (etoro ? ehealth.data?.checks : health.data?.checks) ?? []
  const bad = checks.filter((c) => c.status === 'fail' || c.status === 'warn').length
  const first = etoro
    ? { to: '/today', label: 'Today', icon: <IconSun />, id: 'tab-today' }
    : { to: '/forward-tests', label: 'Tests', icon: <IconTrend />, id: 'tab-forward-tests' }
  return (
    <nav className="tabbar" aria-label="Main (phone)" data-testid="tabbar">
      <NavLink to={{ pathname: first.to, search }} className="tab" id={first.id}>
        {first.icon}
        <span>{first.label}</span>
      </NavLink>
      <NavLink to={{ pathname: '/risk', search }} className="tab" id="tab-risk">
        <IconShield />
        <span>Risk</span>
      </NavLink>
      <NavLink to={{ pathname: '/trade', search }} className="tab" id="tab-trade">
        <IconTrade />
        <span>Trade</span>
      </NavLink>
      <NavLink to={{ pathname: '/market', search }} className="tab" id="tab-market">
        <IconCandles />
        <span>Market</span>
      </NavLink>
      <NavLink to={{ pathname: '/data-health', search }} className="tab" id="tab-data-health">
        <IconPulse />
        <span>Health</span>
        {bad > 0 && <i className="tab-dot" aria-label={`${bad} checks need attention`} />}
      </NavLink>
      <details className="tab-more">
        <summary className="tab" id="tab-more">
          <IconGrid />
          <span>More</span>
        </summary>
        <div className="tab-sheet" role="menu">
          {etoro && (
            <NavLink to={{ pathname: '/forward-tests', search }} className="nav-link" role="menuitem">
              <IconTrend /> Forward tests
            </NavLink>
          )}
          <NavLink to={{ pathname: '/strategies', search }} className="nav-link" role="menuitem">
            <IconPower /> Strategies
          </NavLink>
          <NavLink to={{ pathname: '/research', search }} className="nav-link" role="menuitem">
            <IconFlask /> Studies
          </NavLink>
          <NavLink to={{ pathname: '/research/runs', search }} className="nav-link" role="menuitem">
            <IconGrid /> Backtest runs
          </NavLink>
          <div className="tab-sheet-row">
            <span className="nav-label">Clock</span>
            <TzToggle />
          </div>
        </div>
      </details>
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
        <div className="sidebar-clock">
          <span className="nav-label">Clock</span>
          <TzToggle />
        </div>
        <Nav />
        <HealthFoot />
      </aside>
      <main className="main" id="main">
        <RealBar />
        <AttentionBar />
        <Outlet />
      </main>
      <TabBar />
    </div>
  )
}
