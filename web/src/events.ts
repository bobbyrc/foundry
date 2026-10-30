// The single event stream. One WebSocket to /api/v1/ws feeds every subscriber, reconnects with
// backoff (1s doubling to 5s) and resumes from the last stored event ID. Pages can also watch a
// run's log or a conversation's chat: the server then sends those events only to this socket.
import { useSyncExternalStore } from 'react';
import type { QueryClient, QueryKey } from '@tanstack/react-query';
import { ApiError, getStats } from './api';
import { keys } from './keys';
import type { OnettEvent, WsClientMessage, WsServerMessage } from './types';

export type LiveStatus = 'live' | 'reconnecting';

export type WatchTarget = { run_id: string } | { conversation_id: string };

export interface Watch {
  /** Resolves when the server confirms the subscription. Never resolves if released first. */
  ready: Promise<void>;
  /** Lets go of the watch. Returned at once, so an unmount before `ready` can still clean up. */
  release: () => void;
}

type Listener = (ev: OnettEvent) => void;

interface WatchEntry {
  key: string;
  target: WatchTarget;
  // How many Watch handles hold this entry. The subscription ends when the last one is released.
  count: number;
  // True once the server has confirmed this entry's own subscribe on the current socket.
  acked: boolean;
  waiters: Array<() => void>;
}

const MIN_BACKOFF_MS = 1_000;
// A retry is one small request to a self-hosted daemon, so the cap stays low: a longer one only
// leaves the UI disconnected for that long after the daemon comes back.
const MAX_BACKOFF_MS = 5_000;

function watchKey(target: WatchTarget): string {
  return 'run_id' in target ? `run:${target.run_id}` : `conversation:${target.conversation_id}`;
}

export class EventStream {
  private socket: WebSocket | null = null;
  // True between a socket's open and its close. Only then can frames be sent.
  private open = false;
  private queryClient: QueryClient | null = null;
  // Set by the first successful open and kept across stop()/start(), so any later open is a reconnect.
  private everConnected = false;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private attempt = 0;
  private lastId = 0;
  private running = false;
  private status: LiveStatus = 'reconnecting';
  private readonly listeners = new Set<Listener>();
  private readonly statusListeners = new Set<() => void>();
  private readonly watches = new Map<string, WatchEntry>();
  // Subscribe frames sent on the current socket that the server has not answered yet, per key, in
  // send order. The server answers in order, so the first unanswered one is the next reply. This
  // stops a reply to an earlier watch on the same run from acknowledging a later one.
  private readonly pendingSubscribes = new Map<string, WatchEntry[]>();

  /** Starts the stream. Only call it while signed in: the session cookie is what authenticates it. */
  start(queryClient: QueryClient): void {
    if (this.running) return;
    this.queryClient = queryClient;
    this.running = true;
    window.addEventListener('online', this.retryNow);
    document.addEventListener('visibilitychange', this.retryNow);
    this.connect();
  }

  stop(): void {
    this.running = false;
    window.removeEventListener('online', this.retryNow);
    document.removeEventListener('visibilitychange', this.retryNow);
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    const socket = this.socket;
    // Clear the reference first: the close event fires later, and onclose ignores a socket that is
    // no longer this.socket.
    this.socket = null;
    this.open = false;
    this.pendingSubscribes.clear();
    socket?.close();
    this.attempt = 0;
    this.setStatus('reconnecting');
  }

  getStatus(): LiveStatus {
    return this.status;
  }

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  onStatus(listener: () => void): () => void {
    this.statusListeners.add(listener);
    return () => {
      this.statusListeners.delete(listener);
    };
  }

