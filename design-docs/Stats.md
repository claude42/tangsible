# Stats

## Situation

We are tracking time of each task for each host. But we're not doing too much
with it. Some admins might enjoy a few statistics.

## Ideas

First a tiny one: we're already displaying the execution time of a task on a
particular host in the host line in the tree. In addition, I would like to
add an additional line to the "Task" tab of the drilldown view like that:

Duration: 0.3s

## Dedicated statistics / diagrams

When the run has ended, the s keyboard shortcut will open a separate screen
with statistics. The screen should provide several tabs.

### Tab Hosts

Visualized the total run time for each host. like so

shunya 25.3s *******************************
hydra   3.5s *******
artha  60.0s **********************************************************************

Instead of * characters, maybe we could also use unicode characters of block
with different widths to make the bar representation more accurate (I hope
you know what I mean). 

### Tab Plays

Draw a very similar diagram but instead of comparing host, compare Plays. 

I would like to experiment with the visualization a bit. It should be a
little bit like this. The text within the horizontal bar should be drawn
inverted so it looks like the text is *in* the bar.

If a bar is shorter than the text, the text that extends beyond the bar
should be rendered normally. I hope you understand what I mean.

█Very long play███████████████████████████████████████████████████████████████████ 21.3s

█Not quite as long play████████████████████████████████████                        12.0s




## Decisions and implementation

* Hosts are sorted by duration, slowest first.
* The screen opens only after the run has ended (and only if timing was
  recorded - a run log from before durations existed has none). More tabs
  will follow later (so far Hosts and Plays).
* A host's bar is its total: the sum of its tasks' own durations
  (`uikit.HostDuration`). Hosts run in parallel, so these don't add up to the
  run time. The first bar, `elapsed`, is the run's own time (first task start
  to last finish, gray), drawn on the same scale; there is no description
  text above the bars. Hosts and Plays share one look (`renderBars`,
  `uikit.LabeledBar`): the name inside the bar, inverted on the filled cells,
  the duration in a column on the right (see "Slowest plays tab" below).
* Bars use the eighth blocks (U+258F..U+2588) for their partial last cell, 1/8
  cell resolution, scaled so the longest bar (on Hosts normally elapsed) fills
  the width left of the duration column. A positive value always shows at
  least the thinnest block.
* `s` is a page of its own (`livesession_stats.go`), dispatched like the raw
  output view: Esc / s / q back, Tab / Shift-Tab between tabs, `y` copies the
  tab, `?` help. It re-renders on every `rebuild()`, so a resize rescales the
  bars. It works in `revisit` and in a finished `debug` session; the debug
  keys are off while it is open.
* The Task tab of the drill-down has a `Duration:` line (the host's duration
  for that task), shown only when known.
* A host's bar is red when it had a genuinely failed task (not one ignored
  via `ignore_errors`), maroon when it was unreachable (the tree's own
  colors; failed wins when both apply). Rows are separated by a blank line.

### Slowest plays tab (implemented; first called Plays)

* A play's bar is its wall-clock time: first task start to last finish,
  across all its hosts. Plays run one after another, so unlike the host
  totals these add up to about the run time. Slowest first, the run's
  a blank line between rows. An unnamed play is called `play N`. No `elapsed`
  bar here: the plays are the parts of it (it stays on the Slowest hosts tab).
* The name is drawn inside the bar (`uikit.LabeledBar`): characters on filled
  cells are inverted (the bar's color as background, black or white text by
  luminance - `uikit.ContrastTextColor` - as hex, since a theme can remap the
  palette names), characters beyond the bar are plain. One bar cell precedes
  the text. The bar's partial last cell is an eighth block unless a text
  character needs that cell; a bar under one cell shows its sliver, then the
  name. A name wider than the bar area is cut with `…`.
* The duration is a right-aligned column after the bar (as on Hosts, since the
  Slowest hosts tab was moved to the same visualization).
* Colors as on Hosts: the bar is red when a host genuinely failed in the play
  (not an ignored failure), maroon when one was unreachable.

### Tab Tasks

At the top, provide a dropdown box which can be used to select a play.

