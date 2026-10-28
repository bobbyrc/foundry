import { Link, useLocation } from 'react-router-dom';
import { useProjects } from '../queries';
import { projectLamp, type Lamp, type LampState } from '../lib/lamp';
import type { ProjectView } from '../types';
import './RailLamps.css';

/** How many project rows fit the rail before the rest are left to the Projects page. */
const MAX_ROWS = 8;

/** Stop first, then call, run and off: the order a person would want to read the board in. */
const URGENCY: Record<Lamp, number> = { stop: 0, call: 1, run: 2, off: 3 };

export interface RailRow {
  project: ProjectView;
  state: LampState;
}

/**
 * The rows for the rail: every project ranked most urgent first (ties by name), capped at
 * MAX_ROWS. `overflow` counts the projects left off, so the caller knows whether to show "All
 * projects". Pure, so the order and the cap are easy to test.
 */
export function railRows(projects: ProjectView[]): { shown: RailRow[]; overflow: number } {
  const ranked: RailRow[] = projects
    .map((project) => ({ project, state: projectLamp(project) }))
    .sort((a, b) => URGENCY[a.state.lamp] - URGENCY[b.state.lamp] || a.project.name.localeCompare(b.project.name));
  return { shown: ranked.slice(0, MAX_ROWS), overflow: Math.max(0, ranked.length - MAX_ROWS) };
}

/**
 * A compact lamp row per project for the nav rail. It is the Overview's board in small: the
 * square is the lamp, and the words beside it say what lit it. The Overview has the full board,
 * so the row is hidden there, and under 760px the phone top bar's badges cover the same news.
 */
export function RailLamps() {
  const projects = useProjects();
  const location = useLocation();
  if (location.pathname === '/' || !projects.data || projects.data.length === 0) return null;

  const { shown, overflow } = railRows(projects.data);

  return (
    <nav aria-label="Projects" className="rail-lamps">
      {shown.map(({ project, state }) => (
        <Link
          key={project.name}
          to={`/tasks?project=${encodeURIComponent(project.name)}`}
          className="rail-lamp-row"
        >
          <span className={`rail-lamp-square rail-lamp-${state.lamp}`} aria-hidden="true" />
          <span className="rail-lamp-name">{project.name}</span>
          <span className={`rail-lamp-label rail-lamp-label-${state.lamp}`}>{state.label}</span>
        </Link>
      ))}
      {overflow > 0 ? (
        <Link to="/projects" className="rail-lamp-row rail-lamp-all">
          All projects
        </Link>
      ) : null}
    </nav>
  );
}
