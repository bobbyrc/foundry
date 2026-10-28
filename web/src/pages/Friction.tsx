import type { Friction } from '../types';
import { useFriction } from '../queries';
import { useParamState } from '../useParamState';
import { ErrorLine } from '../components/ErrorLine';
import { PageHeader } from '../components/PageHeader';
import { ProjectSelect } from '../components/ProjectSelect';
import { RelTime } from '../components/RelTime';
import { Table, type Column } from '../components/Table';
import { safeHref } from '../lib/format';

const columns: Column<Friction>[] = [
  { key: 'project', header: 'Project', cell: (f) => f.project },
  { key: 'title', header: 'Title', cell: (f) => f.title },
  { key: 'count', header: 'Count', cell: (f) => f.count },
  {
    key: 'issue',
    header: 'Issue',
    cell: (f) =>
      safeHref(f.issue_url) ? (
        <a href={safeHref(f.issue_url)} target="_blank" rel="noopener noreferrer">
          view issue
        </a>
      ) : (
        '—'
      ),
  },
  { key: 'updated', header: 'Updated', cell: (f) => <RelTime value={f.updated_at} /> },
];

export function Friction() {
  const [project, setProject] = useParamState('project');
  const friction = useFriction(project);
  return (
    <>
      <PageHeader title="Friction" />
      <div className="filter-bar">
        <ProjectSelect value={project} onChange={setProject} />
      </div>
      <ErrorLine error={friction.error} />
      {friction.isSuccess ? (
        <Table columns={columns} rows={friction.data} rowKey={(f) => f.id} empty="No friction reported. Agents report what slows them down here." />
      ) : null}
    </>
  );
}
