import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

// Guards the rules that keep a long tool argument or an unbroken line inside the column.
const css = readFileSync(new URL('../components/Transcript.css', import.meta.url), 'utf8');
const rule = (selector: string): string => {
  const m = new RegExp(`(?:^|\\n)${selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s*\\{([^}]*)\\}`).exec(css);
  return m?.[1] ?? '';
};

describe('transcript layout rules', () => {
  it('lets the column and owner sheets shrink', () => {
    expect(rule('.tr-col')).toContain('min-width: 0');
    expect(rule('.tr-you')).toContain('max-width: min(72ch, 100%)');
  });
  it('ellipses a long tool argument instead of widening the row', () => {
    const arg = rule('.tr-arg');
    for (const decl of ['flex: 1', 'min-width: 0', 'overflow: hidden', 'white-space: nowrap', 'text-overflow: ellipsis']) {
      expect(arg).toContain(decl);
    }
  });
  it('wraps expanded results and terminal lines', () => {
    expect(rule('.tr-tool pre')).toContain('overflow-wrap: anywhere');
    expect(rule('.tr-tx pre')).toContain('overflow-wrap: anywhere');
  });
});
