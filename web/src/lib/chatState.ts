// Small rules the Chat page applies, kept pure so they are testable: what the composer allows and
// which lamp a task agent shows. A lamp always comes with a text label, so colour is never the
// only signal.
import type { AgentConversation, TaskStatus } from '../types';
import { humanize } from './format';

export interface ComposerInput {
  /** Whether the server will take a message (false once closed, or when the task cannot continue). */
  canSend: boolean;
  /** Why not, when `canSend` is false. */
  reason?: string;
  /** A signed-in user who may read but not operate. */
  viewer: boolean;
  /** An agent run is live. The composer stays open: a message sent now is delivered or queued. */
  working: boolean;
  /** Messages waiting for the agent to finish its run. */
  queued: number;
  /** The task agent's role; absent when talking to the orchestrator. */
  role?: string;
}

export interface ComposerState {
  enabled: boolean;
  placeholder: string;
  /** A line that replaces the usual hint, when there is something to explain. */
  note?: string;
}

export const VIEWER_NOTE = 'Viewers can read conversations but not send messages.';

export function composerState(input: ComposerInput): ComposerState {
  const placeholder = `Message the ${input.role ?? 'orchestrator'}…`;
  if (input.viewer) return { enabled: false, placeholder, note: VIEWER_NOTE };
  if (!input.canSend) {
    return { enabled: false, placeholder, note: input.reason || 'This conversation cannot take messages.' };
  }
  return { enabled: true, placeholder };
}

export type ChatLamp = 'run' | 'call' | 'stop' | 'done';

export interface ChatLampState {
  lamp: ChatLamp;
  label: string;
}

/** The sidebar lamp for a task agent. */
export function agentLamp(a: Pick<AgentConversation, 'live' | 'closed' | 'task_status'>): ChatLampState {
  if (a.live) return { lamp: 'run', label: 'working' };
  if (a.closed) return { lamp: 'done', label: 'closed' };
  if (a.task_status === 'blocked') return { lamp: 'stop', label: 'blocked' };
  return { lamp: 'done', label: humanize(a.task_status) };
}

/** The lamp on the chip under an orchestrator turn that created a task. */
export function taskLamp(status: TaskStatus): ChatLampState {
  const label = humanize(status);
  if (status === 'running') return { lamp: 'run', label };
  if (status === 'blocked' || status === 'failed') return { lamp: 'stop', label };
  if (status === 'awaiting_approval') return { lamp: 'call', label };
  return { lamp: 'done', label };
}
