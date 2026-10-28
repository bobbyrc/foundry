// Pure helpers for the config editor. The document is plain JSON (what the server sends as
// `document`), and every edit is a Change that is kept in a list until the owner saves. Nothing
// here mutates its inputs: each function returns a new tree or a new list.
import type { ConfigChange, ConfigProblem, SchemaNode } from '../types';

export type Path = string[];

const INDEX = /^(0|[1-9]\d*)$/;
const ENV_REF = /^\$\{[A-Za-z_][A-Za-z0-9_]*\}$/;

/** True for a decimal list index such as "0" or "12", not "01" or "-1". */
export function isIndex(key: string): boolean {
  return INDEX.test(key);
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function sameKeys(a: Path, b: Path): boolean {
  return a.length === b.length && a.every((key, i) => key === b[i]);
}

/** True when `prefix` is `path` or one of its ancestors: the path lies inside (or is) prefix. */
export function isUnder(path: Path, prefix: Path): boolean {
  return prefix.length <= path.length && prefix.every((key, i) => key === path[i]);
}

/** The value at `path`, or undefined when any step is missing. */
export function getAt(tree: unknown, path: Path): unknown {
  let node: unknown = tree;
  for (const key of path) {
    if (Array.isArray(node)) {
      if (!isIndex(key)) return undefined;
      node = node[Number(key)];
    } else if (isObject(node)) {
      if (!Object.prototype.hasOwnProperty.call(node, key)) return undefined;
      node = node[key];
    } else {
      return undefined;
    }
  }
  return node;
}

/**
 * A new tree with `value` at `path`. Missing mappings on the way are created as {}. Setting an
 * index on a list copies the list; a list index beyond its end is not something the editor does.
 */
export function setAt(tree: unknown, path: Path, value: unknown): unknown {
  if (path.length === 0) return value;
  const [head, ...rest] = path;
  if (Array.isArray(tree)) {
    if (!isIndex(head)) throw new Error(`Cannot set "${head}" in a list`);
    const copy = tree.slice();
    const index = Number(head);
    copy[index] = setAt(copy[index], rest, value);
    return copy;
  }
  const base = isObject(tree) ? tree : {};
  return { ...base, [head]: setAt(base[head], rest, value) };
}

/**
 * A new tree without the value at `path`. Deleting a list element removes it and shifts the
 * later ones down. A missing path leaves the tree as it was (as a copy).
 */
export function deleteAt(tree: unknown, path: Path): unknown {
  if (path.length === 0) return undefined;
  const [head, ...rest] = path;
  if (Array.isArray(tree)) {
    const copy = tree.slice();
    if (!isIndex(head) || Number(head) >= copy.length) return copy;
    if (rest.length === 0) {
      copy.splice(Number(head), 1);
      return copy;
    }
    copy[Number(head)] = deleteAt(copy[Number(head)], rest);
    return copy;
  }
  if (!isObject(tree)) return tree;
  if (!Object.prototype.hasOwnProperty.call(tree, head)) return { ...tree };
  if (rest.length === 0) {
    const copy = { ...tree };
    delete copy[head];
    return copy;
  }
  return { ...tree, [head]: deleteAt(tree[head], rest) };
}

/** Replays the changes, in order, on top of the document. */
export function applyChanges(document: unknown, changes: ConfigChange[]): unknown {
  return changes.reduce<unknown>(
    (tree, change) =>
      change.op === 'delete' ? deleteAt(tree, change.path) : setAt(tree, change.path, change.value),
    document,
  );
}

/** Drops the change for `path` and every change under it. */
export function removeUnder(changes: ConfigChange[], path: Path): ConfigChange[] {
  return changes.filter((change) => !isUnder(change.path, path));
}

/**
 * Adds a change to the list. A change for the same path replaces the earlier one; a set or a
 * delete drops the earlier changes under its path, because the new edit already covers them.
 */
export function addChange(changes: ConfigChange[], change: ConfigChange): ConfigChange[] {
  const kept = changes.filter(
    (earlier) =>
      !sameKeys(earlier.path, change.path) &&
      !(isUnder(earlier.path, change.path) && earlier.path.length > change.path.length),
  );
  return [...kept, change];
}

/**
 * The change list after clearing the value at `path`. A delete is recorded only when the value
 * is still there without the earlier edits, so the file never gets a delete for a key it lacks.
 */
export function clearChange(document: unknown, changes: ConfigChange[], path: Path): ConfigChange[] {
  const kept = removeUnder(changes, path);
  if (getAt(applyChanges(document, kept), path) === undefined) return kept;
  return addChange(kept, { op: 'delete', path });
}

/** "a.b.0.c": the dotted form the server uses in problem paths. */
export function dotted(path: Path): string {
  return path.join('.');
}

/** "stall_timeout" -> "Stall timeout". */
export function humanizeKey(key: string): string {
  const words = key.replace(/[_-]+/g, ' ').trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** The singular of a config key: "projects" -> "project", "policies" -> "policy". */
export function singularize(key: string): string {
  if (/ies$/.test(key)) return key.replace(/ies$/, 'y');
  if (/(ss|us|is)$/.test(key)) return key;
  return key.replace(/s$/, '');
}

/** True for a value written as ${NAME}, which onlet replaces with an environment variable. */
export function isEnvRef(value: unknown): boolean {
  return typeof value === 'string' && ENV_REF.test(value);
}

/** The variable name inside ${NAME}, or null when the value is not an environment reference. */
export function envRefName(value: unknown): string | null {
  return isEnvRef(value) ? (value as string).slice(2, -1) : null;
}

/** The problem messages keyed by their dotted path. */
export function problemsByPath(problems: ConfigProblem[]): Map<string, string[]> {
  const byPath = new Map<string, string[]>();
  for (const problem of problems) {
    const list = byPath.get(problem.path);
    if (list) list.push(problem.message);
    else byPath.set(problem.path, [problem.message]);
  }
  return byPath;
}

/** True when a problem path is `path` itself or sits under it, given the dotted keys of the problems. */
export function hasProblemUnder(problemPaths: Iterable<string>, path: Path): boolean {
  const own = dotted(path);
  for (const problemPath of problemPaths) {
    if (problemPath === own || problemPath.startsWith(`${own}.`)) return true;
  }
  return false;
}

/** The value a new entry or list element starts with, by its schema type. */
export function emptyValue(node: SchemaNode): unknown {
  switch (node.type) {
    case 'section':
    case 'map':
      return {};
    case 'list':
      return [];
    case 'bool':
      return false;
    case 'any':
      return null;
    default:
      return '';
  }
}
