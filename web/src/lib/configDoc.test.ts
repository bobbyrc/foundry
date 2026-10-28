import { describe, expect, it } from 'vitest';
import type { ConfigChange, SchemaNode } from '../types';
import {
  addChange,
  applyChanges,
  clearChange,
  deleteAt,
  dotted,
  emptyValue,
  envRefName,
  getAt,
  hasProblemUnder,
  humanizeKey,
  isEnvRef,
  isIndex,
  isUnder,
  problemsByPath,
  removeUnder,
  setAt,
  singularize,
} from './configDoc';

const doc = {
  server: { addr: ':7777', tls: { on: true } },
  projects: [{ name: 'a' }, { name: 'b' }],
  routing: { 'bug-fix': 'x' },
};

describe('getAt', () => {
  it('reads nested mapping keys and list indexes', () => {
    expect(getAt(doc, ['server', 'addr'])).toBe(':7777');
    expect(getAt(doc, ['projects', '1', 'name'])).toBe('b');
  });

  it('returns the whole tree for an empty path', () => {
    expect(getAt(doc, [])).toBe(doc);
  });

  it('returns undefined for a missing key, a bad index or a scalar parent', () => {
    expect(getAt(doc, ['server', 'missing'])).toBeUndefined();
    expect(getAt(doc, ['projects', '5'])).toBeUndefined();
    expect(getAt(doc, ['projects', 'first'])).toBeUndefined();
    expect(getAt(doc, ['server', 'addr', 'x'])).toBeUndefined();
  });

  it('does not treat inherited properties as keys', () => {
    expect(getAt(doc, ['toString'])).toBeUndefined();
  });
});

describe('setAt', () => {
  it('replaces a value without mutating the tree', () => {
    const next = setAt(doc, ['server', 'addr'], ':8080') as typeof doc;
    expect(next.server.addr).toBe(':8080');
    expect(doc.server.addr).toBe(':7777');
    expect(next).not.toBe(doc);
    expect(next.server).not.toBe(doc.server);
    expect(next.server.tls).toBe(doc.server.tls);
  });

  it('creates missing mappings on the way', () => {
    const next = setAt({}, ['a', 'b', 'c'], 1);
    expect(next).toEqual({ a: { b: { c: 1 } } });
  });

  it('sets a list element, including appending at the length', () => {
    const next = setAt(doc, ['projects', '2'], { name: 'c' }) as typeof doc;
    expect(next.projects).toEqual([{ name: 'a' }, { name: 'b' }, { name: 'c' }]);
    expect(doc.projects).toHaveLength(2);
  });

  it('replaces the whole tree for an empty path', () => {
    expect(setAt(doc, [], 5)).toBe(5);
  });

  it('refuses a non-index key on a list', () => {
    expect(() => setAt(doc, ['projects', 'name'], 1)).toThrow(/list/);
  });
});

describe('deleteAt', () => {
  it('removes a mapping key without mutating the tree', () => {
    const next = deleteAt(doc, ['server', 'addr']) as typeof doc;
    expect(next.server).toEqual({ tls: { on: true } });
    expect(doc.server).toHaveProperty('addr');
  });

  it('removes a list element and shifts the later ones down', () => {
    const next = deleteAt(doc, ['projects', '0']) as typeof doc;
    expect(next.projects).toEqual([{ name: 'b' }]);
    expect(doc.projects).toHaveLength(2);
  });

  it('leaves a missing path alone, as a copy', () => {
    const next = deleteAt(doc, ['server', 'nothing']);
    expect(next).toEqual(doc);
    expect(next).not.toBe(doc);
  });

  it('returns undefined for an empty path', () => {
    expect(deleteAt(doc, [])).toBeUndefined();
  });
});

describe('applyChanges', () => {
  it('replays changes in order on top of the document', () => {
    const changes: ConfigChange[] = [
      { op: 'set', path: ['server', 'addr'], value: ':9000' },
      { op: 'delete', path: ['routing', 'bug-fix'] },
    ];
    const next = applyChanges(doc, changes) as typeof doc;
    expect(next.server.addr).toBe(':9000');
    expect(next.routing).toEqual({});
    expect(doc.server.addr).toBe(':7777');
  });

  it('returns the document itself when there are no changes', () => {
    expect(applyChanges(doc, [])).toBe(doc);
  });
});

describe('addChange', () => {
  it('replaces an earlier change for the same path', () => {
    const first: ConfigChange = { op: 'set', path: ['a'], value: 1 };
    const second: ConfigChange = { op: 'set', path: ['a'], value: 2 };
    expect(addChange([first], second)).toEqual([second]);
  });

  it('lets a delete replace an earlier set at the same path', () => {
    const set: ConfigChange = { op: 'set', path: ['a'], value: 1 };
    const del: ConfigChange = { op: 'delete', path: ['a'] };
    expect(addChange([set], del)).toEqual([del]);
  });

  it('drops earlier changes under a deleted path', () => {
    const under: ConfigChange = { op: 'set', path: ['a', 'b'], value: 1 };
    const other: ConfigChange = { op: 'set', path: ['c'], value: 2 };
    const del: ConfigChange = { op: 'delete', path: ['a'] };
    expect(addChange([under, other], del)).toEqual([other, del]);
  });

  it('drops earlier changes under a set path', () => {
    const under: ConfigChange = { op: 'set', path: ['a', 'b', 'c'], value: 1 };
    const set: ConfigChange = { op: 'set', path: ['a', 'b'], value: { c: 2 } };
    expect(addChange([under], set)).toEqual([set]);
  });

  it('keeps an earlier change above the path, and the order of the rest', () => {
    const above: ConfigChange = { op: 'set', path: ['a'], value: {} };
    const sibling: ConfigChange = { op: 'set', path: ['a', 'x'], value: 1 };
    const own: ConfigChange = { op: 'set', path: ['a', 'y'], value: 2 };
    expect(addChange([above, sibling], own)).toEqual([above, sibling, own]);
  });
});

