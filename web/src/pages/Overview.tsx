import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { getEventsHistory } from '../api';
import { subscribe } from '../events';
import { keys } from '../keys';
import { formatClock } from '../lib/format';
import { elapsedMs, formatDuration, formatRelative, parseTime } from '../lib/time';
import { axisMinutes, axisTicks, runBar } from '../lib/runbar';
import { useNow } from '../lib/clock';
import { AndonBoard } from '../components/AndonBoard';
import { ApprovalCard } from '../components/ApprovalCard';
import { Button } from '../components/Button';
import { Callout } from '../components/Callout';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { RelTime } from '../components/RelTime';
import { RetryTaskControl, CancelTaskButton } from '../components/TaskActions';
import { useCan } from '../auth';
import { useApprovals, useCancelRun, useRuns, useTask, useTasks } from '../queries';
import type { FoundryEvent, Run, Task } from '../types';

const STALE_OUTPUT_MS = 5 * 60_000;
const ACTIVITY_LIMIT = 100;
/** Rows shown before "Show all": enough to see what just happened without burying the page. */
const ACTIVITY_SHOWN = 10;

export function Overview() {
  return (
    <>
      <h1 className="sr-only">Overview</h1>
      <AndonBoard />
      <div className="overview-grid">
        <NeedsYou />
        <div className="overview-side">
          <LiveRuns />
          <Activity />
        </div>
      </div>
    </>
  );
}

function NeedsYou() {
  const approvals = useApprovals('pending');
  const blocked = useTasks({ status: 'blocked', limit: 50 });
  const loaded = approvals.isSuccess && blocked.isSuccess;
  const count = (approvals.data?.length ?? 0) + (blocked.data?.length ?? 0);
  const oldest = useMemo(() => {
    const times = [...(approvals.data ?? []).map((a) => a.created_at), ...(blocked.data ?? []).map((t) => t.updated_at)]
      .map((t) => parseTime(t)?.getTime())
      .filter((t): t is number => t !== undefined);
    return times.length > 0 ? new Date(Math.min(...times)) : null;
  }, [approvals.data, blocked.data]);
  const now = useNow();

  return (
    <section className="section" aria-labelledby="needs-you">
      <h2 id="needs-you">
        Needs you
        {loaded && count > 0 ? (
          <span className="section-note">
            {count} waiting{oldest ? ` · oldest ${formatRelative(oldest, now)}` : ''}
          </span>
        ) : null}
      </h2>
      <ErrorLine error={approvals.error ?? blocked.error} />
      {approvals.data?.map((approval) => <ApprovalCard key={approval.id} approval={approval} />)}
      {blocked.data?.map((task) => <BlockedTaskCard key={task.id} task={task} />)}
      {loaded && count === 0 ? <Empty>Nothing needs you. The factory is handling everything in flight.</Empty> : null}
    </section>
  );
}

function BlockedTaskCard({ task }: { task: Task }) {
  return (
    <article className="call-item is-stop">
      <div className="call-title">
        <span className="lamp lamp-stop" aria-hidden="true" />
        <h3>
          <Link to={`/tasks/${task.id}`}>{task.title}</Link>
        </h3>
      </div>
      <p className="call-meta">
        Blocked · {task.project} · attempt {task.attempts} of {task.max_attempts} · <RelTime value={task.updated_at} />
      </p>
      <div className="call-body">
        <Callout>{task.blocked_reason || 'No reason was recorded.'}</Callout>
        <div className="row-actions">
          <RetryTaskControl taskId={task.id} />
          <CancelTaskButton taskId={task.id} />
        </div>
      </div>
    </article>
  );
}

function LiveRuns() {
  const runs = useRuns({ status: 'starting,running', limit: 50 });
  const now = useNow();
  const longest = Math.max(0, ...(runs.data ?? []).map((r) => now - (parseTime(r.started_at)?.getTime() ?? now)));
  const scale = axisMinutes(longest);
  const count = runs.data?.length ?? 0;

  return (
    <section className="section" aria-labelledby="live-runs">
      <h2 id="live-runs">
        Live runs
        {count > 0 ? <span className="section-note">{count} active</span> : null}
      </h2>
      <ErrorLine error={runs.error} />
      {runs.isSuccess && count === 0 ? <Empty>No runs are active. Queued work starts when a slot is free.</Empty> : null}
      {count > 0 ? (
        <>
          <div className="run-axis" aria-hidden="true">
            <div className="run-axis-ticks">
              {axisTicks(scale).map((tick) => (
                <span key={tick}>{tick}</span>
              ))}
            </div>
          </div>
          <ul className="run-list">
            {runs.data?.map((run) => <RunItem key={run.id} run={run} scale={scale} now={now} />)}
          </ul>
        </>
      ) : null}
    </section>
  );
}

