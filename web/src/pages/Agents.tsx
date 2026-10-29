// The Agents page: every configured role as a card. Owners also add, edit, remove and restore
// agents here; each change is one save of the config file, the same as the config editor makes.
import { useState } from 'react';
import { useCan } from '../auth';
import { AgentCard, type AgentControls } from '../components/AgentCard';
import { AgentEditor } from '../components/AgentEditor';
import { Button } from '../components/Button';
import { Empty } from '../components/Empty';
import { ErrorLine } from '../components/ErrorLine';
import { PageHeader } from '../components/PageHeader';
import { blankForm, formFromAgent, removeChanges, resetChanges, restoreChanges, type AgentEntry } from '../lib/agentChanges';
import { useAgents, useConfigDocument, useSaveConfigDocument } from '../queries';
import type { AgentView, ConfigChange } from '../types';

type Editing = { mode: 'new' } | { mode: 'edit'; agent: AgentView };

export function Agents() {
  const owner = useCan('administer');
  const agents = useAgents();
  // Only owners may read the config file; viewers never ask for it.
  const configDoc = useConfigDocument(owner);
  const save = useSaveConfigDocument();
  const [editing, setEditing] = useState<Editing | null>(null);
  // The last remove, reset or restore that was refused, and the agent it was for. The card shows
  // it under its own buttons; the editor shows its own failures.
  const [failure, setFailure] = useState<{ name: string; error: unknown } | null>(null);

  const doc = owner ? configDoc.data : undefined;
  const fileAgents: Record<string, AgentEntry> = (doc?.document as { agents?: Record<string, AgentEntry> } | undefined)?.agents ?? {};
  const apply = (name: string, changes: ConfigChange[]) => {
    if (!doc) return;
    setFailure(null);
    save.mutate(
      { version: doc.version, changes },
      {
        onError: (err) => {
          setFailure({ name, error: err });
          // The file may have changed under us (a conflict), so take the current one again.
          void configDoc.refetch();
        },
      },
    );
  };

  /** The failure to show under this card: only the card whose action was refused. */
  const failureFor = (agent: AgentView): unknown =>
    failure !== null && failure.name === agent.name ? failure.error : null;

  const controlsFor = (agent: AgentView): AgentControls | undefined => {
    if (!owner) return undefined;
    return {
      busy: !doc || save.isPending,
      onEdit: () => setEditing({ mode: 'edit', agent }),
      onRemove: () => apply(agent.name, removeChanges(agent, fileAgents)),
      onReset: () => apply(agent.name, resetChanges(agent.name, fileAgents)),
      onRestore: () => apply(agent.name, restoreChanges(agent.name, fileAgents)),
    };
  };

  const all = agents.data ?? [];
  const enabled = all.filter((a) => !a.disabled);
  const removed = all.filter((a) => a.disabled);
  const addable = owner && doc !== undefined && editing === null && !save.isPending;

  return (
    <>
      <PageHeader
        title="Agents"
        actions={
          owner ? (
            <Button variant="primary" disabled={!addable} onClick={() => setEditing({ mode: 'new' })}>
              New agent
            </Button>
          ) : undefined
        }
      />
      <ErrorLine error={agents.error} />
      {owner ? <ErrorLine error={configDoc.error} /> : null}

      {editing && doc ? (
        <AgentEditor
          key={editing.mode === 'edit' ? editing.agent.name : 'new'}
          mode={editing.mode}
          initial={editing.mode === 'edit' ? formFromAgent(editing.agent) : blankForm()}
          existing={all}
          version={doc.version}
          onDone={() => setEditing(null)}
          onCancel={() => setEditing(null)}
          onSaveError={() => void configDoc.refetch()}
        />
      ) : null}

      {agents.isSuccess && all.length === 0 ? (
        <Empty>No agents are configured. Roles are set under agents in onlet.yaml.</Empty>
      ) : null}
      <div className="card-grid">
        {enabled.map((agent) => (
          <AgentCard key={agent.name} agent={agent} controls={controlsFor(agent)} failure={failureFor(agent)} />
        ))}
      </div>
      {removed.length > 0 ? (
        <>
          <h2 className="agents-heading">Removed</h2>
          <div className="card-grid">
            {removed.map((agent) => (
              <AgentCard key={agent.name} agent={agent} controls={controlsFor(agent)} failure={failureFor(agent)} />
            ))}
          </div>
        </>
      ) : null}
    </>
  );
}
