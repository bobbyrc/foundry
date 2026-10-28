import { Link } from 'react-router-dom';
import type { Signal } from '../types';
import { useSignals } from '../queries';
import { useParamState } from '../useParamState';
import { ErrorLine } from '../components/ErrorLine';
import { IdChip } from '../components/IdChip';
import { PageHeader } from '../components/PageHeader';
import { ProjectSelect } from '../components/ProjectSelect';
import { RelTime } from '../components/RelTime';
import { Table, type Column } from '../components/Table';
import { safeHref } from '../lib/format';

const columns: Column<Signal>[] = [
  { key: 'project', header: 'Project', cell: (s) => s.project },
  { key: 'source', header: 'Source', cell: (s) => s.source },
  {
    key: 'title',
    header: 'Title',
    cell: (s) =>
      safeHref(s.url) ? (
        <a href={safeHref(s.url)} target="_blank" rel="noopener noreferrer">
          {s.title}
        </a>
      ) : (
        s.title
      ),
  },
  { key: 'level', header: 'Level', cell: (s) => <span className="tag">{s.level}</span> },
  { key: 'count', header: 'Count', cell: (s) => s.count.toLocaleString() },
  { key: 'last_seen', header: 'Last seen', cell: (s) => <RelTime value={s.last_seen} /> },
  {
    key: 'task',
    header: 'Task',
    cell: (s) => (s.task_id ? <Link to={`/tasks/${s.task_id}`}><IdChip id={s.task_id} /></Link> : '—'),
  },
];

export function Signals() {
  const [project, setProject] = useParamState('project');
  const signals = useSignals(project);
  return (
    <>
      <PageHeader title="Signals" />
      <div className="filter-bar">
        <ProjectSelect value={project} onChange={setProject} />
      </div>
      <ErrorLine error={signals.error} />
      {signals.isSuccess ? (
        <Table columns={columns} rows={signals.data} rowKey={(s) => s.id} empty="No production signals. Observed errors appear here." />
      ) : null}
    </>
  );
}
