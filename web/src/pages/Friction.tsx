import type { Friction } from '../types';
import { useFriction } from '../queries';
import { useParamState } from '../useParamState';
import { ErrorLine } from '../components/ErrorLine';
import { Field } from '../components/Field';
import { PageHeader } from '../components/PageHeader';
import { ProjectSelect } from '../components/ProjectSelect';
import { RelTime } from '../components/RelTime';
import { Table, type Column } from '../components/Table';
import { safeHref } from '../lib/format';

/** Open is the default: a report with no known issue state counts as open. */
const ISSUE_FILTERS = [
  { value: 'open', label: 'Open' },
  { value: 'all', label: 'All' },
  { value: 'closed', label: 'Closed' },
] as const;

function matchesIssueFilter(f: Friction, filter: string): boolean {
  if (filter === 'all') return true;
  if (filter === 'closed') return f.issue_state === 'closed';
  return f.issue_state !== 'closed';
}

const columns: Column<Friction>[] = [
  { key: 'project', header: 'Project', cell: (f) => f.project },
  { key: 'title', header: 'Title', cell: (f) => f.title },
  { key: 'count', header: 'Count', cell: (f) => f.count },
  {
    key: 'issue',
    header: 'Issue',
    cell: (f) =>
      safeHref(f.issue_url) ? (
        <>
          <a href={safeHref(f.issue_url)} target="_blank" rel="noopener noreferrer">
            view issue
          </a>
          {f.issue_state === 'closed' ? (
            <>
              {' '}
              <span className="tag">closed</span>
            </>
          ) : null}
        </>
      ) : (
        '—'
      ),
  },
  { key: 'updated', header: 'Updated', cell: (f) => <RelTime value={f.updated_at} /> },
];

export function Friction() {
  const [project, setProject] = useParamState('project');
  const [issue, setIssue] = useParamState('issue');
  const issueFilter = issue || 'open';
  const friction = useFriction(project);
  return (
    <>
      <PageHeader title="Friction" />
      <div className="filter-bar">
        <ProjectSelect value={project} onChange={setProject} />
        <Field label="Issue">
          <select value={issueFilter} onChange={(event) => setIssue(event.target.value)}>
            {ISSUE_FILTERS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <ErrorLine error={friction.error} />
      {friction.isSuccess ? (
        <Table
          columns={columns}
          rows={friction.data.filter((f) => matchesIssueFilter(f, issueFilter))}
          rowKey={(f) => f.id}
          empty="No friction reported. Agents report what slows them down here."
        />
      ) : null}
    </>
  );
}
