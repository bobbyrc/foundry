// Shows why an agent save or removal was refused, in the words the owner needs: the problems
// the server listed, a conflict or reload notice, or the plain error.
import type { ConfigProblem } from '../types';
import { classifySaveFailure } from './config/saveFailure';
import { Callout } from './Callout';
import { ErrorLine } from './ErrorLine';

/** Nothing is shown for no error. */
export function AgentSaveFailure({ error }: { error: unknown }) {
  if (!error) return null;
  const failure = classifySaveFailure(error);
  switch (failure.kind) {
    case 'conflict':
      return (
        <Callout tone="danger" title="Not saved">
          The file changed on disk since this page loaded. The page has reloaded it; try again.
        </Callout>
      );
    case 'problems':
      if (failure.problems.length === 0) return <ErrorLine error={error} />;
      return (
        <Callout tone="danger" title="Not saved">
          <ul className="config-problems">
            {failure.problems.map((problem: ConfigProblem, index) => (
              <li key={index}>
                <span className="mono">{problem.path || 'file'}</span>: {problem.message}
              </li>
            ))}
          </ul>
        </Callout>
      );
    case 'written-not-reloaded':
      return (
        <Callout tone="danger" title="Saved, not reloaded">
          Saved to the file, but onett could not reload it: {failure.message}. Fix the problem and
          reload the page.
        </Callout>
      );
    case 'error':
      return <ErrorLine error={failure.error} />;
  }
}
