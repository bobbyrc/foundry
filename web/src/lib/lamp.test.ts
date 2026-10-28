import { describe, expect, it } from 'vitest';
import { lampDetail, projectLamp } from './lamp';

const counts = (over: Partial<Record<'queued' | 'running' | 'review' | 'awaiting_approval' | 'blocked', number>> = {}) => ({
  queued: 0,
  running: 0,
  review: 0,
  awaiting_approval: 0,
  blocked: 0,
  ...over,
});

describe('projectLamp', () => {
  it('stops the line for a blocked task even when other work runs', () => {
    expect(projectLamp({ paused: false, counts: counts({ blocked: 1, running: 2, awaiting_approval: 1 }) })).toEqual({
      lamp: 'stop',
      label: '1 blocked',
    });
  });

  it('calls the owner for approvals ahead of running work', () => {
    expect(projectLamp({ paused: false, counts: counts({ awaiting_approval: 2, running: 1 }) })).toEqual({
      lamp: 'call',
      label: '2 need you',
    });
    expect(projectLamp({ paused: false, counts: counts({ awaiting_approval: 1 }) }).label).toBe('1 needs you');
  });

  it('shows a paused project as off unless it needs a decision', () => {
    expect(projectLamp({ paused: true, counts: counts({ running: 1 }) })).toEqual({ lamp: 'off', label: 'paused' });
    expect(projectLamp({ paused: true, counts: counts({ blocked: 1 }) }).lamp).toBe('stop');
  });

  it('runs green for running or reviewing work and is off when idle or only queued', () => {
    expect(projectLamp({ paused: false, counts: counts({ running: 3 }) })).toEqual({ lamp: 'run', label: '3 running' });
    expect(projectLamp({ paused: false, counts: counts({ review: 1 }) })).toEqual({ lamp: 'run', label: '1 in review' });
    expect(projectLamp({ paused: false, counts: counts({ queued: 4 }) })).toEqual({ lamp: 'off', label: '4 queued' });
    expect(projectLamp({ paused: false, counts: counts() })).toEqual({ lamp: 'off', label: 'idle' });
  });
});

describe('lampDetail', () => {
  it('lists the other non-zero counts without repeating the label', () => {
    const project = { counts: counts({ running: 2, review: 1, queued: 3 }) };
    expect(lampDetail(project, projectLamp({ ...project, paused: false }))).toBe('1 in review · 3 queued');
  });

  it('is empty when nothing else is going on', () => {
    const project = { counts: counts({ blocked: 1 }) };
    expect(lampDetail(project, projectLamp({ ...project, paused: false }))).toBe('');
  });
});
