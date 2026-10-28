import { describe, expect, it } from 'vitest';
import type { LogLine } from '../types';
import { maxSeq, mergeLogLines } from './logs';

const line = (seq: number, text = `line ${seq}`): LogLine => ({
  run_id: 'r1',
  seq,
  time: '2026-10-27T12:00:00Z',
  kind: 'text',
  text,
});

describe('mergeLogLines', () => {
  it('dedupes by seq and sorts ascending', () => {
    const merged = mergeLogLines([line(1), line(3)], [line(3, 'dup'), line(2), line(4)]);
    expect(merged.map((l) => l.seq)).toEqual([1, 2, 3, 4]);
    expect(merged[2]?.text).toBe('line 3');
  });

  it('keeps the first copy of a duplicate', () => {
    const merged = mergeLogLines([line(1, 'first')], [line(1, 'second')]);
    expect(merged).toHaveLength(1);
    expect(merged[0]?.text).toBe('first');
  });
});

describe('maxSeq', () => {
  it('returns the highest seq or zero', () => {
    expect(maxSeq([])).toBe(0);
    expect(maxSeq([line(2), line(7), line(5)])).toBe(7);
  });
});
