// Turns the owner's agent actions into config-file changes. The Agents page sends these lists to
// the config document endpoint; nothing here touches the network or the DOM. A file entry is
// the raw `agents.<name>` mapping from the file (not the computed AgentView), or undefined when
// the file does not name the agent.
import type { AgentView, ConfigChange } from '../types';

export type AgentEntry = Record<string, unknown>;

/** The fields of the agent editor. `promptMode` says which of the two prompt fields is in use. */
export interface AgentForm {
  name: string;
  description: string;
  harness: string;
  model: string;
  args: string[];
  promptMode: 'ref' | 'text';
  promptRef: string;
  promptText: string;
}

/** The editor's starting values for an existing agent. */
export function formFromAgent(a: AgentView): AgentForm {
  const inline = a.prompt === '';
  return {
    name: a.name,
    description: a.description,
    harness: a.harness,
    model: a.model,
    args: [...a.args],
    promptMode: inline ? 'text' : 'ref',
    promptRef: inline ? '' : a.prompt,
    promptText: a.prompt_text,
  };
}

/** One `set` of the whole agent entry, from what the editor shows. Empty optional fields are left out. */
export function saveChanges(form: AgentForm): ConfigChange[] {
  const value: Record<string, unknown> = { harness: form.harness.trim() };
  if (form.model.trim()) value.model = form.model.trim();
  if (form.description.trim()) value.description = form.description.trim();
  if (form.args.length > 0) value.args = form.args;
  if (form.promptMode === 'text') {
    value.prompt_text = form.promptText;
  } else {
    value.prompt = form.promptRef.trim();
  }
  return [{ op: 'set', path: ['agents', form.name], value }];
}

/**
 * Takes an agent out of use. A custom agent is deleted, since nothing would bring it back. A
 * built-in with no file entry gets an entry that only disables it; an edited one keeps its
 * entry and gets `disabled` set, so its changes survive a restore.
 */
export function removeChanges(a: AgentView, entry: AgentEntry | undefined): ConfigChange[] {
  if (a.origin === 'custom') return [{ op: 'delete', path: ['agents', a.name] }];
  if (entry === undefined) return [{ op: 'set', path: ['agents', a.name], value: { disabled: true } }];
  return [{ op: 'set', path: ['agents', a.name, 'disabled'], value: true }];
}

/**
 * Puts a removed agent back. An entry that only disabled a built-in is deleted, so the built-in
 * returns as it was; any other entry keeps its settings and loses only the `disabled` flag.
 */
export function restoreChanges(name: string, entry: AgentEntry | undefined): ConfigChange[] {
  if (entry === undefined) return [];
  const otherKeys = Object.keys(entry).filter((key) => key !== 'disabled');
  if (otherKeys.length === 0) return [{ op: 'delete', path: ['agents', name] }];
  return [{ op: 'delete', path: ['agents', name, 'disabled'] }];
}

/** Drops the file entry for a built-in, so it returns to the built-in definition. */
export function resetChanges(name: string): ConfigChange[] {
  return [{ op: 'delete', path: ['agents', name] }];
}
