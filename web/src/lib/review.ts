import type { Task } from '../types';

/** The newest finished review child, or undefined. Optional fields are guarded: the API omits empty ones. */
export function latestDoneReview(children: Task[] | undefined): Task | undefined {
  let best: Task | undefined;
  for (const child of children ?? []) {
    if (child.kind !== 'review' || child.status !== 'done') continue;
    if (!best || Date.parse(child.created_at || '') > Date.parse(best.created_at || '')) best = child;
  }
  return best;
}
