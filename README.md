# Tangsible

A terminal UI for `ansible-playbook`, built for interactive use — running
playbooks by hand during development, not unattended automation.

## Why

Running `ansible-playbook` directly is painful when you're actually
watching it: output scrolls by faster than you can read it, a failing
task's real output is buried in a wall of text, and reconstructing
"which hosts succeeded, which failed, and why" after the fact means
scrolling back through a terminal or grepping a log file.

Tangsible wraps `ansible-playbook` in a live, navigable tree of
plays → tasks → hosts. It comes up immediately and updates as the run
progresses — you can scroll around and inspect earlier results while
later tasks are still executing — and once something fails, drilling into
it shows the task's own source, its output, and the full result side by
side, not spread across your scrollback.

## Features

- **Live**, incremental updates — no waiting for the run to finish.
- A **tree view** of plays/tasks/hosts, color-coded by outcome
  (OK/Changed/Skipped/Failed/Unreachable), with expand/collapse and a
  right-aligned host list per task so you can see at a glance who's done
  what.
- A **drill-down view** per host/task result: the task exactly as written
  in your playbook or role (with a light syntax highlight), its output,
  stderr, and the full JSON result — all in one place, colorized and
  navigable to the next/previous task or host without leaving the view.
- Keyboard **and mouse** navigation (see below).
- The playbook path is optional — resolved from an env var, a per-project
  config file, a global config file, or a conventional `site.yml`, in
  that order (see Configuration below).
- **Re-run** a finished playbook without leaving the TUI — the whole thing
  again, or starting from a specific task, with editable tags/hosts — or
  jump straight into that same dialog from the command line with
  `tangsible rerun`, pre-filled from what you last ran (see Re-running
  below).

## Requirements

- Go 1.24+ to build.
- `ansible-playbook`, plus the `ansible.posix` collection:
  ```
  ansible-galaxy collection install ansible.posix
  ```
  (Tangsible relies on that collection's `jsonl` callback plugin to
  stream events live — see `CLAUDE.md` if you're curious why.)
- A real terminal (TTY) — Tangsible opens a full-screen UI, so it can't
  run in a piped/headless shell.

## Install

```
git clone https://code.aw.net/claude/tangsible
cd tangsible
go build ./...
```

This produces a `tangsible` binary in the current directory.

## Usage

```
tangsible run [<playbook.yml>] [ansible-playbook args...]
```

The `run` verb is mandatory (more verbs exist — see `rerun` in Rerun.md).
Anything after the playbook is passed straight through to
`ansible-playbook` — inventory, tags, limits, verbosity, whatever you'd
normally pass. For example:

```
tangsible run site.yml -i inventory.ini --limit webservers -t deploy
```

If you omit the playbook, Tangsible tries to figure out which one you
meant — see Configuration below. If it can't, it prints a usage message
and exits.

To try it out without a real inventory, the bundled fixtures under
`testdata/` are safe to run locally:

```
tangsible run testdata/outcomes.yml -i localhost,
tangsible run testdata/multihost.yml -i testdata/multihost-inventory.ini
```

## Re-running

Once a run has finished (successfully, failed, or interrupted), press `r`
to open a dialog offering to re-run it — the whole playbook again, or
starting from a specific task (`--start-at-task`), with editable tags and
hosts. Confirm with `Enter`; the tree clears and a new run starts without
leaving Tangsible. See `Keyboard-shortcuts.md` for the full rundown of that
dialog's own keys.

The same dialog is also reachable straight from the command line:

```
tangsible rerun [<playbook.yml>] [ansible-playbook args...]
```

`tangsible rerun` with no arguments re-runs whichever playbook you last ran
in this project, with the same tags/hosts, pre-filled into the dialog for
one last look before confirming. Giving a playbook explicitly re-runs
*that* playbook's own last invocation instead (or an empty dialog if it's
never been run before); `-l`/`--tags` given on the `rerun` command line
itself override whatever was recorded. Either way, nothing runs until you
press `Enter` in the dialog — `rerun` never fires off a playbook silently.

This relies on `.tangsible` remembering past invocations — see
Configuration below.

## Keyboard shortcuts

The essentials — see `Keyboard-shortcuts.md` for the complete reference
(Emacs/vim-style navigation aliases, expand/collapse-all, autoscroll
control, and more).

**Main tree**

| Key | Action |
|---|---|
| `↑`/`↓` | Move the cursor |
| `Enter` / `Space` | Toggle a task's host rows, or open a host's result |
| `←`/`→` | Collapse / expand a task |
| `n` / `p` | Jump to the next / previous task |
| `Home` / `End` | Jump to the first / last row |
| `r` | Open the re-run dialog (once the run has finished) |
| `q` / `Ctrl-C` | Interrupt the run, or quit once it's finished |

**Drill-down view**

| Key | Action |
|---|---|
| `Esc` / `Enter` | Close, back to the tree |
| `←`/`→` | Previous / next host (same task) |
| `n` / `p` | Next / previous task (same host) |

