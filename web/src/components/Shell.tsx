import { useEffect, useRef, useState, type ReactNode } from 'react';
import { NavLink } from 'react-router-dom';
import { useCan } from '../auth';
import { useStats } from '../queries';
import { AccountPanel } from './AccountPanel';
import { LiveIndicator } from './LiveIndicator';
import { PauseSwitch } from './PauseSwitch';

const NAV: { to: string; label: string; end?: boolean; approvals?: boolean; blocked?: boolean; admin?: boolean }[] = [
  { to: '/', label: 'Overview', end: true },
  { to: '/projects', label: 'Projects' },
  { to: '/chat', label: 'Chat' },
  { to: '/tasks', label: 'Tasks', blocked: true },
  { to: '/approvals', label: 'Approvals', approvals: true },
  { to: '/runs', label: 'Runs' },
  { to: '/deploys', label: 'Deploys' },
  { to: '/signals', label: 'Signals' },
  { to: '/friction', label: 'Friction' },
  { to: '/loops', label: 'Loops' },
  { to: '/agents', label: 'Agents' },
  { to: '/config', label: 'Config' },
  { to: '/users', label: 'Users', admin: true },
];

function Wordmark() {
  return (
    <span className="wordmark">
      <span className="wordmark-square" aria-hidden="true" />
      onlet
    </span>
  );
}

/** A lit count on the nav: yellow for what waits on you, red for what stopped. */
function CallBadge({ count, label, stop = false, spoken = true }: { count: number; label: string; stop?: boolean; spoken?: boolean }) {
  return (
    <span className={`count-badge${stop ? ' stop' : ''}`}>
      {count}
      <span className={spoken ? 'sr-only' : undefined}> {label}</span>
    </span>
  );
}

/** Sidebar on wide screens; a top bar with a slide-down drawer under 760px. */
export function Shell({ children }: { children: ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const sidebarRef = useRef<HTMLElement>(null);
  const stats = useStats();
  const administer = useCan('administer');
  const pending = stats.data?.approvals_pending ?? 0;
  const blocked = stats.data?.tasks_by_status.blocked ?? 0;
  const close = () => setMenuOpen(false);

  // While the drawer is open, Escape and a tap outside it close it. Taps on the menu button are
  // left to its own onClick, which toggles.
  useEffect(() => {
    if (!menuOpen) return;
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (sidebarRef.current?.contains(target) || menuButtonRef.current?.contains(target)) return;
      setMenuOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      setMenuOpen(false);
      menuButtonRef.current?.focus();
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [menuOpen]);

  return (
    <div className="app">
      <header className="topbar">
        <Wordmark />
        <span className="topbar-right">
          {pending > 0 ? (
            <NavLink to="/approvals" className="topbar-approvals" onClick={close}>
              <CallBadge count={pending} label={pending === 1 ? 'approval' : 'approvals'} spoken={false} />
            </NavLink>
          ) : null}
          {blocked > 0 ? (
            <NavLink to="/tasks?status=blocked" className="topbar-approvals" onClick={close}>
              <CallBadge count={blocked} label="blocked" stop spoken={false} />
            </NavLink>
          ) : null}
          <button
            ref={menuButtonRef}
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
      <aside ref={sidebarRef} id="sidebar" className={`sidebar${menuOpen ? ' open' : ''}`}>
        <div className="sidebar-brand">
          <Wordmark />
        </div>
        <nav aria-label="Main">
          {NAV.filter((item) => !item.admin || administer).map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
              onClick={close}
            >
              <span>{item.label}</span>
              {item.approvals && pending > 0 ? <CallBadge count={pending} label="awaiting approval" /> : null}
              {item.blocked && blocked > 0 ? <CallBadge count={blocked} label="blocked" stop /> : null}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-footer">
          <AccountPanel />
          <PauseSwitch />
          <LiveIndicator />
        </div>
      </aside>
      <main className="main">{children}</main>
    </div>
  );
}