  /**
   * Asks the server to send one run's log or one conversation's chat tokens to this tab. Watches
   * on the same target share one subscription.
   */
  watch(target: WatchTarget): Watch {
    const key = watchKey(target);
    let entry = this.watches.get(key);
    if (!entry) {
      entry = { key, target, count: 0, acked: false, waiters: [] };
      this.watches.set(key, entry);
      this.sendSubscribe(entry);
    }
    const held = entry;
    held.count += 1;
    let released = false;
    const ready = new Promise<void>((resolve) => {
      if (held.acked) resolve();
      else held.waiters.push(resolve);
    });
    this.refreshStatus();
    return {
      ready,
      release: () => {
        if (released) return;
        released = true;
        held.count -= 1;
        if (held.count > 0) return;
        this.watches.delete(held.key);
        if (this.open) this.send({ kind: 'unsubscribe', ...held.target });
        this.refreshStatus();
      },
    };
  }

  private setStatus(next: LiveStatus): void {
    if (this.status === next) return;
    this.status = next;
    for (const listener of this.statusListeners) listener();
  }

  // Live only when the socket is open and every watch has been confirmed on it. Components run
  // their catch-up fetch when this turns live, so it must not turn live before a subscription is in
  // effect.
  private refreshStatus(): void {
    let live = this.open;
    if (live) {
      for (const entry of this.watches.values()) {
        if (!entry.acked) {
          live = false;
          break;
        }
      }
    }
    this.setStatus(live ? 'live' : 'reconnecting');
  }

  private send(msg: WsClientMessage): void {
    this.socket?.send(JSON.stringify(msg));
  }

  private sendSubscribe(entry: WatchEntry): void {
    if (!this.open) return;
    // A subscribe on this socket is unconfirmed until its own subscribed arrives. An acked flag
    // left over from an earlier socket must not count.
    entry.acked = false;
    const queue = this.pendingSubscribes.get(entry.key) ?? [];
    queue.push(entry);
    this.pendingSubscribes.set(entry.key, queue);
    this.send({ kind: 'subscribe', ...entry.target });
  }

  // Skips the rest of a backoff wait when the network returns or the tab is shown again. Browsers
  // throttle timers in background tabs, so a wait that started there can run far past its delay.
  private readonly retryNow = (): void => {
    if (this.timer === null || document.visibilityState !== 'visible') return;
    clearTimeout(this.timer);
    this.timer = null;
    this.attempt = 0;
    this.connect();
  };

  private connect(): void {
    if (!this.running) return;

    // The browser sends the session cookie with the WebSocket upgrade, so the URL carries no
    // credential. The scheme follows the page, so an https page uses wss:.
    const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const query = this.lastId > 0 ? `?after=${this.lastId}` : '';
    const socket = new WebSocket(`${scheme}//${location.host}/api/v1/ws${query}`);
    this.socket = socket;

    socket.onopen = () => {
      if (this.socket !== socket) return;
      const reconnected = this.everConnected;
      this.everConnected = true;
      this.attempt = 0;
      this.open = true;
      // The server knows no subscriptions on a new socket, so every live watch is sent again.
      for (const entry of this.watches.values()) this.sendSubscribe(entry);
      this.refreshStatus();
      // Events sent during the outage are not replayed (live-only ones never are), so refetch
      // every query once rather than trust the cache.
      if (reconnected) void this.queryClient?.invalidateQueries();
    };
    socket.onmessage = (message) => this.handle(message);
    // Browsers fire error and then close for a failed connect, so the retry is scheduled here only.
    socket.onclose = () => {
      if (this.socket !== socket) return;
      this.socket = null;
      this.open = false;
      this.pendingSubscribes.clear();
      for (const entry of this.watches.values()) entry.acked = false;
      this.refreshStatus();
      if (this.running) void this.recover();
    };
  }

