---
name: Onlet
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

# Design System: Onlet

## Overview

**Creative North Star: "The Andon Board"**

Onlet's interface is a factory's andon board: the lamp board on a production line that shows,
at a glance, whether the line runs. A healthy line is quiet green. A project that needs its owner
lights yellow. A line that stopped itself lights red. The whole system exists to make that
readable in one look, on a phone or a second monitor.

The surface is a pale machine-green enamel ground, with a charcoal board carrying the navigation
rail (its project lamps, the account panel and the pause cord), the phone top bar, the Overview's
board of project windows, the connect card and the config editor's save bar. Structure is drawn
with 1px rules and square sheets. There are no content shadows, no gradients, and no decorative
colour: a colour that appears is a state. The density is that of a control room. The Overview is a
board of short windows and two dense columns, not a grid of same-size stat tiles.

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
  merged, deploying. Also the live indicator's square, the board window that runs, the running
  station of a task line, the chat's working square and the streaming caret.
- **Call Yellow** (#f2c230, `lamp-call`): something waits on the owner: approvals, medium risk,
  the nav count for approvals. Also the wordmark square, the text selection colour, the unsaved
  square in the config list and save bar, and the quiet part of a live run's bar.
- **Stop Red** (#d7342a, `lamp-stop`): stopped by itself: blocked, failed, denied, high risk, and
  the pause switch while paused. Also the stopped station of a task line, the DENIED tag, and the
  problem square in the config list.
- **Done Grey** (#7f9d8c light / #6f8e7d dark, `lamp-done`): done and healthy, quieter than run.
  Also the passed stations of a task line and a succeeded run's bar.
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
- **Field White** (#ffffff / #0d1210, `input`): form fields and the chat composer sheet.
- **Outer Rule** (#b9c6bd / #3a4741, `border`): sheet outlines, table head rule.
  **Inner Rule** (#cdd7d0 / #2a332f, `rule`): row dividers inside a sheet.
- **Charcoal Board** (#1f2422 light / #0a0d0c dark, `board`), with **Board Deep** (#151917 /
  #050706, `board-3`) for the windows, the account panel and the pause cord, **Board Lift**
  (#2b312e / #1b211e, `board-2`) for hover and the active nav item, **Board Rule** (#3d4541 /
  #2a322e, `board-rule`) for inner rules, **Board Text** (#eef3ef) and **Board Muted** (#a9b5ae)
  for text on the board. The stop word on the board is #ff8a80, written inline wherever it is used
  (board window counts, rail labels); it is not a token.
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
the board's window names, section labels, table heads, risk badges, the task line's stations,
the wordmark and the stat numerals on project sheets.
**Body Font:** Barlow (with -apple-system, Segoe UI, system-ui, sans-serif).
**Label/Mono Font:** ui-monospace (SF Mono, Menlo) for logs, IDs, SHAs, config and the gutter.

Both Barlow faces are self-hosted from `web/src/assets/fonts/` under the SIL Open Font License,
because the server's Content-Security-Policy allows same-origin fonts only.

**Character:** A plant-signage pairing. The condensed caps read like stencilled station names on a
line, and the regular Barlow reads like a work sheet. Monospace appears only where a machine's
exact text matters.

### Hierarchy
- **Headline** (Barlow Condensed 700, 30px, line-height 1.1, letter-spacing 0.01em; 26px under
  760px): the page title, set as a visually hidden h1 on the Overview and visibly elsewhere. A
  page with a heading of its own (the chat start, the sign-in sheet) uses the same face at the
  same size, in sentence case.
- **Section label** (Barlow Condensed 700, 15px, letter-spacing 0.1em, uppercase): "Needs you",
  "Live runs", "Activity", the config's section titles, and table heads (13px, 0.08em). The grey
  note beside a section label is Barlow 500 at 13.5px, sentence case.
- **Board name** (Barlow Condensed 700, 22px, line-height 1.15, letter-spacing 0.03em, uppercase;
  17px under 760px): a project's name on its board window, and on its project sheet. The line
  window's state is 24px (17px under 760px).
- **Stat numeral** (Barlow Condensed 700, 26px, line-height 1; 22px under 760px): the five counts
  of a project sheet. Blocked is set in the danger text colour, needs-you in the call status ink.
- **Station and meta label** (Barlow Condensed 700, 12px for task-line stations, 12.5px for spec
  sheet and run fact labels, letter-spacing 0.08em, uppercase): the words that name a station or a
  field, set in Muted Ink (in text colour for the current and stopped station).
- **Title** (Barlow 600, 16px, line-height 1.35; 17px inside call items and agent cards): item and
  card titles.
- **Body** (Barlow 400, 15px, line-height 1.5): running text. Paragraph spacing is 8px below.
  Long agent text is clamped to three lines with "show more" in approvals.
- **Nav** (Barlow 500, 15px; 16px in the phone drawer): rail items. Active is 600.
- **Label** (Barlow 500, 13.5px): field labels, the rail count, small notes, "last updated" text,
  the project sheet's facts line (13px) and the rail's project words (12.5px).
- **Control** (Barlow 600, 14px): button text.
- **Mono** (ui-monospace, 13px, line-height 1.5): logs, short SHAs, IDs (last 8 characters), config
  and the 12px clock gutter of the log (64px wide; 52px on a phone, at 11px).

### Named Rules
**The Two Faces Rule.** Condensed caps name things: projects, sections, columns, risk, the line,
its stations, and the labels of the spec sheet and run facts. Regular Barlow says what happened.
Monospace is for text a machine wrote, and nothing else.

## Layout

A fixed rail and one main column. The rail is 216px wide and sticky at full height. The main area
is at most 1360px wide with 28px top and 32px side padding (under 760px: 16px top and sides,
40px bottom).

Overview: the board spans the full main width, with a 14px inner padding, 2px gaps between
windows, and 28px below. Windows fill a row with `auto-fill` at a 176px minimum; the line window
takes a fixed 260px at the right. Below the board, two columns at a 7:5 ratio with 32px between
them: Needs you on the left, Live runs and Activity on the right.

Two-column layouts elsewhere, each with its own side column: Chat is a 260px conversation list
beside the thread (28px gap); the config editor is a 220px section list, sticky, beside the
section (28px gap); Task detail is the main column beside a 300px spec sheet that stays in view
(32px gap). Card grids use `auto-fill` at 300px minimum with 12px gaps; project sheets use 320px.
Run detail is one column: its log takes the space left under the header.

Spacing: no spacing scale is defined in the build. The values in use are 2, 4, 6, 8, 10, 12, 14,
16, 18, 20, 22, 24, 28 and 32px, with a few 3px and 5px alignments and 26px rail lamp rows and 36px
filter controls. Sections are separated by 28px. Inside a sheet, padding is 16px top and bottom
and 18px across (project sheets 14px and 18px). Table cells use 9px by 12px. Dense rows use 2 to
4px. Config fields are separated by 12px of padding and a rule.

Responsive: two breakpoints. Below 760px the rail becomes a charcoal top bar (wordmark, two count
badges as words, a Menu button) with the nav as a full-height drawer under it. The two columns,
the chat layout and the overview grid stack to one column. The board becomes a compact strip with
two windows across and no detail lines, so the first decision fits on the first screen. Tables
become stacked sheets with a `label: value` line per cell, and the feed drops its tag column.
Project sheets stack, the loop grid becomes one sheet per loop name, and the config section list
becomes a row of pill chips that scrolls sideways. Below 960px the task detail is one column: the
spec sheet follows "Needs you", then the rest of the page. From 960px it is the two-column body
described under Components.

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
a board rule inside the charcoal. The one 2px rule is the task line's station rule, which joins
the lamps. Lamps are small squares, never circles: 10px in pills, the rail rows, the loop grid, the
chat's working line and a project sheet's bar (10px tall); 12px for task-line stations, call items,
the line window and a board window's bar; 8px in the config section list and on the phone board.

A denied tool call in the run log is a tinted line with a DENIED tag before its text, and no edge.

## Components

Each component is a square or a 2px-cornered control, with a word that carries its meaning.

### Buttons
- **Shape:** 2px corner, 7px by 14px padding, Barlow 600 at 14px.
- **Primary:** ink fill (`accent`) with white text (`accent-ink`). Used for one action per
  decision: "Approve merge", "Approve", "Save", "Send".
- **Secondary:** transparent, 1px ink outline, ink text; hover fills with Sheet Shade.
- **Danger:** transparent, 1px Danger outline, Danger text; hover tints with 10% Stop Red. Danger
  fill (Stop Red fill, white text) is used for Resume on a paused project's sheet on the Projects
  page.
- **Confirm:** every destructive action asks inline; the button reads "Confirm?" for 3 seconds.
- **Hover / Focus:** colour changes switch in 90ms, in two steps. Focus is a 2px outline in
  `focus` (ink on the ground; call yellow on the board, the phone top bar and in dark mode), offset
  by 2px.
- **Ghost:** transparent, no outline, muted text; turns ink on hover. Used for the Cancel beside a
  form's Save, and the Run button in a loop cell. Cancelling a task or a live run is a danger
  button that asks inline.
- **On the charcoal board:** the filled action is Board Text (#eef3ef) with charcoal text, the
  inverse of ink, because ink would vanish on the board. The rail's Resume and the config Save use
  it; outlined actions there use a Board Muted outline with Board Text.
- **Link button:** text in Link Green, underlined, no box. Used for "Set password", "Remove" in
  lists, "show more", "More" and "Sign out". On the board it is Board Text.

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
- **Project lamps (rail):** between the nav and the footer, one 26px row per project: a 10px lamp
  square, the project name in Board Muted at 13.5px, and on the right the word "1 needs you" or
  "1 blocked" in Call Yellow or #ff8a80 at 12.5px. Rows are ranked stop, call, run, then off, and
  capped at eight, with an "All projects" row when more exist. Each row links to that project's
  tasks. Hidden on the Overview (the board has the full picture) and under 760px. Every row shows
  its state in words beside the lamp ("2 running", "idle", "paused" in Board Muted), so colour is
  never the only signal.
- **Mobile:** the rail is hidden behind a Menu button in a charcoal top bar. The drawer is a solid
  panel from 57px to the bottom, with 16px links and 11px vertical padding. It closes on Escape,
  on a tap outside it, and on a link.
- **Footer (cord):** from the top, the account panel, the pause switch and the live indicator.
  The pause switch is a Board Deep window with a bordered rule. It shows the line state as a lamp
  and a word ("Running" green, "Paused" red with "No new runs start.") and one action on the right
  (outlined Pause, or Board Text filled Resume). A red 1px rule marks a paused line.
- **Account panel:** a Board Deep window with a 1px Board Rule edge. The first line is the signed-in
  name in semi-bold with the role in Board Muted 13px after it, and a "Sign out" link on the right.
  Below it, a "Change password" link. The password dialog is drawn over the page, not inside the
  rail. Its success line, "Password changed.", shows in Board Muted for three seconds.

### Fields
- **Style:** Field White background, 1px Field Rule stroke, 2px corner, 6px by 10px padding, 15px
  Barlow. Placeholder in Muted Ink.
- **Focus:** the 2px `focus` outline with a 2px offset (the same as every control).
- **Field Rule** (#6f7d76 / #64736b, `field`): the 1px field outline, at least 3:1 against both
  the field and the ground around it, as WCAG asks of a control boundary. Hover turns it to Ink.
- **Labels:** Barlow 500 at 13.5px in Muted Ink, above the field, with a 4px gap. Config fields
  use a 14px semi-bold label instead, with the dotted setting path in 12px mono beside it.
- **Checkbox:** a native checkbox tinted Run Green, labelled "On" in the config.

### Chips
- **Style:** Sheet Mist, 1px Outer Rule, 2px corner, 4px by 12px padding, 13.5px, muted text
  (text colour on hover). Used for the chat's example prompts.
- **Open (`chip-on`):** ink text and ink outline on Sheet Shade, weight 600. Used for the active
  filter and the open state of a choice.

### Sheets, call items and tables
- **Sheets (`.card`, `.callout`, `.meta`, `.log`, and the spec, project and config entry sheets):**
  Sheet Mist, 1px Outer Rule, 0px corner, with 16px by 18px padding and 12px below.
- **Call item:** a sheet with a 12px lamp before its title: Call Yellow for approvals (waiting on
  you), Stop Red for blocked tasks (a 55% Stop Red outline on the sheet). The line under the title
  is muted, 13.5px. The body is indented 22px under the lamp.
- **Callout:** a 1px sheet with a 9% Stop Red tint and a 55% Stop Red outline (tone `danger`), used
  for blocked reasons, errors and failed saves. Tone `info` is a Sheet Shade sheet with no tint,
  for a note that says what happens later (a restart is needed). Tone `muted` is a plain outlined
  note. A callout with a title opens with it in bold.
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
  in a lighter red (#ff8a80). A detail line in Board Muted lists the other non-zero counts.
- **Behaviour:** the whole window links to that project's open tasks. The lamp bar switches in
  two steps. On a phone the detail line hides and the name drops to 17px.
- **Line window:** at the right end of the board, 260px wide, on the charcoal with a 1px Board Rule
  edge, with the line's state ("Line running" with a Run Green lamp, or "Line paused" with a Stop
  Red lamp), then active runs, queued tasks and cost today in Board Muted with the numbers in Board
  Text.

### Project sheets (the board window on the ground)
- **Shape:** a Sheet Mist sheet with a 1px outer rule and a 10px lamp bar across its top, in the
  colour the Overview shows for the project (grey when idle or paused). Set on the light ground,
  so it is the board window's sibling, not a charcoal window.
- **Head:** the project name in Board Name caps, the repo as a muted link beneath it, and the
  lamp's words in 14px semi-bold, in the status ink for their state (run, warn, danger, or muted
  for off). The pause control sits at the right: outlined "Pause", stop-red-filled
  "Paused — Resume", or a muted "Paused in onlet.yaml" when the file owns the state.
- **Counts:** five cells in a row between two inner rules, divided by 1px rules: queued, running,
  review, needs you and blocked. Each cell is the stat numeral over a small label. A non-zero cell
  links to the tasks filtered to that project and status; a zero cell is muted and is not a link.
  The label is 12px Barlow Condensed 700 caps (10px on a phone, never broken inside a word).
- **Facts and loops:** two muted 13px lines: the default branch, the deployer and the observers;
  then "Loops: 5 on, 2 off", with the loops that failed named in Stop Red text and their error as
  a tooltip.

### Live run bars and the feed
- **Run bar:** a 12px track with a 1px rule at each quarter, matching the four tick marks of the
  axis. The solid part is Run Green from start to the last output; the part after it, once the run
  has been quiet for 5 minutes, is Call Yellow. The bar length is elapsed time on one shared axis
  that steps through 20m, 40m, 1h, 2h, 4h and 8h, whichever fits the longest run.
- **Feed:** fixed columns (64px clock, 120px event tag, the rest for the message). Rows separate
  by Inner Rule. The event tag is a 12px Barlow label with no border. Newest first.

### Task line (signature)
- **What it is:** the task's path through seven stations (Queued, Running, Review, Approval,
  Merged, Deploying, Done) as 12px lamp squares joined by a 2px Outer Rule through their centres.
  Each station has its label in Station caps beneath. The line sits under the task's meta line,
  between 20px and 28px of space.
- **States:** a station before the task's position shows the done lamp (Done Grey). The current
  station lights in its tone: run green for running, review, merged and deploying; call yellow for
  approval; off for queued. A running task's current lamp blinks. A stopped station is Stop Red,
  and its label becomes the stop word: "Blocked", "Failed" or "Cancelled" (a cancelled task's lamp
  is Idle Grey light, not red). A station the task never needed is a hollow lamp with a Done Grey
  outline (approval when nothing was approved; review when a task finished with no PR). Stations
  not yet reached are dim at 40% opacity, without motion.
- **Text:** the current and stopped stations' labels are in text colour; the rest are muted. Each
  station also carries screen-reader text giving its name and state ("Review: not reached").
- **Narrow:** the line keeps a 392px minimum and scrolls sideways rather than wrapping.
- **Not a kicker:** the muted line above it (kind, project, role, source, then the ID chip) is a
  meta line under the title. It is plain muted text, not a label.

### Task spec sheet
- **Shape:** a `dl` sheet, 300px wide, Sheet Mist, 1px outer rule, 16px by 18px padding, sticky at
  16px from the top of the page. From 960px it is the right column of the task body; below 960px it
  is static and follows "Needs you".
- **Items:** each item is a label above a value, the label in the station-label style (12.5px caps,
  Muted Ink), the value 2px below with 12px of space after it. Risk shows its badge and reasons;
  the pull request is a link with the branch in mono beneath it; the source ref is mono under the
  source. The last item has no bottom space.
- **Decided approvals:** below the task's runs, a list with a 1px Inner Rule above each row. Each
  row is a status pill, the title in semi-bold, the relative time, and the note on its own line in
  muted 13.5px.

### Run time strip and log
- **Time strip:** a section above the log. The axis ticks sit above a 12px track, with a 1px Outer
  Rule under the ticks and tabular figures. To the right of the ticks, the elapsed time, then
  "output 4s ago" or, once quiet for five minutes, "quiet for 7m" in the warning text colour. A
  finished run is one solid bar with no quiet part: Done Grey when it succeeded, Stop Red when it
  stopped itself (failed, stalled, timed out, denied), Idle Grey light when cancelled.
- **Facts:** a wrapping row of label-over-value pairs (branch, cost, tokens, started, duration, exit
  code once ended). "More" is a link-button that opens workdir, session and run ID.
- **Log panel:** the "Log" heading, the line count in tabular figures and a "Follow" checkbox that
  keeps the view on the newest line. The log is a Sheet Mist box with a 1px outer rule. It takes
  the rest of the desktop screen (at least 360px) and 60 to 80% of the height on a phone.
- **Lines:** 13px mono, with a 64px clock gutter in 12px mono (52px and 11px on a phone). Each line
  has a 1px vertical and 14px horizontal padding and tints Sheet Shade on hover.
  - `text`: plain text in body colour.
  - `tool_use`: a drawn chevron before the text. The chevron is an SVG polyline 10px square in run
    green, not a text glyph; wrapped lines hang under the text.
  - `tool_result`: muted; longer than three lines, it shows three and a "show more" link.
  - `stderr`: muted italic. `system`: muted. `error`: Stop Red text.
  - `denial`: a line tinted with 12% Stop Red, the DENIED tag before the text, and no coloured edge.
    The tag is a Stop Red fill with white text, 11px Barlow Condensed 700 caps, 5px side padding,
    16px high. It is a status tag, not a label above content.

### Chat
- **Start:** with no conversation open, a block 640px wide, 8vh from the top (4vh on a phone). It
  has a 30px condensed heading in sentence case ("Talk to the orchestrator"), a muted line on what
  it can and cannot do, the composer, and the example prompts as chips in a row beneath.
- **Conversation list:** a 260px column at the left of the thread, with a rule above the list and
  between rows. Each row is a 10px-padded title, one line and ellipsed, with the relative time
  beneath in muted small type. The open conversation sits on a Sheet Mist panel in semi-bold.
  Under 760px the list becomes a row of chips above the open thread; with no conversation open, the
  full list shows first.
- **Thread:** the title in the condensed face, 22px caps. User messages are right-aligned Sheet Mist
  bubbles with a 1px outer rule, 72ch maximum. Assistant messages are unboxed, rendered Markdown,
  72ch maximum.
- **Working line:** while the orchestrator replies, a 10px Run Green square that blinks, then
  "Orchestrator is working" in muted text and a link to its run ("view run"). The streaming reply
  sits in a provisional message with a blinking green caret (8 by 15px, one second, steps).
- **Composer:** a sheet in Field White with a 1px Outer Rule edge and no radius. The field inside
  has no border of its own, auto-grows to 40vh, and sits over a foot row: the hint "Enter to send ·
  Shift+Enter for a new line" in muted small type at the left and the primary Send at the right.
  The sheet's edge turns to text colour while the field has focus. The field is disabled while the
  orchestrator replies.
- **Viewers:** the thread reads on; in place of the composer, a muted line says viewers can read
  but not send.

### Loop lamp grid
- **Shape:** a Sheet Mist table with a 1px outer rule. The heads are condensed caps (15px, 0.1em)
  in Muted Ink. A row is one loop name in mono 13px. The first column, "all projects", holds loops
  that run across every project and comes first.
- **Cell:** a 10px lamp, then "every 30s" (the interval as configured), then a Ghost "Run" button
  for operators only. The second line says "ran 3m ago" or "never" in muted text, or the last
  error in Stop Red text cut to one line, with the full text as a tooltip. The lamp is Run Green
  when the last run worked, Stop Red when it failed, and Idle Grey light when the loop is off or has
  not run yet. A disabled loop reads "off · every 30s", so the words carry the state as well as the
  lamp.
- **Summary:** one muted line above: "3 loops on · 2 off · 1 failing", with the failing count in
  Stop Red text when any fails.
- **Phone:** one sheet per loop name, its name in mono with a rule under it, and a row per project
  with the lamp, the project name, the interval and the status line.

### Account, sign-in and users
- **Sign-in sheet:** the connect screen is the charcoal board filling the viewport. The sheet is a
  Sheet Mist card, 380px wide, 24px padding, at the centre. It carries the wordmark (a 12px Call
  Yellow square and "onlet" in condensed 24px), the title ("Sign in" or "Create the owner account"),
  a muted lead line when there is one, labelled fields with hints, an error line in Stop Red text,
  and a full-width primary button. While the sign-in state loads, a Board Muted line stands in.
- **Users table:** owners only. A page header with the primary "Add user". Adding opens a sheet
  above the table with username, password and role in one row that wraps on a phone, and "Create
  user" (primary) with "Cancel" (ghost). The table lists username ("(you)" in muted small type after
  your own), a role select, the relative time added, and actions: "Set password" (a link-button that
  opens a password field beneath the row, with Save and Cancel) and "Remove" (a confirmed danger
  button). Cells centre vertically. Refusals from the server show under the table.

### Config editor and save bar
- **Section list:** on the left, 220px wide and sticky. Each section is a row with a 1px Inner Rule
  beneath it; the active one sits on Sheet Mist in semi-bold. An 8px square after a name marks
  unsaved edits (Call Yellow) or problems (Stop Red), with screen-reader text. Under 760px the list
  is a row of pill chips that scrolls sideways.
- **Body:** each section title is a condensed 15px caps heading with a 28px space above (none on
  the first). Help text is 13.5px muted, 72ch at most. Each setting is a block with a 12px
  padding and a 1px Inner Rule beneath; its label is 14px semi-bold, its dotted path 12px mono
  muted beside it. A number, text or duration setting is an input at most 420px wide; a checkbox is
  "On". A `${NAME}` value shows as text with a note that onlet reads that environment variable.
- **Nested:** a section inside a section, or a map or list entry, is indented 16px with a 1px Inner
  Rule down its left edge (12px under 760px). A map is one sheet per entry, with its name in 16px
  semi-bold and "Rename" and "Remove" link-buttons; Remove asks inline ("Remove name? Yes No"). A
  map of plain values is a table with a Remove link per row. A map ends with an add form: a name
  input and a secondary "Add" button. A list is a row per item with a Remove link and ends with a
  secondary "Add" button. A value with no simple form is a monospace textarea (13px, at most
  640px wide) that parses when left.
- **Notes:** above the body. "Saved and reloaded." in muted text for four seconds; a restart note
  in an `info` callout with a Dismiss link; a conflict or a failed reload in a danger callout; the
  refused problems in a danger callout as a list of mono paths.
- **Save bar:** when there are unsaved edits, a strip sticks to the bottom of the page on the
  charcoal board, 12px by 16px padded. It holds a 10px Call Yellow square and "3 unsaved changes",
  then Discard (outlined in Board Muted, Board Text) and Save (Board Text fill, charcoal text). It
  reads "Saving…" while the save runs.
- **YAML tab:** the file as plain monospace text in a Field White box with a 1px Field Rule edge,
  at least 60vh tall, never wrapped, scrolling sideways when long.

## Do's and Don'ts

### Do:
- **Do** pair every lamp, tone and risk level with a word on the same line.
- **Do** keep the primary action ink-filled (`accent` on `accent-ink`), one per decision. On the
  charcoal board, the filled action is Board Text on charcoal.
- **Do** switch state colours in two steps at 90ms. Use `steps(2, jump-none)`.
- **Do** blink only running work (`steps(1, end)`, 1.6s), and stop the blink under
  `prefers-reduced-motion`.
- **Do** use 1px rules and 0px sheets for structure; 2px corners on controls and pills only.
- **Do** keep the charcoal board to the rail (its nav, project lamps, account and pause cord), the
  phone top bar, the Overview board, the connect card and the config save bar.
- **Do** set table cells, count badges, board counts, stat numerals, feed times, the run time strip
  and log line counts in tabular figures.
- **Do** set monospace only for logs, IDs, SHAs, branch names and config.
- **Do** keep each task-line station labelled and give a stopped station its stop word
  ("Blocked", "Failed", "Cancelled") in place of its own label.

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
- **Don't** outline a control with the Outer Rule (#b9c6bd); control boundaries use the Field Rule
  (see Fields).
