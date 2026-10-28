# Renamed from foundry

On 2026-10-28 the project was renamed from **foundry** to **onlet**. The rename is a clean break:
onlet reads only the new names, so an existing install must be moved over once, by hand, while
the daemon is stopped.

| Old | New |
|---|---|
| `foundry` command | `onlet` |
| `foundry.yaml`, per-repo `.foundry/` | `onlet.yaml`, `.onlet/` |
| `FOUNDRY_*` environment variables (`FOUNDRY_TOKEN`, `FOUNDRY_API`, `FOUNDRY_CONFIG`, `FOUNDRY_REF`, ...) | `ONLET_*` |
| data directory `~/.local/share/foundry`, database `foundry.db` | `~/.local/share/onlet`, `onlet.db` |
| agent outbox `<worktree>/.foundry-run/` | `<worktree>/.onlet-run/` |
| branches `foundry/<task>-<slug>` | `onlet/<task>-<slug>` |
| issue-intake label `foundry` (the default) | `onlet` |
| launchd service `io.foundry.serve` | `io.onlet.serve` |
| GitHub repository `bobbyrc/foundry`, Go module `github.com/bobbyrc/foundry` | `bobbyrc/onlet`, `github.com/bobbyrc/onlet` |

## Moving an install

1. Stop the daemon: `foundry service uninstall` if it runs as a service, otherwise stop the
   process.
2. Move the data: `mv ~/.local/share/foundry ~/.local/share/onlet` (or your `server.data_dir`),
   then `mv ~/.local/share/onlet/foundry.db ~/.local/share/onlet/onlet.db`. Move any `-wal` and
   `-shm` files next to it the same way.
3. Rename the config file to `onlet.yaml`, and in it any value that names the old paths
   (`data_dir`, `${...}` references to `FOUNDRY_*` variables in commands).
4. Rename `FOUNDRY_*` to `ONLET_*` wherever you set them: shell profile, `secrets.env`, deploy
   and observer scripts (`$FOUNDRY_REF` becomes `$ONLET_REF`).
5. In every managed repository, rename a `.foundry/` folder to `.onlet/`, and in GitHub rename
   the `foundry` issue label to `onlet` (or set `loops.issue_intake.label: foundry` to keep it).
6. Install the new binary and start it: `onlet service install`, or `onlet serve`.
7. Open the web UI and sign in. Session cookies were renamed, so everyone signs in once more.

Pull requests foundry opened on `foundry/...` branches keep working: the branch name is stored
with each task. New work uses `onlet/...`.
