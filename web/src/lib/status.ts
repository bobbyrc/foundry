// Colour and label rules for status pills and risk badges. Every tone also has a text label,
// so colour is never the only signal.

export type Tone = 'muted' | 'info' | 'review' | 'accent' | 'ok' | 'warn' | 'danger';
export type PillKind = 'task' | 'run' | 'deploy' | 'approval';

const TONES: Record<PillKind, Record<string, Tone>> = {
  task: {
    queued: 'muted',
    running: 'info',
    review: 'review',
    awaiting_approval: 'accent',
    merged: 'info',
    deploying: 'info',
    done: 'ok',
    blocked: 'danger',
    failed: 'danger',
    cancelled: 'muted',
  },
  run: {
    starting: 'info',
    running: 'info',
    succeeded: 'ok',
    failed: 'danger',
    stalled: 'danger',
    timed_out: 'danger',
    denied: 'danger',
    cancelled: 'muted',
  },
  deploy: {
    pending: 'info',
    deploying: 'info',
    verifying: 'info',
    healthy: 'ok',
    failed: 'danger',
    rolled_back: 'danger',
  },
  // The design doc does not name approval colours; these follow the same meaning as tasks.
  approval: {
    pending: 'accent',
    approved: 'ok',
    rejected: 'danger',
  },
};

export function toneFor(kind: PillKind, status: string): Tone {
  return TONES[kind][status] ?? 'muted';
}

/** Only running work pulses. */
export function isPulsing(kind: PillKind, status: string): boolean {
  return (kind === 'task' && status === 'running') || (kind === 'run' && (status === 'starting' || status === 'running'));
}

export function isActiveRun(status: string): boolean {
  return status === 'starting' || status === 'running';
}

/** Risk is unrated when empty; policy treats unrated as high. */
export function riskTone(risk: string | undefined): Tone {
  switch (risk) {
    case 'low':
      return 'ok';
    case 'medium':
      return 'warn';
    case 'high':
      return 'danger';
    default:
      return 'muted';
  }
}

export function riskLabel(risk: string | undefined): string {
  return risk ? risk : 'unrated';
}
