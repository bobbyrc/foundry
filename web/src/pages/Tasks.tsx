import { useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import type { Task, TaskKind } from '../types';
import { useTasks } from '../queries';
import { useParamState } from '../useParamState';
import { Button } from '../components/Button';
import { ErrorLine } from '../components/ErrorLine';
import { Field } from '../components/Field';
import { NewTaskModal } from '../components/NewTaskModal';
import { PageHeader } from '../components/PageHeader';
import { ProjectSelect } from '../components/ProjectSelect';
import { RelTime } from '../components/RelTime';
import { RiskBadge } from '../components/RiskBadge';
import { StatusPill } from '../components/StatusPill';
import { Table, type Column } from '../components/Table';
import { cx, safeHref } from '../lib/format';

/** The statuses shown by default: everything the factory is still moving. */
const ACTIVE = ['queued', 'running', 'review', 'awaiting_approval', 'merged', 'deploying', 'blocked'];
const STATUSES = [...ACTIVE, 'done', 'failed', 'cancelled'];
const KINDS: TaskKind[] = ['feature', 'bugfix', 'issue', 'incident', 'review', 'chore', 'improve', 'question'];

const columns: Column<Task>[] = [
  { key: 'status', header: 'Status', cell: (t) => <StatusPill status={t.status} /> },
  {
    key: 'title',
    header: 'Title',
    cell: (t) => (
      <>
        <Link to={`/tasks/${t.id}`}>{t.title}</Link>
        <div className="muted small">
          <span className="tag">{t.kind}</span> {t.source}
          {t.source_ref ? ` · ${t.source_ref}` : ''}
        </div>
      </>
    ),
  },
  { key: 'project', header: 'Project', cell: (t) => t.project },
  { key: 'role', header: 'Role', cell: (t) => t.role || '—' },
  { key: 'risk', header: 'Risk', cell: (t) => <RiskBadge risk={t.risk} /> },
  {
    key: 'pr',
    header: 'PR',
    cell: (t) =>
      t.pr_number ? (
        safeHref(t.pr_url) ? (
          <a href={safeHref(t.pr_url)} target="_blank" rel="noopener noreferrer">
            #{t.pr_number}
          </a>
        ) : (
          `#${t.pr_number}`
        )
      ) : (
        '—'
      ),
  },
  { key: 'attempts', header: 'Attempts', cell: (t) => `${t.attempts}/${t.max_attempts}` },
  { key: 'updated', header: 'Updated', cell: (t) => <RelTime value={t.updated_at} /> },
];

export function Tasks() {
  const [params, setParams] = useSearchParams();
  const [project, setProject] = useParamState('project');
  const [kind, setKind] = useParamState('kind');
  const [search, setSearch] = useParamState('q');
  const [creating, setCreating] = useState(false);

  // An absent status param means the default active set; an empty one means every status.
  const statusParam = params.get('status');
  const selected = statusParam === null ? ACTIVE : statusParam.split(',').filter(Boolean);
  const setSelected = (next: string[]) =>
    setParams(
      (prev) => {
        const copy = new URLSearchParams(prev);
        copy.set('status', next.join(','));
        return copy;
      },
      { replace: true },
    );
  const toggleStatus = (status: string) =>
    setSelected(selected.includes(status) ? selected.filter((s) => s !== status) : [...selected, status]);
  const isActiveSet = selected.length === ACTIVE.length && ACTIVE.every((s) => selected.includes(s));

  const tasks = useTasks({
    project,
    kind,
    status: selected.join(','),
    limit: 200,
  });

  const rows = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return (tasks.data ?? []).filter((t) => needle === '' || t.title.toLowerCase().includes(needle));
  }, [tasks.data, search]);

  return (
    <>
      <PageHeader title="Tasks" actions={<Button variant="primary" onClick={() => setCreating(true)}>New task</Button>} />
      <div className="filter-bar">
        <ProjectSelect value={project} onChange={setProject} />
        <Field label="Kind">
          <select value={kind} onChange={(event) => setKind(event.target.value)}>
            <option value="">All kinds</option>
            {KINDS.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Search titles">
          <input type="search" value={search} onChange={(event) => setSearch(event.target.value)} />
        </Field>
      </div>
      <div className="chip-row" role="group" aria-label="Status filter">
        <button type="button" className={cx('chip', isActiveSet && 'chip-on')} aria-pressed={isActiveSet} onClick={() => setSelected(ACTIVE)}>
          active
        </button>
        {STATUSES.map((status) => {
          const on = selected.includes(status);
          return (
            <button key={status} type="button" className={cx('chip', on && 'chip-on')} aria-pressed={on} onClick={() => toggleStatus(status)}>
              {status.replace(/_/g, ' ')}
            </button>
          );
        })}
      </div>
      <ErrorLine error={tasks.error} />
      {tasks.isPending ? <p className="muted">Loading tasks…</p> : null}
      {tasks.isSuccess ? (
        <Table
          columns={columns}
          rows={rows}
          rowKey={(t) => t.id}
          empty="No tasks match these filters. Clear a filter, or create a task with New task."
        />
      ) : null}
      <NewTaskModal open={creating} onClose={() => setCreating(false)} defaultProject={project} />
    </>
  );
}
