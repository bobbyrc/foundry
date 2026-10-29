// The save bar that sticks to the bottom while there are unsaved edits, and the notes above the
// editor: saved, restart needed, problems in the file, a conflict, or an error.
import type { ConfigProblem } from '../../types';
import { Callout } from '../Callout';
import { ErrorLine } from '../ErrorLine';

export function SaveBar({
  label,
  saving,
  onSave,
  onDiscard,
}: {
  label: string;
  saving: boolean;
  onSave: () => void;
  onDiscard: () => void;
}) {
  return (
    <div className="config-savebar" role="region" aria-label="Unsaved changes">
      <div className="config-savebar-status">
        <span className="config-savebar-lamp" aria-hidden="true" />
        <span>{label}</span>
      </div>
      <div className="config-savebar-actions">
        <button type="button" className="config-savebar-discard" disabled={saving} onClick={onDiscard}>
          Discard
        </button>
        <button type="button" className="config-savebar-save" disabled={saving} onClick={onSave}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
    </div>
  );
}

export interface ConfigNoticeState {
  saved: boolean;
  restart: string[];
  problems: ConfigProblem[];
  conflict: boolean;
  /** The server's message when a save wrote the file but could not reload it, else null. */
  reloadFailed: string | null;
  error: unknown;
}

/**
 * Notes above the editor. The problems list says which lines were refused; the field-level
 * messages are shown under each field by the editor itself.
 */
export function ConfigNotes({
  state,
  onDismissRestart,
  onLoadLatest,
}: {
  state: ConfigNoticeState;
  onDismissRestart: () => void;
  onLoadLatest: () => void;
}) {
  const { saved, restart, problems, conflict, reloadFailed, error } = state;
  return (
    <div className="config-notes">
      {saved ? <p className="muted config-saved">Saved and reloaded.</p> : null}
      {reloadFailed !== null ? (
        <Callout tone="danger">
          Saved to the file, but onett could not reload it: {reloadFailed}. Fix the problem and press Reload
          from file.
        </Callout>
      ) : null}
      {restart.length > 0 ? (
        <Callout tone="info">
          These settings take effect after onett restarts: {restart.join(', ')}.{' '}
          <button type="button" className="link-button" onClick={onDismissRestart}>
            Dismiss
          </button>
        </Callout>
      ) : null}
      {conflict ? (
        <Callout tone="danger" title="Not saved">
          The file changed on disk since you opened it.{' '}
          <button type="button" className="link-button" onClick={onLoadLatest}>
            Load the latest (discards your edits)
          </button>
        </Callout>
      ) : null}
      {problems.length > 0 ? (
        <Callout tone="danger" title="Not saved">
          <ul className="config-problems">
            {problems.map((problem, index) => (
              <li key={index}>
                <span className="mono">{problem.path || 'file'}</span>: {problem.message}
              </li>
            ))}
          </ul>
        </Callout>
      ) : null}
      <ErrorLine error={error} />
    </div>
  );
}
