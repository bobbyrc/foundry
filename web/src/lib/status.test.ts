import { describe, expect, it } from 'vitest';
import { isPulsing, riskLabel, riskTone, toneFor } from './status';

describe('toneFor', () => {
  it('colours statuses as the design doc specifies', () => {
    expect(toneFor('task', 'queued')).toBe('muted');
    expect(toneFor('task', 'review')).toBe('review');
    expect(toneFor('task', 'awaiting_approval')).toBe('accent');
    expect(toneFor('task', 'done')).toBe('ok');
    expect(toneFor('task', 'blocked')).toBe('danger');
    expect(toneFor('run', 'timed_out')).toBe('danger');
    expect(toneFor('run', 'succeeded')).toBe('ok');
    expect(toneFor('deploy', 'rolled_back')).toBe('danger');
    expect(toneFor('deploy', 'healthy')).toBe('ok');
  });

  it('falls back to muted for unknown statuses', () => {
    expect(toneFor('task', 'mystery')).toBe('muted');
  });
});

describe('isPulsing', () => {
  it('pulses only running work', () => {
    expect(isPulsing('task', 'running')).toBe(true);
    expect(isPulsing('run', 'starting')).toBe(true);
    expect(isPulsing('run', 'succeeded')).toBe(false);
    expect(isPulsing('task', 'queued')).toBe(false);
  });
});

describe('risk', () => {
  it('shows unrated for an empty risk', () => {
    expect(riskLabel('')).toBe('unrated');
    expect(riskTone('')).toBe('muted');
    expect(riskTone('high')).toBe('danger');
  });
});
