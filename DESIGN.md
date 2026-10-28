---
name: Foundry
description: The factory's andon board: a charcoal board lights what needs you over a machine-green enamel ground.
colors:
  bg: "#e3eae4"
  panel: "#f5f8f5"
  panel-2: "#eaf0eb"
  input: "#ffffff"
  field: "#6f7d76"
  border: "#b9c6bd"
  rule: "#cdd7d0"
  text: "#18201c"
  muted: "#4b5751"
  board: "#1f2422"
  board-2: "#2b312e"
  board-3: "#151917"
  board-text: "#eef3ef"
  board-muted: "#a9b5ae"
  board-rule: "#3d4541"
  lamp-run: "#2fa35a"
  lamp-call: "#f2c230"
  lamp-stop: "#d7342a"
  lamp-done: "#7f9d8c"
  lamp-off: "#3a423e"
  lamp-off-light: "#a3b0a8"
  ok: "#1d6f3d"
  warn: "#7a5800"
  danger: "#b0251c"
  link: "#1f5c3e"
  accent: "#18201c"
  accent-ink: "#ffffff"
  focus: "#18201c"
typography:
  headline:
    fontFamily: "'Barlow Condensed', Barlow, -apple-system, system-ui, sans-serif"
    fontSize: "30px"
    fontWeight: 700
    lineHeight: 1.1
    letterSpacing: "0.01em"
  section-label:
    fontFamily: "'Barlow Condensed', Barlow, -apple-system, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 700
    letterSpacing: "0.1em"
  board-name:
    fontFamily: "'Barlow Condensed', Barlow, -apple-system, system-ui, sans-serif"
    fontSize: "22px"
    fontWeight: 700
    lineHeight: 1.15
    letterSpacing: "0.03em"
  title:
    fontFamily: "Barlow, -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.35
  body:
    fontFamily: "Barlow, -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.5
  nav:
    fontFamily: "Barlow, -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 500
  label:
    fontFamily: "Barlow, -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif"
    fontSize: "13.5px"
    fontWeight: 500
  control:
    fontFamily: "Barlow, -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 600
  mono:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
rounded:
  sheet: "0px"
  control: "2px"
  pill: "2px"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.accent-ink}"
    typography: "{typography.control}"
    rounded: "{rounded.control}"
    padding: "7px 14px"
  button-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.text}"
    typography: "{typography.control}"
    rounded: "{rounded.control}"
    padding: "7px 14px"
  button-danger:
    backgroundColor: "transparent"
    textColor: "{colors.danger}"
    typography: "{typography.control}"
    rounded: "{rounded.control}"
    padding: "7px 14px"
  field-input:
    backgroundColor: "{colors.input}"
    textColor: "{colors.text}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "6px 10px"
  nav-item:
    backgroundColor: "transparent"
    textColor: "{colors.board-muted}"
    typography: "{typography.nav}"
    padding: "7px 20px"
  nav-item-active:
    backgroundColor: "{colors.board-2}"
    textColor: "{colors.board-text}"
    typography: "{typography.nav}"
    padding: "7px 20px"
  status-pill:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.text}"
    typography: "{typography.label}"
    rounded: "{rounded.pill}"
    padding: "1px 8px"
  risk-badge:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.text}"
    typography: "{typography.section-label}"
    rounded: "{rounded.pill}"
    padding: "1px 8px"
  board-window:
    backgroundColor: "{colors.board-3}"
    textColor: "{colors.board-text}"
    typography: "{typography.board-name}"
    padding: "0px 0px 12px"
  call-item:
    backgroundColor: "{colors.panel}"
    textColor: "{colors.text}"
    rounded: "{rounded.sheet}"
    padding: "16px 18px"
---

# Design System: Foundry

## Overview

**Creative North Star: "The Andon Board"**

Foundry's interface is a factory's andon board: the lamp board on a production line that shows,
at a glance, whether the line runs. A healthy line is quiet green. A project that needs its owner
lights yellow. A line that stopped itself lights red. The whole system exists to make that
readable in one look, on a phone or a second monitor.

