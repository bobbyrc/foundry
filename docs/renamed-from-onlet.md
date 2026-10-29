# Renamed from onlet

On 2026-10-29 the project was renamed from **onlet** to **onett**. Like the earlier rename from
foundry ([renamed-from-foundry.md](renamed-from-foundry.md)), it is a clean break: onett reads only
the new names, so an existing install must be moved over once, by hand, while the daemon is
stopped.

| Old | New |
|---|---|
| `onlet` command | `onett` |
| `onlet.yaml` (default `~/.config/onlet/onlet.yaml`), per-repo `.onlet/` | `onett.yaml` (`~/.config/onett/onett.yaml`), `.onett/` |
| `ONLET_*` environment variables (`ONLET_TOKEN`, `ONLET_API`, `ONLET_CONFIG`, `ONLET_REF`, ...) | `ONETT_*` |
| data directory `~/.local/share/onlet`, database `onlet.db` | `~/.local/share/onett`, `onett.db` |
| agent outbox `<worktree>/.onlet-run/` | `<worktree>/.onett-run/` |
| branches `onlet/<task>-<slug>` | `onett/<task>-<slug>` |
| issue-intake label `onlet` (the default) | `onett` |
| launchd service `io.onlet.serve` | `io.onett.serve` |
| session cookie `onlet_session` | `onett_session` |
| GitHub repository `bobbyrc/onlet`, Go module `github.com/bobbyrc/onlet` | `bobbyrc/onett`, `github.com/bobbyrc/onett` |

## Moving an install

1. Stop the daemon: `onlet service uninstall` if it runs as a service, otherwise stop the process.
2. Move the data: `mv ~/.local/share/onlet ~/.local/share/onett` (or your `server.data_dir`), then
   `mv ~/.local/share/onett/onlet.db ~/.local/share/onett/onett.db`. Move any `-wal` and `-shm`
   files next to it the same way.
3. Move the config: `mv ~/.config/onlet ~/.config/onett` and rename `onlet.yaml` in it to
   `onett.yaml`. In it, change any value that names the old paths (`data_dir`, `${...}`
   references to `ONLET_*` variables in commands).
4. Rename `ONLET_*` to `ONETT_*` wherever you set them: shell profile, `secrets.env`, deploy and
   observer scripts (`$ONLET_REF` becomes `$ONETT_REF`).
5. In every managed repository, rename a `.onlet/` folder to `.onett/`, and in GitHub rename the
   `onlet` issue label to `onett` (or set `loops.issue_intake.label: onlet` to keep it).
6. Install the new binary and start it: `onett service install`, or `onett serve`.
7. Open the web UI and sign in. The session cookie was renamed, so everyone signs in once more.

Pull requests onlet opened on `onlet/...` branches keep working: the branch name is stored with
each task. New work uses `onett/...`.

Project names are yours and are not renamed. If you rename a project key in `onett.yaml`, past
tasks, runs and deployments stay filed under the old key: they are stored with the project's name.
