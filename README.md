# Tangsible

**A live terminal UI for `ansible-playbook`, for the person actually
watching it run.**

Tangsible wraps `ansible-playbook` in a navigable, incrementally-updating
tree of plays → tasks → hosts, color-coded by outcome. It comes up
immediately, updates live while the run is still in progress, and lets you
drill into any host's result — task source, output, stderr, diff, full
JSON — without losing your place in the run.

```
 tangsible ▸ deploy.yml                                       ⠋ 00:14
────────────────────────────────────────────────────────────────────
 Deploy webapp
   Gather facts                              web-1 OK    web-2 OK
   Install packages                          web-1 OK    web-2 Chgd
 ▶ Configure nginx                           web-1 OK    web-2 Fail
     web-2: Failed — "nginx: [emerg] invalid number of arguments"
   Restart service                           web-1 OK    web-2 ⠂
────────────────────────────────────────────────────────────────────
 ↑↓ move   ↵ expand/open   n/p next/prev task   r rerun   q quit
```
*(a simplified mockup of the actual TUI — the real thing color-codes each
host by outcome and updates this live, line by line, as Ansible runs)*

## Why

Running `ansible-playbook` directly is painful when you're actually
watching it, not just kicking it off and walking away:

- Output scrolls by faster than you can read it — reconstructing "which
  hosts succeeded, which failed, and why" after the fact means scrolling
  back through a terminal or grepping a log file.
- A failing task's real error is usually buried several screens up, mixed
  in with everything that ran fine.
- There's no live overview — you can't tell at a glance how far along a
  multi-host, multi-task run is, or which hosts have diverged from the
  rest, until it's already finished (or you've lost the scrollback).

Tangsible turns that stream of text into a structure you can look at:
scroll around and inspect earlier results while later tasks are still
executing, and once something fails, jump straight to it — the task as
written, its output, and the full result, side by side, not spread across
your scrollback.

## Who it's for

Anyone running `ansible-playbook` **by hand, repeatedly, against a
live-ish environment** — the edit-run-inspect-rerun loop of writing or
debugging a playbook or role, not unattended automation:

- Developers and platform engineers iterating on playbooks/roles against
  dev or staging boxes.
- Anyone who currently keeps a second terminal open just to `tail` a log
  or scroll back to find where things went wrong.
- Small-team/solo ops work in the ~10-host range — not fleet management
  (see [Current limitations](#current-limitations)).

It is *not* aimed at CI pipelines, scheduled/unattended runs, or
orchestrating jobs across a team — for those, the tools below are a
better fit than Tangsible ever intends to be.

## Why not X?

| Instead of... | You get... | Tangsible's difference |
|---|---|---|
| Plain `ansible-playbook` | Full scrollback, but no structure — you `grep`/scroll to find what happened | Same output, restructured into a live, navigable tree — nothing to grep for |
| `ansible-playbook --step` | A pause-and-confirm prompt before each task | Doesn't slow the run down or ask permission — you watch it run at full speed and drill in only where you care to |
| ARA (Ansible Run Analysis) | Rich *post-run* reporting via a web UI, backed by a database/API server | Zero infrastructure — no DB, no server, nothing to stand up — and it's live *during* the run, in your terminal, not a report you open afterward |
| AWX / Ansible Tower / Semaphore | A full orchestration platform: scheduling, RBAC, web UI, job history, teams | Built for one person at a terminal, not a service to deploy — a single static binary with no install beyond it |
| `ansible-console` | Interactive **ad-hoc** command execution, one module call at a time | Runs your actual playbooks — plays, tasks, handlers, roles — not a replacement for playbooks, a better window onto them |

The short version: everything above either doesn't restructure the output
for live watching, or asks you to run infrastructure to get that
restructuring. Tangsible is a single binary that shells out to
`ansible-playbook` exactly as you would, and turns its output into
something you can watch and navigate as it happens.

## Features

- **Live**, incremental updates — no waiting for the run to finish.
- A **tree view** of plays/tasks/hosts, color-coded by outcome
  (OK/Changed/Skipped/Failed/Unreachable), with expand/collapse and a
  host list per task so you can see at a glance who's done what.
- A **drill-down view** per host/task result: the task exactly as written
  in your playbook or role (with a light syntax highlight), its output,
  stderr, a `--diff` tab, and the full JSON result — all in one place,
  colorized and navigable to the next/previous task or host without
  leaving the view.
- **Filtering** — narrow the tree to just Changed, just Failed, or a
  full-text search across task names, source, and output.
- Keyboard **and mouse** navigation (see below).
- The playbook path is optional — resolved from an env var, a per-project
  config file, a global config file, or a conventional `site.yml`, in
  that order (see Configuration below).
- **Re-run** a finished playbook without leaving the TUI — the whole thing
  again, or starting from a specific task, with editable tags/hosts — or
  jump straight into that same dialog from the command line with
  `tangsible rerun`, pre-filled from what you last ran (see Re-running
  below).
- **Test a role on its own** with `tangsible role <name>` — no throwaway
  playbook to hand-write first (see Testing a role below).
- **Debug a Jinja2 template** with `tangsible template <path>` — renders
  it through Ansible's own templating against a real host and shows the
  result (or the error) directly (see Debugging a template below).

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

A leading verb is mandatory — `run` above, plus `rerun`, `role`, and
`template` (see their own sections below). Anything after the playbook is
passed straight through to
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

## Testing a role in isolation

Developing a role usually means it can't just be run — `ansible-playbook`
only ever executes playbooks, so you'd normally have to add the role to
an existing playbook, or hand-write a throwaway stub, just to try it:

```
tangsible role <role_name> [ansible-playbook args...]
```

Tangsible generates a small stub playbook (`hosts: all`, running just
that role — narrow the actual target the normal way, with `-l`/`-i`) and
runs it exactly like `tangsible run` would, live tree and all. The stub
is deleted when Tangsible exits; a mid-session re-run (`r`) reuses it
rather than regenerating it. `tangsible rerun` with no arguments works for
a role the same way it does for a playbook — it remembers whichever you
ran most recently, one or the other.

## Debugging a Jinja2 template

Jinja2 templates are easy to get subtly wrong, and testing one properly
means going through Ansible's own templating rather than a generic Jinja
tool — which normally means crafting a playbook or role just to render
it once:

```
tangsible template <path to template> [<hostname>] [-e ...]
```

This is a different, single-view mode — no tree, just the rendered
output (or, on failure, the Jinja error) for one host at a time.
`hostname` is optional (Tangsible picks the first inventory host if
omitted); `-e` works as usual for passing extra vars. If the template
lives at a role's conventional `roles/<name>/templates/...` path, that
role's own variables are made available automatically, the same as a
real `include_role` would. Press `e` to open the template in `$VISUAL`/
`$EDITOR` and re-render on save, `h` to switch which host it's rendered
against, `q` to quit.

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
| `a` / `c` / `f` | Filter: All / Changed / Failed |
| `/` | Search filter (task name, source, output) |
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

Apache-2.0 — see `LICENSE`.
