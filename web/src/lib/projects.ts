// Derived values for the Projects page. Kept apart from the component so the rules are testable.
import type { Deploy, LoopView, Observer, ProjectCounts, ProjectView } from '../types';
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

/** A project's loops in one line: how many run, how many are off, and which failed last time. */
export interface LoopSummary {
  on: number;
  off: number;
  failing: LoopView[];
}

export function loopSummary(loops: LoopView[]): LoopSummary {
  const failing = loops.filter((loop) => loopHealth(loop) === 'danger');
  const on = loops.filter((loop) => loop.enabled).length;
  return { on, off: loops.length - on, failing };
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

/**
 * A project as the API may send it. The Go side omits observe when a project has none, and a
 * stale or partial response may omit counts, so those fields are optional here.
 */
export interface RawProjectView extends Omit<ProjectView, 'observe' | 'deploy' | 'counts'> {
  observe?: Observer[] | null;
  deploy?: Deploy | null;
  counts?: Partial<ProjectCounts> | null;
}

const NO_COUNTS: ProjectCounts = { queued: 0, running: 0, review: 0, awaiting_approval: 0, blocked: 0 };

/**
 * Gives a project the stable shape the Projects page reads: observe is always an array, deploy is
 * a deployer or null, and counts has every key.
 */
export function normaliseProject(raw: RawProjectView): ProjectView {
  return {
    ...raw,
    observe: raw.observe ?? [],
    deploy: raw.deploy ?? null,
    counts: { ...NO_COUNTS, ...raw.counts },
  };
}
