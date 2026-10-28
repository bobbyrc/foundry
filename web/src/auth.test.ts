import { describe, expect, it } from 'vitest';
import { nextState, type AuthState } from './auth';
import type { User } from './types';

const alice: User = {
  username: 'alice',
  role: 'owner',
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
};
const bob: User = { ...alice, username: 'bob', role: 'viewer' };

const loading: AuthState = { status: 'loading' };
const setup: AuthState = { status: 'setup' };
const signedOut: AuthState = { status: 'signed-out', expired: false };
const signedIn: AuthState = { status: 'signed-in', user: alice };

describe('nextState on load', () => {
  it('goes to setup when no user exists yet', () => {
    expect(nextState(loading, { type: 'loaded', setupRequired: true, user: null })).toEqual({ status: 'setup' });
  });

  it('is signed in when the server returns a user', () => {
    expect(nextState(loading, { type: 'loaded', setupRequired: false, user: alice })).toEqual({
      status: 'signed-in',
      user: alice,
    });
  });

  it('is signed out, and not expired, when there is no session', () => {
    expect(nextState(loading, { type: 'loaded', setupRequired: false, user: null })).toEqual({
      status: 'signed-out',
      expired: false,
    });
  });
});

describe('nextState on sign-in and sign-out', () => {
  it('signing in replaces whatever was shown with the user', () => {
    expect(nextState(signedOut, { type: 'signed-in', user: bob })).toEqual({ status: 'signed-in', user: bob });
  });

  it('signing out clears the expired flag', () => {
    const expired: AuthState = { status: 'signed-out', expired: true };
    expect(nextState(signedIn, { type: 'signed-out' })).toEqual(signedOut);
    expect(nextState(expired, { type: 'signed-out' })).toEqual(signedOut);
  });
});

describe('nextState on a 401', () => {
  it('ends a signed-in session as expired', () => {
    expect(nextState(signedIn, { type: 'unauthorized' })).toEqual({ status: 'signed-out', expired: true });
  });

  it('changes nothing before anyone has signed in', () => {
    expect(nextState(loading, { type: 'unauthorized' })).toBe(loading);
    expect(nextState(setup, { type: 'unauthorized' })).toBe(setup);
    expect(nextState(signedOut, { type: 'unauthorized' })).toBe(signedOut);
  });
});

describe('nextState on a self update', () => {
  it('replaces the signed-in user', () => {
    expect(nextState(signedIn, { type: 'user-updated', user: { ...alice, role: 'operator' } })).toEqual({
      status: 'signed-in',
      user: { ...alice, role: 'operator' },
    });
  });

  it('does nothing when nobody is signed in', () => {
    expect(nextState(signedOut, { type: 'user-updated', user: alice })).toBe(signedOut);
  });
});
