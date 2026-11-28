# View.md

## Situation

Sometimes one doesn't want to run or debug a playbook but one simply wants to
view the playbook, a role or some other file. Of course one could traverse
the directory tree and view the files with less or similar. But that's
somewhat cumbersome.

## Idea

Provide the user with a similar view than run and debug provide but just as a
read-only view. Introduce a new verb: tangsible view

How it should look like compared to run or debug:

* Initially it should render the tree in a similar way to the debug view -
  i.e. only plays and tasks but no host lines. Of course in this static view
  only the tasks known at that point can be rendered. Included tasks or
  similar can not be rendered.
* In contrast to the other trees pressing return / clicking on the task lines
  shall open the drill down view
* The drill down view shall only contain the Task, Task definition and Docs
  pages
* The Task page shows
  * "Task:", "Action:" as in the other views
  * In addition it shall display "Hosts:" with the corresponding definition
    (e.g. group1,!db1)


* the tree keyboard shortcuts "f", "o", "s" make no sense in this view, "r"
  should trigger the re-run dialog as usual


## Later: a Variables tab (2026-11-26)

Not part of the first cut. The drill-down could get a Variables tab for a
chosen host: the variables in effect at the selected task, including play
level (`vars:`, `vars_files:`, role vars, block and task vars). This is how
play-level variables reach the Variables tab (`Variables.md`, "view and
variables"). `view` is built first because both share the static project
layer (plays, roles, vars files).


## As built (2026-11-26)

**Implemented**: `tangsible view [ansible-playbook options] [playbook.yml]`.

* **Reading the playbook.** `--list-tasks` has no source file/line, and a
  static YAML walk can't reproduce roles/blocks/imports and their order, so
  `internal/staticplay` loads the playbook with ansible's own classes
  (`Playbook.load`, `play.compile()`) in an embedded Python script and prints
  JSON: per play its name and `hosts:` as written, per task name, action,
  `path:line`, role, tags and `when`. Implicit `meta` tasks are dropped (as
  `--list-tasks` drops them). Runs through `runner.NewToolCommand`, so it
  follows `--ee`/`[execution_environment]` like `run` does; on the host the
  interpreter is the one `ansible-playbook`'s shebang names (a pipx install
  works). Ansible-core internals: a failure is reported, the TUI never starts
  (exit 1, e.g. an unresolvable module or a YAML error shows ansible's own
  message).
* **The session.** An ordinary `liveSession` in its idle state, like
  `rerun`'s: nothing spawned, tree seeded from the loader (`staticplay.Seed`:
  tasks with no hosts, each with its source path so the Task definition tab
  works, side table `View` for action/host pattern). `s.view != nil` marks
  it. No new TUI: the tree, two-pane layout, search, the drill-down widgets
  are the real ones.
* **Tree.** Plays and tasks only, in execution order; Enter or a click on a
  task opens the drill-down (instead of expanding). Top bar: `View: <name>`
  and the play/task counts - no filter, host list, progress or clock.
  `f`, `o`, `s`, `d`, `E`, `C` do nothing (the keys that belong to a run).
* **Drill-down.** Tabs Task, Task definition, Docs (the same source tab with
  click-for-meaning, docs fetched in the background and absent until ready).
  The Task tab has Role, Task, Action, **Hosts** (the play's pattern as
  written) and, beyond the spec, **When** and **Tags** when the task has them.
  `→`/`←` and `n`/`N` move to the next/previous task of the tree (the tab stays), `e`
  opens the source file, `/` and `y` as always. (←/→ are the run drill-down's host keys; with no hosts here they do what `n`/`N` do.)
* **`r`.** The ordinary re-run dialog; confirming it calls the usual
  `requestRerun`, i.e. a real `run` of the same playbook with the dialog's
  tags/limit/play, in this same session, which stops being a view
  (`endView`) - bars, help and keys are those of a run from then on.
  `--ask-*-pass` flags are answered before the TUI (as for `run`), because
  that run could not ask later.

**Left out for now**: handlers; the expansion of `include_tasks`/`import_*`
with a computed name (the include is one row); `--tags`/`--skip-tags`/
`--limit` do not filter the tree (they are kept for `r`); a templated play
name or host pattern is shown unexpanded; reloading the playbook after `e`
returns from the editor - done, see "Reload" below; the "Variables" tab
(`Variables.md`, which builds on this layer).

### Reload (2026-11-26)

**Implemented.** `R` (tree and task view), and automatically when the editor
opened with `e` returns, reads the playbook again (`viewReload`,
`livesession_view.go`): the loader runs off the event loop, then the result
is swapped in on it - `s.state` is reset and refilled in place (the run
machinery holds the same pointer), `s.view` replaced, the source index merged
again (as a rerun does). Nodes do not survive, so the cursor and the open
task are found again by a key made of what the user sees: the play's name,
the task's name and its position among tasks of that name in plays of that
name (`viewTaskKey`; lines move when a file is edited). The cursor of a task
that is gone falls back to its play; the open drill-down of one that is gone
closes. A bottom-bar note says what happened (`reloaded`, `reloaded - the
task you were looking at is gone`, `reload failed, showing the old version:
<first line of ansible's message>`) and disappears after four seconds. A
playbook that no longer loads (a half-saved edit) never replaces the tree.
Overlapping reloads are ignored; a reload that finishes after `r` started a
run is dropped. A renamed task counts as a gone one - there is no better
identity to match on in a file that changes.
