import { describe, expect, it } from 'vitest';
import { can } from './roles';

describe('can', () => {
  it('lets a viewer read but not act or administer', () => {
    expect(can('viewer', 'operate')).toBe(false);
    expect(can('viewer', 'administer')).toBe(false);
  });

  it('lets an operator act but not administer', () => {
    expect(can('operator', 'operate')).toBe(true);
    expect(can('operator', 'administer')).toBe(false);
  });

  it('lets an owner do both', () => {
    expect(can('owner', 'operate')).toBe(true);
    expect(can('owner', 'administer')).toBe(true);
  });

  it('denies everything when there is no signed-in user', () => {
    expect(can(undefined, 'operate')).toBe(false);
    expect(can(undefined, 'administer')).toBe(false);
  });
});
