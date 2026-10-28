import { Link } from 'react-router-dom';
import { useControl, useProjects, useStats } from '../queries';
import { formatCost } from '../lib/format';
import { lampDetail, projectLamp } from '../lib/lamp';
import { ErrorLine } from './ErrorLine';

/**
 * The Overview's lamp board: one window per project, lit by what the project needs, and the line
 * window that says whether the factory runs. A window links to that project's open tasks.
 */
export function AndonBoard() {
  const projects = useProjects();

  return (
    <section className="board" aria-label="Projects">
      <div className="board-windows">
        {projects.data?.map((project) => {
          const state = projectLamp(project);
          const detail = lampDetail(project, state);
          return (
            <Link key={project.name} to={`/tasks?project=${encodeURIComponent(project.name)}`} className={`board-window lamp-${state.lamp}`}>
              <span className="board-lamp" aria-hidden="true" />
              <span className="board-name">{project.name}</span>
              <span className="board-count">{state.label}</span>
              {detail ? <span className="board-detail">{detail}</span> : null}
            </Link>
          );
        })}
        {projects.isSuccess && projects.data.length === 0 ? (
          <p className="board-empty">No projects yet. Add one with <code>foundry project add</code> and it appears here.</p>
        ) : null}
        {projects.error ? (
          <div className="board-empty">
            <ErrorLine error={projects.error} />
          </div>
        ) : null}
      </div>
      <LineWindow />
    </section>
  );
}

/** Whether the line runs, and the factory-wide numbers that used to be stat cards. */
function LineWindow() {
  const control = useControl();
  const stats = useStats();
  const paused = control.data?.paused ?? false;
  const s = stats.data;

  return (
    <div className="board-line">
      <span className="board-line-state">
        <span className={`lamp ${paused ? 'lamp-stop' : 'lamp-run'}`} aria-hidden="true" />
        {control.data ? (paused ? 'Line paused' : 'Line running') : 'Line'}
      </span>
      <span className="board-line-stats">
        <span>
          <b>{s?.active_runs ?? '—'}</b> active
        </span>
        <span>
          <b>{s?.tasks_by_status.queued ?? 0}</b> queued
        </span>
        <span>
          <b>{s ? formatCost(s.cost_today_usd) : '—'}</b> today
        </span>
      </span>
    </div>
  );
}