## Mouse

Mouse support mirrors the keyboard: wheel/trackpad scroll, click a row to
select and activate it (same as `Enter`), click a host row to view its
output.

## Configuration

When no playbook is given on the command line, Tangsible resolves one
from, in order:

1. `$TANGSIBLE_PLAYBOOK`
2. `.tangsible` in the current directory
3. `$XDG_CONFIG_HOME/tangsible/config.toml` (falling back to
   `~/.config/tangsible/config.toml` if that variable isn't set)
4. `./site.yml`, if present

`.tangsible` and `config.toml` share the same TOML format:

```toml
[general]
default_playbook = "myplaybook.yml"
```

`.tangsible` additionally accumulates invocation history as you use
`tangsible run`/`rerun` — this is what powers `rerun` and pre-fills the
re-run dialog's Tags/Hosts fields. You don't need to (and shouldn't) write
this part by hand:

```toml
[general]
default_playbook = "myplaybook.yml"
last_playbook = "myplaybook.yml"

[[history]]
playbook = "myplaybook.yml"
invocations = ["", "-l webservers", "-l webservers --tags deploy"]
```

`invocations` keeps up to the last 20 entries per playbook, oldest first.
Since it can end up recording real environment details (hostnames, tags,
`-e` extra-vars), consider adding `.tangsible` to your project's
`.gitignore` once it's grown past a bare `default_playbook` — the same way
you wouldn't commit shell history.

`.tangsible` (project-local only — not `config.toml`) also accepts
`default_tree_state`, either `"expanded"` or `"collapsed"` (case-
insensitive; defaults to `"collapsed"` if unset or unrecognized), which
sets whether the very first task row of a run starts expanded or
collapsed:

```toml
[general]
default_tree_state = "expanded"
```

Every task added after the first inherits whatever expand/collapse state
the task added right before it currently has — so pressing `E` mid-run
doesn't just expand everything already on screen, it also makes every
task added from then on start expanded too.

## Current limitations

This is a v1, aimed at the common interactive case rather than full
`ansible-playbook` parity:

- No browsable run history *within* the TUI — each run's tree is replaced
  by the next one (including a re-run's), and there's no way to page back
  through past runs' results. (`.tangsible` does remember past invocation
  *arguments* — see Configuration — but not their outcomes.)
- `--ask-become-pass`/`--ask-vault-pass` already work, with no special
  handling needed — Ansible's own password prompt opens `/dev/tty`
  directly rather than going through Tangsible's own stdin/stdout, and it
  happens before Tangsible's TUI ever touches the terminal, so the two
  never contend for it.
- Interactive input *after* the TUI is already up doesn't work, though —
  a bare `pause:` task (no `seconds:`/`minutes:`) doesn't hang, but it
  also doesn't actually pause — Ansible detects stdin isn't interactive
  and skips waiting immediately, completing the task with a warning
  (`Not waiting for response to prompt as stdin is not interactive`,
  visible if you drill into that task's own output) rather than
  blocking. A timed `pause: seconds: N`/`minutes: N` is unaffected — it's
  a pure sleep, no input involved. The same likely applies to a
  `vars_prompt` on any play after the first (untested) — unlike
  `--ask-become-pass`/`--ask-vault-pass`, which are always asked once,
  up front, before anything else happens.
- **`strategy: free` shows nothing at all** — confirmed live, not just
  suspected: with the `free` strategy, `ansible-playbook`'s jsonl callback
  never emits a task-start event (each host progresses through the task
  list independently, so there's no single "task starts now" moment
  shared across hosts the way the default `linear` strategy has). Every
  part of Tangsible's tree — the play row, every task row, every host
  result — only ever gets created in response to that event, so under
  `free` the tree stays completely empty for the whole run: no play, no
  tasks, no hosts, just a ticking spinner and a final "Playbook completed
  successfully" over a blank screen, even though the playbook genuinely
  ran and finished correctly underneath. Not a rendering glitch — real
  data that's silently never recorded. Supporting `free` properly would
  need tracking "current task" per host rather than one shared task
  across all of them, which touches the tree's own data model, not just
  a display tweak — treated as a distinct, not-yet-planned effort rather
  than an incremental fix.
- Built and tested around the scale of a typical dev inventory (roughly
  10 hosts), not a fleet-management tool.
- Only a limited slice of `ansible-playbook`'s CLI surface is
  special-cased (the rest is passed through as-is).

## Development

`CLAUDE.md` documents the internals in depth — architecture, the event
stream, and the reasoning behind most non-obvious decisions. Quick
reference:

```
go build ./...   # build
go vet ./...     # lint
go test ./...    # unit tests
```

A small set of end-to-end smoke tests also exists (`e2e_rerun_test.go`),
driving the real binary inside a real `tmux` pane — excluded from the
above by default (build tag `e2e`), since they need `tmux` and
`ansible-playbook` installed and are slower than the rest of the suite:

```
go test -tags e2e ./...
```

## License

GPL-2.0 — see `LICENSE`.
