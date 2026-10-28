// The Config page. Owners get the editor (visual by default, YAML as the other tab). Everyone
// else gets the read-only view with secrets hidden. A 403 from the document also falls back to
// the read-only view, in case the role changed while the page was open.
import { useState } from 'react';
import { ApiError } from '../api';
import { useCan } from '../auth';
import { useConfig, useConfigDocument, useReloadConfig } from '../queries';
import { useParamState } from '../useParamState';
import { Button } from '../components/Button';
import { ConfigEditor } from '../components/config/ConfigEditor';
import { YamlEditor } from '../components/config/YamlEditor';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { JsonView } from '../components/JsonView';
import { PageHeader } from '../components/PageHeader';

export function Config() {
  return useCan('administer') ? <OwnerConfig /> : <ViewerConfig />;
}

/** Everyone who is not an owner: the redacted file as JSON, and Reload for operators. */
function ViewerConfig() {
  const config = useConfig();
  const reload = useReloadConfig();
  const canOperate = useCan('operate');

  return (
    <>
      <PageHeader
        title="Config"
        actions={
          canOperate ? (
            <Button disabled={reload.isPending} onClick={() => reload.mutate()}>
              Reload
            </Button>
          ) : undefined
        }
      />
      <p className="muted">Only an owner can change settings. Secrets are hidden.</p>
      <p>
        File: <span className="mono">{config.data?.path || '—'}</span>
        {reload.isSuccess ? <span className="muted small"> · reloaded</span> : null}
      </p>
      <ErrorLine error={config.error ?? reload.error} />
      {config.data ? <JsonView value={config.data} /> : null}
    </>
  );
}

/** The owner's page: the visual editor or the YAML tab, chosen with ?mode=yaml in the URL. */
function OwnerConfig() {
  const [modeParam, setModeParam] = useParamState('mode');
  const mode = modeParam === 'yaml' ? 'yaml' : 'visual';
  const doc = useConfigDocument();
  const reload = useReloadConfig();
  // Whether the open editor has unsaved edits; it reports this up so a switch can ask first.
  const [dirty, setDirty] = useState(false);

  if (doc.error instanceof ApiError && doc.error.status === 403) return <ViewerConfig />;

  const switchMode = (next: 'visual' | 'yaml') => {
    if (next === mode) return;
    if (dirty && !window.confirm('Discard unsaved changes?')) return;
    setDirty(false);
    setModeParam(next === 'yaml' ? 'yaml' : '');
  };

  return (
    <>
      <PageHeader
        title="Config"
        actions={
          <>
            <div className="segmented" role="group" aria-label="Editor mode">
              <button
                type="button"
                className={mode === 'visual' ? 'on' : undefined}
                aria-pressed={mode === 'visual'}
                onClick={() => switchMode('visual')}
              >
                Visual
              </button>
              <button
                type="button"
                className={mode === 'yaml' ? 'on' : undefined}
                aria-pressed={mode === 'yaml'}
                onClick={() => switchMode('yaml')}
              >
                YAML
              </button>
            </div>
            <Button disabled={reload.isPending} onClick={() => reload.mutate()}>
              Reload from file
            </Button>
          </>
        }
      />
      <p className="muted mono config-path">
        {doc.data?.path ?? ''}
        {reload.isSuccess ? <span className="small"> · reloaded</span> : null}
      </p>
      <ErrorLine error={doc.error ?? reload.error} />

      {mode === 'yaml' ? (
        <YamlEditor onDirtyChange={setDirty} />
      ) : doc.data ? (
        <ConfigEditor doc={doc.data} refetch={doc.refetch} onDirtyChange={setDirty} />
      ) : doc.isPending ? (
        <Empty>Loading config…</Empty>
      ) : null}
    </>
  );
}
