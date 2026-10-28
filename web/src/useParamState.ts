import { useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';

/** A filter kept in the URL (?project=foundry) so filtered views can be linked and reloaded. */
export function useParamState(key: string): [string, (value: string) => void] {
  const [params, setParams] = useSearchParams();
  const value = params.get(key) ?? '';
  const set = useCallback(
    (next: string) =>
      setParams(
        (prev) => {
          const copy = new URLSearchParams(prev);
          if (next) copy.set(key, next);
          else copy.delete(key);
          return copy;
        },
        { replace: true },
      ),
    [key, setParams],
  );
  return [value, set];
}
