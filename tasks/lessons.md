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
- To neutralise syntax in text another parser reads (Markdown mentions), break the token itself
  (a zero-width space after the @) rather than wrapping it in more markup. Added markup
  interacts with the markup around it: backtick wrapping merged with neighbouring backticks and
  left mentions bare. And when the guard's own parsing can be wrong, make its errors cosmetic,
  not unsafe.

- Exercise the real flow after changing the runner: a throwaway local repo plus a second daemon
  on a spare port takes minutes, and it found the stranded-branch bug (a retry after a failed PR
  open marked the task done) that every unit test passed. Live drills (kill staging during
  verification, inject a log error) found more than the reviews did for the deploy path.
- Check browser-facing behaviour in the owner's browser (Firefox here), not only in Chromium.
  Browsers differ in what they wait for: Firefox fires EventSource "open" only once body bytes
  arrive, Chrome on the headers. (2026-10-28: a reconnect fix measured fast in Chromium, and
  the owner still waited 15s for "live", one full ping interval, in Firefox.)
- When the owner asks to make an existing surface "first class" or "work like" a product they
  know, start from that product's behaviour and the agent CLIs' own capabilities, and propose
  the smallest design built on them. Do not invent new modes or protocols (a separate "steer"
  versus "message" path) that make onett a harness. (2026-10-29: the owner stopped an agent
  conversations design with "this feels like we're reinventing the wheel ... I'm worried we're
  building a harness"; the agreed design used `claude --input-format stream-json` and the CLIs'
  own resume instead.)
- After a change to how runs end or resume, run the real flow with the real agent CLI before
  calling it done. Unit tests with the fake harness passed while every real follow-up run
  failed: the resumed agent never wrote `result.json`, so each follow-up burned an attempt and
  re-ran the task in a fresh session. Only the live check showed it.
