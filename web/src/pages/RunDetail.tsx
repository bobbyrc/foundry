import { Link, useParams } from 'react-router-dom';
import type { ReactNode } from 'react';
import { cx, formatCost, formatTokens, shortId } from '../lib/format';
import { isActiveRun } from '../lib/status';
import { elapsedMs, formatDuration, parseTime } from '../lib/time';
import { useNow } from '../lib/clock';
import { useCancelRun, useRun, useTask } from '../queries';
import { Button } from '../components/Button';
import { Callout } from '../components/Callout';
import { ErrorLine } from '../components/ErrorLine';
import { IdChip } from '../components/IdChip';
import { LogViewer } from '../components/LogViewer';
import { PageHeader } from '../components/PageHeader';
import { RelTime } from '../components/RelTime';
import { StatusPill } from '../components/StatusPill';

export function RunDetail() {
  const { id = '' } = useParams();
  const run = useRun(id);
  const task = useTask(run.data?.task_id ?? '');
  const cancel = useCancelRun(id);
  const now = useNow();

  if (run.isError) return <ErrorLine error={run.error} />;
  if (!run.data) return <p className="muted">Loading run…</p>;

  const r = run.data;
  const active = isActiveRun(r.status);
  const start = parseTime(r.started_at);
  const end = parseTime(r.ended_at);

  return (
    <>
      <PageHeader
        title={
          <>
            {r.role} · {r.harness}
            {r.model ? ` · ${r.model}` : ''} <StatusPill kind="run" status={r.status} />
          </>
        }
        actions={
          active ? (
            <Button variant="danger" confirm disabled={cancel.isPending} onClick={() => cancel.mutate()}>
              Cancel run
            </Button>
          ) : null
        }
      />
      <ErrorLine error={cancel.error} />
      {r.error ? <Callout title="Error">{r.error}</Callout> : null}

      <dl className="meta">
        <MetaItem label="Task">
          {r.task_id ? <Link to={`/tasks/${r.task_id}`}>{task.data?.task.title ?? shortId(r.task_id)}</Link> : '—'}
        </MetaItem>
        <MetaItem label="Project">{r.project || '—'}</MetaItem>
        <MetaItem label="Branch">{r.branch ? <span className="mono">{r.branch}</span> : '—'}</MetaItem>
        <MetaItem label="Workdir" wide>
          {r.workdir ? <span className="mono">{r.workdir}</span> : '—'}
        </MetaItem>
        <MetaItem label="Session">{r.session_id ? <span className="mono">{r.session_id}</span> : '—'}</MetaItem>
        <MetaItem label="Exit code">{r.exit_code}</MetaItem>
        <MetaItem label="Cost">{formatCost(r.cost_usd)}</MetaItem>
        <MetaItem label="Tokens">{`in ${formatTokens(r.input_tokens)} · out ${formatTokens(r.output_tokens)}`}</MetaItem>
        <MetaItem label="Started">
          <RelTime value={r.started_at} />
        </MetaItem>
        <MetaItem label="Ended">{end ? <RelTime value={r.ended_at} /> : '—'}</MetaItem>
        <MetaItem label="Duration">{start ? formatDuration(elapsedMs(start, end, now)) : '—'}</MetaItem>
        <MetaItem label="Run ID">
          <IdChip id={r.id} />
        </MetaItem>
      </dl>

      <LogViewer runId={r.id} active={active} />
    </>
  );
}

function MetaItem({ label, children, wide = false }: { label: string; children: ReactNode; wide?: boolean }) {
  return (
    <div className={cx('meta-item', wide && 'meta-item-wide')}>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}
