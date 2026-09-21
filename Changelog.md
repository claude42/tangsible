# Changelog

## [Unreleased]

### Added

- Re-run dialog: three new checkboxes - "Resume where failed", "Only failed",
  "Only unreachable" - to limit or resume a rerun against hosts that failed
  or were unreachable in the previous run.
- `rerun` verb: matching `--only-failed`, `--only-unreachable`,
  `--resume-where-failed` flags to pre-fill/pre-check those checkboxes.
- `y` copies the currently active tab's content to the clipboard (via OSC 52)
- Terminal notifications on playbook finish / task failure
- New drill-down tab: File displays the contents of the file associated to
  the task

### Changed

- Drill-down: the Resolved tab is now hidden entirely when there's genuinely
  nothing to show, instead of an empty tab.

### Fixed

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

### Removed

- Re-run dialog: dropped the "Start with task" field.

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

[Unreleased]: https://code.aw.net/claude/tangsible/compare/v0.1.3...HEAD
[0.1.3]: https://code.aw.net/claude/tangsible/compare/v0.1.2...v0.1.3
[0.1.2]: https://code.aw.net/claude/tangsible/compare/v0.1.1...v0.1.2
[0.1.1]: https://code.aw.net/claude/tangsible/compare/v0.1.0...v0.1.1
[0.1.0]: https://code.aw.net/claude/tangsible/src/tag/v0.1.0