function RunItem({ run, scale, now }: { run: Run; scale: number; now: number }) {
  const task = useTask(run.task_id);
  const cancel = useCancelRun(run.id);
  const canOperate = useCan('operate');
  const started = parseTime(run.started_at);
  const lastOutput = parseTime(run.last_output_at);
  const quietMs = lastOutput ? now - lastOutput.getTime() : started ? now - started.getTime() : 0;
  const stale = quietMs > STALE_OUTPUT_MS;
  const bar = started ? runBar(started.getTime(), lastOutput?.getTime() ?? null, now, scale, STALE_OUTPUT_MS) : { flow: 0, quiet: 0 };

  return (
    <li className="run-item">
      <div className="run-item-head">
        <Link to={`/runs/${run.id}`}>{task.data?.task.title ?? 'Loading task…'}</Link>
        {canOperate ? (
          <Button variant="ghost" confirm disabled={cancel.isPending} onClick={() => cancel.mutate()}>
            Cancel
          </Button>
        ) : null}
      </div>
      <div className="run-item-meta">
        <span>
          {run.role} · {run.project}
        </span>
        <span>
          {started ? formatDuration(elapsedMs(started, null, now)) : 'starting'}
          {' · '}
          {stale ? (
            <span className="warn-text">quiet for {formatDuration(quietMs)}</span>
          ) : (
            <>output {lastOutput ? <RelTime value={run.last_output_at} /> : 'none yet'}</>
          )}
        </span>
      </div>
      <div className="run-track" aria-hidden="true">
        <span className="run-flow" style={{ width: `${bar.flow}%` }} />
        {bar.quiet > 0 ? <span className="run-quiet" style={{ left: `${bar.flow}%`, width: `${bar.quiet}%` }} /> : null}
      </div>
      <ErrorLine error={cancel.error} />
    </li>
  );
}

function Activity() {
  const history = useQuery({
    queryKey: keys.eventsHistory,
    queryFn: () => getEventsHistory({ limit: ACTIVITY_LIMIT }),
    refetchInterval: 30_000,
  });
  const [live, setLive] = useState<FoundryEvent[]>([]);
  const [showAll, setShowAll] = useState(false);

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
    <section className="section" aria-labelledby="activity">
      <h2 id="activity">
        Activity <span className="section-note">newest first</span>
      </h2>
      <ErrorLine error={history.error} />
      {history.isSuccess && events.length === 0 ? <Empty>No activity yet. Events appear here as work moves.</Empty> : null}
      <ol className="feed">
        {(showAll ? events : events.slice(0, ACTIVITY_SHOWN)).map((ev, index) => {
          const time = parseTime(ev.time);
          return (
            <li key={ev.id > 0 ? ev.id : `${ev.type}-${index}`} className="feed-row">
              <time className="feed-time" dateTime={time?.toISOString()} title={time?.toLocaleString()}>
                {time ? formatClock(time) : '—'}
              </time>
              <span className="tag" title={ev.type}>
                {eventLabel(ev.type)}
              </span>
              <span className="feed-message">
                {ev.message}
                {ev.task_id ? (
                  <>
                    {' '}
                    <Link to={`/tasks/${ev.task_id}`} className="feed-link">
                      task
                    </Link>
                  </>
                ) : ev.run_id ? (
                  <>
                    {' '}
                    <Link to={`/runs/${ev.run_id}`} className="feed-link">
                      run
                    </Link>
                  </>
                ) : null}
              </span>
            </li>
          );
        })}
      </ol>
      {events.length > ACTIVITY_SHOWN ? (
        <button type="button" className="link-button small feed-more" aria-expanded={showAll} onClick={() => setShowAll(!showAll)}>
          {showAll ? 'Show fewer' : `Show all ${events.length}`}
        </button>
      ) : null}
    </section>
  );
}

/** "approval.requested" reads as "approval requested". */
function eventLabel(type: string): string {
  return type.replace(/[._]/g, ' ');
}
