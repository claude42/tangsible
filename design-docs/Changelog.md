# Changelog

All notable changes to Tangsible are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com), and this
project adheres to [Semantic Versioning](https://semver.org) (pre-1.0: minor =
breaking, patch = features/fixes).

## [Unreleased]

## [0.1.3] - 2026-09-05

### Changed

- `tangsible version` now also reports the host OS and the ansible components in
  use (`ansible-playbook`, `ansible.posix` collection).
- `revisit`: pressing `r` on a list entry now jumps straight to the re-run
  dialog instead of re-running with the same args.
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

### Added

- Live-streaming tree view (plays → tasks → hosts), fed incrementally from the
  `ansible.posix.jsonl` callback while the playbook runs.
- Per-host outcome colouring, collapsed-row host columns with a shrink
  algorithm, and a count-summary fallback when colour isn't usable or hosts
  don't fit.
- Output drill-down with Resolved / Task definition / Docs (`ansible-doc`) /
  Diff / Details tabs; two-pane layout on wide terminals.
- In-tab text search (`/`) across every tabbed view.
- Task filtering: All / Interesting / Changed / Failed / Search.
- `run` / `rerun` verbs, interactive re-run dialog (`r`), and `--start-at-task` /
  `--start-at-play` / editable tags / hosts.
- `revisit` verb to browse and reopen previous runs.
- `host` / `hosts` / `role` / `template` / `diff` inspection verbs.
- `vault` verb: edit individually-encrypted variables in place.
- Mouse support (click, unbounded wheel panning), bash/fish shell completions,
  man pages.
- Ctrl-C / `q` forwarding that matches running `ansible-playbook` directly.
- CI and GoReleaser-driven release pipeline (Forgejo Actions).

[Unreleased]: https://code.aw.net/claude/tangsible/compare/v0.1.3...HEAD
[0.1.3]: https://code.aw.net/claude/tangsible/compare/v0.1.2...v0.1.3
[0.1.2]: https://code.aw.net/claude/tangsible/compare/v0.1.1...v0.1.2
[0.1.1]: https://code.aw.net/claude/tangsible/compare/v0.1.0...v0.1.1
[0.1.0]: https://code.aw.net/claude/tangsible/src/tag/v0.1.0
