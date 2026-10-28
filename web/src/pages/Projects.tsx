import { Link } from 'react-router-dom';
import { useCan } from '../auth';
import { useLoops, useProjects, useSetProjectPaused } from '../queries';
import { Button } from '../components/Button';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { PageHeader } from '../components/PageHeader';
import { cx } from '../lib/format';
import { projectLamp } from '../lib/lamp';
import { loopSummary, repoUrl } from '../lib/projects';
import type { LoopView, ProjectCounts, ProjectView } from '../types';
import './Projects.css';

/** The task counts on a sheet, in the order the factory moves work through them. */
const COUNTS: { key: keyof ProjectCounts; label: string; status: string }[] = [
  { key: 'queued', label: 'queued', status: 'queued' },
  { key: 'running', label: 'running', status: 'running' },
  { key: 'review', label: 'review', status: 'review' },
  { key: 'awaiting_approval', label: 'needs you', status: 'awaiting_approval' },
  { key: 'blocked', label: 'blocked', status: 'blocked' },
];

export function Projects() {
  const projects = useProjects();
  const loops = useLoops();

  return (
    <>
      <PageHeader title="Projects" />
      <ErrorLine error={projects.error ?? loops.error} />
      {projects.isSuccess && projects.data.length === 0 ? (
        <Empty>No projects are configured. Projects are set under projects in onlet.yaml.</Empty>
      ) : null}
      <div className="projects-sheets">
        {projects.data?.map((project) => (
          <ProjectSheet
            key={project.name}
            project={project}
            loops={(loops.data ?? []).filter((loop) => loop.project === project.name)}
          />
        ))}
      </div>
    </>
  );
}

/** One project as a window of the Overview's board: a lamp bar, then its name, counts and facts. */
function ProjectSheet({ project, loops }: { project: ProjectView; loops: LoopView[] }) {
  const lamp = projectLamp(project);
  const url = repoUrl(project.repo);
  const observers = (project.observe ?? []).map((o) => o.name);
  const loopInfo = loopSummary(loops);

  const facts = [
    project.default_branch || null,
    project.deploy ? `deploys with ${project.deploy.type}` : 'no deployer',
    observers.length === 0 ? 'no observers' : `${observers.length} observer${observers.length === 1 ? '' : 's'}`,
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <article className="project-sheet" data-lamp={lamp.lamp}>
      <div className="sheet-lamp" aria-hidden="true" />
      <div className="sheet-body">
        <header className="sheet-head">
          <div className="sheet-id">
            <h3 className="sheet-name">{project.name}</h3>
            {project.repo ? (
              url ? (
                <a className="sheet-repo" href={url} target="_blank" rel="noopener noreferrer">
                  {project.repo}
                </a>
              ) : (
                <span className="sheet-repo">{project.repo}</span>
              )
            ) : null}
            <p className="sheet-state">{lamp.label}</p>
          </div>
          <PauseControl project={project} />
        </header>

        <ul className="sheet-counts" aria-label="Tasks by status">
          {COUNTS.map((count) => {
            const n = project.counts?.[count.key] ?? 0;
            const cells = (
              <>
                <span className="sheet-count-n">{n}</span>
                <span className="sheet-count-label">{count.label}</span>
              </>
            );
            if (n === 0) {
              return (
                <li key={count.key}>
                  <span className="sheet-count is-zero">{cells}</span>
                </li>
              );
            }
            const to = `/tasks?${new URLSearchParams({ project: project.name, status: count.status })}`;
            return (
              <li key={count.key}>
                <Link
                  to={to}
                  className={cx(
                    'sheet-count',
                    count.key === 'blocked' && 'is-blocked',
                    count.key === 'awaiting_approval' && 'is-needs',
                  )}
                >
                  {cells}
                </Link>
              </li>
            );
          })}
        </ul>

        <p className="sheet-facts">{facts}</p>

        <p className="sheet-loops">
          {loops.length === 0 ? (
            'no loops'
          ) : (
            <>
              <Link to="/loops">Loops</Link>: {loopInfo.on} on, {loopInfo.off} off
              {loopInfo.failing.length > 0 ? (
                <>
                  {' · '}
                  <span
                    className="sheet-failing"
                    title={loopInfo.failing.map((l) => `${l.name}: ${l.last_error}`).join('\n')}
                  >
                    {loopInfo.failing.map((l) => l.name).join(', ')} failing
                  </span>
                </>
              ) : null}
            </>
          )}
        </p>
      </div>
    </article>
  );
}

/** The pause toggle. A project paused in onlet.yaml can only be resumed by editing that file. */
function PauseControl({ project }: { project: ProjectView }) {
  const toggle = useSetProjectPaused();
  const mine = toggle.variables?.name === project.name;
  const pending = toggle.isPending && mine;
  const canOperate = useCan('operate');

  let control;
  if (project.paused_by === 'config') {
    control = <span className="muted small sheet-config-pause">Paused in onlet.yaml</span>;
  } else if (!canOperate) {
    control = <span className="muted small">{project.paused ? 'Paused' : 'Running'}</span>;
  } else if (project.paused) {
    control = (
      <Button variant="danger-fill" disabled={pending} onClick={() => toggle.mutate({ name: project.name, pause: false })}>
        Paused — Resume
      </Button>
    );
  } else {
    control = (
      <Button variant="secondary" disabled={pending} onClick={() => toggle.mutate({ name: project.name, pause: true })}>
        Pause
      </Button>
    );
  }

  return (
    <div className="sheet-pause">
      {control}
      {mine ? <ErrorLine error={toggle.error} /> : null}
    </div>
  );
}
