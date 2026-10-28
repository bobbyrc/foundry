import { describe, expect, it } from 'vitest';
import { GLOBAL_PROJECT, loopGrid } from './loopGrid';
import type { LoopView } from '../types';

const loop = (name: string, project: string): LoopView => ({
  name,
  project,
  enabled: true,
  every: '30s',
  last_run_at: '',
});

describe('loopGrid', () => {
  it('sorts loop names and project names alphabetically', () => {
    const grid = loopGrid([
      loop('sweep', 'web'),
      loop('backup', 'api'),
      loop('sweep', 'api'),
      loop('backup', 'web'),
    ]);
    expect(grid.names).toEqual(['backup', 'sweep']);
    expect(grid.projects).toEqual(['api', 'web']);
  });

  it('puts a global column first, labelled by the empty project name', () => {
    const grid = loopGrid([loop('digest', ''), loop('sweep', 'web'), loop('sweep', 'api')]);
    expect(grid.projects).toEqual([GLOBAL_PROJECT, 'api', 'web']);
    expect(grid.cell('digest', GLOBAL_PROJECT)?.name).toBe('digest');
  });

  it('adds no global column when every loop belongs to a project', () => {
    const grid = loopGrid([loop('sweep', 'api')]);
    expect(grid.projects).toEqual(['api']);
  });

  it('returns the loop for a name and project, and undefined for a missing cell', () => {
    const sweepWeb = loop('sweep', 'web');
    const grid = loopGrid([sweepWeb, loop('backup', 'api')]);
    expect(grid.cell('sweep', 'web')).toBe(sweepWeb);
    expect(grid.cell('sweep', 'api')).toBeUndefined();
    expect(grid.cell('nope', 'web')).toBeUndefined();
  });

  it('keeps a name and project apart even when their text runs together', () => {
    const grid = loopGrid([loop('a', 'bc'), loop('ab', 'c')]);
    expect(grid.cell('a', 'c')).toBeUndefined();
    expect(grid.cell('ab', 'c')?.project).toBe('c');
  });

  it('returns empty lists for no loops', () => {
    const grid = loopGrid([]);
    expect(grid.names).toEqual([]);
    expect(grid.projects).toEqual([]);
  });
});
