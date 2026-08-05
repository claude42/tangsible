# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Tangsible is a TUI wrapper for `ansible-playbook`, aimed at interactive use (e.g. during development). The problem it solves and the product decisions behind it are documented in `Purpose.md` — read that first for the "why" before making design choices; this file only covers the "how".

The repo now has a first real (but intentionally minimal) terminal UI, built with `tview`. It is **not live yet**: `ansible-playbook` runs to completion first, then the aggregated tree is shown as an interactive `tview.TreeView` (arrow-key navigation, Enter to expand/collapse a task's host rows, `q`/Ctrl-C to quit). No color-coding, no host command-output drill-down, no elapsed-time display yet — those are follow-up milestones. Because it opens a real terminal UI, running it requires an actual TTY (not a piped/headless shell).

## Commands

```
go build ./...                                    # build
go vet ./...                                       # lint
go run . <playbook.yml> [ansible-playbook args...] # run
```

Try it against the bundled fixtures, e.g.:
```
go run . testdata/outcomes.yml -i localhost,
go run . testdata/multihost.yml -i testdata/multihost-inventory.ini
```

No tests exist yet; once added, run with `go test ./...` (`go test -run <Name> ./...` for a single test).

Running it requires `ansible-playbook` and the `ansible.posix` collection (`ansible-galaxy collection install ansible.posix`) to be installed locally.

## Architecture

**Data source.** The app never talks to Ansible's Python API and never implements a custom callback plugin (deliberately — see Purpose.md's Platform section for the rationale). Instead it shells out to `ansible-playbook` as a subprocess with `ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl` and `ANSIBLE_JSON_INDENT=0` set in its environment. That callback writes one JSON object per line to stdout as each event happens, which is what makes live consumption possible — the built-in `json` callback was considered and rejected because it buffers everything and only writes a single blob after the run completes. Pinning `ANSIBLE_JSON_INDENT=0` guards against a user's `ansible.cfg` overriding the default and breaking the one-event-per-line assumption the line-based scanner depends on.

**Streaming (`main.go`).** The subprocess is started with piped stdout/stderr. Stdout is read line-by-line (`streamEvents`), which now returns the fully-built `*playbookState` once the pipe is exhausted rather than rendering anything itself. Stderr is drained concurrently in its own goroutine (`streamStderr`) and joined via a `done` channel before `cmd.Wait()`, to avoid deadlocking on a full stderr pipe while stdout is still being read. `cmd.Wait()`'s error is captured but not acted on immediately — the TUI is shown first (so a failed run's tree is still viewable), and only afterward does `main` report the error and exit non-zero.

**Event shape (`events.go`).** `rawEvent` decodes the subset of the jsonl schema the app cares about. Empirically confirmed (not formally documented upstream, so re-verify against real output before depending on new fields): `v2_playbook_on_play_start` carries `play.name`; `v2_playbook_on_task_start` carries `task.name`; `v2_runner_on_ok` / `v2_runner_on_skipped` / `v2_runner_on_failed` all share the shape `task` + `hosts: {<hostname>: {...}}`, with the per-host `changed` bool distinguishing OK from Changed within an `_on_ok` event (skipped/failed are their own distinct event names, not a flag). `v2_runner_on_unreachable` is assumed to follow the same shape but hasn't been empirically triggered yet. `v2_playbook_on_stats` carries Ansible's own final per-host tallies, useful as a sanity check against this app's own counts (see caveat below).

**Aggregation (`aggregate.go`).** `playbookState.Apply` consumes one `rawEvent` at a time and builds a `Plays []*playNode` → `Tasks []*taskNode` → `Hosts map[string]outcome` tree. Plays and tasks are created lazily (a play only gets a row once its first task starts), so plays with no executed tasks never appear — matches the decision in `TUI.md`. Ansible's default/linear strategy runs one task across all hosts before starting the next task, so a simple `currentTask` pointer (updated on task-start) is enough to attribute later `runner_on_*` events to the right task, without needing to correlate by task ID. Counts are computed on demand by scanning `taskNode.Hosts` rather than tracked as separate counters, to avoid a second source of truth.

Two deliberate simplifications, both candidates to revisit later if they turn out to be noisy in practice:
- `unreachable` hosts fold into the `outcomeFailed` bucket — no separate column, per a decision that kept `TUI.md`'s 4-column sketch as-is.
- Failed-but-ignored tasks (`ignore_errors: true`) still show as `Fail`. This is *not* how Ansible's own `v2_playbook_on_stats` counts them — Ansible counts an ignored failure toward `ok` (plus a separate `ignored` tally), so this app's rollup will not numerically reconcile with Ansible's own recap stats for playbooks that use `ignore_errors`. Expected, not a bug.

**Plain-text rendering (`render.go`).** `Render` prints the whole tree in one shot, always showing every host line under every task. No longer called from `main`'s normal flow (superseded by the tview UI below) but kept around as a still-useful, dependency-free dump — e.g. for debugging or a future non-interactive mode.

**TUI (`tui.go`).** Built with `github.com/rivo/tview` (on top of `github.com/gdamore/tcell/v2`) — chosen over Bubble Tea's Elm-style architecture specifically because it's a smaller conceptual jump from `tcell`, which prior project experience already covered, and because `TreeView` maps directly onto the play/task/host shape. `buildTree` converts the final `*playbookState` into a `tview.TreeNode` hierarchy (task nodes start `SetExpanded(false)`, host nodes are leaves); `RunTUI` wraps it in a `TreeView` inside a `Flex` with static top/bottom bars (reverse-video, per `TUI.md`) and blocks until quit. Two things worth knowing if touching this file:
- `TreeView` shows its root node by default; `SetTopLevel(1)` hides the synthetic root so plays appear as the top-level rows — easy to reintroduce accidentally if the tree is rebuilt differently.
- `Application.SetInputCapture` explicitly maps `Ctrl-C` (in addition to `q`) to quit. This is necessary, not decorative: `tcell`'s raw mode disables the OS-level SIGINT-on-Ctrl-C, so without this it would arrive as an inert keypress rather than terminate anything. This is a separate concern from the Ctrl-C behavior called for in Purpose.md, which is about interrupting the `ansible-playbook` subprocess itself — moot for now since the TUI only launches after that subprocess has already finished.

**Test fixtures (`testdata/`).** Playbooks (and one inventory file) used to manually exercise the aggregation logic: `basic.yml` (simple sequential tasks including a sleep, demonstrates live streaming), `outcomes.yml` (single host, all four outcome buckets including an ignored failure), `multihost.yml` + `multihost-inventory.ini` (two hosts diverging on the same task, to exercise per-host aggregation within one task).

## Current scope constraints (from Purpose.md)

These aren't accidental omissions — they're explicit v1 decisions and should inform what "done" means for near-term work:

- No run history — single-session only, revisit later.
- No interactive prompt handling (`--ask-become-pass`, vault password prompts, `pause` tasks) — not supported yet.
- Ctrl-C should behave the same as it does when running `ansible-playbook` directly.
- Target scale is ~10 hosts, not hundreds/thousands — this is an interactive dev tool, not a fleet-management tool.
- Only a limited subset of `ansible-playbook`'s CLI surface should be exposed initially; expand only as needed.
