import { Link, useParams } from 'react-router-dom';
import type { ReactNode } from 'react';
import type { Run, Task } from '../types';
import { usePausedProjects, useTask } from '../queries';
import { ApprovalCard } from '../components/ApprovalCard';
import { Callout } from '../components/Callout';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { IdChip } from '../components/IdChip';
import { Markdown } from '../components/Markdown';
import { PageHeader } from '../components/PageHeader';
import { RelTime } from '../components/RelTime';
import { RiskBadge } from '../components/RiskBadge';
import { StatusPill } from '../components/StatusPill';
import { Table, type Column } from '../components/Table';
import { CancelTaskButton, RetryTaskControl } from '../components/TaskActions';
import { formatCost, safeHref } from '../lib/format';
import { elapsedMs, formatDuration, parseTime } from '../lib/time';
import { useNow } from '../lib/clock';

const TERMINAL_NO_CANCEL = ['done', 'failed', 'cancelled'];
const RETRYABLE = ['blocked', 'failed', 'cancelled'];

export function TaskDetail() {
  const { id = '' } = useParams();
  const detail = useTask(id);
  const pausedProjects = usePausedProjects();

  if (detail.isError) return <ErrorLine error={detail.error} />;
  if (!detail.data) return <p className="muted">Loading task…</p>;

  const { task, runs, approvals, children } = detail.data;
  const canCancel = !TERMINAL_NO_CANCEL.includes(task.status);
  const canRetry = RETRYABLE.includes(task.status);
  const queuedInPausedProject = task.status === 'queued' && pausedProjects.includes(task.project);

  return (
    <>
      <PageHeader
        title={
          <>
            {task.title} <StatusPill status={task.status} />
          </>
        }
        actions={
          <>
            {canCancel ? <CancelTaskButton taskId={task.id} /> : null}
            {canRetry ? <RetryTaskControl taskId={task.id} /> : null}
          </>
        }
      />
      {task.status === 'blocked' ? <Callout title="Blocked">{task.blocked_reason || 'No reason was recorded.'}</Callout> : null}
      {queuedInPausedProject ? (
        <Callout tone="muted">This project is paused in foundry.yaml, so queued work will not start.</Callout>
      ) : null}

      <dl className="meta">
        <MetaItem label="Project">{task.project}</MetaItem>
        <MetaItem label="Kind">{task.kind}</MetaItem>
        <MetaItem label="Role">{task.role || '—'}</MetaItem>
        <MetaItem label="Source">
          {task.source}
          {task.source_ref ? <span className="mono"> · {task.source_ref}</span> : null}
        </MetaItem>
        <MetaItem label="Branch">{task.branch ? <span className="mono">{task.branch}</span> : '—'}</MetaItem>
        <MetaItem label="PR">
          {task.pr_number ? (
            safeHref(task.pr_url) ? (
              <a href={safeHref(task.pr_url)} target="_blank" rel="noopener noreferrer">
                #{task.pr_number}
              </a>
            ) : (
              `#${task.pr_number}`
            )
          ) : (
            '—'
          )}
        </MetaItem>
        <MetaItem label="Risk">
          <RiskBadge risk={task.risk} />
          {task.risk_reasons && task.risk_reasons.length > 0 ? (
            <ul className="reasons">
              {task.risk_reasons.map((reason) => (
                <li key={reason}>{reason}</li>
              ))}
            </ul>
          ) : null}
        </MetaItem>
        <MetaItem label="Attempts">{`${task.attempts}/${task.max_attempts}`}</MetaItem>
        <MetaItem label="Created">
          <RelTime value={task.created_at} />
        </MetaItem>
        <MetaItem label="Updated">
          <RelTime value={task.updated_at} />
        </MetaItem>
        <MetaItem label="ID">
          <IdChip id={task.id} />
        </MetaItem>
        {task.parent_id ? (
          <MetaItem label="Parent">
            <Link to={`/tasks/${task.parent_id}`}>
              <IdChip id={task.parent_id} />
            </Link>
          </MetaItem>
        ) : null}
      </dl>

      <Section title="Summary">
        {task.summary ? <Markdown text={task.summary} /> : <Empty>No summary yet. The latest agent summary appears here.</Empty>}
      </Section>
      <Section title="Body">
        {task.body ? <Markdown text={task.body} /> : <Empty>This task has no body.</Empty>}
      </Section>
      <Section title="Approvals">
        {approvals.length === 0 ? <Empty>No approvals for this task.</Empty> : null}
        {approvals.map((approval) => (
          <ApprovalCard key={approval.id} approval={approval} showTask={false} />
        ))}
      </Section>
      <Section title="Subtasks">
        <Table<Task>
          columns={[
            { key: 'status', header: 'Status', cell: (t) => <StatusPill status={t.status} /> },
            { key: 'title', header: 'Title', cell: (t) => <Link to={`/tasks/${t.id}`}>{t.title}</Link> },
            { key: 'kind', header: 'Kind', cell: (t) => t.kind },
            { key: 'role', header: 'Role', cell: (t) => t.role || '—' },
          ]}
          rows={children}
          rowKey={(t) => t.id}
          empty="No subtasks. Follow-up work the agent splits off appears here."
        />
      </Section>
      <Section title="Runs">
        <RunsTable runs={runs} />
      </Section>
    </>
  );
}

function MetaItem({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="meta-item">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="section">
      <h2>{title}</h2>
      {children}
    </section>
  );
}

function RunsTable({ runs }: { runs: Run[] }) {
  const now = useNow();
  const columns: Column<Run>[] = [
    { key: 'status', header: 'Status', cell: (r) => <StatusPill kind="run" status={r.status} /> },
    {
      key: 'harness',
      header: 'Harness',
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
        if (!start) return '—';
        return formatDuration(elapsedMs(start, parseTime(r.ended_at), now));
      },
    },
    { key: 'cost', header: 'Cost', cell: (r) => formatCost(r.cost_usd) },
    { key: 'link', header: 'Run', cell: (r) => <Link to={`/runs/${r.id}`}><IdChip id={r.id} /></Link> },
  ];
  return <Table columns={columns} rows={runs} rowKey={(r) => r.id} empty="No runs yet. A run starts when the task is picked up." />;
}
