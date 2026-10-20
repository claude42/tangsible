# Tangsible

**A live terminal UI for `ansible-playbook`, for those actually
watching it run.**

Tangsible turns the rapidly scrolling output of ansible-playbook into a live,
navigable tree of plays → tasks → hosts. You can inspect results,
stdout/stderr, diffs and variables without losing your place - even while the
playbook is still running.

You can also rerun whole playbooks or parts of them as well as revisit and
compare previous runs.

In addition, tangsible provides useful functionality for debugging Jinja2
templates, editing individual encrypted variables and analyzing host
information.

![Tangsible demo](assets/demo.gif)

## Why

Running `ansible-playbook` directly is painful when you're actually
watching it, not just kicking it off and walking away:

- The output scrolls by faster than you can read it - reconstructing "which
  hosts succeeded, which failed, and why" after the fact means scrolling
  back through a terminal or grepping a log file.
- A failing task's real error is usually buried several screens up, mixed
  in with everything that ran fine.
- There's no live overview - you can't tell at a glance how far along a
  multi-host, multi-task run is, or which hosts have diverged from the
  rest, until it's already finished (or you've lost the scrollback).

Tangsible turns that stream of text into a structure you can navigate.  You
can inspect earlier results while later tasks are still executing. Once
something fails, you can jump straight to it. You can view the task as
written, its output, and the full result, side by side, instead of spread
across your scrollback.

## Who it's for

Anyone running `ansible-playbook` **by hand, repeatedly, against development,
staging, or otherwise interactive environments** - the edit-run-inspect-rerun
loop of writing or debugging a playbook or role, not unattended automation:

- Developers and platform engineers iterating on playbooks/roles against
  dev or staging boxes.
- Anyone who currently keeps a second terminal open just to `tail` a log
  or scroll back to find where things went wrong.
- Developers and operators working with relatively small inventories -
  Tangsible isn't intended as a fleet-management UI.

It is *not* aimed at CI pipelines, scheduled/unattended runs, or
orchestrating jobs across a team. For those purposes, other tools will be a
better fit than Tangsible ever intends to be.

## Why not ansible-navigator?

