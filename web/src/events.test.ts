import { describe, expect, it, vi } from 'vitest';
import type { QueryClient } from '@tanstack/react-query';
import { invalidateFor, keysFor } from './events';
import { keys } from './keys';
import type { FoundryEvent } from './types';

const ev = (type: string, extra: Partial<FoundryEvent> = {}): FoundryEvent => ({
  id: 1,
  time: '2026-10-27T12:00:00Z',
  type,
  message: '',
  ...extra,
});

describe('keysFor', () => {
  it('maps task events to tasks, the task detail and stats', () => {
    expect(keysFor(ev('task.updated', { task_id: 't1' }))).toEqual([keys.tasksAll, keys.stats, keys.task('t1')]);
    expect(keysFor(ev('task.created'))).toEqual([keys.tasksAll, keys.stats]);
  });

  it('maps run.updated to runs and the run detail', () => {
    const keysOut = keysFor(ev('run.updated', { run_id: 'r1', task_id: 't1' }));
    expect(keysOut).toContainEqual(keys.runsAll);
    expect(keysOut).toContainEqual(keys.run('r1'));
  });

  it('maps approval events to approvals and stats', () => {
    expect(keysFor(ev('approval.requested', { task_id: 't1' }))).toEqual([
      keys.approvalsAll,
      keys.stats,
      keys.task('t1'),
    ]);
    expect(keysFor(ev('approval.decided'))).toContainEqual(keys.approvalsAll);
  });

  it('maps deploy, signal and friction events to their lists', () => {
    expect(keysFor(ev('deploy.updated'))).toEqual([keys.deploymentsAll]);
    expect(keysFor(ev('signal.new'))).toEqual([keys.signalsAll]);
    expect(keysFor(ev('friction.reported'))).toEqual([keys.frictionAll]);
    expect(keysFor(ev('issue.filed'))).toEqual([keys.frictionAll]);
  });

  it('maps chat.message to that conversation only', () => {
    const keysOut = keysFor(ev('chat.message', { data: { conversation_id: 'c1', text: 'hi' } }));
    expect(keysOut).toEqual([keys.conversationsAll, keys.conversation('c1')]);
  });

  it('never refetches for live-only events', () => {
    expect(keysFor(ev('run.log', { data: [] }))).toEqual([]);
    expect(keysFor(ev('chat.delta', { data: { conversation_id: 'c1', text: 'x' } }))).toEqual([]);
  });

  it('ignores event types it does not know', () => {
    expect(keysFor(ev('something.new'))).toEqual([]);
  });
});

describe('invalidateFor', () => {
  it('invalidates every key the event maps to', () => {
    const invalidateQueries = vi.fn().mockResolvedValue(undefined);
    const client = { invalidateQueries } as unknown as QueryClient;
    invalidateFor(ev('task.updated', { task_id: 't1' }), client);
    expect(invalidateQueries.mock.calls).toEqual([
      [{ queryKey: keys.tasksAll }],
      [{ queryKey: keys.stats }],
      [{ queryKey: keys.task('t1') }],
    ]);
  });
});