The surface is a pale machine-green enamel ground, with a charcoal board carrying the navigation
rail, the phone top bar, the Overview's board of project windows, and the connect card. Structure
is drawn with 1px rules and square sheets. There are no content shadows, no gradients, and no
decorative colour: a colour that appears is a state. The density is that of a control room. The
Overview is a board of short windows and two dense columns, not a grid of same-size stat tiles.

The system refuses the category dashboard of stat cards and uniform cards. Counts are set into
lamp windows and one line window, and each card earns its place by what it asks of the user.

**Key Characteristics:**
- A charcoal board over a machine-green enamel ground; charcoal is reserved for the board.
- State is carried by a square lamp and a word together. Colour alone never signals anything.
- Lamps switch in two steps of 90ms. Nothing fades. Running work blinks.
- Structure is 1px rules and square sheets; controls and pills have 2px corners.
- Two faces: Barlow Condensed caps for names and labels, Barlow for text, monospace for machine
  text (logs, IDs, SHAs, config).
- Light by default; dark follows the system's dark preference with the same roles.

## Colors

The palette is a single hue family: a green-grey enamel ground, a green-black ink, and a charcoal
board. Four lamp colours (run green, call yellow, stop red, and a grey done) and an off grey carry
state. Every colour is set in `:root` and changed for dark in one media query.

