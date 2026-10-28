// The Loops page's lamp grid: one row per loop name, one column per project. The layout rules
// live here, apart from the component, so they can be tested.
import type { LoopView } from '../types';

/** The project a global loop has. It gets its own column, which the page labels "all projects". */
export const GLOBAL_PROJECT = '';

export interface LoopGrid {
  /** Loop names, sorted A to Z. */
  names: string[];
  /** Project names sorted A to Z. GLOBAL_PROJECT comes first when any global loop exists. */
  projects: string[];
  /** The loop with this name in this project, or undefined when that project has no such loop. */
  cell(name: string, project: string): LoopView | undefined;
}

const compare = (a: string, b: string) => a.localeCompare(b);

// JSON keeps a name and a project from running together, whatever characters they contain.
const cellKey = (name: string, project: string) => JSON.stringify([name, project]);

export function loopGrid(loops: LoopView[]): LoopGrid {
  const names = new Set<string>();
  const projectNames = new Set<string>();
  const byCell = new Map<string, LoopView>();
  let hasGlobal = false;

  for (const loop of loops) {
    names.add(loop.name);
    if (loop.project === GLOBAL_PROJECT) {
      hasGlobal = true;
    } else {
      projectNames.add(loop.project);
    }
    const key = cellKey(loop.name, loop.project);
    if (!byCell.has(key)) byCell.set(key, loop);
  }

  const projects = [...projectNames].sort(compare);
  return {
    names: [...names].sort(compare),
    projects: hasGlobal ? [GLOBAL_PROJECT, ...projects] : projects,
    cell: (name, project) => byCell.get(cellKey(name, project)),
  };
}
