import { useEffect, useState, type FormEvent } from 'react';
import { createPortal } from 'react-dom';
import { ApiError, changePassword } from '../api';
import { signOut, useMe } from '../auth';
import { errorMessage } from '../lib/format';
import { Button } from './Button';
import { Field } from './Field';
import { Modal } from './Modal';

const MIN_PASSWORD = 12;
const NOTICE_MS = 3_000;

/**
 * Who is signed in, with sign-out and change-password. It sits in the rail footer, above the
 * pause switch. The dialog is portalled to the body so the rail's light text does not reach it.
 */
export function AccountPanel() {
  const me = useMe();
  const [changing, setChanging] = useState(false);
  const [notice, setNotice] = useState(false);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(false), NOTICE_MS);
    return () => window.clearTimeout(timer);
  }, [notice]);

  if (!me) return null;

  return (
    <div className="account">
      <div className="account-row">
        <span className="account-name">
          {me.username}
          <span className="account-role"> {me.role}</span>
        </span>
        <button type="button" className="link-button" onClick={() => void signOut()}>
          Sign out
        </button>
      </div>
      <button
        type="button"
        className="link-button"
        onClick={() => {
          setNotice(false);
          setChanging(true);
        }}
      >
        Change password
      </button>
      {notice ? (
        <p className="account-notice" role="status">
          Password changed.
        </p>
      ) : null}
      {changing
        ? createPortal(
            <ChangePassword
              onClose={() => setChanging(false)}
              onSaved={() => {
                setChanging(false);
                setNotice(true);
              }}
            />,
            document.body,
          )
        : null}
    </div>
  );
}

/**
 * The words for a refused password change. 403 is a wrong current password, 429 is the lockout
 * after too many tries, and 422 carries the server's own reason (for example, too short). A 401
 * never gets here as a password problem: it means the session ended, and the app signs out.
 */
function passwordChangeMessage(err: unknown): string {
  if (err instanceof ApiError && err.status === 403) return 'The current password is wrong.';
  if (err instanceof ApiError && err.status === 429) return 'Too many attempts. Try again in a few minutes.';
  return errorMessage(err);
}

function ChangePassword({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (next !== confirm) return setError('The passwords do not match.');
    if (next.length < MIN_PASSWORD) return setError(`The password must be at least ${MIN_PASSWORD} characters.`);
    setBusy(true);
    try {
      await changePassword(current, next);
      onSaved();
    } catch (err) {
      setBusy(false);
      setError(passwordChangeMessage(err));
    }
  };

  return (
    <Modal open title="Change password" onClose={onClose}>
      <form className="form-stack" onSubmit={(event) => void submit(event)}>
        <Field label="Current password">
          <input
            type="password"
            autoComplete="current-password"
            autoFocus
            required
            value={current}
            onChange={(event) => setCurrent(event.target.value)}
          />
        </Field>
        <Field label="New password" hint={`At least ${MIN_PASSWORD} characters.`}>
          <input
            type="password"
            autoComplete="new-password"
            required
            value={next}
            onChange={(event) => setNext(event.target.value)}
          />
        </Field>
        <Field label="Confirm">
          <input
            type="password"
            autoComplete="new-password"
            required
            value={confirm}
            onChange={(event) => setConfirm(event.target.value)}
          />
        </Field>
        {error ? (
          <p className="error-line" role="alert">
            {error}
          </p>
        ) : null}
        <div className="row-actions">
          <Button type="submit" variant="primary" disabled={busy}>
            Save
          </Button>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </form>
    </Modal>
  );
}
