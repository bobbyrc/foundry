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

/** An empty form for a new agent. */
export function blankForm(): AgentForm {
  return {
    name: '',
    description: '',
    harness: '',
    model: '',
    args: [],
    promptMode: 'text',
    promptRef: '',
    promptText: '',
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

/** Whether the file names `name` under `agents`. */
function hasEntry(fileAgents: Record<string, AgentEntry>, name: string): boolean {
  return Object.prototype.hasOwnProperty.call(fileAgents, name);
}

/**
 * Deletes the file entry for `name`. When it is the only entry under `agents`, the whole `agents`
 * mapping goes, so the file never keeps an empty `agents: {}`.
 */
function deleteEntry(name: string, fileAgents: Record<string, AgentEntry>): ConfigChange {
  const onlyEntry = Object.keys(fileAgents).length === 1 && hasEntry(fileAgents, name);
  return onlyEntry ? { op: 'delete', path: ['agents'] } : { op: 'delete', path: ['agents', name] };
}

/**
 * Takes an agent out of use. A custom agent is deleted, since nothing would bring it back. A
 * built-in with no file entry gets an entry that only disables it; an edited one keeps its
 * entry and gets `disabled` set, so its changes survive a restore.
 */
export function removeChanges(a: AgentView, fileAgents: Record<string, AgentEntry>): ConfigChange[] {
  if (a.origin === 'custom') return [deleteEntry(a.name, fileAgents)];
  if (!hasEntry(fileAgents, a.name)) {
    return [{ op: 'set', path: ['agents', a.name], value: { disabled: true } }];
  }
  return [{ op: 'set', path: ['agents', a.name, 'disabled'], value: true }];
}

/**
 * Puts a removed agent back. An entry that only disabled a built-in is deleted, so the built-in
 * returns as it was; any other entry keeps its settings and loses only the `disabled` flag.
 */
export function restoreChanges(name: string, fileAgents: Record<string, AgentEntry>): ConfigChange[] {
  if (!hasEntry(fileAgents, name)) return [];
  const otherKeys = Object.keys(fileAgents[name]).filter((key) => key !== 'disabled');
  if (otherKeys.length === 0) return [deleteEntry(name, fileAgents)];
  return [{ op: 'delete', path: ['agents', name, 'disabled'] }];
}

/** Drops the file entry for a built-in, so it returns to the built-in definition. */
export function resetChanges(name: string, fileAgents: Record<string, AgentEntry>): ConfigChange[] {
  return [deleteEntry(name, fileAgents)];
}
