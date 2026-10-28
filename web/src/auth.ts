import { useSyncExternalStore } from 'react';

// The bearer token lives in localStorage. A 401 from any API call (api.ts dispatches
// UNAUTHORIZED_EVENT) flips the app back to the Connect page until a new token is saved.

const TOKEN_KEY = 'foundry.token';
export const UNAUTHORIZED_EVENT = 'foundry:unauthorized';

const listeners = new Set<() => void>();
let unauthorized = false;

function emit(): void {
  for (const listener of listeners) listener();
}

export function getToken(): string | null {
  try {
    const token = window.localStorage.getItem(TOKEN_KEY)?.trim();
    return token ? token : null;
  } catch {
    // Storage can be blocked (private window, disabled site data): treat as no token.
    return null;
  }
}

export function saveToken(token: string): void {
  try {
    window.localStorage.setItem(TOKEN_KEY, token.trim());
  } catch {
    // Without storage the token lasts only until the next reload.
  }
  unauthorized = false;
  emit();
}

/** Reads `#token=...` from the URL once, stores it and drops it from the address bar. */
export function captureTokenFromHash(): void {
  const match = /(?:^#|&)token=([^&]+)/.exec(window.location.hash);
  if (!match?.[1]) return;
  saveToken(decodeURIComponent(match[1]));
  window.history.replaceState(null, '', window.location.pathname + window.location.search);
}

export function isUnauthorized(): boolean {
  return unauthorized;
}

export function subscribeAuth(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useUnauthorized(): boolean {
  return useSyncExternalStore(subscribeAuth, isUnauthorized);
}

if (typeof window !== 'undefined') {
  window.addEventListener(UNAUTHORIZED_EVENT, () => {
    if (!unauthorized) {
      unauthorized = true;
      emit();
    }
  });
}
