import { describe, expect, it } from 'vitest';
import { formatCost, formatTokens, safeHref, shortId, shortSha } from './format';

describe('ids and shas', () => {
  it('shortens ids to 8 characters and shas to 7', () => {
    expect(shortId('0123456789abcdef')).toBe('01234567');
    expect(shortSha('0123456789abcdef')).toBe('0123456');
  });
});

describe('formatCost', () => {
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
