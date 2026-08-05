# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Tangsible is a TUI wrapper for `ansible-playbook`, aimed at interactive use (e.g. during development). The problem it solves and the product decisions behind it are documented in `Purpose.md` — read that first for the "why" before making design choices; this file only covers the "how".

The TUI (built with `tview`) is now live: it comes up immediately and updates incrementally as jsonl events arrive, while `ansible-playbook` is still running — arrow-key navigation, Enter to expand/collapse a task's host rows, `q`/Ctrl-C to quit (or, while the process is still running, to interrupt it — see below). No color-coding, no host command-output drill-down, no elapsed-time display yet — those are follow-up milestones. Because it opens a real terminal UI, running it requires an actual TTY (not a piped/headless shell).

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

**Concurrency (`main.go`).** The TUI (`app.Run()`) and the event stream now run concurrently, coordinated by two `atomic.Bool` flags:
- `processDone` — set once `cmd.Wait()` returns; read by `tui.go`'s input-capture handler to decide whether Ctrl-C/`q` should interrupt the child or close the TUI (see below).
- `quitting` — set once the user has actually chosen to quit (or, defensively, right after `app.Run()` returns for any reason); read by `streamEvents` before every event so it stops pushing updates into a closed/closing UI.

`streamEvents(r io.Reader, applyLive func(rawEvent), quitting *atomic.Bool) []string` reads stdout line-by-line and calls `applyLive(ev)` for each decoded event — `applyLive` (from `tui.go`'s `NewLiveTUI`) wraps `app.QueueUpdateDraw(func() { state.Apply(ev) })`, so `state.Apply` (and the hooks it invokes, see Aggregation below) always runs on tview's event-loop goroutine. Once `quitting` is set, the loop keeps draining `r` (so `ansible-playbook`'s stdout pipe never backs up and blocks *it*) but stops calling `applyLive` — necessary because tview's update queue is a fixed 100-slot buffer that nothing drains once the app has stopped; without this, the goroutine could block forever and the program would hang instead of exiting. A narrow, intentionally-accepted residual race remains in the handoff between checking `quitting` and the in-flight `QueueUpdateDraw` call — documented in the code rather than engineered away, consistent with this project's style elsewhere.

`streamStderr` and the diagnostic prints that used to go straight to stdout (the `v2_playbook_on_stats` cross-check, "(not JSON)" notices, scanner errors) are now all collected into string slices instead, and only flushed by `main` after `app.Run()` returns and the real terminal is restored — printing directly to the terminal while tview's alternate screen is active would corrupt the display. `cmd.Wait()`'s error is likewise captured but not acted on until after the TUI closes, so a failed/interrupted run's tree is still viewable first.

**Event shape (`events.go`).** `rawEvent` decodes the subset of the jsonl schema the app cares about. Empirically confirmed (not formally documented upstream, so re-verify against real output before depending on new fields): `v2_playbook_on_play_start` carries `play.name`; `v2_playbook_on_task_start` carries `task.name`; `v2_runner_on_ok` / `v2_runner_on_skipped` / `v2_runner_on_failed` all share the shape `task` + `hosts: {<hostname>: {...}}`, with the per-host `changed` bool distinguishing OK from Changed within an `_on_ok` event (skipped/failed are their own distinct event names, not a flag). `v2_runner_on_unreachable` is assumed to follow the same shape but hasn't been empirically triggered yet. `v2_playbook_on_stats` carries Ansible's own final per-host tallies, useful as a sanity check against this app's own counts (see caveat below).

**Aggregation (`aggregate.go`).** `playbookState.Apply` consumes one `rawEvent` at a time and builds a `Plays []*playNode` → `Tasks []*taskNode` → `Hosts map[string]outcome` tree. Plays and tasks are created lazily (a play only gets a row once its first task starts), so plays with no executed tasks never appear — matches the decision in `TUI.md`. Ansible's default/linear strategy runs one task across all hosts before starting the next task, so a simple `currentTask` pointer (updated on task-start) is enough to attribute later `runner_on_*` events to the right task, without needing to correlate by task ID. Counts are computed on demand by scanning `taskNode.Hosts` rather than tracked as separate counters, to avoid a second source of truth.

`playbookState` also exposes three optional, nil-checked hooks — `OnPlayAdded`, `OnTaskAdded`, `OnHostRecorded` — invoked by `Apply` right as each thing happens, deliberately typed using only this file's own types so it stays free of any UI dependency. `tui.go` wires these to grow the tview tree incrementally instead of rebuilding it from scratch on every event (a rebuild would reset all expand/collapse state and the current selection each time). Because `Apply` always runs from inside `app.QueueUpdateDraw` (see Concurrency above), `playbookState` is safe to mutate with no mutex — but only because it's always that one goroutine doing it; it's not a general-purpose concurrent structure.

Two deliberate simplifications, both candidates to revisit later if they turn out to be noisy in practice:
- `unreachable` hosts fold into the `outcomeFailed` bucket — no separate column, per a decision that kept `TUI.md`'s 4-column sketch as-is.
- Failed-but-ignored tasks (`ignore_errors: true`) still show as `Fail`. This is *not* how Ansible's own `v2_playbook_on_stats` counts them — Ansible counts an ignored failure toward `ok` (plus a separate `ignored` tally), so this app's rollup will not numerically reconcile with Ansible's own recap stats for playbooks that use `ignore_errors`. Expected, not a bug.

**Plain-text rendering (`render.go`).** `Render` prints the whole tree in one shot, always showing every host line under every task. No longer called from `main`'s normal flow (superseded by the tview UI below) but kept around as a still-useful, dependency-free dump — e.g. for debugging or a future non-interactive mode.

**TUI (`tui.go`).** Built with `github.com/rivo/tview` (on top of `github.com/gdamore/tcell/v2`) — chosen over Bubble Tea's Elm-style architecture specifically because it's a smaller conceptual jump from `tcell`, which prior project experience already covered, and because `TreeView` maps directly onto the play/task/host shape. `NewLiveTUI` builds an initially-empty tree, wires `playbookState`'s hooks (see Aggregation above) to grow it node-by-node via bookkeeping maps (`map[*playNode]*tview.TreeNode`, `map[*taskNode]*tview.TreeNode`, plus a per-task map of host leaves), wraps it in a `Flex` with static top/bottom bars (reverse-video, per `TUI.md`), and returns both the `*tview.Application` and an `applyLive func(rawEvent)` closure — `main.go` calls `app.Run()` and feeds events through `applyLive` but never imports `tview` itself, keeping this the only file that touches `tview`/`tcell`. Things worth knowing if touching this file:
- `TreeView` shows its root node by default; `SetTopLevel(1)` hides the synthetic root so plays appear as the top-level rows — easy to reintroduce accidentally if the tree is rebuilt differently.
- `Application.SetInputCapture` maps `Ctrl-C` and `'q'` identically, and their behavior depends on `processDone`: while `ansible-playbook` is still running, both forward `SIGINT` to it (`proc.Signal(os.Interrupt)`) and leave the TUI open so the run's outcome stays visible; once the process has finished, both close the TUI instead. This restores Purpose.md's "Ctrl-C behaves like running ansible-playbook directly" decision, which `tcell`'s raw mode would otherwise silently break — raw mode disables the OS's own Ctrl-C-to-SIGINT delivery, so without this explicit forwarding the child would stop receiving the interrupt it used to get for free (this only started to matter once the TUI could be live *while* the child was still running — it was moot in the previous static-tree milestone).

**Test fixtures (`testdata/`).** Playbooks (and one inventory file) used to manually exercise the aggregation logic: `basic.yml` (simple sequential tasks including a sleep, demonstrates live streaming), `outcomes.yml` (single host, all four outcome buckets including an ignored failure), `multihost.yml` + `multihost-inventory.ini` (two hosts diverging on the same task, to exercise per-host aggregation within one task).

## Current scope constraints (from Purpose.md)

These aren't accidental omissions — they're explicit v1 decisions and should inform what "done" means for near-term work:

- No run history — single-session only, revisit later.
- No interactive prompt handling (`--ask-become-pass`, vault password prompts, `pause` tasks) — not supported yet.
- Ctrl-C should behave the same as it does when running `ansible-playbook` directly — implemented, see `tui.go`'s `SetInputCapture` note above.
- Target scale is ~10 hosts, not hundreds/thousands — this is an interactive dev tool, not a fleet-management tool.
- Only a limited subset of `ansible-playbook`'s CLI surface should be exposed initially; expand only as needed.
