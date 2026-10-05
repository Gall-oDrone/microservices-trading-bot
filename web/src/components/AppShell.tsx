import { NavLink, Outlet } from 'react-router'
import { useForwardTests, useHealth, useRisk } from '../api/client'
import { ago } from '../lib/format'
import { IconCandles, IconFlask, IconLogo, IconPulse, IconShield, IconTrend } from './icons'
import { Badge } from './ui'

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

function Nav() {
  const ft = useForwardTests()
  const risk = useRisk()
  const missed = ft.data?.books.filter((b) => b.run.status === 'missed').length ?? 0
  const blocks = risk.data?.blocks ?? 0
  const warns = risk.data?.warnings ?? 0
  return (
    <nav className="nav" aria-label="Main">
      <span className="nav-label">Trading</span>
      <NavLink to="/" end className="nav-link" id="nav-forward-tests">
        <IconTrend /> Forward tests
        {missed > 0 && (
          <span className="count">
            <Badge tone="warn">{missed}</Badge>
          </span>
        )}
      </NavLink>
      <NavLink to="/risk" className="nav-link" id="nav-risk">
        <IconShield /> Risk
        {(blocks > 0 || warns > 0) && (
          <span className="count">
            <Badge tone={blocks > 0 ? 'block' : 'warn'}>{blocks > 0 ? blocks : warns}</Badge>
          </span>
        )}
      </NavLink>
      <span className="nav-label" style={{ marginTop: 14 }}>
        Next phases
      </span>
      <span className="nav-link disabled" aria-disabled="true" title="Phase 2">
        <IconFlask /> Research
        <span className="count faint">P2</span>
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
        </div>
        <Nav />
        <HealthFoot />
      </aside>
      <main className="main" id="main">
        <Outlet />
      </main>
    </div>
  )
}
