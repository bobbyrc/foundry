// One agent on the Agents page: what it is, what it runs with, what uses it, and (for an owner)
// the controls that change it. The page does the saving; the card only asks for an action.
import { useState } from 'react';
import type { AgentView } from '../types';
import { AgentSaveFailure } from './AgentSaveFailure';
import { Button } from './Button';

/** The first lines of a prompt shown before "Show full prompt". */
const PREVIEW_LINES = 8;

/** The server always refuses to disable the orchestrator (chat needs it), so it has no Remove. */
const ORCHESTRATOR = 'orchestrator';

export interface AgentControls {
  /** True while a save is in flight: every control is disabled. */
  busy: boolean;
  onEdit: () => void;
  /** Takes the agent out of use: a built-in or edited agent is disabled, a custom one is deleted. */
  onRemove: () => void;
  onReset: () => void;
  onRestore: () => void;
}

/** The badge text: a removed agent says so whatever its origin. */
export function agentBadge(a: AgentView): string {
  if (a.disabled) return 'removed';
  return a.origin === 'builtin' ? 'built-in' : a.origin;
}

/**
 * `failure` is the reason the last action on this card was refused, or null. It is shown under
 * the card's own buttons, so the owner sees it next to the click that caused it.
 */
export function AgentCard({
  agent,
  controls,
  failure = null,
}: {
  agent: AgentView;
  controls?: AgentControls;
  failure?: unknown;
}) {
  const [expanded, setExpanded] = useState(false);
  const [comparing, setComparing] = useState(false);

  const lines = agent.prompt_text.split('\n');
  const truncated = lines.length > PREVIEW_LINES && !expanded;
  const shownPrompt = truncated ? lines.slice(0, PREVIEW_LINES).join('\n') : agent.prompt_text;
  const customized = agent.origin === 'edited' && !agent.prompt_updates;
  const canCompare = customized && agent.builtin_prompt_text !== undefined;
  const busy = controls?.busy ?? false;
  // A full prompt or a side-by-side compare needs the room of the whole grid row.
  const wide = expanded || (canCompare && comparing);

  return (
    <article className={wide ? 'card agent-card agent-card--wide' : 'card agent-card'}>
      <div className="card-head">
        <h3 className="mono">{agent.name}</h3>
        <span className="tag">{agentBadge(agent)}</span>
      </div>
      <p className="muted">{agent.description || 'No description.'}</p>
      <dl className="kv">
        <dt>Harness</dt>
        <dd>
          {agent.harness} <span className="muted">({agent.harness_type})</span>
        </dd>
        <dt>Model</dt>
        <dd>{agent.model || 'harness default'}</dd>
        <dt>Used for</dt>
        <dd>
          {(agent.uses ?? []).length > 0 ? (
            <ul className="agent-uses">
              {(agent.uses ?? []).map((use, index) => (
                <li key={index}>
                  {use.kind ? <span className="tag">{use.kind}</span> : null}
                  {use.description}
                  {use.project ? <span className="muted"> in {use.project}</span> : null}
                </li>
              ))}
            </ul>
          ) : (
            <span className="muted">Not used by any task.</span>
          )}
        </dd>
      </dl>

      <div className="agent-prompt-head">
        <span className="muted small">Prompt</span>
        {agent.prompt ? <span className="mono small muted">{agent.prompt}</span> : <span className="muted small">written inline</span>}
      </div>
      {agent.prompt_error ? (
        <p className="error-line">{agent.prompt_error}</p>
      ) : (
        <>
          {customized ? (
            <p className="muted small">Customized: no longer receives onett&apos;s prompt updates.</p>
          ) : null}
          {canCompare && comparing ? (
            <div className="agent-compare">
              <div>
                <span className="muted small">Your prompt</span>
                <pre className="agent-prompt">{agent.prompt_text}</pre>
              </div>
              <div>
                <span className="muted small">Built-in</span>
                <pre className="agent-prompt">{agent.builtin_prompt_text}</pre>
              </div>
            </div>
          ) : (
            <pre className="agent-prompt">{shownPrompt}</pre>
          )}
          <div className="agent-prompt-links">
            {lines.length > PREVIEW_LINES ? (
              <button type="button" className="link-button small" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
                {expanded ? 'Show less' : 'Show full prompt'}
              </button>
            ) : null}
            {canCompare ? (
              <button type="button" className="link-button small" aria-expanded={comparing} onClick={() => setComparing(!comparing)}>
                {comparing ? 'Hide built-in' : 'Compare with built-in'}
              </button>
            ) : null}
          </div>
        </>
      )}

      {controls ? (
        <div className="agent-actions users-actions">
          {agent.disabled ? (
            <Button variant="primary" disabled={busy} onClick={controls.onRestore}>
              Restore
            </Button>
          ) : (
            <>
              <Button disabled={busy} onClick={controls.onEdit}>
                Edit
              </Button>
              {agent.origin === 'custom' || agent.name !== ORCHESTRATOR ? (
                <Button variant="danger" confirm disabled={busy} onClick={controls.onRemove}>
                  {agent.origin === 'custom' ? 'Delete' : 'Remove'}
                </Button>
              ) : null}
              {agent.origin === 'edited' ? (
                <Button variant="danger" confirm disabled={busy} onClick={controls.onReset}>
                  Reset to built-in
                </Button>
              ) : null}
            </>
          )}
        </div>
      ) : null}
      <AgentSaveFailure error={failure} />
    </article>
  );
}