  /** Checks the session (a 401 stops the stream) and schedules the next connect attempt. */
  private async recover(): Promise<void> {
    try {
      await getStats();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        this.stop();
        return;
      }
      // Any other failure (server down) falls through to the backoff below.
    }
    if (!this.running) return;
    const delay = Math.min(MAX_BACKOFF_MS, MIN_BACKOFF_MS * 2 ** this.attempt);
    this.attempt += 1;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.connect();
    }, delay);
  }

  private handle(message: MessageEvent<string>): void {
    let msg: WsServerMessage;
    try {
      msg = JSON.parse(message.data) as WsServerMessage;
    } catch {
      return;
    }
    switch (msg.kind) {
      case 'event':
        this.deliver(msg.event);
        return;
      case 'subscribed':
        this.acknowledge(msg.run_id, msg.conversation_id);
        return;
      case 'error':
        // The server refused a frame. Nothing to deliver; the watch it named stays unconfirmed.
        console.error('onett stream refused a frame', msg.message);
        return;
      default:
        // unsubscribed: the client already dropped the watch when it released it.
        return;
    }
  }

  private deliver(ev: OnettEvent): void {
    if (typeof ev.id === 'number' && ev.id > this.lastId) this.lastId = ev.id;
    for (const listener of [...this.listeners]) {
      try {
        listener(ev);
      } catch (err) {
        console.error('onett event listener failed', err);
      }
    }
  }

  private acknowledge(runId: string | undefined, conversationId: string | undefined): void {
    const key = runId ? `run:${runId}` : conversationId ? `conversation:${conversationId}` : null;
    if (key === null) return;
    const queue = this.pendingSubscribes.get(key);
    const sent = queue?.shift();
    if (queue && queue.length === 0) this.pendingSubscribes.delete(key);
    // Only the entry that sent this subscribe is confirmed. A watch released meanwhile has no
    // entry any more, so its reply is dropped.
    if (!sent || this.watches.get(key) !== sent) return;
    sent.acked = true;
    for (const resolve of sent.waiters) resolve();
    sent.waiters = [];
    this.refreshStatus();
  }
}

export const eventStream = new EventStream();

export function subscribe(listener: Listener): () => void {
  return eventStream.subscribe(listener);
}

/** Watches one run or conversation on the shared stream. See EventStream.watch. */
export function watch(target: WatchTarget): Watch {
  return eventStream.watch(target);
}

export function useLiveStatus(): LiveStatus {
  return useSyncExternalStore(
    (listener) => eventStream.onStatus(listener),
    () => eventStream.getStatus(),
    () => 'reconnecting' as LiveStatus,
  );
}

/** The query keys an event makes stale. Live-only events (run.log, chat.delta) refetch nothing. */
export function keysFor(ev: OnettEvent): QueryKey[] {
  const out: QueryKey[] = [];
  const add = (...queryKeys: QueryKey[]) => out.push(...queryKeys);
  const taskId = ev.task_id || undefined;
  const runId = ev.run_id || undefined;

  if (ev.type.startsWith('task.')) {
    add(keys.tasksAll, keys.stats);
    if (taskId) add(keys.task(taskId));
  } else if (ev.type === 'run.updated') {
    add(keys.runsAll, keys.stats);
    if (runId) add(keys.run(runId));
    if (taskId) add(keys.task(taskId));
  } else if (ev.type.startsWith('approval.')) {
    add(keys.approvalsAll, keys.stats);
    if (taskId) add(keys.task(taskId));
  } else if (ev.type === 'deploy.updated') {
    add(keys.deploymentsAll);
  } else if (ev.type === 'signal.new') {
    add(keys.signalsAll);
  } else if (ev.type === 'friction.reported' || ev.type === 'issue.filed') {
    add(keys.frictionAll);
  } else if (ev.type === 'chat.message') {
    add(keys.conversationsAll);
    const data = ev.data as { conversation_id?: unknown } | undefined;
    if (typeof data?.conversation_id === 'string') add(keys.conversation(data.conversation_id));
  } else if (ev.type === 'pr.opened' || ev.type === 'pr.merged') {
    add(keys.tasksAll);
    if (taskId) add(keys.task(taskId));
  } else if (ev.type === 'loop.error') {
    add(keys.loopsAll);
  } else if (ev.type === 'system') {
    add(keys.control, keys.stats, keys.projectsAll);
  }
  return out;
}

export function invalidateFor(ev: OnettEvent, queryClient: QueryClient): void {
  for (const queryKey of keysFor(ev)) {
    void queryClient.invalidateQueries({ queryKey });
  }
}
