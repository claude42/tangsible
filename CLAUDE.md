# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Tangsible is a TUI wrapper for `ansible-playbook`, aimed at interactive use (e.g. during development). The problem it solves and the product decisions behind it are documented in `Purpose.md` — read that first for the "why" before making design choices; this file only covers the "how".

The repo is still pre-TUI: it currently prints a live plain-text redraw of the play/task/host tree to stdout (no color, no interactivity, no expand/collapse) rather than rendering an actual terminal UI. This is the data model the eventual TUI will render, built and validated standalone first.

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

**Streaming (`main.go`).** The subprocess is started with piped stdout/stderr. Stdout is read line-by-line (`streamEvents`); stderr is drained concurrently in its own goroutine (`streamStderr`) and joined via a `done` channel before `cmd.Wait()`, to avoid deadlocking on a full stderr pipe while stdout is still being read.

**Event shape (`events.go`).** `rawEvent` decodes the subset of the jsonl schema the app cares about. Empirically confirmed (not formally documented upstream, so re-verify against real output before depending on new fields): `v2_playbook_on_play_start` carries `play.name`; `v2_playbook_on_task_start` carries `task.name`; `v2_runner_on_ok` / `v2_runner_on_skipped` / `v2_runner_on_failed` all share the shape `task` + `hosts: {<hostname>: {...}}`, with the per-host `changed` bool distinguishing OK from Changed within an `_on_ok` event (skipped/failed are their own distinct event names, not a flag). `v2_runner_on_unreachable` is assumed to follow the same shape but hasn't been empirically triggered yet. `v2_playbook_on_stats` carries Ansible's own final per-host tallies, useful as a sanity check against this app's own counts (see caveat below).

**Aggregation (`aggregate.go`).** `playbookState.Apply` consumes one `rawEvent` at a time and builds a `Plays []*playNode` → `Tasks []*taskNode` → `Hosts map[string]outcome` tree. Plays and tasks are created lazily (a play only gets a row once its first task starts), so plays with no executed tasks never appear — matches the decision in `TUI.md`. Ansible's default/linear strategy runs one task across all hosts before starting the next task, so a simple `currentTask` pointer (updated on task-start) is enough to attribute later `runner_on_*` events to the right task, without needing to correlate by task ID. Counts are computed on demand by scanning `taskNode.Hosts` rather than tracked as separate counters, to avoid a second source of truth.

Two deliberate simplifications, both candidates to revisit later if they turn out to be noisy in practice:
- `unreachable` hosts fold into the `outcomeFailed` bucket — no separate column, per a decision that kept `TUI.md`'s 4-column sketch as-is.
- Failed-but-ignored tasks (`ignore_errors: true`) still show as `Fail`. This is *not* how Ansible's own `v2_playbook_on_stats` counts them — Ansible counts an ignored failure toward `ok` (plus a separate `ignored` tally), so this app's rollup will not numerically reconcile with Ansible's own recap stats for playbooks that use `ignore_errors`. Expected, not a bug.

**Rendering (`render.go`).** `Render` prints the whole current tree on every event (not just the changed part), always showing every host line under every task — there's no navigation/expand state yet to decide what to hide. `main.go` calls it after every event, separated by a divider, so the terminal scrollback shows the model's progression over time.

**Test fixtures (`testdata/`).** Playbooks (and one inventory file) used to manually exercise the aggregation logic: `basic.yml` (simple sequential tasks including a sleep, demonstrates live streaming), `outcomes.yml` (single host, all four outcome buckets including an ignored failure), `multihost.yml` + `multihost-inventory.ini` (two hosts diverging on the same task, to exercise per-host aggregation within one task).

## Current scope constraints (from Purpose.md)

These aren't accidental omissions — they're explicit v1 decisions and should inform what "done" means for near-term work:

- No run history — single-session only, revisit later.
- No interactive prompt handling (`--ask-become-pass`, vault password prompts, `pause` tasks) — not supported yet.
- Ctrl-C should behave the same as it does when running `ansible-playbook` directly.
- Target scale is ~10 hosts, not hundreds/thousands — this is an interactive dev tool, not a fleet-management tool.
- Only a limited subset of `ansible-playbook`'s CLI surface should be exposed initially; expand only as needed.
