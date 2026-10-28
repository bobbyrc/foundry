import { useRunLoop, useLoops } from '../queries';
import { Button } from '../components/Button';
import { ErrorLine } from '../components/ErrorLine';
import { PageHeader } from '../components/PageHeader';
import { RelTime } from '../components/RelTime';
import { Table, type Column } from '../components/Table';
import type { LoopView } from '../types';
import { parseTime } from '../lib/time';

export function Loops() {
  const loops = useLoops();
  const runLoop = useRunLoop();

  const columns: Column<LoopView>[] = [
    { key: 'name', header: 'Name', cell: (l) => <span className="mono">{l.name}</span> },
    { key: 'project', header: 'Project', cell: (l) => l.project || 'all projects' },
    {
      key: 'enabled',
      header: 'Enabled',
      cell: (l) => <span className={`tag ${l.enabled ? 'tag-ok' : ''}`}>{l.enabled ? 'enabled' : 'disabled'}</span>,
    },
    { key: 'every', header: 'Every', cell: (l) => l.every },
    {
      key: 'last_run',
      header: 'Last run',
      cell: (l) => (parseTime(l.last_run_at) ? <RelTime value={l.last_run_at} /> : 'never'),
    },
    {
      key: 'last_error',
      header: 'Last error',
      cell: (l) =>
        l.last_error ? (
          <span className="danger-text ellipsis" title={l.last_error}>
            {l.last_error}
          </span>
        ) : (
          '—'
        ),
    },
    {
      key: 'run',
      header: 'Run',
      cell: (l) => {
        const pending = runLoop.isPending && runLoop.variables?.name === l.name && runLoop.variables.project === l.project;
        return (
          <Button disabled={pending} onClick={() => runLoop.mutate({ name: l.name, project: l.project })}>
            {pending ? 'Starting…' : 'Run now'}
          </Button>
        );
      },
    },
  ];

  return (
    <>
      <PageHeader title="Loops" />
      <ErrorLine error={loops.error ?? runLoop.error} />
      {runLoop.isSuccess && runLoop.variables ? (
        <p className="muted small" role="status">
          Started {runLoop.variables.name}
          {runLoop.variables.project ? ` for ${runLoop.variables.project}` : ''}.
        </p>
      ) : null}
      {loops.isSuccess ? (
        <Table
          columns={columns}
          rows={loops.data}
          rowKey={(l) => `${l.name}/${l.project}`}
          empty="No loops are configured. Loops are set under loops in foundry.yaml."
        />
      ) : null}
    </>
  );
}
