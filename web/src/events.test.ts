import { afterEach, describe, expect, it, vi } from 'vitest';
import type { QueryClient } from '@tanstack/react-query';
import { EventStream, invalidateFor, keysFor } from './events';
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

describe('EventStream reconnects', () => {
  class FakeEventSource {
    static instances: FakeEventSource[] = [];
    onopen: (() => void) | null = null;
    onmessage: (() => void) | null = null;
    onerror: (() => void) | null = null;
    constructor(readonly url: string) {
      FakeEventSource.instances.push(this);
    }
    addEventListener(): void {}
    close(): void {}
  }

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    FakeEventSource.instances = [];
  });

  function setup() {
    vi.useFakeTimers();
    vi.stubGlobal('EventSource', FakeEventSource);
    vi.stubGlobal('window', {
      addEventListener: () => {},
    });
    const invalidateQueries = vi.fn().mockResolvedValue(undefined);
    const client = { invalidateQueries } as unknown as QueryClient;
    const stream = new EventStream();
    stream.start(client);
    return { stream, client, invalidateQueries };
  }

  it('does not invalidate on the first connect', () => {
    const { invalidateQueries } = setup();
    FakeEventSource.instances[0]!.onopen?.();
    expect(invalidateQueries).not.toHaveBeenCalled();
  });

  it('invalidates all queries once after a reconnect', async () => {
    const { stream, invalidateQueries } = setup();
    FakeEventSource.instances[0]!.onopen?.();
    FakeEventSource.instances[0]!.onerror?.();
    // recover() awaits getStats (which fails here without a server) then backs off 1s.
    await vi.advanceTimersByTimeAsync(1_000);
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeEventSource.instances).toHaveLength(2);

    FakeEventSource.instances[1]!.onopen?.();
    expect(invalidateQueries).toHaveBeenCalledTimes(1);
    expect(invalidateQueries).toHaveBeenCalledWith();
    expect(stream.getStatus()).toBe('live');
  });
});
