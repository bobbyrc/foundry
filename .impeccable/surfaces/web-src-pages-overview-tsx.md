---
version: 1
slug: "web-src-pages-overview-tsx"
primary_target: "web/src/pages/Overview.tsx"
related_targets: ["web/src/components/Shell.tsx","web/src/styles.css"]
---

## Scope

Onett web UI Overview (`/`), the first surface of a replacement visual world that then rolls
out to every page. Mode: Operate. Users: a developer checking in from a phone, or keeping the
control room open on a second monitor. Job: see in one glance what needs them, decide approvals
and blocked tasks with the gate evidence in view, and watch live work. Constraints: same-origin
fonts only (CSP), no inline scripts, colour never the only signal, both themes WCAG AA.

## Direction contract

THESIS: The factory's andon board. A healthy line is quiet green, a project that needs you lights yellow, one that stopped itself lights red. It refuses the category dashboard of stat tiles and same-size cards.

OWN-WORLD: Machine-green enamel ground (#E3EAE4 light, near-black green in dark) under a charcoal board (#1F2422) that carries the nav rail and the andon board. Lamps: run #2FA35A, call #F2C230, stop #D7342A, off #3A423E. Barlow Condensed caps for board and section labels, Barlow for text, mono only for SHAs, IDs and logs. Structure is 1px rules and square corners; no content shadows.

STORY: The visitor sees which projects need them, approves or rejects with risk, reasons and the exact commit in view, unblocks stopped work, and watches runs as bars against their limits.

FIRST VIEWPORT: Charcoal rail left (nav, approvals and blocked counts, the cord: line state plus Pause/Resume, live state). Across the top of the main area, the andon board: one window per project (lamp bar, name, counts) and the line window (running or paused, active, queued, cost today) at its right end. Below: Needs you (7fr) beside Live runs on one shared time axis and Activity (5fr). The primary action, Approve merge, sits in the first Needs-you item.

FORM: Andon board, kept by the user from round one (candidate 6 of my ordered list); seed:72589382e58f62ad. Raises: lamps switch, never fade (two-step, 90ms); structure in 1px rules; windows show lamp, name, count, details on focus; run length is duration; live columns never move.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Unresolved

- Deferred: a compact lamp row on pages other than Overview, and fuller window details on focus.
