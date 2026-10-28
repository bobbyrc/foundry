import { Link, useParams } from 'react-router-dom';
import type { ReactNode } from 'react';
import { cx, formatCost, formatTokens, shortId } from '../lib/format';
import { isActiveRun } from '../lib/status';
import { elapsedMs, formatDuration, parseTime } from '../lib/time';
import { useNow } from '../lib/clock';
import { axisMinutes, axisTicks, runBar } from '../lib/runbar';
import { useCan } from '../auth';
import { useCancelRun, useRun, useTask } from '../queries';
import { Button } from '../components/Button';
import { Callout } from '../components/Callout';
import { ErrorLine } from '../components/ErrorLine';
import { IdChip } from '../components/IdChip';
import { LogViewer } from '../components/LogViewer';
import { RelTime } from '../components/RelTime';
import { StatusPill } from '../components/StatusPill';
import './RunDetail.css';

/** Output silence after which the time bar draws its yellow quiet part (same rule as Overview). */
const QUIET_AFTER_MS = 5 * 60_000;

export function RunDetail() {
  const { id = '' } = useParams();
  const run = useRun(id);
  const task = useTask(run.data?.task_id ?? '');
  const cancel = useCancelRun(id);
  const now = useNow();
  const canOperate = useCan('operate');

  if (run.isError) return <ErrorLine error={run.error} />;
  if (!run.data) return <p className="muted">Loading run…</p>;

  const r = run.data;
  const active = isActiveRun(r.status);
  const start = parseTime(r.started_at);
  const end = parseTime(r.ended_at);
  const lastOutput = parseTime(r.last_output_at);
  const title = [r.role, r.project].filter(Boolean).join(' · ');

  return (
    <div className="run-detail">
      <header className="page-header run-header">
        <div className="run-heading">
          <h1>
            {title} <StatusPill kind="run" status={r.status} />
          </h1>
          <p className="run-sub muted">
            {r.harness}
            {r.model ? ` · ${r.model}` : ''}
            {r.task_id ? (
              <>
                {' · '}
                <Link to={`/tasks/${r.task_id}`}>{task.data?.task.title ?? shortId(r.task_id)}</Link>
              </>
            ) : null}
          </p>
        </div>
        {active && canOperate ? (
          <div className="page-actions">
            <Button variant="danger" confirm disabled={cancel.isPending} onClick={() => cancel.mutate()}>
              Cancel run
            </Button>
          </div>
        ) : null}
      </header>
      <ErrorLine error={cancel.error} />

      {start ? (
        <TimeStrip status={r.status} start={start} end={end} lastOutput={lastOutput} now={now} />
      ) : null}

      <dl className="run-facts">
        <Fact label="Branch">{r.branch ? <span className="mono">{r.branch}</span> : '—'}</Fact>
        <Fact label="Cost">{formatCost(r.cost_usd, r.cost_estimated)}</Fact>
        <Fact label="Tokens">{`in ${formatTokens(r.input_tokens)} · out ${formatTokens(r.output_tokens)}`}</Fact>
        <Fact label="Started">
          <RelTime value={r.started_at} />
        </Fact>
        <Fact label="Duration">{start ? formatDuration(elapsedMs(start, end, now)) : '—'}</Fact>
        {end ? <Fact label="Exit code">{r.exit_code}</Fact> : null}
      </dl>
      <details className="run-more">
        <summary className="link-button">More</summary>
        <dl className="run-facts">
          <Fact label="Workdir">{r.workdir ? <span className="mono">{r.workdir}</span> : '—'}</Fact>
          <Fact label="Session">{r.session_id ? <span className="mono">{r.session_id}</span> : '—'}</Fact>
          <Fact label="Run ID">
            <IdChip id={r.id} />
          </Fact>
        </dl>
      </details>

      {r.error ? <Callout title="Error">{r.error}</Callout> : null}

      <LogViewer runId={r.id} active={active} />
    </div>
  );
}

/**
 * The run's bar on its own time axis. A live run is drawn to its elapsed time, green up to the
 * last output and yellow after a quiet spell. A finished run is one solid bar in its end colour.
 */
function TimeStrip({
  status,
  start,
  end,
  lastOutput,
  now,
}: {
  status: string;
  start: Date;
  end: Date | null;
  lastOutput: Date | null;
  now: number;
}) {
  const active = isActiveRun(status);
  const stop = active || !end ? now : end.getTime();
  const elapsed = Math.max(0, stop - start.getTime());
  const scale = axisMinutes(elapsed);
  const bar = active
    ? runBar(start.getTime(), lastOutput?.getTime() ?? null, now, scale, QUIET_AFTER_MS)
    : runBar(start.getTime(), null, stop, scale, Infinity);
  const quietMs = lastOutput ? now - lastOutput.getTime() : elapsed;
  const quiet = active && quietMs > QUIET_AFTER_MS;

  return (
    <section className={cx('run-time', !active && `run-time--${endTone(status)}`)} aria-label="Run time">
      <div className="run-time-head">
        <div className="run-time-ticks" aria-hidden="true">
          {axisTicks(scale).map((tick) => (
            <span key={tick}>{tick}</span>
          ))}
        </div>
        <p className="run-time-text">
          {formatDuration(elapsed)}
          {active ? (
            <>
              {' · '}
              {quiet ? (
                <span className="warn-text">quiet for {formatDuration(quietMs)}</span>
              ) : (
                <>output {lastOutput ? <RelTime value={lastOutput.toISOString()} /> : 'none yet'}</>
              )}
            </>
          ) : null}
        </p>
      </div>
      <div className="run-track run-time-track" aria-hidden="true">
        <span className="run-flow" style={{ width: `${bar.flow}%` }} />
        {bar.quiet > 0 ? <span className="run-quiet" style={{ left: `${bar.flow}%`, width: `${bar.quiet}%` }} /> : null}
      </div>
    </section>
  );
}

/** Which lamp colour a finished run's bar takes: stopped runs red, cancelled grey, the rest done grey. */
function endTone(status: string): 'stop' | 'off' | 'done' {
  if (status === 'cancelled') return 'off';
  if (status === 'succeeded') return 'done';
  return 'stop';
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="run-fact">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}
