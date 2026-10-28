// Andon lamps: one lamp per project on the Overview board. The rules live here so they are
// testable. A lamp's colour always comes with a text label, so colour is never the only signal.
import type { ProjectView } from '../types';

/** run: work is flowing. call: something waits on you. stop: work stopped itself. off: idle or paused. */
export type Lamp = 'run' | 'call' | 'stop' | 'off';

export interface LampState {
  lamp: Lamp;
  /** What lit the lamp, in words: "1 blocked", "2 need you", "3 running". */
  label: string;
}

/**
 * The most urgent condition wins: a blocked task stops the line even when other work flows, and
 * an approval outranks running work because only the owner can move it. A paused project shows
 * off unless something stopped or waits on the owner, since those still need a decision.
 */
export function projectLamp(project: Pick<ProjectView, 'counts' | 'paused'>): LampState {
  const c = project.counts;
  if (c.blocked > 0) return { lamp: 'stop', label: `${c.blocked} blocked` };
  if (c.awaiting_approval > 0) {
    return { lamp: 'call', label: c.awaiting_approval === 1 ? '1 needs you' : `${c.awaiting_approval} need you` };
  }
  if (project.paused) return { lamp: 'off', label: 'paused' };
  if (c.running > 0) return { lamp: 'run', label: `${c.running} running` };
  if (c.review > 0) return { lamp: 'run', label: `${c.review} in review` };
  if (c.queued > 0) return { lamp: 'off', label: `${c.queued} queued` };
  return { lamp: 'off', label: 'idle' };
}

/** The secondary line under a window's label: the other counts that are not zero. */
export function lampDetail(project: Pick<ProjectView, 'counts'>, shown: LampState): string {
  const c = project.counts;
  const parts: string[] = [];
  if (c.running > 0 && !shown.label.endsWith('running')) parts.push(`${c.running} running`);
  if (c.review > 0 && !shown.label.endsWith('review')) parts.push(`${c.review} in review`);
  if (c.queued > 0 && !shown.label.endsWith('queued')) parts.push(`${c.queued} queued`);
  return parts.join(' · ');
}
