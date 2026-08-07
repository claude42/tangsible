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
git clone https://codeberg.org/claude42/tangsible.git
cd tangsible
go build ./...
```

This produces a `tangsible` binary in the current directory.

## Usage

```
tangsible [<playbook.yml>] [ansible-playbook args...]
```

Anything after the playbook is passed straight through to
`ansible-playbook` — inventory, tags, limits, verbosity, whatever you'd
normally pass. For example:

```
tangsible site.yml -i inventory.ini --limit webservers -t deploy
```

If you omit the playbook, Tangsible tries to figure out which one you
meant — see Configuration below. If it can't, it prints a usage message
and exits.

To try it out without a real inventory, the bundled fixtures under
`testdata/` are safe to run locally:

```
tangsible testdata/outcomes.yml -i localhost,
tangsible testdata/multihost.yml -i testdata/multihost-inventory.ini
```

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

`.tangsible` and `config.toml` share the same minimal TOML format:

```toml
[general]
default_playbook = "myplaybook.yml"
```

## Current limitations

This is a v1, aimed at the common interactive case rather than full
`ansible-playbook` parity:

- No run history — each run is its own session.
- No interactive prompts yet (`--ask-become-pass`, vault passwords,
  `pause` tasks).
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
```

## License

GPL-2.0 — see `LICENSE`.
