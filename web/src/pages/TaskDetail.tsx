import { Link, useParams } from 'react-router-dom';
import type { ReactNode } from 'react';
import type { Approval, Run, Task } from '../types';
import { usePausedProjects, useTask } from '../queries';
import { ApprovalCard } from '../components/ApprovalCard';
import { Callout } from '../components/Callout';
import { ErrorLine } from '../components/ErrorLine';
import { IdChip } from '../components/IdChip';
import { Markdown } from '../components/Markdown';
import { PageHeader } from '../components/PageHeader';
import { RelTime } from '../components/RelTime';
import { RiskBadge } from '../components/RiskBadge';
import { StatusPill } from '../components/StatusPill';
import { Table, type Column } from '../components/Table';
import { TaskLine } from '../components/TaskLine';
import { CancelTaskButton, RetryTaskControl } from '../components/TaskActions';
import { formatCost, safeHref } from '../lib/format';
import { elapsedMs, formatDuration, parseTime } from '../lib/time';
import { useNow } from '../lib/clock';
import './TaskDetail.css';

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
  const pendingApprovals = approvals.filter((a) => a.status === 'pending');
  const decidedApprovals = approvals.filter((a) => a.status !== 'pending');
  const needsYou = task.status === 'blocked' || queuedInPausedProject || pendingApprovals.length > 0;
  const kicker = [task.kind, task.project, task.role, `from ${task.source}`].filter(Boolean).join(' · ');
  const actions = (
    <>
      {canCancel ? <CancelTaskButton taskId={task.id} /> : null}
      {canRetry ? <RetryTaskControl taskId={task.id} /> : null}
    </>
  );

  return (
    <>
      <PageHeader
        title={
          <>
            {task.title} <StatusPill status={task.status} />
          </>
        }
        actions={task.status === 'blocked' ? null : actions}
      />
      <p className="task-meta muted">
        <span>{kicker}</span>
        <IdChip id={task.id} />
        {runs.length > 0 ? <Link to={`/chat/task/${task.id}`}>Open conversation</Link> : null}
      </p>
      <TaskLine status={task.status} hasPR={Boolean(task.pr_number)} approvalCount={approvals.length} />

      <div className="task-body">
        <div className="task-main">
          {needsYou ? (
            <Section title="Needs you" className="task-needs">
              {task.status === 'blocked' ? (
                <>
                  <Callout title="Blocked">{task.blocked_reason || 'No reason was recorded.'}</Callout>
                  <div className="task-needs-actions">{actions}</div>
                </>
              ) : null}
              {queuedInPausedProject ? (
                <Callout tone="muted">This project is paused in onett.yaml, so queued work will not start.</Callout>
              ) : null}
              {pendingApprovals.map((approval) => (
                <ApprovalCard key={approval.id} approval={approval} showTask={false} />
              ))}
            </Section>
          ) : null}

          <div className="task-rest">
            {task.summary ? (
              <Section title="Summary">
                <Markdown text={task.summary} />
              </Section>
            ) : null}
            {task.body ? (
              <Section title="Body">
                <Markdown text={task.body} />
              </Section>
            ) : null}
            <Section title="Runs">
              <RunsTable runs={runs} />
            </Section>
            {children.length > 0 ? (
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
            ) : null}
            {decidedApprovals.length > 0 ? (
              <Section title="Decided approvals">
                <DecidedApprovals approvals={decidedApprovals} />
              </Section>
            ) : null}
          </div>
        </div>

        <dl className="spec-sheet">
          <SpecItem label="Risk">
            <RiskBadge risk={task.risk} />
            {task.risk_reasons && task.risk_reasons.length > 0 ? (
              <ul className="reasons">
                {task.risk_reasons.map((reason) => (
                  <li key={reason}>{reason}</li>
                ))}
              </ul>
            ) : null}
          </SpecItem>
          {task.pr_number ? (
            <SpecItem label="Pull request">
              {safeHref(task.pr_url) ? (
                <a href={safeHref(task.pr_url)} target="_blank" rel="noopener noreferrer">
                  #{task.pr_number}
                </a>
              ) : (
                `#${task.pr_number}`
              )}
              {task.branch ? <div className="mono spec-sub">{task.branch}</div> : null}
            </SpecItem>
          ) : task.branch ? (
            <SpecItem label="Branch">
              <span className="mono">{task.branch}</span>
            </SpecItem>
          ) : null}
          <SpecItem label="Attempts">{`${task.attempts} of ${task.max_attempts}`}</SpecItem>
          <SpecItem label="Project">{task.project}</SpecItem>
          <SpecItem label="Kind">{task.kind}</SpecItem>
          {task.role ? <SpecItem label="Role">{task.role}</SpecItem> : null}
          <SpecItem label="Source">
            {task.source}
            {task.source_ref ? <div className="mono spec-sub">{task.source_ref}</div> : null}
          </SpecItem>
          {task.parent_id ? (
            <SpecItem label="Parent">
              <Link to={`/tasks/${task.parent_id}`}>
                <IdChip id={task.parent_id} />
              </Link>
            </SpecItem>
          ) : null}
          <SpecItem label="Created">
            <RelTime value={task.created_at} />
          </SpecItem>
          <SpecItem label="Updated">
            <RelTime value={task.updated_at} />
          </SpecItem>
          <SpecItem label="ID">
            <IdChip id={task.id} />
          </SpecItem>
        </dl>
      </div>
    </>
  );
}

function SpecItem({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="spec-item">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function Section({ title, className, children }: { title: string; className?: string; children: ReactNode }) {
  return (
    <section className={className ? `section ${className}` : 'section'}>
      <h2>{title}</h2>
      {children}
    </section>
  );
}

function DecidedApprovals({ approvals }: { approvals: Approval[] }) {
  return (
    <ul className="decided-list">
      {approvals.map((a) => (
        <li key={a.id} className="decided-row">
          <StatusPill kind="approval" status={a.status} />
          <span className="decided-title">{a.title}</span>
          <RelTime value={a.decided_at} />
          {a.note ? <span className="decided-note">{a.note}</span> : null}
        </li>
      ))}
    </ul>
  );
}

function RunsTable({ runs }: { runs: Run[] }) {
  const now = useNow();
  // The attempt number counts runs in the order they started, from 1.
  const attempt = new Map(
    [...runs]
      .sort((a, b) => (parseTime(a.started_at)?.getTime() ?? 0) - (parseTime(b.started_at)?.getTime() ?? 0))
      .map((r, i) => [r.id, i + 1]),
  );
  const columns: Column<Run>[] = [
    { key: 'attempt', header: '#', cell: (r) => attempt.get(r.id) ?? '—' },
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
    { key: 'cost', header: 'Cost', cell: (r) => formatCost(r.cost_usd, r.cost_estimated) },
    { key: 'link', header: 'Run', cell: (r) => <Link to={`/runs/${r.id}`}><IdChip id={r.id} /></Link> },
  ];
  return <Table columns={columns} rows={runs} rowKey={(r) => r.id} empty="No runs yet. A run starts when the task is picked up." />;
}
