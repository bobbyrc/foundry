import { Link } from 'react-router-dom';
import type { Deployment } from '../types';
import { useDeployments } from '../queries';
import { useParamState } from '../useParamState';
import { ErrorLine } from '../components/ErrorLine';
import { IdChip } from '../components/IdChip';
import { PageHeader } from '../components/PageHeader';
import { ProjectSelect } from '../components/ProjectSelect';
import { RelTime } from '../components/RelTime';
import { StatusPill } from '../components/StatusPill';
import { Table, type Column } from '../components/Table';
import { safeHref, shortSha } from '../lib/format';

const columns: Column<Deployment>[] = [
  { key: 'project', header: 'Project', cell: (d) => d.project },
  { key: 'ref', header: 'Ref', cell: (d) => <span className="mono" title={d.ref}>{shortSha(d.ref)}</span> },
  { key: 'state', header: 'State', cell: (d) => <StatusPill kind="deploy" status={d.state} /> },
  {
    key: 'url',
    header: 'URL',
    cell: (d) =>
      safeHref(d.url) ? (
        <a href={safeHref(d.url)} target="_blank" rel="noopener noreferrer">
          {d.url}
        </a>
      ) : (
        '—'
      ),
  },
  {
    key: 'task',
    header: 'Task',
    cell: (d) => (d.task_id ? <Link to={`/tasks/${d.task_id}`}><IdChip id={d.task_id} /></Link> : '—'),
  },
  { key: 'started', header: 'Started', cell: (d) => <RelTime value={d.started_at} /> },
  { key: 'updated', header: 'Updated', cell: (d) => <RelTime value={d.updated_at} /> },
  { key: 'notes', header: 'Notes', cell: (d) => d.notes || '—' },
];

export function Deploys() {
  const [project, setProject] = useParamState('project');
  const deployments = useDeployments(project);
  return (
    <>
      <PageHeader title="Deploys" />
      <div className="filter-bar">
        <ProjectSelect value={project} onChange={setProject} />
      </div>
      <ErrorLine error={deployments.error} />
      {deployments.isSuccess ? (
        <Table columns={columns} rows={deployments.data} rowKey={(d) => d.id} empty="No deploys yet. Merged work is released here." />
      ) : null}
    </>
  );
}