describe('clearChange', () => {
  it('records a delete for a key that is in the file', () => {
    const changes = clearChange(doc, [], ['server', 'addr']);
    expect(changes).toEqual([{ op: 'delete', path: ['server', 'addr'] }]);
  });

  it('records nothing for a key that is only in an earlier set', () => {
    const set: ConfigChange = { op: 'set', path: ['server', 'new'], value: 'x' };
    expect(clearChange(doc, [set], ['server', 'new'])).toEqual([]);
  });

  it('records a delete when an earlier set of an ancestor supplies the value', () => {
    const ancestor: ConfigChange = { op: 'set', path: ['extra'], value: { k: 'v' } };
    expect(clearChange({}, [ancestor], ['extra', 'k'])).toEqual([
      ancestor,
      { op: 'delete', path: ['extra', 'k'] },
    ]);
  });
});

describe('removeUnder', () => {
  it('drops the path and everything under it, but no siblings', () => {
    const changes: ConfigChange[] = [
      { op: 'set', path: ['a'], value: 1 },
      { op: 'set', path: ['a', 'b'], value: 2 },
      { op: 'set', path: ['ab'], value: 3 },
    ];
    expect(removeUnder(changes, ['a'])).toEqual([{ op: 'set', path: ['ab'], value: 3 }]);
  });
});

describe('isUnder', () => {
  it('is true for the path itself and its descendants, not for a sibling with a longer name', () => {
    expect(isUnder(['a', 'b'], ['a'])).toBe(true);
    expect(isUnder(['a'], ['a'])).toBe(true);
    expect(isUnder(['ab'], ['a'])).toBe(false);
    expect(isUnder(['a'], ['a', 'b'])).toBe(false);
  });
});

describe('dotted', () => {
  it('joins the path with dots', () => {
    expect(dotted(['a', 'b', '0', 'c'])).toBe('a.b.0.c');
    expect(dotted([])).toBe('');
  });
});

describe('humanizeKey', () => {
  it('makes the first letter a capital and the underscores spaces', () => {
    expect(humanizeKey('stall_timeout')).toBe('Stall timeout');
    expect(humanizeKey('max_runs_per_project')).toBe('Max runs per project');
    expect(humanizeKey('server')).toBe('Server');
    expect(humanizeKey('')).toBe('');
  });
});

describe('singularize', () => {
  it('drops the plural ending of a config key', () => {
    expect(singularize('projects')).toBe('project');
    expect(singularize('policies')).toBe('policy');
    expect(singularize('loops')).toBe('loop');
    expect(singularize('address')).toBe('address');
  });
});

describe('isEnvRef', () => {
  it('accepts ${NAME} with a letter or underscore first', () => {
    expect(isEnvRef('${FOUNDRY_TOKEN}')).toBe(true);
    expect(isEnvRef('${_x1}')).toBe(true);
  });

  it('rejects anything else', () => {
    expect(isEnvRef('$FOUNDRY_TOKEN')).toBe(false);
    expect(isEnvRef('${1X}')).toBe(false);
    expect(isEnvRef('x${NAME}')).toBe(false);
    expect(isEnvRef('${NAME}x')).toBe(false);
    expect(isEnvRef(7)).toBe(false);
    expect(isEnvRef(undefined)).toBe(false);
  });

  it('gives the variable name', () => {
    expect(envRefName('${PORT}')).toBe('PORT');
    expect(envRefName('15m')).toBeNull();
  });
});

describe('problemsByPath', () => {
  it('groups messages by path, keeping their order', () => {
    const map = problemsByPath([
      { path: 'server.addr', message: 'bad' },
      { path: '', message: 'whole file' },
      { path: 'server.addr', message: 'also bad' },
    ]);
    expect(map.get('server.addr')).toEqual(['bad', 'also bad']);
    expect(map.get('')).toEqual(['whole file']);
    expect(map.size).toBe(2);
  });

  it('is empty for no problems', () => {
    expect(problemsByPath([]).size).toBe(0);
  });
});

describe('hasProblemUnder', () => {
  it('finds a problem on the path or below it, not a sibling that shares a prefix', () => {
    expect(hasProblemUnder(['server.addr'], ['server'])).toBe(true);
    expect(hasProblemUnder(['server'], ['server'])).toBe(true);
    expect(hasProblemUnder(['serverx.addr'], ['server'])).toBe(false);
    expect(hasProblemUnder([''], ['server'])).toBe(false);
  });
});

describe('isIndex', () => {
  it('accepts decimal indexes only', () => {
    expect(isIndex('0')).toBe(true);
    expect(isIndex('12')).toBe(true);
    expect(isIndex('01')).toBe(false);
    expect(isIndex('-1')).toBe(false);
    expect(isIndex('name')).toBe(false);
  });
});

describe('emptyValue', () => {
  const node = (type: SchemaNode['type']): SchemaNode => ({ key: 'k', type });

  it('gives the starting value for each schema type', () => {
    expect(emptyValue(node('section'))).toEqual({});
    expect(emptyValue(node('map'))).toEqual({});
    expect(emptyValue(node('list'))).toEqual([]);
    expect(emptyValue(node('bool'))).toBe(false);
    expect(emptyValue(node('any'))).toBeNull();
    expect(emptyValue(node('string'))).toBe('');
    expect(emptyValue(node('int'))).toBe('');
  });
});
