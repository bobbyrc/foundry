import { describe, expect, it } from 'vitest';
import { isConnected, saveToken, subscribeAuth } from './auth';

describe('saveToken', () => {
  it('connects on a first visit and tells subscribers, so the Connect page goes away', () => {
    expect(isConnected()).toBe(false);
    let seen: boolean | null = null;
    const unsubscribe = subscribeAuth(() => {
      seen = isConnected();
    });
    saveToken(' abc123 ');
    unsubscribe();
    expect(seen).toBe(true);
    expect(isConnected()).toBe(true);
  });
});
