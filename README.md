# Tangsible

**A live terminal UI for `ansible-playbook`, for the person actually
watching it run.**

Tangsible wraps `ansible-playbook` in a live, navigable
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
- Developers and operators working with relatively small inventories —
  Tangsible isn't intended as a fleet-management UI.

It is *not* aimed at CI pipelines, scheduled/unattended runs, or
orchestrating jobs across a team — for those, the tools below are a
better fit than Tangsible ever intends to be.

## How Tangsible differs

| Instead of... | You get... | Tangsible's difference |
|---|---|---|
| Plain `ansible-playbook` | Full scrollback, but no structure — you `grep`/scroll to find what happened | Same output, restructured into a live, navigable tree — nothing to grep for |
| `ansible-playbook --step` | A pause-and-confirm prompt before each task | Doesn't slow the run down or ask permission — you watch it run at full speed and drill in only where you care to |
| ARA (Ansible Run Analysis) | Rich *post-run* reporting via a web UI, backed by a database/API server | Zero infrastructure — no DB, no server, nothing to stand up — and it's live *during* the run, in your terminal, not a report you open afterward |
| AWX / Ansible Tower / Semaphore | A full orchestration platform: scheduling, RBAC, web UI, job history, teams | Built for one person at a terminal, not a service to deploy — a single static binary with no install beyond it |
| `ansible-console` | Interactive **ad-hoc** command execution, one module call at a time | Runs your actual playbooks — plays, tasks, handlers, roles — not a replacement for playbooks, a better window onto them |

In short: Tangsible is a development and troubleshooting interface for
ansible-playbook, not a replacement for Ansible or an orchestration platform.
Tangsible is a single binary that shells out to `ansible-playbook` exactly as
you would, and turns its output into something you can watch and navigate as
it happens.

## Features

### Run and inspect

- **Live updates** — the TUI appears immediately and updates while the playbook
  is still running.
- **Tree view** — plays, tasks and hosts are shown in a navigable tree, with
  outcomes such as OK, Changed, Skipped, Failed and Unreachable clearly
  visible.
- **Drill-down** — inspect a host/task result with the task source, output,
  stderr, --diff output and full JSON result in one place.
- **Filtering and search** — show only Changed or Failed results, or search task
  names, source and output.
- **Keyboard and mouse navigation** — use the keyboard or mouse throughout the
  TUI.

### Iterate

Tangsible is designed around the development loop of finding something wrong, changing it and trying again.

- **Re-run from the TUI** — rerun the whole playbook or start at a specific task
  or limit tags and hosts in subsequent runs
- **Edit tags and hosts** before rerunning.
- **`tangsible rerun`** — open the same rerun workflow directly from the command
  line, pre-filled from the previous invocation.

### Develop Ansible

Tangsible provides a couple of shortcuts for common development tasks that
normally require writing temporary playbooks.

- **Test a role in isolation** with `tangsible role <role>`.
- **Debug a Jinja2 template** with `tangsible template <path>`, using Ansible's own
  templating against a real inventory host. Press `e` to edit the template and
  automatically render it again after saving.

## Requirements

- Go 1.24+ to build.
- `ansible-playbook`
- The `ansible.posix` collection:
  ```
  ansible-galaxy collection install ansible.posix
  ```
  Tangsible relies on that collection's `jsonl` callback plugin to receive
  Ansible events while the playbook is running.
- A real terminal (TTY) — Tangsible opens a full-screen UI and cannot run in
  a piped or headless shell.

## Install

Currently, Tangsible is built from source:

```
git clone https://code.aw.net/claude/tangsible
cd tangsible
go build ./...
```

This produces a `tangsible` binary in the current directory.

Pre-built binaries will follow soon.

## Quick start

Run a playbook:

```
tangsible run playbook.yml
```

Ansible arguments can be passed after the playbook as usual:

```
tangsible run site.yml -i inventory.ini --limit webservers --tags deploy
```

Tangsible passes arguments through to ansible-playbook, so your normal
inventory, tag, limit and verbosity options continue to work.

The playbook path can also be omitted when Tangsible can resolve one from its
configuration. See Configuration.

### Try it without an inventory

The repository contains small fixtures that can be run locally:

```
tangsible run testdata/outcomes.yml -i localhost,
tangsible run testdata/multihost.yml -i testdata/multihost-inventory.ini
```

## Re-running

Once a run has finished — successfully, with failures, or interrupted — press
`r` to open the rerun dialog.

