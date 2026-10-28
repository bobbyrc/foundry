import { describe, expect, it } from 'vitest';
import { parseTokenFragment } from './auth';

describe('parseTokenFragment', () => {
  it('reads the token from a fragment', () => {
    expect(parseTokenFragment('#token=abc123')).toBe('abc123');
    expect(parseTokenFragment('#other=1&token=abc123')).toBe('abc123');
  });

  it('decodes percent-encoding', () => {
    expect(parseTokenFragment('#token=a%2Bb%3D')).toBe('a+b=');
  });

  it('returns null when there is no token fragment', () => {
    expect(parseTokenFragment('')).toBeNull();
    expect(parseTokenFragment('#/tasks')).toBeNull();
  });

  it('ignores an empty token', () => {
    expect(parseTokenFragment('#token=')).toBeNull();
  });

  it('ignores a malformed percent-escape instead of throwing', () => {
    expect(() => parseTokenFragment('#token=%E0%A4%A')).not.toThrow();
    expect(parseTokenFragment('#token=%E0%A4%A')).toBeNull();
  });

  it('ignores tokens containing whitespace or control characters', () => {
    expect(parseTokenFragment('#token=%20%20')).toBeNull();
    expect(parseTokenFragment('#token=ab%0Acd')).toBeNull();
    expect(parseTokenFragment('#token=ab%20cd')).toBeNull();
  });
});
