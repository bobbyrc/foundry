import { useMemo } from 'react';
import { Link } from 'react-router-dom';
import type { Run } from '../types';
import { useRuns, useTask } from '../queries';
import { useParamState } from '../useParamState';
import { Field } from '../components/Field';
import { IdChip } from '../components/IdChip';
import { PageHeader } from '../components/PageHeader';
import { ProjectSelect } from '../components/ProjectSelect';
import { RelTime } from '../components/RelTime';
import { StatusPill } from '../components/StatusPill';
import { Table, type Column } from '../components/Table';
import { ErrorLine } from '../components/ErrorLine';
import { formatCost } from '../lib/format';
import { elapsedMs, formatDuration, parseTime } from '../lib/time';
import { useNow } from '../lib/clock';
import './Runs.css';

const STATUS_OPTIONS: [string, string][] = [
  ['', 'Any status'],
  ['starting,running', 'Active'],
  ['succeeded', 'Succeeded'],
  ['failed', 'Failed'],
  ['stalled', 'Stalled'],
  ['timed_out', 'Timed out'],
  ['denied', 'Denied'],
  ['cancelled', 'Cancelled'],
];

const isActive = (run: Run) => run.status === 'starting' || run.status === 'running';

/** The run's task: its title links to the run, and a line under it names the project and task. */
function TaskCell({ run }: { run: Run }) {
  const task = useTask(run.task_id);
  return (
    <div className="run-task">
      {task.isSuccess ? (
        <Link className="run-task-title" to={`/runs/${run.id}`}>
          {task.data.task.title}
        </Link>
      ) : task.isError ? (
        <span className="run-task-title muted">Task unavailable</span>
      ) : (
        <span className="run-task-title muted">Loading task…</span>
      )}
      <span className="run-task-meta">
        {run.project} · task{' '}
        <Link to={`/tasks/${run.task_id}`}>
          <IdChip id={run.task_id} />
        </Link>
      </span>
    </div>
  );
}

export function Runs() {
  const [project, setProject] = useParamState('project');
  const [status, setStatus] = useParamState('status');
  const runs = useRuns({ project, status, limit: 200 });
  const now = useNow();

  const options = STATUS_OPTIONS.some(([value]) => value === status)
    ? STATUS_OPTIONS
    : [...STATUS_OPTIONS, [status, status] as [string, string]];

  // The server returns runs newest first. Array sort is stable, so moving active runs to the top
  // keeps that order inside each group.
  const rows = useMemo(
    () => (runs.data ? [...runs.data].sort((a, b) => Number(isActive(b)) - Number(isActive(a))) : []),
    [runs.data],
  );

  const columns: Column<Run>[] = [
    { key: 'status', header: 'Status', cell: (r) => <StatusPill kind="run" status={r.status} /> },
    { key: 'task', header: 'Task', cell: (r) => <TaskCell run={r} /> },
    { key: 'role', header: 'Role', cell: (r) => r.role },
    {
      key: 'harness',
      header: 'Harness · model',
      cell: (r) => (
        <>
          {r.harness}
          {r.model ? ` · ${r.model}` : ''}
        </>
      ),
    },
    { key: 'started', header: 'Started', cell: (r) => <RelTime value={r.started_at} /> },
    {
      key: 'duration',
      header: 'Duration',
      cell: (r) => {
        const start = parseTime(r.started_at);
        return start ? formatDuration(elapsedMs(start, parseTime(r.ended_at), now)) : '—';
      },
    },
    { key: 'cost', header: 'Cost', cell: (r) => formatCost(r.cost_usd, r.cost_estimated) },
  ];

  return (
    <>
      <PageHeader title="Runs" />
      <div className="filter-bar">
        <ProjectSelect value={project} onChange={setProject} />
        <Field label="Status">
          <select value={status} onChange={(event) => setStatus(event.target.value)}>
            {options.map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <ErrorLine error={runs.error} />
      {runs.isPending ? <p className="muted">Loading runs…</p> : null}
      {runs.isSuccess ? (
        <Table columns={columns} rows={rows} rowKey={(r) => r.id} empty="No runs match these filters." />
      ) : null}
    </>
  );
}
