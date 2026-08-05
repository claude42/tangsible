# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Tangsible is a TUI wrapper for `ansible-playbook`, aimed at interactive use (e.g. during development). The problem it solves and the product decisions behind it are documented in `Purpose.md` — read that first for the "why" before making design choices; this file only covers the "how".

The repo currently contains a single-file prototype (`main.go`), not the TUI itself. Its purpose is to validate the core data-flow assumption the whole app depends on: that `ansible-playbook` output can be consumed live, event-by-event, while the playbook is still running.

## Commands

```
go build ./...                                    # build
go vet ./...                                       # lint
go run . <playbook.yml> [ansible-playbook args...] # run the prototype
```

No tests exist yet; once added, run with `go test ./...` (`go test -run <Name> ./...` for a single test).

Running the prototype requires `ansible-playbook` and the `ansible.posix` collection (`ansible-galaxy collection install ansible.posix`) to be installed locally.

## Architecture

**Data source.** The app never talks to Ansible's Python API and never implements a custom callback plugin (deliberately — see Purpose.md's Platform section for the rationale). Instead it shells out to `ansible-playbook` as a subprocess with `ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl` set in its environment. That callback writes one JSON object per line to stdout as each event happens, which is what makes live consumption possible — the built-in `json` callback was considered and rejected because it buffers everything and only writes a single blob after the run completes.

**Streaming.** `main.go` starts the subprocess with piped stdout/stderr. Stdout is read line-by-line (`streamEvents`) and each line is decoded as a generic JSON event and printed immediately, so events surface incrementally rather than after the process exits. Stderr is drained concurrently in its own goroutine (`streamStderr`) and joined via a `done` channel before `cmd.Wait()`, to avoid deadlocking on a full stderr pipe while stdout is still being read.

**Event shape.** Events from `ansible.posix.jsonl` aren't parsed into typed structs yet — the prototype decodes into `map[string]interface{}` and dumps keys/contents as-is. Observed event fields so far: `_event` (e.g. `v2_playbook_on_play_start`, `v2_playbook_on_task_start`, `v2_runner_on_ok`), `_timestamp`, `task`, `hosts`. This schema isn't formally documented upstream, so treat it as empirically discovered rather than a stable contract — re-verify against real output when depending on new fields.

## Current scope constraints (from Purpose.md)

These aren't accidental omissions — they're explicit v1 decisions and should inform what "done" means for near-term work:

- No run history — single-session only, revisit later.
- No interactive prompt handling (`--ask-become-pass`, vault password prompts, `pause` tasks) — not supported yet.
- Ctrl-C should behave the same as it does when running `ansible-playbook` directly.
- Target scale is ~10 hosts, not hundreds/thousands — this is an interactive dev tool, not a fleet-management tool.
- Only a limited subset of `ansible-playbook`'s CLI surface should be exposed initially; expand only as needed.