Then show a breakdown like this (not sure if that's the best visulaization,
let's try it).

██████████████                                                                            4.3s

              ███                                                                         0.1s

                 ███████████████████████████████████                                      6.0s

                                                    ████████████████████████████████████  7.2s


Write the task names "into" the bars in the same way as in the other two diagrams.

### Play timeline tab (implemented; first called Timeline)

Plays on a timeline, the Task timeline tab's visualization one level up (shared
`renderTimeline`, `uikit.SpanBar`): the plays in playbook order, each bar
starting when the play's first task started (relative to the run's first task
start) and as long as its wall time; the run's elapsed time fills the bar
area. No picker. Colors as on the Slowest plays tab (failed red, unreachable maroon).
The first row is a gray `elapsed` bar over the full width with the run's
elapsed time (the same row as on Slowest hosts): the ruler the plays are read
against, not counted as a play. Both timelines get it from `renderTimeline`.
Plays without timing are left out.

### Task timeline tab (implemented; first called Tasks)

* One play at a time, the first by default, chosen with a drop-down above the
  bars (`tview.DropDown`, styled by `uikit.StyleDropDown`). Plays without any
  timed task are not offered; no "all plays" entry.
* A timeline: a task's bar starts where its first host started (relative to
  the play's first task start) and is as long as its wall time (first host
  start to last host finish); the play's own wall time fills the bar area, shown as a gray
  full-width `elapsed` bar on top (as on Play timeline).
  Tasks stay in execution order, a blank line between rows. The duration
  column is the same wall time. A task without timing is a bare name.
* Colored as in the tree, by the task's worst outcome across hosts: failed
  red, unreachable maroon, an ignored failure in its own color, changed,
  ok, skipped.
* The name is inside the bar like elsewhere (`uikit.SpanBar`); for a late
  short bar it doesn't fit to the right, so it is right-aligned to the end of
  the bar area, ending two cells short of it (never over the duration column): inverted where it overlaps
  the bar, plain where it doesn't.
* Left / Right switch to the previous / next play without opening the picker
  (they stop at the ends); the `P` of the label is underlined as the hint.
* The drop-down is not focused normally (Tab switches tabs): `p` opens it, and
  while open `handleStatsViewKey` forwards every key to it (called directly,
  nested primitives don't reliably get forwarded keys). `HeaderedText` draws
  the drop-down after the text so the open list isn't painted over.
  `SetOptions` also replaces the selected callback - pass it every time.

### Slowest tasks tab (implemented)

* Every task of the whole run in one list, as on the Slowest plays tab (`renderBars`,
  `uikit.LabeledBar`): the task name inside the bar, the duration in the right
  column, longest first (ties keep playbook order), a blank line between rows.
* A task's bar is its wall time as on the Task timeline tab (first host start to last
  host finish), all on one scale (the longest fills the bar area).
* Colored as in the tree by the task's worst outcome across hosts (failed,
  unreachable, ignored, changed, ok, skipped - `taskColor`).
* Tasks without timing are left out (nothing to rank them by). No play
  column: the name alone, so equal names in different plays look alike.

### Host overview

The first tab: the recap's per-host table (the lines below Summary in the
tree - `recapHostRowText`, same column widths and colors), one line per host,
not expandable. Shown whenever the screen is.

### CSV export (`c`)

`c` writes the run's timing to a CSV file in the current directory
(`statsexport.go`): a header and one line per host and executed task, in
execution order - `host`, `play`, `task`, `start time`, `end time`, `result`
(OK, Changed, Skipped, Failed, Unreachable). Times are RFC 3339 with
milliseconds in the local zone, empty if Ansible gave none; a play without a
name is "play N" as on the other tabs. The name is revisit's export name
(`revisit.ExportFileNameExt`: start time and command line, filename-safe) with
`.csv`. No dialog: the result (or the error) shows in the bottom bar, and an
existing file is never overwritten, so a second `c` on the same run reports
"file exists".

### Tab names and order

Host overview, Slowest hosts, Slowest plays, Play timeline, Slowest tasks, Task timeline: a
name says the scope (host/play/task) and the view (a ranking by time, or a
timeline). The `###` headings above keep the names the tabs had while they
were designed (Hosts, Plays, Timeline, Tasks, All tasks).
