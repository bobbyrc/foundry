// Sorts a refused config save into what the editor must show. Kept apart from the components so
// the rule can be tested on its own.
import { ApiError, ConfigApiError } from '../../api';
import type { ConfigProblem } from '../../types';

export type SaveFailure =
  /** 409: the file changed on disk since the editor opened it. Nothing was written. */
  | { kind: 'conflict' }
  /** 422: the edited file is invalid. Nothing was written; `problems` says where. */
  | { kind: 'problems'; problems: ConfigProblem[] }
  /**
   * 500: foundry writes the file before it reloads it, so the new text may already be on disk
   * even though the save reports failure. The editor must show the file as it is now.
   */
  | { kind: 'written-not-reloaded'; message: string }
  | { kind: 'error'; error: unknown };

export function classifySaveFailure(err: unknown): SaveFailure {
  if (err instanceof ConfigApiError && err.status === 409) return { kind: 'conflict' };
  if (err instanceof ConfigApiError && err.status === 422) return { kind: 'problems', problems: err.problems };
  if (err instanceof ApiError && err.status === 500) return { kind: 'written-not-reloaded', message: err.message };
  return { kind: 'error', error: err };
}
