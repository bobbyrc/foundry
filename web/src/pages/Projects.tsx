import { Link } from 'react-router-dom';
import { useLoops, useProjects, useSetProjectPaused } from '../queries';
import { Button } from '../components/Button';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { PageHeader } from '../components/PageHeader';
import { cx } from '../lib/format';
import { loopHealth, loopHealthLabel, repoUrl } from '../lib/projects';
import type { LoopView, ProjectCounts, ProjectView } from '../types';

/** The task counts on a card, in the order the factory moves work through them. */
const COUNTS: { key: keyof ProjectCounts; label: string; status: string }[] = [
  { key: 'queued', label: 'queued', status: 'queued' },
  { key: 'running', label: 'running', status: 'running' },
  { key: 'review', label: 'review', status: 'review' },
  { key: 'awaiting_approval', label: 'awaiting approval', status: 'awaiting_approval' },
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
        <Empty>No projects are configured. Projects are set under projects in foundry.yaml.</Empty>
      ) : null}
      <div className="card-grid projects-grid">
        {projects.data?.map((project) => (
          <ProjectCard
            key={project.name}
            project={project}
            loops={(loops.data ?? []).filter((loop) => loop.project === project.name)}
          />
        ))}
      </div>
    </>
  );
}

function ProjectCard({ project, loops }: { project: ProjectView; loops: LoopView[] }) {
  const url = repoUrl(project.repo);
  const observers = (project.observe ?? []).map((o) => o.name);

  return (
    <article className="card project-card">
      <header className="card-head">
        <div className="project-title">
          <h3>{project.name}</h3>
          {project.repo ? (
            url ? (
              <a className="small muted" href={url} target="_blank" rel="noopener noreferrer">
                {project.repo}
              </a>
            ) : (
              <span className="small muted">{project.repo}</span>
            )
          ) : null}
        </div>
        <PauseControl project={project} />
      </header>

      <div className="project-counts">
        {COUNTS.map((count) => {
          const n = project.counts?.[count.key] ?? 0;
          const to = `/tasks?${new URLSearchParams({ project: project.name, status: count.status })}`;
          return (
            <Link key={count.key} to={to} className={cx('project-count', n === 0 && 'is-zero')}>
              {n} {count.label}
            </Link>
          );
        })}
      </div>

      <div className="project-meta muted small">
        <span>Default branch: {project.default_branch || '—'}</span>
        <span>{project.deploy ? `Deployer: ${project.deploy.type}` : 'no deployer'}</span>
        <span>{observers.length > 0 ? `Observers: ${observers.join(', ')}` : 'no observers'}</span>
      </div>

      {loops.length > 0 ? (
        <ul className="loop-pills" aria-label="Loops">
          {loops.map((loop) => {
            const health = loopHealth(loop);
            return (
              <li key={loop.name}>
                <span className="loop-pill" title={health === 'danger' ? loop.last_error : undefined}>
                  <span className={`dot tone-${health}`} aria-hidden="true" />
                  <span className="sr-only">{loopHealthLabel(loop)}: </span>
                  {loop.name}
                </span>
              </li>
            );
          })}
        </ul>
      ) : null}
    </article>
  );
}

/** The pause toggle. A project paused in foundry.yaml can only be resumed by editing that file. */
function PauseControl({ project }: { project: ProjectView }) {
  const toggle = useSetProjectPaused();
  const mine = toggle.variables?.name === project.name;
  const pending = toggle.isPending && mine;

  let control;
  if (project.paused_by === 'config') {
    control = <span className="muted small project-config-pause">Paused in foundry.yaml</span>;
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
    <div className="project-pause">
      {control}
      {mine ? <ErrorLine error={toggle.error} /> : null}
    </div>
  );
}
