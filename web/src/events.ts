// The single event stream. One EventSource to /api/v1/events feeds every subscriber, reconnects
// with backoff (1s doubling to 30s) and resumes from the last stored event ID.
import { useSyncExternalStore } from 'react';
import type { QueryClient, QueryKey } from '@tanstack/react-query';
import { ApiError, getStats } from './api';
import { getToken } from './auth';
import { keys } from './keys';
import type { FoundryEvent } from './types';

// Mirrors the Ev* constants in internal/domain/domain.go. Browsers only deliver named SSE events
// to listeners registered by name, so each known type is registered below; onmessage covers
// unnamed events.
export const EVENT_TYPES = [
  'task.created',
  'task.updated',
  'run.updated',
  'run.log',
  'pr.opened',
  'pr.merged',
  'policy.decision',
  'approval.requested',
  'approval.decided',
  'deploy.updated',
  'signal.new',
  'friction.reported',
  'issue.filed',
  'chat.message',
  'chat.delta',
  'loop.error',
  'notification',
  'system',
] as const;

export type LiveStatus = 'live' | 'reconnecting';

type Listener = (ev: FoundryEvent) => void;

const MIN_BACKOFF_MS = 1_000;
const MAX_BACKOFF_MS = 30_000;

export class EventStream {
  private source: EventSource | null = null;
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

  start(queryClient: QueryClient): void {
    if (this.running || getToken() === null) return;
    this.queryClient = queryClient;
    this.running = true;
    this.connect();
  }

  stop(): void {
    this.running = false;
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    this.source?.close();
    this.source = null;
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

  private setStatus(next: LiveStatus): void {
    if (this.status === next) return;
    this.status = next;
    for (const listener of this.statusListeners) listener();
  }

  private connect(): void {
    const token = getToken();
    if (!this.running || token === null) return;

    const params = new URLSearchParams({ token });
    if (this.lastId > 0) params.set('after', String(this.lastId));
    const source = new EventSource(`/api/v1/events?${params.toString()}`);
    this.source = source;

    source.onopen = () => {
      const reconnected = this.everConnected;
      this.everConnected = true;
      this.attempt = 0;
      this.setStatus('live');
      // Events sent during the outage are not replayed (live-only ones never are), so refetch
      // every query once rather than trust the cache.
      if (reconnected) void this.queryClient?.invalidateQueries();
    };
    source.onmessage = (message) => this.handle(message);
    for (const type of EVENT_TYPES) {
      source.addEventListener(type, (message) => this.handle(message));
    }
    source.onerror = () => {
      source.close();
      if (this.source === source) this.source = null;
      this.setStatus('reconnecting');
      if (this.running) void this.recover();
    };
  }

  /** Checks the token (a 401 stops the stream) and schedules the next connect attempt. */
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
    let ev: FoundryEvent;
    try {
      ev = JSON.parse(message.data) as FoundryEvent;
    } catch {
      return;
    }
    if (typeof ev.id === 'number' && ev.id > this.lastId) this.lastId = ev.id;
    for (const listener of [...this.listeners]) {
      try {
        listener(ev);
      } catch (err) {
        console.error('foundry event listener failed', err);
      }
    }
  }
}

export const eventStream = new EventStream();

export function subscribe(listener: Listener): () => void {
  return eventStream.subscribe(listener);
}

export function useLiveStatus(): LiveStatus {
  return useSyncExternalStore(
    (listener) => eventStream.onStatus(listener),
    () => eventStream.getStatus(),
    () => 'reconnecting' as LiveStatus,
  );
}

/** The query keys an event makes stale. Live-only events (run.log, chat.delta) refetch nothing. */
export function keysFor(ev: FoundryEvent): QueryKey[] {
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

export function invalidateFor(ev: FoundryEvent, queryClient: QueryClient): void {
  for (const queryKey of keysFor(ev)) {
    void queryClient.invalidateQueries({ queryKey });
  }
}
