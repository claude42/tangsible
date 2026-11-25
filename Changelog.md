# Changelog

## [Unreleased]

### Changed

- `ee init` without `--base`/`base_image` now picks the community base image matching your host's ansible-core (the newest release of the same series if there is no exact one) instead of always the built-in default, which remains the fallback when the registry cannot be reached; `--latest` also takes the newest base image. `--no-galaxy` is replaced by `--offline`, which skips the registry lookup too

### Added

- `debug`: `r` starts over - back to the idle state with nothing run, rebuilt from the playbook and roles as they are on disk now (so edits show), breakpoints kept; the next F5/F10/F11 starts the run. `r` opens a small dialog (tags, skip tags, limit hosts) and Enter restarts; a paused run is ended by it, which the dialog says; also works in the drill-down
- `tangsible vault` supports several vault ids: `--vault-id [label@]source` (repeatable; a file, a script or `prompt`), `--encrypt-vault-id`, and `vault_identity_list`/`vault_encrypt_identity` from `ansible.cfg` (or their environment variables). A value is decrypted with the password for its label first, then any other; a changed value keeps its label. Values none of your passwords opens show as `<vaulted: label>` and are saved unchanged, so someone with only the dev password can edit a file that also holds prod secrets
- `tangsible vault` opens a file even when no given password decrypts any of its values (all show as `<vaulted: label>`), so you can add values under a second vault id; before, it failed with an HMAC error
- `tangsible vault` on a file that does not exist creates it empty, and an empty file opens in the editor instead of failing; whatever variables you add are encrypted
- `debug`: the drill-down has a Variables tab, like the one of `tangsible host`: the variables the debugger saw when the task paused (vault values masked, facts and ansible's own variables left out), sorted, each with where it was defined (file and line, `-e`, `registered`)
- Statistics screen: `c` exports the run's timing as a CSV file (a line per host and task: play, start and end time, result), named like a `revisit` export (a dialog lets you change it)

### Fixed

- Warnings and deprecations that ansible-core prints itself instead of putting them in a task result (a deprecated module such as `apt_repository`, a reserved variable name, a deprecated filter) now give the running task the ⚠ marker, count for the "Interesting" filter and show in its drill-down as "Warnings (task)"; the callback plugin reports them as a new `tangsible_message` event. A message that names its source (`Origin: file:line`) goes to the task defined there, even when ansible printed it while loading the roles, before the run started; others go to the task started last, which can be the one before the task they are about
- A task result that carries only `deprecations` (ansible-core 2.19+ reports them apart from `warnings`) now gets the ⚠ marker, counts in the recap's `warnings`, and shows the deprecation (with its removal version) in the Warnings section of the drill-down
- `ee init --resolve-packages`: the binary a `lookup('pipe', ...)` runs (e.g. in `group_vars`) is now detected and its package looked up, like a controller-side `shell` task's

### Changed

- `ee init -t/--tag <name>` (repeatable): writes the definition's `options.tags` for real instead of the commented suggestion

- `ee init --resolve-packages`: a binary several packages provide is now a commented list, one `# - package` line per candidate, so picking one means removing the `# `

## [0.5.0] - 2026-11-22

### Added

- Execution environments: `tangsible --ee <image>` can run ansible-playbook inside a container image, project files are mounted into the container. 

- `tangsible ee init [playbook] [--base image]` drafts an `execution-environment.yml` from a playbook which includes the collections and binaries its modules and roles need, the base image's package manager, Python and ansible-core, and a `# TODO` for what can't be determined statically
- `tangsible ee verify` builds an `execution-environment.yml` into an image and checks it against the playbook with `ansible-playbook --syntax-check`.
- `tangsible ee build [-f file] [-t tag ...]` builds an `execution-environment.yml` with `ansible-builder`.
- `tangsible ee push [-f file]` uploads the image names an `execution-environment.yml` lists in `options.tags` to their registry.
- `ansible.builtin.debug` output now gets repeated below the summary

### Changed

- `run`, `rerun`, `role`, `debug`, `host`, `hosts`, `inventory`, `template`, `vault`, `revisit` and `ee verify` accept options before the playbook: `tangsible run --ee img site.yml` or `-i hosts site.yml` (the playbook is the first argument that is not an option or an option's value; `--` ends the options). Before, such a command ran the default playbook and the typed one together
- A run that sends no first event within 2 s (a container being started, a slow connection) now brings up the TUI with an animated "Waiting for ansible-playbook to start" row instead of staying blank

### Fixed

- `tangsible rerun` could hang at its password prompt when the password was typed right away: the terminal-colour query, still waiting for its reply, swallowed the keystrokes (only when `-K`/`--ask-*` came from history, not the command line)
- `run` and `debug` no longer show a warning that merely repeats the error beside it when ansible-playbook fails before the playbook starts (a missing vault password file used to appear twice); a warning that adds something, like the host pattern that failed to match, is kept
- `debug`: when ansible-playbook fails before it can list the tasks the message now leads with ansible's own error instead of "couldn't predict this playbook's own task list (--list-tasks probe failed)"

## [0.4.2] - 2026-11-18

### Changed

- Release pipeline: the `.deb` and `.rpm` packages are attached to the GitHub mirror release too

## [0.4.1] - 2026-11-18

### Fixed

- Release pipeline: the Debian package upload didn't get its token. v0.4.0 was tagged but never published; v0.4.1 has its changes

## [0.4.0] - 2026-11-18

### Added

- `s` opens a statistics screen once the run has ended;
- `host`: the Summary tab's Mounts also lists ZFS and other mounts Ansible's own mount facts skip (via `mount_facts`, ansible-core 2.18+)
- `host`: new Variables tab with the host's inventory-level variables as YAML, each with where it is defined; vaulted values show as `<vaulted>`
- `host`: new Facts tab with every gathered fact as YAML
- `host`: the Plays tab shows when each play last ran on the host
- `host`: More information in the Summary tab
- `host`/`template`: tabs still loading or rendering show a busy spinner next to their placeholder text
- `role`: accepts a fully qualified collection role name (`namespace.collection.role`)
- Roles outside the projec t (`~/.ansible/roles`, `/etc/ansible/roles`, `role_path`, inside collections (`namespace.collection.role`), roles used via a play's `collections:`, roles pulled in as `meta/main.yml` dependencies or by `include_role`/`import_role` are now found too
- YAML tabs show comments (a `# ...` line or a trailing `# ...`) in the annotation colour instead of the value colour
- Task definition tab: Click on a task keyword keyword or module option will show only help
- Task definition tab (debug only): clicking on a variable shows it's value and origin
- `debug`: the variable watch shows now also shows its origin
- `debug`: vaulted values show as `<vaulted>` in the Resolved tab and the variable watch's autocomplete (the watch itself still shows them)
- `debug`: the Resolved tab shows a task's registered value inline, as `register: name = value`
- Two-pane layouts (`run` and friends, `hosts`, `inventory`): drag the divider with the mouse to resize the panes
- `color_scheme = "auto"` (the new default) picks the dark or light colour set by asking the terminal for its background colour (OSC 11)
- Progress is also shown in the terminal's own progress indicator (OSC 9;4) where supported; `terminal_progress = "auto" | "on" | "off"`
- `revisit`: `R` names a previous run; the name shows in the list before the command line
- `revisit`: `P` pins a run to the top of the list (marked `*`, exempt from the history cap)
- `revisit`: Delete / Backspace removes a previous run and its saved files (after a confirmation)
- `revisit`: `E` exports a run to one human-readable text file; `tangsible revisit file.txt` opens it
- New verb: `tangsible help [verb]` lists every verb in one line, or one verb's synopsis
- Releases include `.deb` and `.rpm` packages (amd64, arm64)

### Changed

- The bundled callback plugin is now `callback/tangsible_events.py`
- The callback plugin is compiled into the binary (extracted to `~/.cache/tangsible/` on first use): `go install` and `go run` work without setup, the release archives and `install.sh` no longer ship or install it
- `config.toml`: unknown keys and invalid values for fixed-choice `[general]` settings now print a warning after exit (they still fall back to the default)
- `debug`: the top bar no longer shows the elapsed-time clock
- `debug`: variables are captured for every task (the Resolved tab works for any task); the `--all-vars`/`-a` option is gone, and the capture is ~8x faster
- `host`/`hosts`/`inventory`: the Summary tab's Mounts show the total size first, then the free space, e.g. `/  18.7 GB (4.7 GB available)`
- Faster startup: `run`/`role`/`debug`/`rerun` ~300 ms (host groups load in the background), `run`/`role` ~250 ms (no `ansible-config` call before spawning), `role`/`template` ~250 ms (shared, early `ansible-config` call)

### Fixed

- Resolved tab: an expression using an undefined name (typically `item` in a loop) stays as written instead of failing the whole tab; the other expressions still resolve
- `debug`: a run whose only problem was unreachable hosts ends "completed - one or more hosts were unreachable", as in `run`, instead of "Playbook failed (exit code 4)"

- Two-pane drill-down: the tree pane keeps its hostnames and host-row alignment, rendering like the single-pane tree at the pane's width
- `tangsible vault` no longer deletes the blank lines between entries when saving
- The Variables tab finds the same files Ansible does: subdirectories, `.json` and extensionless files (not only `.yml`/`.yaml` directly in `host_vars/<host>/`)
- The Variables tab also finds files next to an inventory set via `ANSIBLE_INVENTORY` or `ansible.cfg` (not only `-i`)
- `debug`: a variable set in the watch explorer now reaches the redone task's arguments (F12), not only its conditions
- `debug`: `strategy: free` plays now run as linear (marked in the top bar) instead of refusing to start or misattributing tasks
- `--ask-become-pass`/`--ask-vault-pass`/`--ask-pass` no longer hang a re-run (`run`/`rerun`/`role`) or `debug`'s F5: the password is asked once at startup and handed to Ansible as a private temporary file
- `host`/`hosts`/`inventory`: the Facts tab no longer shows inventory variables when the facts come from the fact cache

## [0.3.0] - 2026-10-24

### Added

- New verb: `tangsible inventory` - shows an interactive tree view of the
  whole inventory similar to ansible-inventory --graph
- New `debug` verb: set breakpoints on a playbook's own tasks, then run or
  step through it and inspect variables at each pause

### Fixed

- Resolved tab: an expression using an undefined name (typically `item` in a loop) stays as written instead of failing the whole tab; the other expressions still resolve
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

- Resolved tab: an expression using an undefined name (typically `item` in a loop) stays as written instead of failing the whole tab; the other expressions still resolve
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

[Unreleased]: https://code.aw.net/claude/tangsible/compare/v0.5.0...HEAD
[0.5.0]: https://code.aw.net/claude/tangsible/compare/v0.4.2...v0.5.0
[0.4.2]: https://code.aw.net/claude/tangsible/compare/v0.4.1...v0.4.2
[0.4.1]: https://code.aw.net/claude/tangsible/compare/v0.4.0...v0.4.1
[0.4.0]: https://code.aw.net/claude/tangsible/compare/v0.3.0...v0.4.0
[0.3.0]: https://code.aw.net/claude/tangsible/compare/v0.2.0...v0.3.0
[0.2.0]: https://code.aw.net/claude/tangsible/compare/v0.1.3...v0.2.0
[0.1.3]: https://code.aw.net/claude/tangsible/compare/v0.1.2...v0.1.3
[0.1.2]: https://code.aw.net/claude/tangsible/compare/v0.1.1...v0.1.2
[0.1.1]: https://code.aw.net/claude/tangsible/compare/v0.1.0...v0.1.1
[0.1.0]: https://code.aw.net/claude/tangsible/src/tag/v0.1.0
