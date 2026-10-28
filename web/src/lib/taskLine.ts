// The task's "line": the stations a task passes through on its way to done, and the state of
// each one. Pure, so the rules can be tested without rendering. The page draws it as lamps.
import type { TaskStatus } from '../types';

export type StationKey = 'queued' | 'running' | 'review' | 'approval' | 'merged' | 'deploying' | 'done';

/** passed: behind the task. current: where it is now. stopped: where it halted. skipped: never
 * needed. ahead: not reached yet. */
export type StationState = 'passed' | 'current' | 'stopped' | 'skipped' | 'ahead';

export interface Station {
  key: StationKey;
  label: string;
  state: StationState;
  /** Set on the current and stopped stations only. */
  tone?: 'run' | 'call' | 'stop' | 'off';
}

const ORDER: StationKey[] = ['queued', 'running', 'review', 'approval', 'merged', 'deploying', 'done'];

const LABELS: Record<StationKey, string> = {
  queued: 'Queued',
  running: 'Running',
  review: 'Review',
  approval: 'Approval',
  merged: 'Merged',
  deploying: 'Deploying',
  done: 'Done',
};

/** The station a live status sits at. Blocked, failed and cancelled stop elsewhere. */
const CURRENT: Partial<Record<TaskStatus, StationKey>> = {
  queued: 'queued',
  running: 'running',
  review: 'review',
  awaiting_approval: 'approval',
  merged: 'merged',
  deploying: 'deploying',
  done: 'done',
};

const CURRENT_TONE: Partial<Record<TaskStatus, Station['tone']>> = {
  queued: 'off',
  running: 'run',
  review: 'run',
  awaiting_approval: 'call',
  merged: 'run',
  deploying: 'run',
};

/**
 * The stations of one task, in order. Every station before the current one has passed, except
 * approval (skipped when nothing was approved: it merged on its own) and review (skipped for a
 * task that finished without a PR, because it was answered without code). Blocked and failed
 * stop at review when there is a PR, otherwise at running; cancelled does the same but is shown
 * as off, not as a fault.
 */
export function taskLine(input: { status: TaskStatus; hasPR: boolean; approvalCount: number }): Station[] {
  const { status, hasPR, approvalCount } = input;
  const stopped = status === 'blocked' || status === 'failed' || status === 'cancelled';
  const pivot: StationKey = stopped ? (hasPR ? 'review' : 'running') : (CURRENT[status] ?? 'queued');
  const pivotIndex = ORDER.indexOf(pivot);

  return ORDER.map((key, index): Station => {
    const label = LABELS[key];
    if (index > pivotIndex) return { key, label, state: 'ahead' };
    if (index === pivotIndex && stopped) {
      return { key, label, state: 'stopped', tone: status === 'cancelled' ? 'off' : 'stop' };
    }
    // A done task's own station is passed, not current; it falls through to the rules below.
    if (index === pivotIndex && status !== 'done') {
      return { key, label, state: 'current', tone: CURRENT_TONE[status] };
    }
    if (key === 'approval' && approvalCount === 0) return { key, label, state: 'skipped' };
    if (key === 'review' && !hasPR && status === 'done') return { key, label, state: 'skipped' };
    return { key, label, state: 'passed' };
  });
}
