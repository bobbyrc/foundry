import { useState } from 'react';
import { saveToken } from '../auth';
import { Button } from '../components/Button';
import { Field } from '../components/Field';

export function Connect({ rejected }: { rejected: boolean }) {
  const [value, setValue] = useState('');

  return (
    <div className="connect">
      <form
        className="card connect-card"
        onSubmit={(event) => {
          event.preventDefault();
          const token = value.trim();
          if (token) saveToken(token);
        }}
      >
        <h1>Connect to foundry</h1>
        <Field label="Token">
          <input type="password" autoComplete="off" autoFocus required value={value} onChange={(event) => setValue(event.target.value)} />
        </Field>
        <p className="muted small">
          Run <code>foundry token</code> or read <code>{'<data_dir>/token'}</code>.
        </p>
        {rejected ? (
          <p className="error-line" role="alert">
            That token was rejected. Check it and try again.
          </p>
        ) : null}
        <Button type="submit" variant="primary">
          Connect
        </Button>
      </form>
    </div>
  );
}
