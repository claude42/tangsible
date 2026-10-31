# Default Project

Status: idea, discussed but not planned or built. Nothing in the code
implements any of this yet.

## Situation

Tangsible is directory-based: `ansible.cfg`, a relative inventory,
`./.tangsible/config.toml`, history (`state.toml`) and saved runs
(`runs/`) are all found relative to the directory it's started in, and the
playbook defaults to `./site.yml`. Started anywhere else, `tangsible run`
ends in a usage error.

`default_playbook` (`ConfigOptions.md`) doesn't fix that. A relative value
(`site.yml`) just repeats the `./site.yml` fallback; an absolute value
always runs the same playbook but leaves the rest of the working directory
as it was (Ansible still reads `ansible.cfg` and relative inventory paths
from the current directory, tangsible's history and saved runs are the
current directory's).

## Motivation

The user can start tangsible from any directory, and if the current
directory isn't a project, tangsible uses a standard one.

## Idea

A new setting, working name `default_project`: a directory.

- If tangsible is started in a directory that is already a project (it has
  an `ansible.cfg` or a `site.yml`), or a playbook is named on the command
  line: normal behavior, `default_project` is ignored.
- Otherwise tangsible changes into `default_project` and works normally
  from there.

A `chdir` makes everything consistent at once - `ansible.cfg`, relative
inventory, the project's own `.tangsible/config.toml`, its history and its
saved runs - which an absolute `default_playbook` can't do.

## Gotchas

**Safety.** `run` can change real hosts. Today the wrong directory fails
with a usage error; with a fallback project it silently runs something.
Mitigations:

- Always show where it landed: a stderr note before the TUI starts (like the
  existing "playbook from ..." note, printed before `app.Run()` so it
  doesn't violate "never print while the TUI runs") plus the project path in
  the top bar.
- A `--here` flag (or environment variable) that turns the fallback off.
- Possibly a one-time confirmation. Works against "just start it", so only
  if the note proves not to be enough.

**What counts as "in a project".**

- Only `./ansible.cfg` counts. Ansible also reads `~/.ansible.cfg` and
  `/etc/ansible/ansible.cfg`, and the first is common enough that counting
  it would switch the feature off for many users.
- Other possible markers: `./.tangsible/config.toml`, or a
  `default_playbook` (cascaded, `ResolvePlaybook`) that resolves to an
  existing file.
- A playbook that is named on the command line but doesn't exist in the
  current directory: leaning towards an error as today, i.e. the fallback
  applies only when no playbook was named. Open question 1.

**Relative arguments break after the chdir.** `-i hosts.ini`,
`-e @vars.yml`, `--vault-password-file ./pw` and similar mean something
else once the directory changes, so the fallback would silently read the
wrong files. Options: skip the fallback when passthrough arguments contain
relative paths, resolve them against the original directory before
changing, or refuse with a message. Plain `tangsible run` (no arguments) is
the main case but not the only one.

**Config ordering is circular.** `default_project` has to come from the
global or system `config.toml`, never the project one: a project config in
the current directory means the current directory already is a project.
Tangsible would read global and system settings, decide to change
directory, then re-read everything from the new project. Settings are
currently read once early in `session.Main` and colors are applied before
any TUI exists, so this needs a two-stage read. This is the main
implementation cost; not yet investigated in `session.Main`, so how
invasive it is isn't known.

**Verbs.**

| Verb | Fallback? |
|------|-----------|
| `run`, `rerun`, `debug`, `host`, `hosts`, `inventory` | Yes. `host`/`inventory` also pick up the project's `ansible.cfg` inventory. |
| `role` | Probably yes, but its stub file is written in the cwd, which becomes the project directory - a surprise to decide on. |
| `revisit` | History comes from the new directory's `.tangsible/runs`, so it shows the project's runs. |
| `vault`, `template` | No: they take file arguments that are typically relative. |
| `version`, `help` | Never. |

**Smaller points.**

- A `default_project` that doesn't exist: a clear error or warning, not a
  silent skip.
- `~` expansion in the value, if wanted.
- Name: "project" in tangsible usually means "a directory with
  `.tangsible/`". `default_project` is fine; `fallback_project` says more
  about when it applies.

## Leaning

Do it, with these constraints:

- Global and system config only.
- Applied only when no playbook was named.
- Visible note, plus a `--here` escape hatch.

## Open questions

1. Should an explicitly named but missing playbook (`tangsible run foo.yml`)
   fall back too, or error as today?
2. Is the visible note enough for the safety concern, or should the first
   fallback ask for confirmation?
3. How to deal with relative passthrough arguments (skip the fallback,
   resolve first, or refuse)?

## If built

Per `CLAUDE.md`'s housekeeping: add the key to `ConfigOptions.md` and
`examples/config.toml`, update `completions/` and `man/` for `--here`, and
add a Changelog bullet.
