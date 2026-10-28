import { describe, expect, it } from 'vitest';
import { elapsedMs, formatDuration, formatRelative, parseTime } from './time';

const NOW = Date.parse('2026-10-27T12:00:00Z');
const ago = (ms: number) => new Date(NOW - ms);

describe('formatRelative', () => {
  it('uses the largest sensible unit', () => {
    expect(formatRelative(ago(1_000), NOW)).toBe('just now');
    expect(formatRelative(ago(12_000), NOW)).toBe('12s ago');
    expect(formatRelative(ago(3 * 60_000), NOW)).toBe('3m ago');
    expect(formatRelative(ago(2 * 3_600_000), NOW)).toBe('2h ago');
    expect(formatRelative(ago(4 * 86_400_000), NOW)).toBe('4d ago');
  });

  it('says "in" for times noticeably in the future', () => {
    expect(formatRelative(new Date(NOW + 90_000), NOW)).toBe('in 1m 30s');
  });
});

describe('formatDuration', () => {
  it('formats seconds, minutes and hours', () => {
    expect(formatDuration(12_000)).toBe('12s');
    expect(formatDuration(184_000)).toBe('3m 04s');
    expect(formatDuration(3_900_000)).toBe('1h 05m');
    expect(formatDuration(-5)).toBe('0s');
  });
});

describe('parseTime', () => {
  it('treats empty values and Go zero time as never', () => {
    expect(parseTime(undefined)).toBeNull();
    expect(parseTime('')).toBeNull();
    expect(parseTime('0001-01-01T00:00:00Z')).toBeNull();
    expect(parseTime('not a date')).toBeNull();
  });

  it('parses RFC 3339 timestamps', () => {
    expect(parseTime('2026-10-27T11:59:00Z')?.getTime()).toBe(NOW - 60_000);
  });
});

describe('elapsedMs', () => {
  it('measures to now while a span is open', () => {
    const start = ago(90_000);
    expect(elapsedMs(start, null, NOW)).toBe(90_000);
    expect(elapsedMs(start, ago(30_000), NOW)).toBe(60_000);
  });
});