[ansible-navigator](https://github.com/ansible/ansible-navigator/) is a great
tool. It provides a comprehensive TUI for Ansible - including things like
browsing inventory, collections or configuration options.

Unfortunately ansible-navigator didn't solve my particular problems when
developing and debugging playbooks, roles and templates. Because of this,
**Tangsible** focuses on workflows such as navigating tasks and hosts,
inspecting diffs and resolved variables, or revisiting and comparing previous
runs. It also lets you debug Jinja2 templates and inspect host variables.

I designed Tangsible's UI around the information and options I need for these
specific tasks.  I'm eager to hear if it also fits your requirements or what
changes might be necessary.

## Features

### Run and inspect

- **Live updates** - the TUI appears immediately and updates while the playbook
  is still running. It even shows a progress bar!
- **Tree view** - plays, tasks and hosts are shown in a navigable tree, with
  outcomes such as OK, Changed, Skipped, Failed and Unreachable are clearly
  visible.
- **Drill-down** - inspect a host/task result with the task source, output,
  stderr, --diff output and full JSON result in one place.
- **Filtering and search** - show only Changed or Failed results, or search task
  names, source and output.
- **Keyboard and mouse navigation** - use the keyboard or mouse throughout the
  TUI.
- **Desktop integration** - desktop notifications, progress visualization in
  the window title, copy output to clipboard.

### Iterate

Tangsible is designed around the development loop of finding something wrong,
changing it and trying again.

- **Re-run from the TUI** - rerun the whole playbook or start at a specific play, 
  or limit tags or only failed hosts in subsequent runs
- **Edit tags and hosts** before rerunning.
- **`tangsible rerun`** - open the same rerun workflow directly from the command
  line, pre-filled from the previous invocation.
- **Revisit** all results and outputs of previous runs and compare differences between two runs.

### Develop Ansible

Tangsible provides a couple of shortcuts for common development tasks that
normally require writing temporary playbooks.

- **Run / test a role in isolation** with `tangsible role <role>`.
- **Set breakpoints and step through a playbook**, inspecting variables at each pause, with `tangsible debug <playbook>`.
- **Debug a Jinja2 template** with `tangsible template <path>`, using Ansible's own templating against a real inventory host. Press `e` to edit the template and automatically render it again after saving.
- **Easily edit individual encrypted variables** without copy / pasting encrypted strings or typing secrets on the command line with `tangsible vault`.
- A **host-centric view** of all relevant information about a particular host with `tangsible host <hostname>` &ndash; facts, groups, plays, host_vars
- A **tree view of the whole inventory** with `tangsible inventory` &ndash; groups as collapsible nodes, hosts as leaves, similar to `ansible-inventory --graph`

## Requirements

- Go 1.24+ to build.
- `ansible-playbook`
- A real terminal (TTY) - Tangsible opens a full-screen UI and cannot run in
  a piped or headless shell.

Tangsible ships its own bundled ansible callback plugin to receive Ansible
events while a playbook runs, so no extra collection install is needed -
see [License](#license) for the (GPL-3.0) plugin's own licensing.

## Install

### With one line

Interactive:
```
curl -fsSL https://gettangsible.aw.net/ | sh
```

Unattended:
```
curl -fsSL https://gettangsible.aw.net/ | sh -s -- --yes
```

Will install into the users home directory (i.e. `~/.local/bin`,
`~/.local/share`) but will also honor `--prefix`.

### From a binary archive

Go to [Releases](https://code.aw.net/claude/tangsible/releases) and download
a binary archive and install via `install.sh`.

### Via go install

Use

```
go install code.aw.net/claude/tangsible@latest
```

to install it in `$GOPATH/bin` (usually `~/go/bin`). This installs only the
binary, not the bundled callback plugin. Fetch `callback/tangsible_jsonl.py`
from this repo and either place it at `$XDG_DATA_HOME/tangsible/` (default
`~/.local/share/tangsible/`) or point `$TANGSIBLE_CALLBACK_DIR` at it.
`tangsible version` reports whether it was actually found.

### From source

```
git clone https://code.aw.net/claude/tangsible
cd tangsible
go build ./...
```

This produces a `tangsible` binary in the current directory. You can use the
provided `install.sh` to install it.

### System-wide, or as a distro package

`install.sh --prefix /usr/local` (run as root) installs tangsible for every
user on a machine without building a package. If you're packaging tangsible
for a distro instead (`.deb`/`.rpm`/AUR/nix/...), see `PACKAGING.md` for the
file layout and why `install.sh` isn't the right tool for that job.

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

Once a run has finished - successfully, with failures, or interrupted — press
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

and analyze / drill down into all results and outcomes in the same ways as if
it just ran.

Press `d` after a run or revisit to get a visual diff of its outcomes
compared to a previous run.

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


## Debugging a playbook (breakpoints)

```
tangsible debug [playbook.yml] [--all-vars|-a] [ansible-playbook args...]
```

This is the same live tree, two-pane layout, and tabbed drill-down as a
normal run - not a separate screen - with breakpoints layered on top. It
shows every play and task up front (so breakpoints can be armed before
anything has run), lets you set them directly on the tree, then run or
step through the playbook: `F5` runs to the next armed breakpoint (or to
completion with none set), `F9` arms/disarms a breakpoint on the selected
task (or click its row), `F10` skips the rest of the current task's hosts
and stops at the next task's first one, and `F11` steps to the very next
pause regardless of breakpoints (host by host, within the same task
included). Built on ansible-core's own task debugger (`debugger:
always`), spliced onto a disposable copy of the playbook's directory -
the real files on disk are never touched.

Whenever a pause surfaces, the drill-down opens automatically for that
task and host, with a working Resolved tab - unlike a normal run's own
(disabled) one, it fills in every variable reference from that task's own
captured state, including `when:`/`failed_when:`/`changed_when:`/`until:`
conditions and a `register:` target's own result.

By default, variables are only captured at a pause actually shown to you
(an armed breakpoint, a step, or a task failure), so the Resolved tab
works for that task, not yet for one you haven't stopped at.
`--all-vars`/`-a` captures every task's own variables as it's reached
instead, so the Resolved tab works no matter which task you later
navigate back to - at the cost of that capture's own real time on every
task, whether or not the run ever stops there.

`v` opens a variable watch explorer: a text field with live autocomplete
(name and current value, from whichever host/task is currently selected)
for adding a watched expression - a bare name, or a dotted/bracket-index
path into one, like `foo.bar` or `foo[0]`. Watched values stay visible
and re-evaluate live as you navigate to a different host/task, greyed
out once you close the explorer with Esc; one that can't currently be
resolved shows as `<undefined>` rather than as an error. The explorer's
own bottom-of-screen keyboard hints replace the usual ones while it's
open, and its "Watched variables" headline is colored like the rest of
the debug chrome.

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

`ansible-vault` either allows you to encrypt a whole file via `edit` or
`encrypt`- which will result in huge diff on every change or you can create
individually encrypted variables via `encrypt_string` which involves
specifying secrets on the command line, copy/pasting encrypted strings and
decryption is even more messy.

```
tangsible vault <filename>
```

provides an `ansible-vault edit` like experience but operates on individual
variables. One can simply add / edit / remove encrypted variables in the text
editor while diffs stay minimal.

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

Or browse the whole inventory as a tree (groups as collapsible nodes, hosts
as leaves, similar to `ansible-inventory --graph`) with
```
tangsible inventory [playbook.yml] [ansible-playbook args...]
```
Opening a host from the tree shows the exact same detail view as `tangsible hosts`.

## Keyboard shortcuts

The essentials - see `Keyboard-shortcuts.md` for the complete reference
(Emacs/vim-style navigation aliases, expand/collapse-all, autoscroll
control, and more).

**Main tree**

| Key | Action |
|---|---|
| `Enter` | Toggle a task's host rows, or open a host's result |
| `←`/`→` | Collapse / expand a task |
| `C`/`E` | Collapse / expand all |
| `n`/`N` | Jump to the next / previous task |
| `F`| Follow new tasks | 
| `f`| Filter: All / Changed / Failed |
| `/` | Search filter (task name, source, output) |
| `r` | Open the re-run dialog (once the run has finished) |
| `o` | Show ansible-playbook's console output (`F` follows, `Esc` returns) |
| `?`/`F1` | Help: all of the tree view's shortcuts |
| `q`/`Ctrl-C` | Interrupt the run, or quit once it's finished |

**Drill-down view**

| Key | Action |
|---|---|
| `Tab` / `Shift Tab` | Switch between tabs | 
| `e` | Edit task file |
| `n`/`N` | Next / previous task (same host) |
| `←`/`→` | Previous / next host (same task) |
| `/` | Search this tab's text; `n`/`N` for next/previous match while a search is active |
| `?`/`F1` | Help: all of this view's shortcuts |
| `Esc`/`Enter` | Close, back to the tree |

**Debug mode additions**

The exact same Main tree and Drill-down view above, with five extra keys
layered on (no separate "debug view" of its own):

| Key | Action |
|---|---|
| `F5` | Run - to the next armed breakpoint, or to completion with none set |
| `F9` | Arm / disarm a breakpoint on the selected task (or click its row) |
| `F10` | Next task - skip the rest of this task's hosts, stop at the next task's first one |
| `F11` | Step - stop at the very next pause, armed or not (host by host within the same task too) |
| `v` | Open the variable watch explorer |

**Template view**

| Key | Action |
|---|---|
| `Tab` / `Shift Tab` | Switch between tabs | 
| `e` | Edit template file |
| `h` | Change host |
| `/` | Search this tab's text; `n`/`N` for next/previous match while a search is active |
| `?`/`F1` | Help: all of this view's shortcuts |
| `q`/`Ctrl-C` | Quit |

Mouse navigation is supported as well. Every view has `?`/`F1` help listing
its shortcuts (F1 only in the re-run dialog).

## Configuration

When no playbook is given on the command line, Tangsible resolves one
from, in order:

1. `$TANGSIBLE_PLAYBOOK`
2. `.tangsible/config.toml` in the current directory
3. `$XDG_CONFIG_HOME/tangsible/config.toml` (default:
   `~/.config/tangsible/config.toml`)
4. `/etc/tangsible/config.toml`, an admin-set system-wide default
5. `./site.yml`, if present

The project, global and system `config.toml` files all share the same TOML
format, and every `[general]` key cascades project → global → system (the
more specific tier wins only when it actually sets that key):

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

- **Execution environments** are so far not (yet) supported; neither is **ansible-runner**.
- **Interactive input after the TUI starts is not supported** in `run`/`rerun`/`role`. For example, a bare pause: task does not wait for input, and `--step` will not work. (`tangsible debug` is the dedicated way to drive ansible-core's own task debugger - see "Debugging a playbook" above; it isn't available from inside the regular live tree.)
  
  Timed pauses work normally.
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

`go build`/`go run .` alone won't have the bundled callback plugin installed,
so a plain build from source needs `$TANGSIBLE_CALLBACK_DIR` pointed at the
repo's `callback/` directory to run a playbook, e.g.:

```
TANGSIBLE_CALLBACK_DIR="$PWD/callback" go run . run site.yml
```

There are also end-to-end smoke tests that run the real binary inside a
`tmux` pane:

```
go test -tags e2e ./...
```

These require `tmux` and `ansible-playbook` and are excluded from the normal
test run.

## License

The `tangsible` binary/CLI is **Apache-2.0** licensed - see `LICENSE`.

The bundled `callback/tangsible_jsonl.py` Ansible callback plugin is a
**GPL-3.0-or-later** derivative of `ansible.posix.jsonl` - see
`callback/LICENSE`.

## Contact

`tangsible@aw.net`
