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
  will follow later.
* A host's bar is its total: the sum of its tasks' own durations
  (`uikit.HostDuration`). Hosts run in parallel, so these don't add up to the
  run time. The first bar, `elapsed`, is the run's own time (first task start
  to last finish, gray), drawn on the same scale; there is no description
  text above the bars.
* Bars use the eighth blocks (U+258F..U+2588, `uikit.BarEighths`) for 1/8
  cell resolution and are scaled so the longest bar (normally elapsed) fills the width left
  after the label and duration columns. A positive value always shows at
  least the thinnest block.
* `s` is a page of its own (`livesession_stats.go`), dispatched like the raw
  output view: Esc / s / q back, Tab / Shift-Tab between tabs, `y` copies the
  tab, `?` help. It re-renders on every `rebuild()`, so a resize rescales the
  bars. It works in `revisit` and in a finished `debug` session; the debug
  keys are off while it is open.
* The Task tab of the drill-down has a `Duration:` line (the host's duration
  for that task), shown only when known.
* A host's line is red when it had a genuinely failed task (not one ignored
  via `ignore_errors`), maroon when it was unreachable (the tree's own
  colors; failed wins when both apply). Hosts are separated by a blank line.

