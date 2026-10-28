// Derived values for the Projects page. Kept apart from the component so the rules are testable.
import type { LoopView } from '../types';
import { parseTime } from './time';

/** owner/name as GitHub writes it. Anything else (a URL, a bare name) gets no link. */
const REPO_PATTERN = /^[A-Za-z0-9._-]+\/[A-Za-z0-9._-]+$/;

export function repoUrl(repo: string): string | undefined {
  return REPO_PATTERN.test(repo) ? `https://github.com/${repo}` : undefined;
}

export type LoopHealth = 'ok' | 'danger' | 'muted';

/**
 * A disabled loop is muted whatever its last error says, since it is not running. An error is
 * shown as danger. A loop that has never run has nothing to judge yet, so it is muted.
 */
export function loopHealth(loop: LoopView): LoopHealth {
  if (!loop.enabled) return 'muted';
  if (loop.last_error) return 'danger';
  if (!parseTime(loop.last_run_at)) return 'muted';
  return 'ok';
}

const HEALTH_LABEL: Record<LoopHealth, string> = {
  ok: 'last run ok',
  danger: 'last run failed',
  muted: 'disabled or not yet run',
};

/** Screen-reader text for the pill's dot, so health is not carried by colour alone. */
export function loopHealthLabel(loop: LoopView): string {
  return HEALTH_LABEL[loopHealth(loop)];
}
