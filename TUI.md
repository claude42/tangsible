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
* Rendering of the TASK should be as follows  
  * The title of the task should be on the left (of course appropriately
    indented, current implementation is fine)
  * The "OK: 01, Chgd: 01, Skip: 01, Fail: 00" part should be rendered on the far
    right of the line. Each number should be printed with a fixed width of
    two digits.
  * If the length of the title plus the length of the "OK: 1, Chgd: 1, Skip: 1, Fail: 0"
    part exceeds the total line length, the title shall be abbreviated
    accordingly.
* Autoscrolling
  * By default, the view will autoscroll once it has reached the bottom of
    the window and new elements arrive.
  * The user can escape from autoscrolling by navigating with the cursor keys
  * We introduce a couple of new shortcuts
    * Home key -> jump to the first line
    * End key as well as 'G' -> jump to the last line
      * This will also re-enable autoscrolling
  * Finding from user testing:
    * When scrolling beyond the upper or lower end of the list the cursor wraps
      around to the other end - not what I would have expected. Therefore ->
      remove this functionality
      * Instead when the user is already at the first line and presses cursor
        up -> nothing happens
      * When user is already at the last line and presses cursor down ->
        nothing happens

## Open questions / decisions

* Progress indicator for a task that's still running (no host results back
  yet) — decided to rely on the elapsed-time display in the title bar for
  now, rather than a dedicated per-task spinner/indicator. May be too
  inconspicuous in practice; revisit once there's something to click
  through.

## New ideas for the task lines

I'm not convinced about my original idea on how to display the task lines -
especially the part with the OK, Skip, Chgd, Fail. Looking at it for a real
playbook, it doesn't convey that much information and it's hard to identify
task where actually something has changed or failed.

Therefore I'm toying around with different ideas on an alternative
visualization. At first a screen shot and then some explanations

```
_setup.yml | Hosts: hades, nirvana, mukti | Tags: base_packages | Time: 01:32___
PLAY: First play
PLAY: Some play
  TASK: Some task              abyss artha charon hades moksha mukti nirvana zen
  TASK: Another task, longer title   abys arth charo hades moksh mukti nirva zen
PLAY: Next play
  TASK: Yet another task with an awkwardly long name   ab ar ch ha mo mu nir zen

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

### Comments

* Task title is display left aligned, idented - as before
* The list of all host names is displayed right aligned
* The color of each host name indicates OK, Skipped, Changed, Fail, Unreachable
* The order of the hosts should be the same for each task (to make it easier
  to scan visually). Initally I would sort it alphabetically, maybe we make it
  configurable later on.
  * It would be great if hosts for which the task did not run yet are still
    displayed in grey and then "light up" in the respective colors as soon as
    respective tasks have been completed.
* If task name + all the host names (plus add'l 3 spaces between task name
  and first host name) are longer than the line length, the following
  algorithm applies
  * First: shorten task title (minimum 15 characters). If everything fits now -> done
  * Second: gradually shorten hostnames
    * Take the longest host name (at the moment) and reduce by one
    * If everything fits now -> done, otherwise repeat
  * It is accepted that truncation of hostnames my lead to collisions. This
    part is only meant for a quick glance. The user can always open the task.
* Visualization when the task is selected and opened should be the same as
  before



## Tree View - third iteration

* Task title is displayed left aligned, indented - same as before
* Current task shows the busy spinner on the far left - same as before
* If possible, all task title shall be rendered in full. If necessary, task
  titles can be shorted to a minimum of 30 characters but not shorter - same
  as before

* NEW: Host names are rendered after the task title. Left aligned (not right
  aligned as in previous versions).
* If all host names fit -> everything's fine
* If not, titles shall be shrinked to accomodate - same as before
* NEW: Important: host names start at the same column no matter if the
  respective task title is very short or long or has been cut short or not
* The color of each host name indicates OK, Skipped, Changed, Fail,
  Unreachable - same as before
* The order of the hosts should be the same for each task (to make it easier
  to scan visually). - same as before

* If task name + all the host names (plus add'l 3 spaces between task name
  and first host name) are longer than the line length, the following
  algorithm applies - this whole point is the same as before
  * First: shorten task title (minimum 15 characters). If everything fits now -> done
  * Second: gradually shorten hostnames
    * Take the longest host name (at the moment) and reduce by one
    * If everything fits now -> done, otherwise repeat
  * It is accepted that truncation of hostnames my lead to collisions. This
    part is only meant for a quick glance. The user can always open the task.

* The current task is visualized via an inverse cursor. But the cursor shall
  only cover the task title but *not* the hostnames. They shall always be
  rendered on black background

Rough sketch 1

```
_setup.yml   01:42   Filter: All________________________________________________
PLAY: First play
PLAY: Some play
  TASK: Some task                                      ab ar ch ha mo mu nir zen
  TASK: Another task, longer title                     ab ar ch ha mo mu nir zen
PLAY: Next play
  TASK: Yet another task with an awkwardly long name   ab ar ch ha mo mu nir zen
    hades:   OK
    nirvana: Changed
    mukti:   Skipped
...
...
...


________________________________________________________________________________
 up/down navigate; enter: show info; q: exit
```

Rough sketch 2

```
_setup.yml   01:42   Filter: All________________________________________________
PLAY: First play
PLAY: Some play
  TASK: Some task          abyss hades mukti
  TASK: Another task,      abyss hades mukti
PLAY: Next play
  TASK: Yet another task   abyss hades mukti
    hades:   OK
    nirvana: Changed
    mukti:   Skipped
...
...
...


________________________________________________________________________________
 up/down navigate; enter: show info; q: exit
```
