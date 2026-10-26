# Changelog

## [Unreleased]

### Added

- Roles outside the project (`~/.ansible/roles`, `/etc/ansible/roles`, `roles_path`, ...) are now found by `role`, shown in the Task definition tab and recognized as roles by Details/Resolved/`template`; completions follow `$ANSIBLE_ROLES_PATH`
- New verb: `tangsible help [verb]` lists every verb in one line, or one verb's synopsis

### Fixed

- `tangsible vault` no longer deletes the blank lines between entries when saving

## [0.3.0] - 2026-10-24

### Added

- New verb: `tangsible inventory` - shows an interactive tree view of the
  whole inventory similar to ansible-inventory --graph
- New `debug` verb: set breakpoints on a playbook's own tasks, then run or
  step through it and inspect variables at each pause

### Fixed

- `hosts`/`inventory`: two-pane mode now behaves like `run`'s own drill-down
  view
- `hosts`/`inventory`: revisiting a host already opened this session now
  renders all five tabs instantly from cache instead of re-fetching the
  content every time.
- Drill-down: `←`/`→` at a task's first/last host now rolls into the
  previous/next task's last/first host instead of stopping there.
- `template`: role-owned templates now see `role_path` and `role_name`.

## [0.2.0] - 2026-10-08

### Added

- Re-run dialog: three new checkboxes - "Resume where failed", "Only failed",
  "Only unreachable" - to limit or resume a rerun against hosts that failed
  or were unreachable in the previous run.
- `rerun` verb: matching `--only-failed`, `--only-unreachable`,
  `--resume-where-failed` flags to pre-fill/pre-check those checkboxes.
- Re-run dialog: "Limit hosts to:" now also autocompletes inventory group
  names, not just hosts seen so far this run.
- Drill down view: `y` copies the currently active tab's content to the
  clipboard (via OSC 52)
- Terminal notifications on playbook finish / task failure
- New drill-down tab: File displays the contents of the file associated to
  the task
- Tangsible now ships its own bundled ansible callback plugin
  instead of relying on the `ansible.posix` collection's `jsonl` callback.
- Per-host task durations and total runtimes
- Handler tasks are now tagged `[Handler]` in the tree, distinguishing them
  from regular tasks.
- A task that failed with `ignore_errors: true` is now tagged `ignored` in
  the tree, and the end-of-run summary gains a matching "ignored" category.
- `strategy: free` is now supported - including showing more than one task's
  spinner at once
- ansible-playbook stderr output is now shown inline, right below the
  "Playbook failed" line
- System-wide installs via install.sh --prefix &lt;dir&gt;
- `o` shows ansible-playbook's own console output, live while it runs
- `?`/F1 opens a help dialog listing the current view's shortcuts, in every
  view (F1 only in the re-run dialog, where it also explains the fields)
- Support system-wide config file /etc/tangsible/config.toml
- `template` now accepts more than one hostname, group names or "all". Each
  host gets its own tab, colored red if its own last render errored.
- New `template_hosts_max` config key (default 10) - `template` now asks for
  confirmation before rendering against more hosts than this.
- A progress percentage is shown in the progress bar and in the terminal's
  window title while a run is live.
- `hosts`: each row in the host list now shows a green/red connectivity
  indicator based on `ansible.builtin.ping`.
- `hosts`: on a wide enough terminal, the host list and the selected host's
  detail view now open side by side instead of full-screen, with the detail
  pane following the list cursor live.
- `host`/`hosts`: new "Recent" section on the Summary tab, listing the last
  few times any playbook or role run touched this host, with per-run outcome
  counts.
- Support for customizing colors in config.toml as well as theme support and
  dark / light mode.

### Changed

- Drill-down: the Resolved tab is now hidden entirely when there's genuinely
  nothing to show, instead of an empty tab.
- Ignored failures (`ignore_errors: true`) now render in the same orange
  used for their `ignored` tag everywhere a host's outcome is shown (tree
  rows, drill-down status line, diff view), instead of alarming red.

### Fixed

- `template`: quitting while a render was running could leave
  `/tmp/tangsible-template-out-*` files behind; its scratch files are now
  also cleaned up on SIGTERM/SIGHUP (e.g. closing the terminal).
- Re-run dialog: opening the output drill-down for a failed host and then
  closing it before pressing `r` could leave the dialog's own text fields
  silently unable to accept keyboard input (typing, Tab) - mouse
  interaction and Escape still worked.
- `rerun`: "Resume where failed" failed the whole rerun outright when the
  target play had no explicit `name:` in the playbook.
- Two-paned drill-down: keyboard scrolling (arrows, Ctrl-F/Ctrl-B, Home/End)
  silently did nothing until the output pane was clicked first.
- Diff mode: clicking a tab label in the drill-down did nothing (Tab/
  Shift-Tab always worked).
- Clicking on a blank row in some dialogs leaked the click through to the
  element below it.
- Search dialog: Escape didn't close it after a focus-stealing click had
  occurred first.
- `host`/`hosts`: Summary and "Everything known" tabs now always show
  consistent information

### Removed

- Re-run dialog: dropped the "Start with task" field.
- The `ansible.posix` collection is no longer required.

## [0.1.3] - 2026-09-05

### Changed

- `tangsible version` now also reports the host OS and the ansible components in
  use (`ansible-playbook`, `ansible.posix` collection).
- `revisit`: pressing `r` on a list entry now jumps to the re-run dialog
  instead of re-running with the same args.
- Man pages are now authored in scdoc; a `Makefile` provides `build`/`install`
  targets for them.
- README / install-section rewording.

### Removed

- **Breaking:** dropped the `[vault] password_file` key from
  `.tangsible/config.toml` as a vault-password source. Use `--vault-password-file`
  or `ANSIBLE_VAULT_PASSWORD_FILE` instead.

### Security

- `vault`: scratch files used while editing individually-encrypted variables are
  now hardened against leaving plaintext on disk.

## [0.1.2] - 2026-09-01

### Added

- `install.sh`, a per-user installer (curl-pipe or run from an unpacked
  archive), now bundled into every release archive.

## [0.1.1] - 2026-09-01

### Added

- `tangsible version` verb, reporting the build stamps (version / commit / date)
  baked in at release time.
- Release workflow now mirrors the built artifacts onto the GitHub mirror
  release.

### Changed

- CI/release build moved to Go 1.27.

## [0.1.0] - 2026-08-31

Initial release. A `tview` TUI wrapper for `ansible-playbook` aimed at
interactive development use.

[Unreleased]: https://code.aw.net/claude/tangsible/compare/v0.3.0...HEAD
[0.3.0]: https://code.aw.net/claude/tangsible/compare/v0.2.0...v0.3.0
[0.2.0]: https://code.aw.net/claude/tangsible/compare/v0.1.3...v0.2.0
[0.1.3]: https://code.aw.net/claude/tangsible/compare/v0.1.2...v0.1.3
[0.1.2]: https://code.aw.net/claude/tangsible/compare/v0.1.1...v0.1.2
[0.1.1]: https://code.aw.net/claude/tangsible/compare/v0.1.0...v0.1.1
[0.1.0]: https://code.aw.net/claude/tangsible/src/tag/v0.1.0
