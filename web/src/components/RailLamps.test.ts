import { describe, expect, it } from 'vitest';
import type { ProjectView } from '../types';
import { railRows } from './RailLamps';

const project = (name: string, over: { running?: number; blocked?: number; awaiting?: number; paused?: boolean } = {}): ProjectView =>
  ({
    name,
    paused: over.paused ?? false,
    counts: {
      queued: 0,
      running: over.running ?? 0,
      review: 0,
      awaiting_approval: over.awaiting ?? 0,
      blocked: over.blocked ?? 0,
    },
  }) as unknown as ProjectView;

describe('railRows', () => {
  it('orders stop, then call, then run, then off', () => {
    const { shown, overflow } = railRows([
      project('idle'),
      project('working', { running: 1 }),
      project('waiting', { awaiting: 1 }),
      project('broken', { blocked: 1 }),
    ]);
    expect(shown.map((row) => row.state.lamp)).toEqual(['stop', 'call', 'run', 'off']);
    expect(shown.map((row) => row.project.name)).toEqual(['broken', 'waiting', 'working', 'idle']);
    expect(overflow).toBe(0);
  });

  it('breaks ties by name', () => {
    const { shown } = railRows([project('zeta', { running: 1 }), project('alpha', { running: 1 }), project('mid', { running: 1 })]);
    expect(shown.map((row) => row.project.name)).toEqual(['alpha', 'mid', 'zeta']);
  });

  it('shows at most eight rows and reports the overflow', () => {
    const projects = Array.from({ length: 11 }, (_, i) => project(`p${String(i).padStart(2, '0')}`));
    const { shown, overflow } = railRows(projects);
    expect(shown).toHaveLength(8);
    expect(overflow).toBe(3);
    expect(shown[0].project.name).toBe('p00');
  });

  it('keeps the most urgent projects when capping', () => {
    const projects = [
      ...Array.from({ length: 8 }, (_, i) => project(`idle${i}`)),
      project('stuck', { blocked: 2 }),
    ];
    const { shown, overflow } = railRows(projects);
    expect(shown[0].project.name).toBe('stuck');
    expect(shown.some((row) => row.project.name === 'idle7')).toBe(false);
    expect(overflow).toBe(1);
  });

  it('reports no overflow at exactly eight projects', () => {
    const projects = Array.from({ length: 8 }, (_, i) => project(`p${i}`));
    expect(railRows(projects)).toMatchObject({ overflow: 0 });
    expect(railRows(projects).shown).toHaveLength(8);
  });

  it('returns nothing for no projects', () => {
    expect(railRows([])).toEqual({ shown: [], overflow: 0 });
  });
});
