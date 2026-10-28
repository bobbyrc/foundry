// Who is signed in. The server holds the session in an HttpOnly cookie that the browser sends by
// itself, so this module keeps only what the UI needs to draw: loading, setup, signed out (and
// whether the session just ended) or signed in as a user. Nothing is stored in the browser.
import { useSyncExternalStore } from 'react';
import * as api from './api';
import { can, type Action } from './lib/roles';
import type { User } from './types';

/** Dispatched on window by api.ts when a normal call comes back 401: the session has ended. */
export const UNAUTHORIZED_EVENT = 'onlet:unauthorized';

export type AuthState =
  | { status: 'loading' }
  | { status: 'setup' }
  | { status: 'signed-out'; expired: boolean }
  | { status: 'signed-in'; user: User };

export type AuthEvent =
  | { type: 'loaded'; setupRequired: boolean; user: User | null }
  | { type: 'signed-in'; user: User }
  | { type: 'signed-out' }
  | { type: 'unauthorized' }
  | { type: 'user-updated'; user: User };

/**
 * The state machine, with no side effects, so each transition can be tested. A 401 only ends a
 * session that was signed in; before sign-in it means nothing has changed.
 */
export function nextState(state: AuthState, event: AuthEvent): AuthState {
  switch (event.type) {
    case 'loaded':
      if (event.setupRequired) return { status: 'setup' };
      if (event.user) return { status: 'signed-in', user: event.user };
      return { status: 'signed-out', expired: false };
    case 'signed-in':
      return { status: 'signed-in', user: event.user };
    case 'signed-out':
      return { status: 'signed-out', expired: false };
    case 'unauthorized':
      return state.status === 'signed-in' ? { status: 'signed-out', expired: true } : state;
    case 'user-updated':
      return state.status === 'signed-in' ? { status: 'signed-in', user: event.user } : state;
  }
}

const RETRY_MS = 2_000;

const listeners = new Set<() => void>();
let state: AuthState = { status: 'loading' };
let retryTimer: ReturnType<typeof setTimeout> | null = null;

function dispatch(event: AuthEvent): void {
  const next = nextState(state, event);
  if (next === state) return;
  state = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getAuthState(): AuthState {
  return state;
}

/** Asks the server who is signed in. Called once at startup, and again after a self role change. */
export async function loadAuthState(): Promise<void> {
  if (retryTimer !== null) {
    clearTimeout(retryTimer);
    retryTimer = null;
  }
  try {
    const res = await api.getAuthState();
    dispatch({ type: 'loaded', setupRequired: res.setup_required, user: res.user });
  } catch {
    // The server is unreachable: stay on "loading" and ask again shortly.
    retryTimer = setTimeout(() => void loadAuthState(), RETRY_MS);
  }
}

/** Signs in. Rejects with an ApiError (401 wrong credentials, 429 too many attempts). */
export async function signIn(username: string, password: string): Promise<void> {
  const user = await api.login(username, password);
  dispatch({ type: 'signed-in', user });
}

/** Creates the first owner account and signs in as it. Rejects with 409 when one already exists. */
export async function setUp(username: string, password: string): Promise<void> {
  const user = await api.setupOwner(username, password);
  dispatch({ type: 'signed-in', user });
}

/** Ends the session on the server and drops back to the sign-in page. */
export async function signOut(): Promise<void> {
  try {
    await api.logout();
  } finally {
    // Even when the request fails, the screen must not keep showing the account.
    dispatch({ type: 'signed-out' });
  }
}

/** Updates the signed-in user after its own password or role changed. */
export function setSignedInUser(user: User): void {
  dispatch({ type: 'user-updated', user });
}

export function useAuthState(): AuthState {
  return useSyncExternalStore(subscribe, getAuthState);
}

/** The signed-in user, or null before sign-in and after the session ends. */
export function useMe(): User | null {
  const auth = useAuthState();
  return auth.status === 'signed-in' ? auth.user : null;
}

/** Whether the signed-in user may do `action`. False while nobody is signed in. */
export function useCan(action: Action): boolean {
  const me = useMe();
  return can(me?.role, action);
}

if (typeof window !== 'undefined') {
  window.addEventListener(UNAUTHORIZED_EVENT, () => dispatch({ type: 'unauthorized' }));
}