You can rerun the complete playbook, start at a specific task, change the
target hosts, change tasks.

The same workflow is available from the command line:

```
tangsible rerun [playbook.yml] [ansible-playbook args...]
```

## Testing a role in isolation

Normally, `ansible-playbook` runs playbooks rather than individual roles.
Testing a role in isolation therefore usually means creating a temporary
playbook first.

Tangsible can create that temporary wrapper for you:

```
tangsible role role_name  [ansible-playbook args...]
```

It runs the role against `hosts: all`, while normal Ansible options such as
`-i` and `-l` can be used to select the actual target.


## Debugging a Jinja2 template

Debugging a template properly means rendering it through Ansible's own
templating system, with the variables and inventory context that Ansible will
actually use.

Tangsible provides a shortcut:

```
tangsible template <path-to-template> [<hostname>] [-e ...]
```

The template is rendered for one inventory host and the result is displayed
directly in the terminal. Extra variables can be specified with `-e`.  If
rendering fails, the Jinja error is shown instead.

If the template is in a conventional role template path, Tangsible also makes
that role's variables available automatically.

Press `e` to open the template in `$VISUAL` / `$EDITOR` and render it again
after saving.

## Keyboard shortcuts

The essentials — see `Keyboard-shortcuts.md` for the complete reference
(Emacs/vim-style navigation aliases, expand/collapse-all, autoscroll
control, and more).

**Main tree**

| Key | Action |
|---|---|
| `Enter` / `Space` | Toggle a task's host rows, or open a host's result |
| `←`/`→` | Collapse / expand a task |
| `C`/`E` | Collapse / expand all |
| `n`/`p` | Jump to the next / previous task |
| `F`| Follow new tasks | 
| `f`| Filter: All / Changed / Failed |
| `/` | Search filter (task name, source, output) |
| `r` | Open the re-run dialog (once the run has finished) |
| `q`/`Ctrl-C` | Interrupt the run, or quit once it's finished |

**Drill-down view**

| Key | Action |
|---|---|
| `Tab` / `Shift Tab` | Switch between tabs | 
| `n`/`p` | Next / previous task (same host) |
| `←`/`→` | Previous / next host (same task) |
| `Esc`/`Enter` | Close, back to the tree |

**Template view**

| Key | Action |
|---|---|
| `Tab` / `Shift Tab` | Switch between tabs | 
| `n`/`p` | Next / previous task (same host) |
| `←`/`→` | Previous / next host (same task) |
| `q`/`Ctrl-C` | Quit |

Mouse navigation is supported as well.

## Configuration

When no playbook is given on the command line, Tangsible resolves one
from, in order:

1. `$TANGSIBLE_PLAYBOOK`
2. `.tangsible` in the current directory
3. `$XDG_CONFIG_HOME/tangsible/config.toml` (default:
   `~/.config/tangsible/config.toml`)
4. `./site.yml`, if present

`.tangsible` and `config.toml` share the same TOML format:

```toml
[general]
default_playbook = "myplaybook.yml"
default_tree_state = "collapsed"
```

The project-local `.tangsible` also stores invocation history used by
tangsible rerun.

Because that history can contain hostnames, tags and extra variables, you may
want to add `.tangsible` to your project's `.gitignore`.

## Current limitations

Tangsible is currently aimed at the common interactive use case rather than
complete `ansible-playbook` feature parity.

- **`strategy: free`** is not supported. Tangsible's current data model
  assumes a shared task progression across hosts, whereas `free` allows each
  host to progress independently.
- **Interactive input after the TUI starts is not supported**. For example, a
  bare pause: task does not wait for input. Timed pauses work normally.
  `--ask-become-pass` and `--ask-vault-pass` work normally because Ansible
  handles those prompts before Tangsible takes over the terminal.
- **Scale**: Tangsible is developed and tested primarily against relatively
  small development inventories. It is not intended to be a fleet-management
  interface.
- **CLI parity**: only a limited subset of ansible-playbook options is
  interpreted specially by Tangsible; other arguments are passed through to
  Ansible.

## Development

Tangsible is written in Go.

```
go build ./...   # build
go vet ./...     # lint
go test ./...    # unit tests
```

There are also end-to-end smoke tests that run the real binary inside a
`tmux` pane:

```
go test -tags e2e ./...
```

These require `tmux` and `ansible-playbook` and are excluded from the normal test run.

## License

Apache-2.0 — see `LICENSE`.
