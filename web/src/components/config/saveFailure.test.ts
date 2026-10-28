import { describe, expect, it } from 'vitest';
import { ApiError, ConfigApiError } from '../../api';
import { classifySaveFailure } from './saveFailure';

describe('classifySaveFailure', () => {
  it('treats a 500 as written but not reloaded, keeping the server message', () => {
    const err = new ConfigApiError(500, 'listen tcp: address in use', []);
    expect(classifySaveFailure(err)).toEqual({ kind: 'written-not-reloaded', message: 'listen tcp: address in use' });
  });

  it('treats a plain 500 the same way', () => {
    const err = new ApiError(500, 'reload failed');
    expect(classifySaveFailure(err)).toEqual({ kind: 'written-not-reloaded', message: 'reload failed' });
  });

  it('maps 409 to a conflict', () => {
    expect(classifySaveFailure(new ConfigApiError(409, 'changed', []))).toEqual({ kind: 'conflict' });
  });

  it('keeps the problems list for 422', () => {
    const problems = [{ path: 'server.addr', message: 'bad address' }];
    expect(classifySaveFailure(new ConfigApiError(422, 'invalid', problems))).toEqual({
      kind: 'problems',
      problems,
    });
  });

  it('leaves other failures as plain errors', () => {
    const err = new ConfigApiError(403, 'owner only', []);
    expect(classifySaveFailure(err)).toEqual({ kind: 'error', error: err });
  });
});
