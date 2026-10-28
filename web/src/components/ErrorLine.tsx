import { errorMessage } from '../lib/format';

export function ErrorLine({ error }: { error: unknown }) {
  if (!error) return null;
  return (
    <p className="error-line" role="alert">
      {errorMessage(error)}
    </p>
  );
}
