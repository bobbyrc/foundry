import { describe, expect, it } from 'vitest';
import { loopHealth, normaliseProject, repoUrl, type RawProjectView } from './projects';
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

describe('normaliseProject', () => {
  const base: RawProjectView = {
    name: 'foundry',
    repo: 'bobbyrc/foundry',
    path: '/repos/foundry',
    default_branch: 'main',
    policy: 'default',
    paused: false,
    paused_by: '',
  };

  it('fills a missing observe with an empty array and a missing deploy with null', () => {
    const view = normaliseProject(base);
    expect(view.observe).toEqual([]);
    expect(view.deploy).toBeNull();
  });

  it('turns a null observe into an empty array', () => {
    expect(normaliseProject({ ...base, observe: null }).observe).toEqual([]);
  });

  it('keeps observers and deploy when present', () => {
    const observe = [{ name: 'sentry', type: 'sentry' }];
    const deploy = { type: 'vercel', verify_for: '1m', timeout: '5m' };
    const view = normaliseProject({ ...base, observe, deploy });
    expect(view.observe).toBe(observe);
    expect(view.deploy).toBe(deploy);
  });

  it('gives every count key a zero when counts are missing', () => {
    expect(normaliseProject(base).counts).toEqual({
      queued: 0,
      running: 0,
      review: 0,
      awaiting_approval: 0,
      blocked: 0,
    });
  });

  it('keeps the given counts and fills the rest', () => {
    expect(normaliseProject({ ...base, counts: { queued: 2 } }).counts.queued).toBe(2);
    expect(normaliseProject({ ...base, counts: { queued: 2 } }).counts.blocked).toBe(0);
  });
});
