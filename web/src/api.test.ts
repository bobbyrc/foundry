import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { changePassword, getStats, login, logout, setUserRole } from './api';
import { UNAUTHORIZED_EVENT } from './auth';

// api.ts reads and dispatches on `window`, which Node does not have. A plain EventTarget is
// enough: it can be listened to and dispatched on, like the browser window.
let target: EventTarget;
let fetchMock: ReturnType<typeof vi.fn>;

function respond(status: number, body?: unknown): Response {
  if (body === undefined) return new Response(null, { status });
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

beforeEach(() => {
  target = new EventTarget();
  vi.stubGlobal('window', target);
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function sentHeaders(call = 0): Record<string, string> {
  const init = fetchMock.mock.calls[call]![1] as RequestInit;
  return (init.headers ?? {}) as Record<string, string>;
}

describe('api requests', () => {
  it('sends no Authorization header on a normal call', async () => {
    fetchMock.mockResolvedValue(respond(200, { version: 'x', queue: 0 }));
    await getStats();
    expect(sentHeaders()).not.toHaveProperty('Authorization');
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/stats');
  });

  it('sends no Authorization header on the auth calls either', async () => {
    fetchMock.mockResolvedValue(respond(200, { username: 'alice', role: 'owner', created_at: '', updated_at: '' }));
    await login('alice', 'correct horse battery');
    expect(sentHeaders()).not.toHaveProperty('Authorization');
    expect(JSON.parse((fetchMock.mock.calls[0]![1] as RequestInit).body as string)).toEqual({
      username: 'alice',
      password: 'correct horse battery',
    });
  });

  it('sends the password change inside the session, with the same JSON body', async () => {
    fetchMock.mockResolvedValue(respond(204));
    await changePassword('old password here', 'new password here');
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/password');
    expect(JSON.parse((fetchMock.mock.calls[0]![1] as RequestInit).body as string)).toEqual({
      current: 'old password here',
      new: 'new password here',
    });
  });

  it('sends the role change as JSON to the encoded user path', async () => {
    fetchMock.mockResolvedValue(respond(200, { username: 'a b', role: 'viewer', created_at: '', updated_at: '' }));
    await setUserRole('a b', 'viewer');
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/users/a%20b/role');
    expect(sentHeaders()).toHaveProperty('Content-Type', 'application/json');
  });
});

describe('api 401 handling', () => {
  it('dispatches UNAUTHORIZED_EVENT when a normal call is refused', async () => {
    fetchMock.mockResolvedValue(respond(401, { error: 'unauthorized' }));
    const seen = vi.fn();
    target.addEventListener(UNAUTHORIZED_EVENT, seen);
    await expect(getStats()).rejects.toMatchObject({ status: 401, message: 'unauthorized' });
    expect(seen).toHaveBeenCalledTimes(1);
  });

  it('does not dispatch UNAUTHORIZED_EVENT for a wrong password at sign-in', async () => {
    fetchMock.mockResolvedValue(respond(401, { error: 'wrong credentials' }));
    const seen = vi.fn();
    target.addEventListener(UNAUTHORIZED_EVENT, seen);
    await expect(login('alice', 'nope')).rejects.toMatchObject({ status: 401, message: 'wrong credentials' });
    expect(seen).not.toHaveBeenCalled();
  });

  it('dispatches UNAUTHORIZED_EVENT when a password change is refused with 401', async () => {
    fetchMock.mockResolvedValue(respond(401, { error: 'unauthorized' }));
    const seen = vi.fn();
    target.addEventListener(UNAUTHORIZED_EVENT, seen);
    await expect(changePassword('a', 'b')).rejects.toMatchObject({ status: 401 });
    expect(seen).toHaveBeenCalledTimes(1);
  });

  it('does not dispatch UNAUTHORIZED_EVENT for a wrong current password (403)', async () => {
    fetchMock.mockResolvedValue(respond(403, { error: 'the current password is wrong' }));
    const seen = vi.fn();
    target.addEventListener(UNAUTHORIZED_EVENT, seen);
    await expect(changePassword('wrong', 'b')).rejects.toMatchObject({
      status: 403,
      message: 'the current password is wrong',
    });
    expect(seen).not.toHaveBeenCalled();
  });

  it('dispatches UNAUTHORIZED_EVENT when sign-out finds the session gone', async () => {
    fetchMock.mockResolvedValue(respond(401, { error: 'unauthorized' }));
    const seen = vi.fn();
    target.addEventListener(UNAUTHORIZED_EVENT, seen);
    await expect(logout()).rejects.toMatchObject({ status: 401 });
    expect(seen).toHaveBeenCalledTimes(1);
  });
});
