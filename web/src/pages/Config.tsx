import { useConfig, useReloadConfig } from '../queries';
import { Button } from '../components/Button';
import { ErrorLine } from '../components/ErrorLine';
import { JsonView } from '../components/JsonView';
import { PageHeader } from '../components/PageHeader';

export function Config() {
  const config = useConfig();
  const reload = useReloadConfig();

  return (
    <>
      <PageHeader
        title="Config"
        actions={
          <Button disabled={reload.isPending} onClick={() => reload.mutate()}>
            Reload
          </Button>
        }
      />
      <p className="muted">Edit this file and press Reload. Secrets are hidden.</p>
      <p>
        File: <span className="mono">{config.data?.path || '—'}</span>
        {reload.isSuccess ? <span className="muted small"> · reloaded</span> : null}
      </p>
      <ErrorLine error={config.error ?? reload.error} />
      {config.data ? <JsonView value={config.data} /> : null}
    </>
  );
}
