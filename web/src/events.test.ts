import { afterEach, describe, expect, it, vi } from 'vitest';
import type { QueryClient } from '@tanstack/react-query';
import { EventStream, invalidateFor, keysFor } from './events';
import { keys } from './keys';
import type { OnletEvent } from './types';

const ev = (type: string, extra: Partial<OnletEvent> = {}): OnletEvent => ({
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

  // Listeners the stream registers on window and document, so a test can fire them.
  const fired = new EventTarget();

  function setup() {
    vi.useFakeTimers();
    vi.stubGlobal('EventSource', FakeEventSource);
    vi.stubGlobal('window', fired);
    vi.stubGlobal('document', Object.assign(fired, { visibilityState: 'visible' }));
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

  it('caps the reconnect delay at 5s', async () => {
    setup();
    // Each failure doubles the delay: 1s, 2s, 4s, then 5s from there on.
    for (const delay of [1_000, 2_000, 4_000, 5_000, 5_000]) {
      const count = FakeEventSource.instances.length;
      FakeEventSource.instances[count - 1]!.onerror?.();
      await vi.advanceTimersByTimeAsync(delay);
      expect(FakeEventSource.instances).toHaveLength(count + 1);
    }
  });

  it.each(['online', 'visibilitychange'])('reconnects at once on %s', async (type) => {
    setup();
    FakeEventSource.instances[0]!.onerror?.();
    // Let recover() settle into its 1s backoff wait.
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeEventSource.instances).toHaveLength(1);

    fired.dispatchEvent(new Event(type));
    expect(FakeEventSource.instances).toHaveLength(2);
  });

  it('stops listening for online and visibility once stopped', async () => {
    const { stream } = setup();
    FakeEventSource.instances[0]!.onerror?.();
    await vi.advanceTimersByTimeAsync(0);
    stream.stop();

    fired.dispatchEvent(new Event('online'));
    expect(FakeEventSource.instances).toHaveLength(1);
  });
});
