# Tangsible

**A live terminal UI for `ansible-playbook`, for the person actually
watching it run.**


Tangsible wraps `ansible-playbook` in a live, navigable tree of plays → tasks
→ hosts, color-coded by outcome. It let's you efficiently monitor running playbooks and drill into any task results, outputs, variable definitions etc. without loosing your place in the run.

You can rerun playbooks or parts of it as well as revisit and compare previous runs.

In addition tangsible provide useful functionality to debug Jinja2 templates, edit individual encrypted variables and analyze host information.

![Tangsible demo](assets/demo.gif)

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
| Ansible Navigator | Complex functionality, images, collections, execution environments | Focus on playbooks, intuitive UI, provides all the necessary information to easily run and debug playbooks |
| AWX / Ansible Tower / Semaphore | A full orchestration platform: scheduling, RBAC, web UI, job history, teams | Built for one person at a terminal, not a service to deploy — a single static binary with no install beyond it |

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
- **Revisit** all results and outputs of a previous runs and compare differences between two runs.

### Develop Ansible

Tangsible provides a couple of shortcuts for common development tasks that
normally require writing temporary playbooks.

- **Run / test a role in isolation** with `tangsible role <role>`.
- **Debug a Jinja2 template** with `tangsible template <path>`, using Ansible's own templating against a real inventory host. Press `e` to edit the template and automatically render it again after saving.
- **Easily edited individual encrypted variables** without copy / pasting encrypted strings or typing secrets on the command line with `tangsible vault`.
- A **host-centric view** about every relevant information about a particular host with `tangsible host <hostname>` &ndash; facts, groups, plays, host_vars


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

Or you can simply do
```
go install code.aw.net/claude/tangsible@latest
```
to install it in `$GOPATH/bin` (usually `~/go/bin`).

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

## Revisit and compare previous runs

Pick a previous run with
```
tangsible revisit
```

and analyze / drill down into all results and outcomes in the same ways as if it just ran.

Press `d` after a run or revisit to get a visuall diff of its outcomes compared to a previous run.

## Running a role in isolation

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

## Encrypt individual variables

`ansible-vault`either allows you to encrypt a whole file via `edit` or `encrypt`- which will result in huge diff on every change or you can create individually encrypted variables via `encrypt_string` which involves specifying secrets on the command line, copy/pasting encrypted strings and decryption is even more messy.

```
tangsible vault <filename>
```

provides an `ansible-vault edit` like experience but operates on individual variables. One can simply add / edit / remove encrypted variables in the text editor while diffs stay minimal.

## Analyze hosts

```
tangsible host hostname [playbook.yml] [ansible-playbook args...]
```

Provides in-depth information about a particular:
- FQDN, IP addresses, OS, architecture, etc.
- All groups a host belongs to (directly or indirectly) - i.e. a host-centric view compared to the group-centric view of inventory.yml
- All plays that will be executed for that host
- host_vars
- all collected facts

Browse through all hosts with
```
tangsible hosts
```

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
| `n`/`N` | Jump to the next / previous task |
| `F`| Follow new tasks | 
| `f`| Filter: All / Changed / Failed |
| `/` | Search filter (task name, source, output) |
| `r` | Open the re-run dialog (once the run has finished) |
| `q`/`Ctrl-C` | Interrupt the run, or quit once it's finished |

**Drill-down view**

| Key | Action |
|---|---|
| `Tab` / `Shift Tab` | Switch between tabs | 
| `e` | Edit task file |
| `n`/`N` | Next / previous task (same host) |
| `←`/`→` | Previous / next host (same task) |
| `/` | Search this tab's text; `n`/`N` for next/previous match while a search is active |
| `Esc`/`Enter` | Close, back to the tree |

**Template view**

| Key | Action |
|---|---|
| `Tab` / `Shift Tab` | Switch between tabs | 
| `e` | Edit template file |
| `h` | Change host |
| `/` | Search this tab's text; `n`/`N` for next/previous match while a search is active |
| `q`/`Ctrl-C` | Quit |

Mouse navigation is supported as well.

`tangsible hosts`/`tangsible host`'s own detail view and `tangsible diff`'s
own drill-down aren't tabulated separately here, but support the same `/`
in-tab search shown above.

## Configuration

When no playbook is given on the command line, Tangsible resolves one
from, in order:

1. `$TANGSIBLE_PLAYBOOK`
2. `.tangsible/config.toml` in the current directory
3. `$XDG_CONFIG_HOME/tangsible/config.toml` (default:
   `~/.config/tangsible/config.toml`)
4. `./site.yml`, if present

`.tangsible/config.toml` and the global `config.toml` share the same TOML
format:

```toml
[general]
default_playbook = "myplaybook.yml"
default_tree_state = "collapsed"
```

Invocation history used by `tangsible rerun` lives in .tangsible/state.toml
and should best be gitignored.

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
