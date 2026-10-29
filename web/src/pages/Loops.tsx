import './Loops.css';
import { useCan } from '../auth';
import { useRunLoop, useLoops } from '../queries';
import { Button } from '../components/Button';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { PageHeader } from '../components/PageHeader';
import { RelTime } from '../components/RelTime';
import type { LoopView } from '../types';
import { GLOBAL_PROJECT, loopGrid } from '../lib/loopGrid';
import { formatGoDuration } from '../lib/format';
import { loopHealth, loopHealthLabel } from '../lib/projects';
import { parseTime } from '../lib/time';

/** The column label for a project name. A global loop has no project, so it says so. */
const projectLabel = (project: string) => (project === GLOBAL_PROJECT ? 'all projects' : project);

/** The lamp for one loop, lit by loopHealth. The words are read aloud, so colour is not the only signal. */
function Lamp({ loop }: { loop: LoopView }) {
  return (
    <>
      <span className={`loop-lamp loop-lamp-${loopHealth(loop)}`} aria-hidden="true" />
      <span className="sr-only">{loopHealthLabel(loop)}</span>
    </>
  );
}

/** The second line of a loop's status: its error if it has one, else when it last ran. */
function LastRun({ loop }: { loop: LoopView }) {
  if (loop.last_error) {
    return (
      <span className="danger-text loop-error" title={loop.last_error}>
        {loop.last_error}
      </span>
    );
  }
  if (!parseTime(loop.last_run_at)) return <span className="muted">never</span>;
  return (
    <span className="muted">
      ran <RelTime value={loop.last_run_at} />
    </span>
  );
}

export function Loops() {
  const loops = useLoops();
  const runLoop = useRunLoop();
  const canOperate = useCan('operate');

  // "Run now" is an operator action: viewers see the lamps without the button.
  const runButton = (loop: LoopView) => {
    if (!canOperate) return null;
    const pending = runLoop.isPending && runLoop.variables?.name === loop.name && runLoop.variables.project === loop.project;
    return (
      <Button
        variant="ghost"
        className="loop-run"
        disabled={pending}
        onClick={() => runLoop.mutate({ name: loop.name, project: loop.project })}
      >
        {pending ? 'Starting…' : 'Run'}
      </Button>
    );
  };

  const list = loops.isSuccess ? loops.data : [];
  const grid = loopGrid(list);
  const enabled = list.filter((l) => l.enabled).length;
  const failing = list.filter((l) => loopHealth(l) === 'danger').length;

  return (
    <>
      <PageHeader title="Loops" />
      <ErrorLine error={loops.error} />
      {runLoop.isSuccess && runLoop.variables ? (
        <p className="muted small" role="status">
          Started {runLoop.variables.name}
          {runLoop.variables.project ? ` for ${runLoop.variables.project}` : ''}.
        </p>
      ) : null}
      {loops.isSuccess && list.length === 0 ? (
        <Empty>No loops are configured. Loops are set under loops in onett.yaml.</Empty>
      ) : null}
      {loops.isSuccess && list.length > 0 ? (
        <>
          <p className="muted small loops-summary">
            {enabled} {enabled === 1 ? 'loop' : 'loops'} on · {list.length - enabled} off ·{' '}
            <span className={failing > 0 ? 'danger-text' : undefined}>{failing} failing</span>
          </p>

          {/* Desktop: one lamp per loop and project. */}
          <div className="loops-grid-wrap">
            <table className="loops-grid">
              <thead>
                <tr>
                  <th scope="col" className="loops-head-name">
                    Loop
                  </th>
                  {grid.projects.map((project) => (
                    <th key={project} scope="col" className="loops-head-project">
                      {projectLabel(project)}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {grid.names.map((name) => (
                  <tr key={name}>
                    <th scope="row" className="loop-name">
                      {name}
                    </th>
                    {grid.projects.map((project) => {
                      const loop = grid.cell(name, project);
                      if (!loop) {
                        return (
                          <td key={project} className="muted">
                            —
                          </td>
                        );
                      }
                      return (
                        <td key={project}>
                          <div className="loop-cell">
                            <div className="loop-cell-top">
                              <Lamp loop={loop} />
                              <span>
                                {!loop.enabled ? <span className="loop-off">off · </span> : null}
                                every {formatGoDuration(loop.every)}
                              </span>
                              {runButton(loop)}
                            </div>
                            <div className="loop-cell-status">
                              <LastRun loop={loop} />
                            </div>
                          </div>
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Phone: one sheet per loop name, with a row for each project it runs in. */}
          <div className="loops-sheets">
            {grid.names.map((name) => (
              <section key={name} className="loop-sheet">
                <h3 className="loop-sheet-name mono">{name}</h3>
                <ul className="loop-sheet-rows">
                  {grid.projects.map((project) => {
                    const loop = grid.cell(name, project);
                    if (!loop) return null;
                    return (
                      <li key={project} className="loop-sheet-row">
                        <div className="loop-sheet-main">
                          <div className="loop-cell-top">
                            <Lamp loop={loop} />
                            <span className="loop-sheet-project">{projectLabel(project)}</span>
                          </div>
                          <span>
                            {!loop.enabled ? <span className="loop-off">off · </span> : null}
                            every {formatGoDuration(loop.every)}
                          </span>
                          <div className="loop-cell-status">
                            <LastRun loop={loop} />
                          </div>
                        </div>
                        {runButton(loop)}
                      </li>
                    );
                  })}
                </ul>
              </section>
            ))}
          </div>

          <ErrorLine error={runLoop.error} />
        </>
      ) : null}
    </>
  );
}
