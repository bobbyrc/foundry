import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { getEventsHistory } from '../api';
import { subscribe } from '../events';
import { keys } from '../keys';
import { formatCost } from '../lib/format';
import { elapsedMs, formatDuration, parseTime } from '../lib/time';
import { useNow } from '../lib/clock';
import { ApprovalCard } from '../components/ApprovalCard';
import { Button } from '../components/Button';
import { Callout } from '../components/Callout';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { IdChip } from '../components/IdChip';
import { RelTime } from '../components/RelTime';
import { StatCard } from '../components/StatCard';
import { StatusPill } from '../components/StatusPill';
import { PageHeader } from '../components/PageHeader';
import { RetryTaskControl, CancelTaskButton } from '../components/TaskActions';
import { useApprovals, useCancelRun, useRuns, useStats, useTask, useTasks } from '../queries';
import type { FoundryEvent, Run, Task } from '../types';

const STALE_OUTPUT_MS = 5 * 60_000;
const ACTIVITY_LIMIT = 100;

export function Overview() {
  const stats = useStats();
  const s = stats.data;
  const byStatus = s?.tasks_by_status ?? {};

  return (
    <>
      <PageHeader title="Overview" />
      <div className="stat-row">
        <StatCard label="Active runs" value={s?.active_runs} href="/runs?status=starting%2Crunning" />
        <StatCard label="Queued" value={byStatus.queued} href="/tasks?status=queued" />
        <StatCard label="Awaiting approval" value={s?.approvals_pending} href="/approvals" />
        <StatCard label="Blocked" value={byStatus.blocked} href="/tasks?status=blocked" />
        <StatCard label="Cost today" value={s ? formatCost(s.cost_today_usd) : undefined} href="/runs" />
      </div>
      <div className="two-col">
        <div className="col">
          <NeedsYou />
          <LiveRuns />
        </div>
        <Activity />
      </div>
    </>
  );
}

function NeedsYou() {
  const approvals = useApprovals('pending');
  const blocked = useTasks({ status: 'blocked', limit: 50 });
  const loaded = approvals.isSuccess && blocked.isSuccess;
  const empty = loaded && approvals.data.length === 0 && blocked.data.length === 0;

  return (
    <section className="section">
      <h2>Needs you</h2>
      <ErrorLine error={approvals.error ?? blocked.error} />
      {approvals.data?.map((approval) => <ApprovalCard key={approval.id} approval={approval} />)}
      {blocked.data?.map((task) => <BlockedTaskCard key={task.id} task={task} />)}
      {empty ? <Empty>Nothing needs you. The factory is handling everything in flight.</Empty> : null}
    </section>
  );
}

function BlockedTaskCard({ task }: { task: Task }) {
  return (
    <article className="card">
      <header className="card-head">
        <h3>
          <Link to={`/tasks/${task.id}`}>{task.title}</Link>
        </h3>
        <StatusPill status={task.status} />
      </header>
      <Callout title="Blocked">{task.blocked_reason || 'No reason was recorded.'}</Callout>
      <div className="row-actions">
        <RetryTaskControl taskId={task.id} />
        <CancelTaskButton taskId={task.id} />
      </div>
    </article>
  );
}

function LiveRuns() {
  const runs = useRuns({ status: 'starting,running', limit: 50 });
  return (
    <section className="section">
      <h2>Live runs</h2>
      <ErrorLine error={runs.error} />
      {runs.isSuccess && runs.data.length === 0 ? <Empty>No runs are active. Queued work starts when a slot is free.</Empty> : null}
      {runs.data?.map((run) => <RunRow key={run.id} run={run} />)}
    </section>
  );
}

function RunRow({ run }: { run: Run }) {
  const now = useNow();
  const task = useTask(run.task_id);
  const cancel = useCancelRun(run.id);
  const started = parseTime(run.started_at);
  const lastOutput = parseTime(run.last_output_at);
  const stale = lastOutput !== null && now - lastOutput.getTime() > STALE_OUTPUT_MS;

  return (
    <article className="card run-row">
      <header className="card-head">
        <span className="muted small">
          {run.role} · {run.project}
        </span>
        <StatusPill kind="run" status={run.status} />
      </header>
      <h3>
        <Link to={`/runs/${run.id}`}>{task.data?.task.title ?? 'Loading task…'}</Link>
      </h3>
      <p className="small muted">
        {started ? `running ${formatDuration(elapsedMs(started, null, now))}` : 'starting'}
        {' · '}
        <span className={stale ? 'warn-text' : undefined}>
          last output {lastOutput ? <RelTime value={run.last_output_at} /> : 'none yet'}
        </span>
      </p>
      <div className="row-actions">
        <Button variant="danger" confirm disabled={cancel.isPending} onClick={() => cancel.mutate()}>
          Cancel
        </Button>
      </div>
      <ErrorLine error={cancel.error} />
    </article>
  );
}

function Activity() {
  const history = useQuery({
    queryKey: keys.eventsHistory,
    queryFn: () => getEventsHistory({ limit: ACTIVITY_LIMIT }),
    refetchInterval: 30_000,
  });
  const [live, setLive] = useState<FoundryEvent[]>([]);

  useEffect(
    () =>
      subscribe((ev) => {
        // Output and chat deltas are not activity; they would flood the feed.
        if (ev.type === 'run.log' || ev.type === 'chat.delta') return;
        setLive((prev) => [ev, ...prev].slice(0, ACTIVITY_LIMIT));
      }),
    [],
  );

  const events = useMemo(() => {
    const seen = new Set<string>();
    const merged: FoundryEvent[] = [];
    for (const ev of [...live, ...(history.data ?? [])]) {
      const key = ev.id > 0 ? `id:${ev.id}` : `${ev.type}:${ev.time}:${ev.message}`;
      if (seen.has(key)) continue;
      seen.add(key);
      merged.push(ev);
    }
    return merged
      .sort((a, b) => (parseTime(b.time)?.getTime() ?? 0) - (parseTime(a.time)?.getTime() ?? 0))
      .slice(0, ACTIVITY_LIMIT);
  }, [live, history.data]);

  return (
    <section className="section">
      <h2>Activity</h2>
      <ErrorLine error={history.error} />
      {history.isSuccess && events.length === 0 ? <Empty>No activity yet. Events appear here as work moves.</Empty> : null}
      <ol className="feed">
        {events.map((ev, index) => (
          <li key={ev.id > 0 ? ev.id : `${ev.type}-${index}`} className="feed-row">
            <span className="tag mono">{ev.type}</span>
            <span className="feed-message">{ev.message}</span>
            <span className="feed-links">
              {ev.task_id ? <Link to={`/tasks/${ev.task_id}`}><IdChip id={ev.task_id} /></Link> : null}
              {ev.run_id ? <Link to={`/runs/${ev.run_id}`}><IdChip id={ev.run_id} /></Link> : null}
            </span>
            <RelTime value={ev.time} />
          </li>
        ))}
      </ol>
    </section>
  );
}
