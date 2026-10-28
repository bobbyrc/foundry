import { useState, type FormEvent } from 'react';
import { loadAuthState, useMe } from '../auth';
import { Button } from '../components/Button';
import { ErrorLine } from '../components/ErrorLine';
import { Field } from '../components/Field';
import { PageHeader } from '../components/PageHeader';
import { RelTime } from '../components/RelTime';
import { Table, type Column } from '../components/Table';
import { useCreateUser, useDeleteUser, useSetUserPassword, useSetUserRole, useUsers } from '../queries';
import type { Role, User } from '../types';

const ROLES: Role[] = ['viewer', 'operator', 'owner'];

/** Owner only. Lists the accounts and adds, re-roles, re-passwords and removes them. */
export function Users() {
  const me = useMe();
  const users = useUsers();
  const create = useCreateUser();
  const setRole = useSetUserRole();
  const setPassword = useSetUserPassword();
  const remove = useDeleteUser();

  const [adding, setAdding] = useState(false);
  const [newName, setNewName] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [newRole, setNewRole] = useState<Role>('operator');
  const [passwordFor, setPasswordFor] = useState<string | null>(null);
  const [resetPassword, setResetPassword] = useState('');
  // Errors from the row actions (a role or removal the server refused) show under the table.
  const [failure, setFailure] = useState<unknown>(null);

  const closeAdd = () => {
    create.reset();
    setAdding(false);
    setNewName('');
    setNewPassword('');
    setNewRole('operator');
  };

  const submitNew = (event: FormEvent) => {
    event.preventDefault();
    create.mutate(
      { username: newName.trim(), password: newPassword, role: newRole },
      { onSuccess: closeAdd },
    );
  };

  const changeRole = (user: User, role: Role) => {
    if (role === user.role) return;
    setFailure(null);
    setRole.mutate(
      { name: user.username, role },
      {
        onSuccess: () => {
          // Changing your own role changes what the UI may show, so ask the server again.
          if (user.username === me?.username) void loadAuthState();
        },
        onError: setFailure,
      },
    );
  };

  const savePassword = (user: User) => {
    setFailure(null);
    setPassword.mutate(
      { name: user.username, password: resetPassword },
      {
        onSuccess: () => {
          setPasswordFor(null);
          setResetPassword('');
        },
        onError: setFailure,
      },
    );
  };

  const removeUser = (user: User) => {
    setFailure(null);
    remove.mutate(user.username, { onError: setFailure });
  };

  const columns: Column<User>[] = [
    {
      key: 'username',
      header: 'Username',
      cell: (u) => (
        <>
          {u.username}
          {u.username === me?.username ? <span className="muted small"> (you)</span> : null}
        </>
      ),
    },
    {
      key: 'role',
      header: 'Role',
      cell: (u) => {
        const saving = setRole.isPending && setRole.variables?.name === u.username;
        return (
          <select
            aria-label={`Role for ${u.username}`}
            value={u.role}
            disabled={saving}
            onChange={(event) => changeRole(u, event.target.value as Role)}
          >
            {ROLES.map((role) => (
              <option key={role} value={role}>
                {role}
              </option>
            ))}
          </select>
        );
      },
    },
    { key: 'added', header: 'Added', cell: (u) => <RelTime value={u.created_at} /> },
    {
      key: 'actions',
      header: 'Actions',
      cell: (u) => {
        const editing = passwordFor === u.username;
        return (
          <>
            <div className="users-actions">
              <button
                type="button"
                className="link-button small"
                aria-expanded={editing}
                onClick={() => {
                  setPasswordFor(editing ? null : u.username);
                  setResetPassword('');
                }}
              >
                Set password
              </button>
              <Button
                variant="danger"
                confirm
                disabled={remove.isPending && remove.variables === u.username}
                onClick={() => removeUser(u)}
              >
                Remove
              </Button>
            </div>
            {editing ? (
              <form
                className="inline-form"
                onSubmit={(event) => {
                  event.preventDefault();
                  savePassword(u);
                }}
              >
                <Field label={`New password for ${u.username}`}>
                  <input
                    type="password"
                    autoComplete="new-password"
                    required
                    autoFocus
                    value={resetPassword}
                    onChange={(event) => setResetPassword(event.target.value)}
                  />
                </Field>
                <div className="row-actions">
                  <Button type="submit" variant="primary" disabled={setPassword.isPending}>
                    Save
                  </Button>
                  <Button variant="ghost" onClick={() => setPasswordFor(null)}>
                    Cancel
                  </Button>
                </div>
              </form>
            ) : null}
          </>
        );
      },
    },
  ];

  return (
    <>
      <PageHeader title="Users" actions={<Button variant="primary" onClick={() => setAdding(true)}>Add user</Button>} />

      {adding ? (
        <form className="card users-add" onSubmit={submitNew}>
          <div className="users-form-row">
            <Field label="Username">
              <input
                type="text"
                name="username"
                autoComplete="off"
                autoFocus
                required
                value={newName}
                onChange={(event) => setNewName(event.target.value)}
              />
            </Field>
            <Field label="Password">
              <input
                type="password"
                name="password"
                autoComplete="new-password"
                required
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
              />
            </Field>
            <Field label="Role">
              <select value={newRole} onChange={(event) => setNewRole(event.target.value as Role)}>
                {ROLES.map((role) => (
                  <option key={role} value={role}>
                    {role}
                  </option>
                ))}
              </select>
            </Field>
          </div>
          <ErrorLine error={create.error} />
          <div className="row-actions">
            <Button type="submit" variant="primary" disabled={create.isPending}>
              Create user
            </Button>
            <Button variant="ghost" onClick={closeAdd}>
              Cancel
            </Button>
          </div>
        </form>
      ) : null}

      <ErrorLine error={users.error} />
      {users.isSuccess ? <Table columns={columns} rows={users.data} rowKey={(u) => u.username} empty="No users yet." /> : null}
      <ErrorLine error={failure} />
    </>
  );
}
