# Implicit `run`

Status: **spec** (not implemented).

## Goal

`tangsible` without a verb behaves like `tangsible run`, so the common case is
shorter to type:

```
tangsible site.yml -i hosts        # = tangsible run site.yml -i hosts
tangsible -i hosts --limit web1    # = tangsible run -i hosts --limit web1
tangsible                          # = tangsible run --dialog
```

Nothing that works today changes: every invocation that is valid now has a
verb first, and a first argument that is not a verb is a usage error now
(`ParseVerb`, exit 2), so nothing valid depends on that path.

## The rule

`config.ParseVerb` keeps recognising the verbs first. When `args[0]` is not a
verb, the invocation is an implicit `run` if:

1. there are no arguments at all (bare `tangsible`, see below), or
2. `args[0]` starts with `-` (a flag; `-h`/`--help` are handled before, and
   stay tangsible's own, `--version`/`-V` see below), or
3. `args[0]` ends in `.yml` or `.yaml` (case-insensitive).

Anything else (`tangsible rrun site.yml`, `tangsible site`) is still the usage
error. Reasons:

- **A verb typo must not turn into a playbook.** If every unknown word were a
  playbook, `rrun` would reach ansible-playbook and fail with "the playbook:
  rrun could not be found", a worse message than the usage line.
- **New verbs cannot collide.** A verb never contains a dot, so a playbook
  named `deploy.yml` can never become ambiguous when a verb called `deploy` is
  added. An extensionless playbook still needs `tangsible run name`.
- **A file named like a verb** (`run`, `host`, ...) needs `tangsible run host`:
  the verb wins. That is the documented, rare case.

The implicit form is exactly `run` with `args` unchanged: the playbook is
`args[0]` when it is not a flag (`SplitPlaybookArgs`), else it resolves through
`TANGSIBLE_PLAYBOOK` / `default_playbook` / `./site.yml` (`ResolvePlaybook`);
`--dialog`/`--no-dialog`, `--ee`/`--no-ee` and `--start-at-play` work as
for `run`. History, run logs and `rerun` do not tell the forms apart.

## Bare `tangsible` opens the run dialog

A bare `tangsible` is `run` with the dialog opened first (the dialog `rerun`
shows, `RerunDialog.md`): nothing starts until the user confirms it. A run
starts a playbook, which can change things, and a bare command is the one form
that is easy to type by accident, so it asks first. The dialog is the existing
one, with the default playbook and no extra arguments filled in.

How it is decided: bare `tangsible` passes `verbDefault = true` to
`ResolveDialogVisibility` instead of `run`'s `false`. So `run_dialog = "never"`
still turns the dialog off (a user who set that gets a plain run), and
`always` is the same as the default here. Only the no-argument form is
affected; `tangsible -i hosts` and `tangsible site.yml` start at once, as
`run` does.

When no playbook can be determined (no `site.yml`, no `default_playbook`, no
`$TANGSIBLE_PLAYBOOK`), bare `tangsible` prints `run`'s own error ("no playbook
given, and none could be determined from ...") with the top-level usage line,
not a dialog for nothing.

## `--version`

`--version` and `-V` as a first argument are the `version` verb (today they
are a usage error; under rule 2 they would otherwise reach ansible-playbook and
print Ansible's version, which is confusing). After a verb they stay
ansible-playbook's own.

## What changes

- `internal/config/resolve.go`: `ParseVerb` returns the implicit `run` (verb
  `run`, `rest` = all args, `ok` true) under the rules above, plus a way for
  `session.Main` to know the invocation was bare (a third result or a separate
  `IsBareInvocation(args)`), so it can pass `verbDefault = true`.
- `internal/session/main.go`: the `VerbRun` branch uses that for its dialog
  decision; the usage error remains for everything else.
- `internal/config/help.go`: the top-level usage and `help` text say the verb
  is optional for `run` and that bare `tangsible` opens the dialog;
  `help_test.go` keeps help, man page and verbs consistent.
- `completions/tangsible.bash`, `completions/tangsible.fish`: the first
  argument completes `*.yml`/`*.yaml` next to the verbs; after an implicit
  playbook or flag, completion behaves as after `run`.
- `man/tangsible.1(.scd)`, `README.md`, `Changelog.md`, `CLAUDE.md` (verb
  paragraph), `ConfigOptions.md` (`run_dialog`: bare `tangsible` shows it).

## Tests

- `TestParseVerb`: no args, `site.yml`, `SITE.YAML`, `-i`, `--dialog`,
  `--version`, a verb still a verb, `rrun` and `site` still rejected, a
  verb-named file after the verb (`run host`).
- Dialog decision: bare passes `verbDefault = true`; `never` wins; an explicit
  `--no-dialog` on an otherwise implicit form wins.
- e2e (tmux): bare `tangsible` in a directory with a `site.yml` shows the
  dialog and starts nothing before Enter; `tangsible testdata/outcomes.yml -i
  localhost,` runs like `run`.
