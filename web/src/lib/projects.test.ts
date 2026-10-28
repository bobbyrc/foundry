import { describe, expect, it } from 'vitest';
import { loopHealth, repoUrl } from './projects';
import type { LoopView } from '../types';

const loop = (extra: Partial<LoopView> = {}): LoopView => ({
  name: 'pr_shepherd',
  project: 'foundry',
  enabled: true,
  every: '5m',
  last_run_at: '2026-10-27T12:00:00Z',
  last_error: '',
  ...extra,
});

describe('repoUrl', () => {
  it('links owner/name only', () => {
    expect(repoUrl('bobbyrc/foundry')).toBe('https://github.com/bobbyrc/foundry');
    expect(repoUrl('https://example.com/x/y')).toBeUndefined();
    expect(repoUrl('foundry')).toBeUndefined();
    expect(repoUrl('')).toBeUndefined();
  });
});

describe('loopHealth', () => {
  it('is ok after a run with no error', () => {
    expect(loopHealth(loop())).toBe('ok');
  });

  it('is danger when the last run had an error', () => {
    expect(loopHealth(loop({ last_error: 'gh: 502' }))).toBe('danger');
  });

  it('is muted when disabled, even with a stale error', () => {
    expect(loopHealth(loop({ enabled: false }))).toBe('muted');
    expect(loopHealth(loop({ enabled: false, last_error: 'old' }))).toBe('muted');
  });

  it('is muted when the loop has never run (Go zero time or empty)', () => {
    expect(loopHealth(loop({ last_run_at: '0001-01-01T00:00:00Z' }))).toBe('muted');
    expect(loopHealth(loop({ last_run_at: '' }))).toBe('muted');
  });
});
