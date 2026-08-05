# UI design ideas / brainstorming for the TUI interface

## Rough sketch

```
_setup.yml | Hosts: hades, nirvana, mukti | Tags: base_packages | Time: 01:32___
PLAY: First play
PLAY: Some play
  TASK: Some task                               OK: 3, Chgd: 0, Skip: 0, Fail: 0
  TASK: Another task                            OK: 0, Chgd: 2, Skip: 1, Fail: 0
PLAY: Next play
  TASK: Yet another task                        OK: 1, Chgd: 1, Skip: 1, Fail: 0
    hades:   OK
    nirvana: Changed
    mukti:   Skipped
    | Output
    | From the executed command
...
...
...


________________________________________________________________________________
 up/down navigate; enter: show info; q: exit
```

## Notes

* Title bar shows playbook filename, selected hosts and tags (if
  applicable) plus elapsed time
* Bottom bar shall be used to show a few keyboard commands (still TBD)
* Both Title and bottom bar should be rendered in reverse in a different
  color
* Plays where no tasks have been executed (e.g. because of host or tags
  settings) will not be shown at all
  * Implementation-wise: a play's row is only created once its first task
    actually starts, rather than shown eagerly on play-start and hidden/
    removed if nothing ran.
  * Maybe we'll add a toggle later on but for now only show plays with
    actual activity
* Initially only the play + task level is shown.
* Ask playbook progresses the display is updated.
  * It will scroll automatically once the output reaches the last line
  * But when the user presses cursor up to navigate automatic scrolling is
    stopped. Resumed by jumping to the latest entry (e.g. `G`/`End`,
    `less`/`tail -f`-style), rather than a dedicated separate key.
* When the user navigates, the currently selected play or task or host
  shall be displayed in reverse in a different collor
* When the user navigates to a task and hits return the tree opens and
  additional information for the individual hosts is displayed. Enter
  toggles: pressing it again on an already-open task collapses it.
* When the user selects a host an presses enter, the full output of the
  command is displayed
* Status values (OK, Changed, Skipped, Failed) are shown in color, both in
  the per-task rollup counts and the per-host status lines. Exact palette
  still TBD, but conceptually similar to ansible-playbook's own
  green/yellow/cyan/red convention.

## Open questions / decisions

* Progress indicator for a task that's still running (no host results back
  yet) — decided to rely on the elapsed-time display in the title bar for
  now, rather than a dedicated per-task spinner/indicator. May be too
  inconspicuous in practice; revisit once there's something to click
  through.

