import { useEffect, useState, type FormEvent } from 'react';
import { ApiError } from '../api';
import { setUp, signIn } from '../auth';
import { BrandMark } from '../components/BrandMark';
import { Button } from '../components/Button';
import { Field } from '../components/Field';
import { errorMessage } from '../lib/format';

const MIN_PASSWORD = 12;

/**
 * The sign-in sheet. With `setup` true it creates the first owner account instead: onlet has
 * no users yet. `expired` shows that a signed-in session ended and asks for sign-in again.
 */
export function SignIn({ setup, expired }: { setup: boolean; expired: boolean }) {
  const [mode, setMode] = useState<'setup' | 'signin'>(setup ? 'setup' : 'signin');
  // Follow the server's answer when it changes (for example after a reload of the sign-in state).
  useEffect(() => setMode(setup ? 'setup' : 'signin'), [setup]);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const creating = mode === 'setup';

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (creating) {
      if (password !== confirm) return setError('The passwords do not match.');
      if (password.length < MIN_PASSWORD) return setError(`The password must be at least ${MIN_PASSWORD} characters.`);
    }
    setBusy(true);
    try {
      if (creating) await setUp(username, password);
      else await signIn(username, password);
      // On success the App switches to the signed-in screen and this sheet unmounts.
    } catch (err) {
      setBusy(false);
      if (err instanceof ApiError && creating && err.status === 409) {
        // Someone else finished setup first: the owner exists, so this is a sign-in now.
        setMode('signin');
        setNotice('An owner account already exists. Sign in.');
        setPassword('');
        setConfirm('');
      } else if (err instanceof ApiError && !creating && err.status === 401) {
        setError('Wrong username or password.');
      } else if (err instanceof ApiError && !creating && err.status === 429) {
        setError('Too many attempts. Try again in a few minutes.');
      } else {
        setError(errorMessage(err));
      }
    }
  };

  return (
    <div className="connect">
      <form className="card connect-card auth-card" onSubmit={(event) => void submit(event)}>
        <span className="auth-wordmark">
          <BrandMark />
          onlet
        </span>
        <h1>{creating ? 'Create the owner account' : 'Sign in'}</h1>

        {creating ? (
          <p className="muted">
            This account can change settings and add other people. Create it before you make onlet reachable from
            other machines.
          </p>
        ) : null}
        {!creating && expired ? <p className="muted">Your session ended. Sign in again.</p> : null}
        {notice ? <p className="muted">{notice}</p> : null}

        <Field label="Username" hint={creating ? 'Lowercase letters, digits, dots, dashes and underscores.' : undefined}>
          <input
            type="text"
            name="username"
            autoComplete="username"
            autoFocus
            required
            value={username}
            onChange={(event) => setUsername(event.target.value)}
          />
        </Field>
        <Field label="Password" hint={creating ? `At least ${MIN_PASSWORD} characters.` : undefined}>
          <input
            type="password"
            name="password"
            autoComplete={creating ? 'new-password' : 'current-password'}
            required
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </Field>
        {creating ? (
          <Field label="Confirm password">
            <input
              type="password"
              name="confirm"
              autoComplete="new-password"
              required
              value={confirm}
              onChange={(event) => setConfirm(event.target.value)}
            />
          </Field>
        ) : null}

        {error ? (
          <p className="error-line" role="alert">
            {error}
          </p>
        ) : null}
        <Button type="submit" variant="primary" disabled={busy}>
          {creating ? 'Create account' : 'Sign in'}
        </Button>
      </form>
    </div>
  );
}
