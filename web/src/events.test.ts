import { afterEach, describe, expect, it, vi } from 'vitest';
import type { QueryClient } from '@tanstack/react-query';
import { EventStream, invalidateFor, keysFor } from './events';
import { keys } from './keys';
import type { OnettEvent, WsClientMessage, WsServerMessage } from './types';

const ev = (type: string, extra: Partial<OnettEvent> = {}): OnettEvent => ({
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

  it('maps chat.message to the conversation, every task conversation and the agent list', () => {
    const keysOut = keysFor(ev('chat.message', { data: { conversation_id: 'c1', text: 'hi' } }));
    expect(keysOut).toEqual([
      keys.conversationsAll,
      keys.conversation('c1'),
      keys.taskConversationsAll,
      keys.agentConversations,
    ]);
  });

  it('maps conversation.updated to that task conversation and the agent list', () => {
    expect(keysFor(ev('conversation.updated', { data: { task_id: 't1' } }))).toEqual([
      keys.taskConversation('t1'),
      keys.agentConversations,
    ]);
  });

  it('refreshes conversations and run logs when a run changes', () => {
    const keysOut = keysFor(ev('run.updated', { run_id: 'r1', task_id: 't1' }));
    expect(keysOut).toContainEqual(keys.conversationsAll);
    expect(keysOut).toContainEqual(keys.taskConversationsAll);
    expect(keysOut).toContainEqual(keys.agentConversations);
    expect(keysOut).toContainEqual(keys.runLogs('r1'));
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

describe('EventStream over WebSocket', () => {
  // Stands in for the browser WebSocket. Tests drive it: open(), receive() a server frame, drop()
  // when the connection goes away. close() does nothing, as in a browser where onclose comes later.
  class FakeWebSocket {
    static instances: FakeWebSocket[] = [];
    onopen: (() => void) | null = null;
    onmessage: ((message: { data: string }) => void) | null = null;
    onclose: (() => void) | null = null;
    readyState = 0;
    readonly sent: string[] = [];
    constructor(readonly url: string) {
      FakeWebSocket.instances.push(this);
    }
    send(data: string): void {
      this.sent.push(data);
    }
    close(): void {
      this.readyState = 3;
    }
    open(): void {
      this.readyState = 1;
      this.onopen?.();
    }
    receive(msg: WsServerMessage): void {
      this.onmessage?.({ data: JSON.stringify(msg) });
    }
    drop(): void {
      this.readyState = 3;
      this.onclose?.();
    }
    sentMessages(): WsClientMessage[] {
      return this.sent.map((data) => JSON.parse(data) as WsClientMessage);
    }
  }

  // Listeners the stream registers on window and document, so a test can fire them.
  const fired = new EventTarget();
  const socket = (): FakeWebSocket => FakeWebSocket.instances[FakeWebSocket.instances.length - 1]!;
  let started: EventStream | null = null;

  afterEach(() => {
    // Stop each stream so its window and document listeners do not pile up across tests.
    started?.stop();
    started = null;
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    FakeWebSocket.instances = [];
  });

  function setup(protocol = 'http:') {
    vi.useFakeTimers();
    vi.stubGlobal('WebSocket', FakeWebSocket);
    vi.stubGlobal('location', { protocol, host: 'onett.test' });
    vi.stubGlobal('window', fired);
    vi.stubGlobal('document', Object.assign(fired, { visibilityState: 'visible' }));
    const invalidateQueries = vi.fn().mockResolvedValue(undefined);
    const client = { invalidateQueries } as unknown as QueryClient;
    const stream = new EventStream();
    started = stream;
    stream.start(client);
    return { stream, client, invalidateQueries };
  }

  // Waits out a failed connect: getStats fails here (no server), then the backoff timer runs.
  async function reconnect(delay = 1_000): Promise<void> {
    await vi.advanceTimersByTimeAsync(delay);
    await vi.advanceTimersByTimeAsync(0);
  }

  it('connects to /api/v1/ws over ws: on http', () => {
    setup('http:');
    expect(socket().url).toBe('ws://onett.test/api/v1/ws');
  });

  it('connects over wss: on https', () => {
    setup('https:');
    expect(socket().url).toBe('wss://onett.test/api/v1/ws');
  });

  it('resumes from the last event id on reconnect', async () => {
    setup();
    socket().open();
    socket().receive({ kind: 'event', event: ev('task.updated', { id: 7 }) });
    socket().drop();
    await reconnect();
    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(socket().url).toBe('ws://onett.test/api/v1/ws?after=7');
  });

  it('does not invalidate on the first connect', () => {
    const { invalidateQueries } = setup();
    socket().open();
    expect(invalidateQueries).not.toHaveBeenCalled();
  });

  it('with no watches, is live on open and invalidates all queries once after a reconnect', async () => {
    const { stream, invalidateQueries } = setup();
    socket().open();
    expect(stream.getStatus()).toBe('live');
    socket().drop();
    await reconnect();
    expect(FakeWebSocket.instances).toHaveLength(2);

    socket().open();
    expect(invalidateQueries).toHaveBeenCalledTimes(1);
    expect(invalidateQueries).toHaveBeenCalledWith();
    expect(stream.getStatus()).toBe('live');
  });

  it('caps the reconnect delay at 5s', async () => {
    setup();
    // Each failure doubles the delay: 1s, 2s, 4s, then 5s from there on.
    for (const delay of [1_000, 2_000, 4_000, 5_000, 5_000]) {
      const count = FakeWebSocket.instances.length;
      socket().drop();
      await vi.advanceTimersByTimeAsync(delay);
      expect(FakeWebSocket.instances).toHaveLength(count + 1);
    }
  });

  it.each(['online', 'visibilitychange'])('reconnects at once on %s', async (type) => {
    setup();
    socket().drop();
    // Let recover() settle into its 1s backoff wait.
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeWebSocket.instances).toHaveLength(1);

    fired.dispatchEvent(new Event(type));
    expect(FakeWebSocket.instances).toHaveLength(2);
  });

  it('stops listening for online and visibility once stopped', async () => {
    const { stream } = setup();
    socket().drop();
    await vi.advanceTimersByTimeAsync(0);
    stream.stop();

    fired.dispatchEvent(new Event('online'));
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it('stops the stream when getStats answers 401', async () => {
    const { stream } = setup();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"error":"no session"}', { status: 401 })));
    socket().drop();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(stream.getStatus()).toBe('reconnecting');
  });

  it('sends one subscribe for two watches of one run, and one unsubscribe after both release', () => {
    const { stream } = setup();
    socket().open();
    const first = stream.watch({ run_id: 'r1' });
    const second = stream.watch({ run_id: 'r1' });
    expect(socket().sentMessages()).toEqual([{ kind: 'subscribe', run_id: 'r1' }]);

    socket().receive({ kind: 'subscribed', run_id: 'r1' });
    return Promise.all([first.ready, second.ready]).then(() => {
      first.release();
      expect(socket().sentMessages()).toHaveLength(1);
      second.release();
      expect(socket().sentMessages()).toEqual([
        { kind: 'subscribe', run_id: 'r1' },
        { kind: 'unsubscribe', run_id: 'r1' },
      ]);
    });
  });

  it('is reconnecting after open with a watch, and live once the watch is subscribed', async () => {
    const { stream } = setup();
    const w = stream.watch({ run_id: 'r1' });
    socket().open();
    expect(stream.getStatus()).toBe('reconnecting');

    socket().receive({ kind: 'subscribed', run_id: 'r1' });
    await w.ready;
    expect(stream.getStatus()).toBe('live');
  });

  it('re-sends every live watch on reconnect and stays reconnecting until each is acknowledged', async () => {
    const { stream } = setup();
    socket().open();
    const run = stream.watch({ run_id: 'r1' });
    const chat = stream.watch({ conversation_id: 'c1' });
    socket().receive({ kind: 'subscribed', run_id: 'r1' });
    socket().receive({ kind: 'subscribed', conversation_id: 'c1' });
    await Promise.all([run.ready, chat.ready]);
    expect(stream.getStatus()).toBe('live');

    socket().drop();
    expect(stream.getStatus()).toBe('reconnecting');
    await reconnect();
    socket().open();
    expect(socket().sentMessages()).toEqual([
      { kind: 'subscribe', run_id: 'r1' },
      { kind: 'subscribe', conversation_id: 'c1' },
    ]);
    expect(stream.getStatus()).toBe('reconnecting');

    socket().receive({ kind: 'subscribed', run_id: 'r1' });
    expect(stream.getStatus()).toBe('reconnecting');
    socket().receive({ kind: 'subscribed', conversation_id: 'c1' });
    expect(stream.getStatus()).toBe('live');
  });

  it('lets go of a watch before it is acknowledged, and is live without waiting for it', () => {
    const { stream } = setup();
    socket().open();
    const w = stream.watch({ run_id: 'r1' });
    expect(stream.getStatus()).toBe('live');

    w.release();
    expect(stream.getStatus()).toBe('live');
    expect(socket().sentMessages()).toEqual([
      { kind: 'subscribe', run_id: 'r1' },
      { kind: 'unsubscribe', run_id: 'r1' },
    ]);

    // A late acknowledgement for the released watch changes nothing and throws nothing.
    expect(() => socket().receive({ kind: 'subscribed', run_id: 'r1' })).not.toThrow();
    expect(stream.getStatus()).toBe('live');
  });

  it('does not let a late subscribed for an earlier watch acknowledge a new one on the same run', async () => {
    const { stream } = setup();
    socket().open();
    const earlier = stream.watch({ run_id: 'r1' });
    earlier.release();
    const later = stream.watch({ run_id: 'r1' });
    let laterReady = false;
    void later.ready.then(() => {
      laterReady = true;
    });

    // The server answers in order: the first subscribe (for the earlier watch), then the second.
    socket().receive({ kind: 'subscribed', run_id: 'r1' });
    await Promise.resolve();
    expect(laterReady).toBe(false);
    socket().receive({ kind: 'subscribed', run_id: 'r1' });
    return expect(later.ready).resolves.toBeUndefined();
  });

  it('leaves a live status live when a watch is added and acknowledged', async () => {
    const { stream } = setup();
    socket().open();
    expect(stream.getStatus()).toBe('live');

    const w = stream.watch({ run_id: 'r1' });
    expect(socket().sentMessages()).toEqual([{ kind: 'subscribe', run_id: 'r1' }]);
    expect(stream.getStatus()).toBe('live');

    socket().receive({ kind: 'subscribed', run_id: 'r1' });
    expect(stream.getStatus()).toBe('live');
    await w.ready;
  });

  it('does not hold the status back with a watch added during an open gate', async () => {
    const { stream } = setup();
    const gated = stream.watch({ run_id: 'r1' });
    socket().open();
    expect(stream.getStatus()).toBe('reconnecting');

    const added = stream.watch({ conversation_id: 'c1' });
    expect(stream.getStatus()).toBe('reconnecting');

    // Only the gated watch decides; the added one is still unacknowledged.
    socket().receive({ kind: 'subscribed', run_id: 'r1' });
    expect(stream.getStatus()).toBe('live');
    await gated.ready;

    socket().receive({ kind: 'subscribed', conversation_id: 'c1' });
    await added.ready;
    expect(stream.getStatus()).toBe('live');
  });

  it('does not count a watch as confirmed on a new socket after stop and start', async () => {
    const { stream, client } = setup();
    socket().open();
    const w = stream.watch({ run_id: 'r1' });
    socket().receive({ kind: 'subscribed', run_id: 'r1' });
    await w.ready;
    expect(stream.getStatus()).toBe('live');

    stream.stop();
    stream.start(client);
    socket().open();
    expect(socket().sentMessages()).toEqual([{ kind: 'subscribe', run_id: 'r1' }]);
    expect(stream.getStatus()).toBe('reconnecting');
  });

  it('sends subscribe on the next open for a watch created while disconnected', () => {
    const { stream } = setup();
    const w = stream.watch({ run_id: 'r1' });
    expect(socket().sent).toEqual([]);

    socket().open();
    expect(socket().sentMessages()).toEqual([{ kind: 'subscribe', run_id: 'r1' }]);
    expect(stream.getStatus()).toBe('reconnecting');

    socket().receive({ kind: 'subscribed', run_id: 'r1' });
    return expect(w.ready).resolves.toBeUndefined();
  });

  it('passes event frames to subscribe() listeners and ignores error frames', () => {
    const { stream } = setup();
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    const listener = vi.fn();
    stream.subscribe(listener);
    socket().open();

    const event = ev('run.log', { id: 0, run_id: 'r1', data: { seq: 1 } });
    socket().receive({ kind: 'event', event });
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener).toHaveBeenCalledWith(event);

    socket().receive({ kind: 'error', message: 'bad subscribe' });
    expect(listener).toHaveBeenCalledTimes(1);
    expect(consoleError).toHaveBeenCalled();
  });
});
