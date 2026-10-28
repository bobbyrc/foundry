import { describe, expect, it } from 'vitest';
import { latestDoneReview } from './review';
import type { Task } from '../types';

const child = (extra: Partial<Task>): Task =>
  ({ id: 'x', kind: 'review', status: 'done', created_at: '2026-10-27T12:00:00Z', ...extra }) as Task;

describe('latestDoneReview', () => {
  it('returns undefined for no children', () => {
    expect(latestDoneReview(undefined)).toBeUndefined();
    expect(latestDoneReview([])).toBeUndefined();
  });

  it('ignores non-review kinds and unfinished reviews', () => {
    const kids = [child({ id: 'a', kind: 'chore' }), child({ id: 'b', status: 'running' }), child({ id: 'c', status: 'failed' })];
    expect(latestDoneReview(kids)).toBeUndefined();
  });

  it('picks the newest done review', () => {
    const kids = [
      child({ id: 'old', created_at: '2026-10-27T10:00:00Z' }),
      child({ id: 'new', created_at: '2026-10-27T14:00:00Z' }),
      child({ id: 'newer-running', status: 'running', created_at: '2026-10-27T15:00:00Z' }),
    ];
    expect(latestDoneReview(kids)?.id).toBe('new');
  });
});
