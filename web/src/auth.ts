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

const TOKEN_FRAGMENT = /(?:^#|&)token=([^&]*)/;

// A bearer token is never empty, contains no whitespace or control characters.
const VALID_TOKEN = /^[^\s\x00-\x1f\x7f]+$/;

/** Extracts the token from a `#token=...` fragment. Returns null when absent, malformed or invalid. */
export function parseTokenFragment(hash: string): string | null {
  const match = TOKEN_FRAGMENT.exec(hash);
  if (!match?.[1]) return null;
  let token: string;
  try {
    token = decodeURIComponent(match[1]);
  } catch {
    // A bad percent-escape (e.g. "%E0%A4%A") must not throw at startup.
    return null;
  }
  return VALID_TOKEN.test(token) ? token : null;
}

/** Reads `#token=...` from the URL once, stores it if valid and always drops the fragment. */
export function captureTokenFromHash(): void {
  const hash = window.location.hash;
  if (!TOKEN_FRAGMENT.test(hash)) return;
  const token = parseTokenFragment(hash);
  if (token !== null) saveToken(token);
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
