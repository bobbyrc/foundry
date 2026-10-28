import { useState, type ReactNode } from 'react';
import { NavLink } from 'react-router-dom';
import { useStats } from '../queries';
import { LiveIndicator } from './LiveIndicator';
import { PauseSwitch } from './PauseSwitch';

const NAV: { to: string; label: string; end?: boolean; approvals?: boolean }[] = [
  { to: '/', label: 'Overview', end: true },
  { to: '/chat', label: 'Chat' },
  { to: '/tasks', label: 'Tasks' },
  { to: '/approvals', label: 'Approvals', approvals: true },
  { to: '/runs', label: 'Runs' },
  { to: '/deploys', label: 'Deploys' },
  { to: '/signals', label: 'Signals' },
  { to: '/friction', label: 'Friction' },
  { to: '/loops', label: 'Loops' },
  { to: '/agents', label: 'Agents' },
  { to: '/config', label: 'Config' },
];

function Wordmark() {
  return (
    <span className="wordmark">
      <span className="wordmark-square" aria-hidden="true" />
      foundry
    </span>
  );
}

/** Sidebar on wide screens; a top bar with a slide-down drawer under 760px. */
export function Shell({ children }: { children: ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false);
  const stats = useStats();
  const pending = stats.data?.approvals_pending ?? 0;
  const close = () => setMenuOpen(false);
  const badge = pending > 0 ? <span className="count-badge">{pending}</span> : null;

  return (
    <div className="app">
      <header className="topbar">
        <Wordmark />
        <span className="topbar-right">
          {pending > 0 ? <NavLink to="/approvals" className="topbar-approvals" onClick={close}>{badge}</NavLink> : null}
          <button
            type="button"
            className="btn btn-secondary menu-button"
            aria-expanded={menuOpen}
            aria-controls="sidebar"
            onClick={() => setMenuOpen((open) => !open)}
          >
            Menu
          </button>
        </span>
      </header>
      <aside id="sidebar" className={`sidebar${menuOpen ? ' open' : ''}`}>
        <div className="sidebar-brand">
          <Wordmark />
        </div>
        <nav aria-label="Main">
          {NAV.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
              onClick={close}
            >
              <span>{item.label}</span>
              {item.approvals && pending > 0 ? <span className="count-badge">{pending}</span> : null}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-footer">
          <PauseSwitch />
          <LiveIndicator />
        </div>
      </aside>
      <main className="main">{children}</main>
    </div>
  );
}
