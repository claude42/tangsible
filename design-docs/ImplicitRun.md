# Implicit `run`

Status: **spec** (not implemented). Includes the flag-before-playbook rule
("Options before the playbook").

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
the first positional argument (`SplitPlaybookArgs`, see "Options before the
playbook"), else it resolves through
`TANGSIBLE_PLAYBOOK` / `default_playbook` / `./site.yml` (`ResolvePlaybook`);
`--dialog`/`--no-dialog`, `--ee`/`--no-ee` and `--start-at-play` work as
for `run`. History, run logs and `rerun` do not tell the forms apart.

## Options before the playbook

Today the playbook has to come first: `SplitPlaybookArgs` treats an `args[0]`
starting with `-` as "no playbook given", so everything after it, a playbook
included, is passthrough. `tangsible run --ee img site.yml` therefore resolves
the default playbook, strips `--ee img` and appends the typed `site.yml` to
it: ansible-playbook gets `site.yml site.yml` and plays it twice (any flag
first does the same, `-i inv site.yml` too). With an implicit `run` (rule 2)
a flag-first invocation becomes ordinary, so this is fixed here, not later.

The rule: **the playbook is the first argument that is neither a flag nor a
flag's value.** `SplitPlaybookArgs` walks `args` from the left:

- `--` ends the options: the next argument is the playbook, the rest
  passthrough (the `--` itself is dropped).
- `--name=value` is one argument, skipped.
- `--name` (long) or `-x` (short) that *takes a value* is skipped together with
  the next argument; one that takes none is skipped alone.
- A short cluster (`-vvv`, `-Kk`, `-ei`): letters are switches until one that
  takes a value; if it is the last letter, the next argument is its value, if
  letters follow it they are the value (`-ifile`, as ansible's argparse reads
  it).
- Anything else is the playbook. Arguments before it keep their order in
  `rest`, arguments after it follow them, so ansible-playbook sees every
  flag once, in the order typed, and the playbook once.
- No such argument: no playbook given (`explicit` false), `rest` is all of
  `args`, as today.

`tangsible run site.yml -i hosts` is unchanged (the first argument is the
playbook), as is every invocation that has no flag before the playbook.

**Knowing which flags take a value.** Tangsible's own flags (`--ee` takes one;
`--no-ee`, `--dialog`, `--no-dialog` none; `--start-at-play` one) are known in
code. For `ansible-playbook`'s the table is a static list, one entry per
flag, taken from `ansible-playbook --help`: every one is a switch or takes
exactly one value (`--tags`, `-i`, `-e`, `-l`, `-u`, `--vault-id`, ... take
one, repeating is allowed; `-v`, `--check`, `--diff`, `-K`, `-k` take none).
Choices made:

- **A flag not in the table is a switch.** That is what the old rule assumed
  for everything, so an unknown (new) flag with a value before the playbook is
  the one case that misreads, `--newflag val site.yml` takes `val` for the
  playbook. A test compares the table with the installed
  `ansible-playbook --help` (skipped without ansible) so the drift is caught
  when the table is updated, not by a user.
- **No runtime parsing of `--help`** in v1: one more process before every
  start for a table that changes about once per ansible-core release. If drift
  proves a problem, the same parser the test uses can feed a cached table
  later.
- **The verbs have one table each**, because they call different tools. v1
  covers the verbs that spawn `ansible-playbook` and take a playbook: `run`,
  `rerun`, `role`, `debug`, and (their passthrough is the same ansible-playbook
  options) `host` (hostname first, then the playbook, each found by the walk),
  `hosts`, `inventory` and `ee verify` (after its own flags are removed),
  and `template` (path, then host spec) and `vault` (the file; the
  ansible-playbook table already has `--vault-password-file` as a
  value-taking flag, `--ask-vault-pass` as a switch). `revisit` (its first
  positional is a log or run, not a playbook; it keeps the old rule) stays as
  is. `ee init`, `build` and `push` parse their own flags in one loop that
  accepts them anywhere (`--base` and `-f`/`-t` consume their value), so
  they already behave this way.
  `rerun` and `debug` have their own first-positional meaning, which the
  implementation checks against the new rule (`rerunresolve.go`,
  `debugverb.go`).
- **Synthetic flags are stripped first** (`--dialog`, `--no-dialog`, `--ee X`,
  `--no-ee`, `--start-at-play X`), as the code already does elsewhere, so
  they never reach the walk as unknowns; their values cannot be mistaken for
  the playbook.

A value that looks like a playbook is still a value (`-e site.yml` is an extra
variable file name; `-i a.yml site.yml` is an inventory and a playbook), and
the playbook can still be an extensionless name (`tangsible run -i hosts site`),
since the rule is positional, not by extension. The `.yml` test in "The rule"
is only for the verb-less form's first argument.

Examples, under implicit `run` or after `run`:

```
tangsible run --ee img site.yml      # playbook site.yml, passthrough: none (--ee is ours)
tangsible -i hosts site.yml          # playbook site.yml, passthrough: -i hosts
tangsible -i hosts -vv               # no playbook: default_playbook / site.yml, passthrough: -i hosts -vv
tangsible -e @vars.yml -- site.yml   # playbook site.yml, passthrough: -e @vars.yml
tangsible --check --diff site.yml    # playbook site.yml (switches skipped)
```

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

- `internal/config/resolve.go`: `SplitPlaybookArgs` takes the walk above (a
  per-verb table of value-taking flags; a new small file next to it holds
  `ansible-playbook`'s). `ParseVerb` returns the implicit `run` (verb
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

- `TestSplitPlaybookArgs`: playbook first (unchanged); `--ee img site.yml`,
  `--ee=img site.yml`, `-i hosts site.yml`, `-ihosts site.yml`, `-vvv site.yml`,
  `-vi hosts site.yml`, `-e a=1 -e b=2 site.yml`, `--check --diff site.yml`,
  `-- site.yml`, only flags (no playbook), `-e site.yml` alone (value, no
  playbook), an extensionless playbook, `rest` order, the unknown-flag case.
  The table-versus-`--help` test (skipped without ansible).
- e2e: `tangsible run --ee img site.yml` starts one play, not two (the
  command line in the `o` view names the playbook once).
- `TestParseVerb`: no args, `site.yml`, `SITE.YAML`, `-i`, `--dialog`,
  `--version`, a verb still a verb, `rrun` and `site` still rejected, a
  verb-named file after the verb (`run host`).
- Dialog decision: bare passes `verbDefault = true`; `never` wins; an explicit
  `--no-dialog` on an otherwise implicit form wins.
- e2e (tmux): bare `tangsible` in a directory with a `site.yml` shows the
  dialog and starts nothing before Enter; `tangsible testdata/outcomes.yml -i
  localhost,` runs like `run`.