### Primary
- **Ink Green Black** (#18201c light / #e4ebe6 dark, `accent`): the primary action. The primary
  button is filled with ink and reads "Approve merge" or "Retry". Also the text colour of the
  ink-filled button (`accent-ink` #ffffff light, #111614 dark). Focus rings are ink in light and
  call yellow in dark.

### Secondary
- **Run Green** (#2fa35a, `lamp-run`; #5fd08b text in dark): work is flowing: running, in review,
  merged, deploying. Also the live indicator's square and the board window that runs.
- **Call Yellow** (#f2c230, `lamp-call`): something waits on the owner: approvals, medium risk,
  the nav count for approvals. Also the wordmark square and the text selection colour.
- **Stop Red** (#d7342a, `lamp-stop`): stopped by itself: blocked, failed, denied, high risk, and
  the pause switch while paused.
- **Done Grey** (#7f9d8c light / #6f8e7d dark, `lamp-done`): done and healthy, quieter than run.
- **Idle Grey** (#3a423e light / #2c3430 dark `lamp-off`; #a3b0a8 light, #56635c dark
  `lamp-off-light` on sheets): idle, queued, cancelled, paused. The board's unlit lamp bar.

### Tertiary
- **Status Ink** (`ok` #1d6f3d, `warn` #7a5800, `danger` #b0251c light; `ok` #5fd08b, `warn`
  #f2c230, `danger` #ff7a70 dark): the same status meaning as lamps, darkened or lightened to
  4.5:1 text contrast on the ground. `run`, `info` and `review` alias `ok`; `call` aliases `warn`.
- **Link Green** (#1f5c3e light / #8fd6a8 dark, `link`): links and the one clickable text colour.

### Neutral
- **Machine Enamel** (#e3eae4 light / #111614 dark, `bg`): the page ground.
- **Sheet Mist** (#f5f8f5 / #181e1b, `panel`): sheets, cards, tables, the log and meta grids.
  **Sheet Shade** (#eaf0eb / #202723, `panel-2`): hover rows, code, the selected chat item.
- **Field White** (#ffffff / #0d1210, `input`): form fields.
- **Outer Rule** (#b9c6bd / #3a4741, `border`): sheet outlines, table head rule.
  **Inner Rule** (#cdd7d0 / #2a332f, `rule`): row dividers inside a sheet.
- **Charcoal Board** (#1f2422 light / #0a0d0c dark, `board`), with **Board Deep** (#151917 /
  #050706, `board-3`) for the windows and the cord, **Board Lift** (#2b312e / #1b211e, `board-2`)
  for hover and the active nav item, **Board Rule** (#3d4541 / #2a322e, `board-rule`) for inner
  rules, **Board Text** (#eef3ef) and **Board Muted** (#a9b5ae) for text on the board.
- **Muted Ink** (#4b5751 light / #a1aea7 dark, `muted`): secondary text, labels, timestamps.

### Named Rules
**The Lamp Is State Rule.** A lamp colour means a state and nothing else. The two decorative uses
the build carries (the wordmark square and the text selection colour) are both call yellow; no new
decorative use follows them.

**The Ink Not Lamp Rule.** The primary action is ink, never a lamp colour, so a lamp keeps its
meaning on every screen.

**The Word Beside Lamp Rule.** A lamp, a tone or a risk level is never the only signal. Each
carries a word: "Stop", "Waiting on you", "1 blocked", "quiet for 7m", "high".

## Typography

**Display Font:** Barlow Condensed (with Barlow, system-ui, sans-serif). Used for page titles,
the board's window names, section labels, table heads, risk badges and the wordmark.
**Body Font:** Barlow (with -apple-system, Segoe UI, system-ui, sans-serif).
**Label/Mono Font:** ui-monospace (SF Mono, Menlo) for logs, IDs, SHAs, config and the gutter.

Both Barlow faces are self-hosted from `web/src/assets/fonts/` under the SIL Open Font License,
because the server's Content-Security-Policy allows same-origin fonts only.

**Character:** A plant-signage pairing. The condensed caps read like stencilled station names on a
line, and the regular Barlow reads like a work sheet. Monospace appears only where a machine's
exact text matters.

### Hierarchy
- **Headline** (Barlow Condensed 700, 30px, line-height 1.1, letter-spacing 0.01em; 26px under
  760px): the page title, set as a visually hidden h1 on the Overview and visibly elsewhere.
- **Section label** (Barlow Condensed 700, 15px, letter-spacing 0.1em, uppercase): "Needs you",
  "Live runs", "Activity" and table heads (13px, 0.08em) and meta labels (12.5px, 0.08em).
  The grey note beside a section label is Barlow 500 at 13.5px, sentence case.
- **Board name** (Barlow Condensed 700, 22px, line-height 1.15, letter-spacing 0.03em, uppercase;
  17px under 760px): a project's name on its board window. The line window's state is 24px (17px
  under 760px).
- **Title** (Barlow 600, 16px, line-height 1.35; 17px inside call items and agent cards): item and
  card titles.
- **Body** (Barlow 400, 15px, line-height 1.5): running text. Paragraph spacing is 8px below.
  Long agent text is clamped to three lines with "show more" in approvals.
- **Nav** (Barlow 500, 15px; 16px in the phone drawer): rail items. Active is 600.
- **Label** (Barlow 500, 13.5px): field labels, the rail count, small notes, "last updated" text.
- **Control** (Barlow 600, 14px): button text.
- **Mono** (ui-monospace, 13px, line-height 1.5): logs, short SHAs, IDs (last 8 characters), config
  and the 12px line-number gutter.

### Named Rules
**The Two Faces Rule.** Condensed caps name things: projects, sections, columns, risk, the line.
Regular Barlow says what happened. Monospace is for text a machine wrote, and nothing else.

## Layout

A fixed rail and one main column. The rail is 216px wide and sticky at full height. The main area
is at most 1360px wide with 28px top and 32px side padding (under 760px: 16px top and sides,
40px bottom).

Overview: the board spans the full main width, with a 14px inner padding, 2px gaps between
windows, and 28px below. Windows fill a row with `auto-fill` at a 176px minimum; the line window
takes a fixed 260px at the right. Below the board, two columns at a 7:5 ratio with 32px between
them: Needs you on the left, Live runs and Activity on the right. Other two-column layouts
(Chat, detail pages) use 1.4:1 or a 240px side column with 28px gap. Card grids use `auto-fill`
at 300px minimum with 12px gaps.

Spacing: no spacing scale is defined in the build. The values in use are 2, 4, 6, 8, 10, 12, 14,
16, 18, 20, 22, 24, 28 and 32px. Sections are separated by 28px. Inside a sheet, padding is
16px top and bottom and 18px across. Table cells use 9px by 12px. Dense rows use 2 to 4px.

Responsive: one breakpoint at 760px. Below it the rail becomes a charcoal top bar (wordmark, two
count badges as words, a Menu button) with the nav as a full-height drawer under it. The two
columns, the chat layout and the overview grid stack to one column. The board becomes a compact
strip with two windows across and no detail lines, so the first decision fits on the first
screen. Tables become stacked sheets with a `label: value` line per cell, and the feed drops its
tag column.

## Elevation & Depth

Flat. Depth comes from tone and rule, not from shadow: the ground is Machine Enamel, sheets are
Sheet Mist, the board is Charcoal, and a 1px outer rule draws every sheet's edge. Nothing at
rest or on hover is lifted.

The one shadow in the build belongs to an overlay: the modal sits on a 60% ink scrim and casts a
soft shadow to lift it off the scrim. No content takes a shadow.

### Shadow Vocabulary
- **Overlay Modal** (`box-shadow: 0 16px 40px rgb(10 13 12 / 0.35)`, over a scrim of
  `rgb(10 13 12 / 0.6)`): the modal sheet only. Not used on any content sheet, card or button.

### Named Rules
**The Flat Sheet Rule.** Content sits on 1px rules and tonal layers. A shadow appears only on an
overlay that covers the page.

## Shapes

Square. Sheets, call items, table wrappers, the board, its windows, the lamps and the wordmark
square have a 0px corner. Controls (buttons, fields, the segmented control, chips) and pills
(status, risk, count, tags) have a 2px corner, which reads as square at a glance.

Structure is drawn with 1px rules: the outer rule around a sheet, the inner rule between rows, and
a board rule inside the charcoal. Lamps are small squares (10px in pills, a 12px bar on a board
window, 8px on the phone), never circles. A denied tool call in the run log has a 1px inset Stop
Red edge.

## Components

Each component is a square or a 2px-cornered control, with a word that carries its meaning.

### Buttons
- **Shape:** 2px corner, 7px by 14px padding, Barlow 600 at 14px.
- **Primary:** ink fill (`accent`) with white text (`accent-ink`). Used for one action per
  decision: "Approve merge", "Approve", "Save".
- **Secondary:** transparent, 1px ink outline, ink text; hover fills with Sheet Shade.
- **Danger:** transparent, 1px Danger outline, Danger text; hover tints with 10% Stop Red. Danger
  fill (Stop Red fill, white text) is used for Resume on a paused project.
- **Confirm:** every destructive action asks inline; the button reads "Confirm?" for 3 seconds.
- **Hover / Focus:** colour changes switch in 90ms, in two steps. Focus is a 2px outline in
  `focus` (ink on the ground; call yellow on the board, the phone top bar and in dark mode), offset
  by 2px.
- **Ghost:** transparent, no outline, muted text; turns ink on hover. Used for Cancel on live runs.

### Status pill and risk badge
- **Status pill:** a 10px square lamp followed by the status in words, on a Sheet Mist chip with a
  1px outer rule, 13.5px Barlow 500. The lamp is the only colour. Running pills blink.
- **Risk badge:** Barlow Condensed 700, 13px, caps, letter-spacing 0.08em. Low: outlined, text
  colour. Medium: Call Yellow fill with ink text. High: Stop Red fill with white text. Unrated:
  muted text with no outline.

### Navigation
- **Style:** Barlow 500 at 15px on the charcoal rail, Board Muted text, 7px by 20px padding.
  Active is Board Lift background with Board Text, weight 600. Hover is Board Lift.
- **Counts:** a 2px-cornered count in Barlow Condensed 700 at 13px: Call Yellow with ink text for
  approvals, Stop Red with white text for blocked tasks. Each count carries screen-reader text.
- **Mobile:** the rail is hidden behind a Menu button in a charcoal top bar. The drawer is a solid
  panel from 57px to the bottom, with 16px links and 11px vertical padding. It closes on Escape,
  on a tap outside it, and on a link.
- **Footer (cord):** the pause switch is a Board Deep window with a bordered rule. It shows the line
  state as a lamp and a word ("Running" green, "Paused" red with "No new runs start.") and one
  action on the right (outlined Pause, or filled Resume). A red 1px rule marks a paused line.

### Fields
- **Style:** Field White background, 1px Field Rule stroke, 2px corner, 6px by 10px padding, 15px
  Barlow. Placeholder in Muted Ink.
- **Focus:** the 2px `focus` outline with a 2px offset (the same as every control).
- **Field Rule** (#6f7d76 / #64736b, `field`): the 1px field outline, at least 3:1 against both
  the field and the ground around it, as WCAG asks of a control boundary. Hover turns it to Ink.
- **Labels:** Barlow 500 at 13.5px in Muted Ink, above the field, with a 4px gap.

### Sheets, call items and tables
- **Sheets (`.card`, `.callout`, `.meta`, `.log`):** Sheet Mist, 1px Outer Rule, 0px corner, with
  16px by 18px padding and 12px below.
- **Call item:** a sheet with a 12px lamp before its title: Call Yellow for approvals (waiting on
  you), Stop Red for blocked tasks (a 55% Stop Red outline on the sheet). The line under the title
  is muted, 13.5px. The body is indented 22px under the lamp.
- **Callout:** a 1px sheet with a 9% Stop Red tint and a 55% Stop Red outline. Used for blocked
  reasons. Its informative twin is a Sheet Shade sheet with no tint.
- **Tables:** Sheet Mist table wrapper with a 1px outer rule. Heads are Barlow Condensed caps in
  Muted Ink with a stronger rule under them. Rows separate by Inner Rule and tint with Sheet Shade
  on hover. Under 760px each row becomes a sheet.
- **Empty:** a muted centred line in a 1px dashed Outer Rule, saying what would appear and why it
  is empty.

### Board windows (signature)
- **Shape:** a Board Deep window, 0px corner, with a 12px lamp bar across its top in the lamp's
  colour. Lamp bars are Idle Grey when a project is idle or paused, Run Green when work runs or is
  in review, Call Yellow when approvals wait, and Stop Red when a task is blocked.
- **Text:** the project name in Board Name caps, then the count in words ("1 blocked", "2 need
  you", "3 running") in Barlow 600 at 14px. A waiting count is set in Call Yellow; a blocked count
  in a lighter red (#ff8a80).
  A detail line in Board Muted lists the other non-zero counts.
- **Behaviour:** the whole window links to that project's open tasks. The lamp bar switches in
  two steps. On a phone the detail line hides and the name drops to 17px.
- **Line window:** at the right end of the board, 260px wide, on the charcoal with a 1px Board Rule
  edge, with the line's state
  ("Line running" with a Run Green lamp, or "Line paused" with a Stop Red lamp), then active runs,
  queued tasks and cost today in Board Muted with the numbers in Board Text.

### Live run bars and the feed
- **Run bar:** a 12px track with a 1px rule at each third. The solid part is Run Green from start
  to the last output; the part after it, once the run has been quiet for 5 minutes, is Call Yellow.
  The bar length is elapsed time on one shared axis (10m, 20m, 30m, 1h... whichever fits the
  longest run).
- **Feed:** fixed columns (64px clock, 120px event tag, the rest for the message). Rows separate
  by Inner Rule. The event tag is a 12px Barlow label with no border. Newest first.

## Do's and Don'ts

### Do:
- **Do** pair every lamp, tone and risk level with a word on the same line.
- **Do** keep the primary action ink-filled (`accent` on `accent-ink`), one per decision.
- **Do** switch state colours in two steps at 90ms. Use `steps(2, jump-none)`.
- **Do** blink only running work (`steps(1, end)`, 1.6s), and stop the blink under
  `prefers-reduced-motion`.
- **Do** use 1px rules and 0px sheets for structure; 2px corners on controls and pills only.
- **Do** keep the charcoal board to the rail, the phone top bar, the Overview board and the connect
  card.
- **Do** set table cells, count badges, board counts and feed times in tabular figures.
- **Do** set monospace only for logs, IDs, SHAs, branch names and config.

### Don't:
- **Don't** fade a lamp or animate a colour over time. A state changes in two steps or not at all.
- **Don't** put a content shadow on a sheet, card, call item, or button. The modal overlay is the
  only shadow.
- **Don't** fill a button with a lamp colour, except the Stop Red fill on Resume of a paused
  project.
- **Don't** introduce a decorative lamp colour. The wordmark square and text selection are the only
  exceptions.
- **Don't** use colour alone for a state. Each state needs its word.
- **Don't** set a control or sheet at more than 2px radius (sheets 0px, controls and pills 2px).
- **Don't** set body text in monospace.
- **Don't** use the charcoal board for a content sheet.
- **Don't** copy the 1px field stroke as a control boundary; it is below 3:1 (see Fields).
