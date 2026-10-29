// The agent editor sheet: a new agent, or the settings of an existing one. It saves the whole
// entry in one config change and stays open with the server's reason when the save is refused.
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useConfig, useSaveConfigDocument } from '../queries';
import { blankForm, formFromAgent, saveChanges, type AgentForm } from '../lib/agentChanges';
import type { AgentView } from '../types';
import { Button } from './Button';
import { AgentSaveFailure } from './AgentSaveFailure';
import { Field } from './Field';

/**
 * The editor. `mode` "new" asks for a name (refusing one already in use) and offers to start
 * from an existing agent; "edit" keeps the name fixed.
 */
export function AgentEditor({
  mode,
  initial,
  existing,
  version,
  onDone,
  onCancel,
  onSaveError,
}: {
  mode: 'new' | 'edit';
  /** The starting values. For "new" these are blank, or a copy of the agent chosen to start from. */
  initial: AgentForm;
  /** Names of every agent already configured, used to refuse a duplicate name. */
  existing: AgentView[];
  /** The config document version the save is based on. */
  version: string;
  onDone: () => void;
  onCancel: () => void;
  /** Called when a save is refused, so the owner of `version` can reload the document. */
  onSaveError: () => void;
}) {
  const config = useConfig();
  const save = useSaveConfigDocument();
  const [form, setForm] = useState<AgentForm>(initial);
  const [startFrom, setStartFrom] = useState('');
  const [nameError, setNameError] = useState<string | null>(null);
  const [failure, setFailure] = useState<unknown>(null);
  const formRef = useRef<HTMLFormElement>(null);

  // Opening the sheet brings it into view with the first field focused, so a click on a card
  // far down the page still leaves the owner at the form.
  useEffect(() => {
    const sheet = formRef.current;
    if (!sheet) return;
    sheet.scrollIntoView({ block: 'start' });
    sheet.querySelector<HTMLElement>('input, select, textarea')?.focus();
  }, []);

  const harnessNames = Object.keys(config.data?.harnesses ?? {});
  // An agent may name a harness that is not in the list (a file edited by hand): keep it selectable.
  const harnessOptions = form.harness && !harnessNames.includes(form.harness) ? [form.harness, ...harnessNames] : harnessNames;

  const update = (patch: Partial<AgentForm>) => setForm((prev) => ({ ...prev, ...patch }));

  const chooseStart = (name: string) => {
    setStartFrom(name);
    const source = existing.find((a) => a.name === name);
    // Keep the name the owner typed; everything else comes from the chosen agent, or is blank.
    setForm({ ...(source ? formFromAgent(source) : blankForm()), name: form.name });
  };

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const name = form.name.trim();
    if (mode === 'new') {
      if (!name) return setNameError('A name is required.');
      if (existing.some((a) => a.name === name)) return setNameError(`An agent named "${name}" already exists.`);
    }
    setNameError(null);
    setFailure(null);
    const args = form.args.map((arg) => arg.trim()).filter(Boolean);
    save.mutate(
      { version, changes: saveChanges({ ...form, name, args }) },
      {
        onSuccess: onDone,
        onError: (err) => {
          setFailure(err);
          onSaveError();
        },
      },
    );
  };

  return (
    <form ref={formRef} className="card agent-editor" onSubmit={submit} aria-label={mode === 'new' ? 'New agent' : `Edit ${form.name}`}>
      <h2 className="agent-editor-title">{mode === 'new' ? 'New agent' : <>Edit <span className="mono">{form.name}</span></>}</h2>

      {mode === 'new' ? (
        <div className="agent-editor-row">
          <Field label="Name" hint={nameError ?? 'The role name that tasks are routed to.'}>
            <input
              type="text"
              name="name"
              autoComplete="off"
              autoFocus
              required
              aria-invalid={nameError !== null}
              value={form.name}
              onChange={(event) => update({ name: event.target.value })}
            />
          </Field>
          <Field label="Start from" hint="Copies another agent's settings and prompt.">
            <select value={startFrom} onChange={(event) => chooseStart(event.target.value)}>
              <option value="">Blank</option>
              {existing.map((a) => (
                <option key={a.name} value={a.name}>
                  {a.name}
                </option>
              ))}
            </select>
          </Field>
        </div>
      ) : null}

      <Field label="Description">
        <input type="text" value={form.description} onChange={(event) => update({ description: event.target.value })} />
      </Field>

      <div className="agent-editor-row">
        <Field label="Harness" hint="Which configured harness runs this agent.">
          <select required value={form.harness} onChange={(event) => update({ harness: event.target.value })}>
            <option value="">Choose a harness</option>
            {harnessOptions.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Model" hint="Leave empty for the harness default.">
          <input type="text" value={form.model} onChange={(event) => update({ model: event.target.value })} />
        </Field>
      </div>

      <Field label="Args" hint="One per line.">
        <textarea rows={3} value={form.args.join('\n')} onChange={(event) => update({ args: event.target.value.split('\n') })} />
      </Field>

      <Field label="Prompt">
        <select value={form.promptMode} onChange={(event) => update({ promptMode: event.target.value as AgentForm['promptMode'] })}>
          <option value="text">Write the prompt here</option>
          <option value="ref">Use a prompt reference</option>
        </select>
      </Field>
      {form.promptMode === 'ref' ? (
        <Field label="Prompt reference" hint="A built-in as builtin:name, or a path to a Markdown file.">
          <input type="text" required autoComplete="off" value={form.promptRef} onChange={(event) => update({ promptRef: event.target.value })} />
        </Field>
      ) : (
        <Field label="Instructions">
          <textarea
            rows={10}
            required
            spellCheck={false}
            className="mono"
            value={form.promptText}
            onChange={(event) => update({ promptText: event.target.value })}
          />
        </Field>
      )}

      <AgentSaveFailure error={failure} />
      <div className="row-actions">
        <Button type="submit" variant="primary" disabled={save.isPending}>
          {mode === 'new' ? 'Create agent' : 'Save agent'}
        </Button>
        <Button variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </form>
  );
}
