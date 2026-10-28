import { describe, expect, it } from 'vitest';
import { tokenizeJson } from './highlight';

describe('tokenizeJson', () => {
  it('classifies keys, strings, numbers and literals', () => {
    const tokens = tokenizeJson('{\n  "port": 7777,\n  "name": "onlet",\n  "on": true\n}');
    const kinds = tokens.filter((t) => t.kind !== 'text').map((t) => [t.kind, t.text]);
    expect(kinds).toEqual([
      ['key', '"port"'],
      ['number', '7777'],
      ['key', '"name"'],
      ['string', '"onlet"'],
      ['key', '"on"'],
      ['literal', 'true'],
    ]);
  });

  it('round-trips the source text exactly', () => {
    const src = '{"a":[1,-2.5e3,null],"b":"x\\"y"}';
    expect(tokenizeJson(src).map((t) => t.text).join('')).toBe(src);
  });
});
