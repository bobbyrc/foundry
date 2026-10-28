# Lessons

- Before acting on a probe's output (e.g. "no active runs → safe to restart"), confirm the probe
  itself succeeded. An erroring command prints nothing, and nothing looks like "zero". (2026-10-28:
  an old binary rejected a new config key, the run-count probe failed silently, and the daemon was
  restarted on an unverified "idle" — harmless only because it really was idle.)
- When changing a config file that a running older binary also reads, the old binary's strict
  parsing rejects the new keys: swap the binary first, then add the keys.
- Don't keep fast-forwarding a factory's base branch while its open PRs touch the same files: each
  move re-creates the merge conflict and burns the task's attempts (issue #21's task blocked after 3).
  Batch base updates, or pause the factory, then sync once.
- Before moving the base under PRs that wait for the owner, test-merge each one
  (`git merge-tree --write-tree HEAD <head>`). Most conflicts tonight were two branches appending
  tests to the end of the same test file: put new tests in a new file and the pending PR stays
  mergeable.
- Friction issues often describe problems already fixed on the main branch. Check the code before
  queueing one, and close it with the fixing commit instead.
- Issue intake reads only the issue body. Put a spec for the factory in the body, not in a comment
  (comments from maintainers are included only since #34).
