import { describe, expect, it } from 'vitest';
import { formatCost, formatGoDuration, formatTokens, safeHref, shortId, shortSha } from './format';

describe('ids and shas', () => {
  it('shows the last 8 characters of an id and the first 7 of a sha', () => {
    expect(shortId('0123456789abcdef')).toBe('89abcdef');
    expect(shortId('01a18092-1111-7aaa-8bbb-ccccdddd0001')).toBe('dddd0001');
    expect(shortSha('0123456789abcdef')).toBe('0123456');
  });
});

describe('formatCost', () => {
  it('marks estimates with ~', () => {
    expect(formatCost(0.42, true)).toBe('~$0.42');
    expect(formatCost(0.001, true)).toBe('~<$0.01');
  });
  it('formats dollars and shows tiny amounts as a bound', () => {
    expect(formatCost(0.42)).toBe('$0.42');
    expect(formatCost(1)).toBe('$1.00');
    expect(formatCost(0.004)).toBe('<$0.01');
    expect(formatCost(0)).toBe('$0.00');
    expect(formatCost(undefined)).toBe('—');
  });
});

describe('formatTokens', () => {
  it('abbreviates large counts', () => {
    expect(formatTokens(950)).toBe('950');
    expect(formatTokens(12_300)).toBe('12.3k');
    expect(formatTokens(15_000)).toBe('15k');
    expect(formatTokens(1_500_000)).toBe('1.5M');
  });
});

describe('safeHref', () => {
  it('keeps http and https only', () => {
    expect(safeHref('https://github.com/x/y/pull/1')).toBe('https://github.com/x/y/pull/1');
    expect(safeHref('http://localhost:3000')).toBe('http://localhost:3000');
    expect(safeHref('javascript:alert(1)')).toBeUndefined();
    expect(safeHref('')).toBeUndefined();
    expect(safeHref(undefined)).toBeUndefined();
  });
});

describe('formatGoDuration', () => {
  it('drops zero parts from Go duration strings', () => {
    expect(formatGoDuration('1m0s')).toBe('1m');
    expect(formatGoDuration('10m0s')).toBe('10m');
    expect(formatGoDuration('24h0m0s')).toBe('24h');
    expect(formatGoDuration('1h30m0s')).toBe('1h30m');
    expect(formatGoDuration('1h0m30s')).toBe('1h30s');
  });
  it('keeps durations that have no zero parts', () => {
    expect(formatGoDuration('30s')).toBe('30s');
    expect(formatGoDuration('1m30s')).toBe('1m30s');
    expect(formatGoDuration('1h30m')).toBe('1h30m');
    expect(formatGoDuration('1m30.5s')).toBe('1m30.5s');
  });
  it('shows zero as 0s', () => {
    expect(formatGoDuration('0s')).toBe('0s');
    expect(formatGoDuration('0h0m0s')).toBe('0s');
  });
  it('returns anything it cannot parse unchanged', () => {
    expect(formatGoDuration('')).toBe('');
    expect(formatGoDuration('5')).toBe('5');
    expect(formatGoDuration('10 minutes')).toBe('10 minutes');
    expect(formatGoDuration('30m0s junk')).toBe('30m0s junk');
    expect(formatGoDuration('500ms')).toBe('500ms');
  });
});
