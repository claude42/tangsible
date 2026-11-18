# `tangsible ee init`: draft an execution-environment.yml

## Status

**Implemented (v1).** Decisions come from the discussion recorded in
`EETemplate.md` (the raw input: original idea, Q&A, earlier brainstorm).
This document is the settled v1 scope; `EETemplate.md` stays as history.
"Implementation notes" at the end lists where the code made a call the spec
left open.

## Problem

The stock EE images carry almost no collections (not even
`community.general`), so using an EE for real work (`ExecutionEnvironments.md`)
means building a custom image with `ansible-builder`, which needs an
`execution-environment.yml`. Most of that file is boilerplate, and the
collection list is derivable from the playbook.

## Goal

A standalone verb that writes a **draft** `execution-environment.yml`:
everything tangsible can determine is filled in, everything it can't is a
terse `# TODO`. The promise is "draft with annotated uncertainty", not a
working definition. The user reviews and extends it.

Non-goals (v1): building the image or running `ansible-builder` (not needed,
not a dependency of `ee init`; a separate `ee verify` that does build, see
"Verifying a definition"); filters and tests (v1 was modules only, lookups came
later); merging into or
updating an existing file; docker (podman only, like the rest of EE support);
Galaxy auth, private automation hub, `ansible.cfg` server settings; an
implied-dependency rules table (see "Later").

## Usage

```
tangsible ee init [playbook] [--base <image>]
```

- Playbook resolved like `run`'s (`ResolvePlaybook`: argument ->
  `$TANGSIBLE_PLAYBOOK` -> `default_playbook` -> `./site.yml`).
- Batch: runs, writes the file, prints a short summary (what was found, how
  many `TODO`s), exits. No TUI.
- Output is `./execution-environment.yml`. **If it exists, fail with an
  error and write nothing** (exit non-zero; message names the file). No
  overwrite, no merge, no second file.
- Fails with a clear message if `podman` is unavailable.
- Builder schema **v3 only** (`version: 3`).

## Base image

Lookup order: `--base <image>` flag -> `[execution_environment].base_image`
(new key, cascaded like the rest) -> built-in default
`ghcr.io/ansible-community/community-ee-base:2.21.6-1`.

**`[execution_environment].image` is deliberately never consulted.** It is
what tangsible *runs*: either a stock image (no better than the built-in
default) or the custom image built from an earlier draft. Basing a new draft
on the latter makes the EE derive from itself, and any later extension of
this feature (re-drafting, updating) would start applying things
recursively. `base_image` names the upstream explicitly.

**Built-in default:** one constant in the code, so `ee init` works with no
flags and no config. Its tag is pinned (community-ee-base tags are
`<ansible-core version>-<n>`; `2.21.6-1` was what `latest` pointed to on
2026-11-17), because a moving tag would make the inspected facts and the
draft's pins change between runs. It is a maintenance item: bump it
occasionally, and check it still has Python and pip, which ansible-builder
needs. When the default is used, the draft's header comment says so
(the base was not the user's choice) and so does the summary. `ConfigOptions.md`
documents the default next to `base_image`; the existing `latest` examples
there can stay.

The image is pulled if missing, using the existing pull logic (`runner/ee.go`,
`pull_policy`), then inspected with `podman run --rm <image> ...`:

| Fact | Used for |
|---|---|
| `/etc/os-release` | pick `package_manager_path` (dnf/microdnf/apt/...) |
| python path (symlinks resolved), version | a comment, and the `python_interpreter` stanza (`TODO` if there is none) |
| package owning that python | `rpm -qf` / `dpkg -S` in the image: `package_system` of the stanza |
| pip present? | `TODO` if absent |
| ansible-core version | a comment, and the `ansible_core` stanza (`TODO` if there is none) |

Python and ansible-core already in the base image need no key in a v3
definition (`python_interpreter` names a package to *install*, `ansible_core`
a pip requirement), so the draft states what the base has and offers
commented-out stanzas, pre-filled with the detected values, to uncomment and
adjust:

```yaml
  # The base image has Python 3.14.10 (/usr/bin/python3) and ansible-core 2.21.6.
  # python_interpreter:
  #   package_system: python3
  #   python_path: /usr/bin/python3.14
  # ansible_core:
  #   package_pip: ansible-core==2.21.6
  # ansible_runner:
  #   package_pip: ansible-runner
```

`python_path` is the interpreter with symlinks resolved. `package_system` is
the package that owns it in the image's own package database (`rpm -qf`, or
`dpkg -S` on Debian and Ubuntu): offline, no repository metadata, and for
`community-ee-base` it is `python3`, not the `python3.14` a guess from the
version would give. Where no package owns it (a python.org image keeps Python
in `/usr/local`) or the image has neither tool, `package_system` falls back to
the usual rpm name for the version (`python3.14`) and the draft says it is a
guess. Not done: `apt-file search`/`dnf provides` (what *could* be installed),
which need network and repository metadata - only useful for suggesting a
different Python than the installed one.

Inspection failures degrade to `TODO` comments, never to guessed values.

## Analysis (modules only)

1. **Collect.** Walk the playbook: plays, `tasks`, `pre_tasks`,
   `post_tasks`, `handlers`, `block`/`rescue`/`always`, `roles:`,
   `include_role`/`import_role`, role `meta/main.yml` dependencies, and the
   legacy `action:` / `local_action:` forms. Record each task's module name
   and where it was found (file:line).
2. **Resolve names to collections**, using the *host's* Ansible as the source
   of truth (e.g. `ansible-doc`, which reports the owning collection and
   follows ansible-core's redirects), honoring `collections:` on plays and
   roles for short names. An FQCN needs no lookup for its collection.
   `ansible.builtin` is dropped (it ships with ansible-core).
3. **Unresolvable** -> `# TODO` with the location:
   - a module that the host has no collection for (collection not installed
     on the host - exactly the case where the user needs an EE; a smarter
     lookup is a later iteration),
   - a dynamic module name (templated),
   - a templated `include_tasks`/`import_tasks`/`include_role` target.
4. **`requirements.yml`**: if the project has one (`requirements.yml` or
   `collections/requirements.yml`), its collection entries go into
   `dependencies.galaxy`, with their versions. Detected collections it doesn't
   list are **added** (marked as detected). Role entries in it are carried
   over as-is.
5. **Versions**: a `requirements.yml` version wins; otherwise the host's
   installed version (`ansible-galaxy collection list`); otherwise unpinned
   with a comment. A header comment says pins come from the host and are for
   the user to keep or drop.

Not done in v1: `requires_ansible` compatibility check, binaries from
`command`/`shell` as commented suggestions, filter/lookup analysis (all in
"Later").

## Output

Terse, reference-bearing comments (a collection is annotated with the modules
that need it and where); we'll see after use whether that's too much. Shape
(abridged; the real draft starts with a header comment). Explanatory comments
and the commented-out stanzas to uncomment are separated by blank lines:

```yaml
---
version: 3

images:
  base_image:
    name: ghcr.io/ansible-community/community-ee-base:2.21.6-1

dependencies:
  # The base image has Python 3.14.10 (/usr/bin/python3) and ansible-core 2.21.6.
  # To change them, uncomment (...):

  # python_interpreter:
  #   package_system: python3
  #   python_path: /usr/bin/python3.14
  # ansible_core:
  #   package_pip: ansible-core==2.21.6
  # ansible_runner:
  #   package_pip: ansible-runner

  galaxy:
    collections:
      # detected: community.docker.docker_image (roles/web/tasks/main.yml:2), docker_container (site.yml:5)
      - name: community.docker
        version: 4.7.0

  # Items the scan couldn't place:
  # TODO: site.yml:11: module "frobnicate": no collection found on this host
  # TODO: include/import with a templated target (detected 3 in all): what they load can't be determined statically

options:
  package_manager_path: /usr/bin/microdnf

  # The names the built image gets (ansible-builder build --tag overrides them).
  # To set them, uncomment and adjust:

  # tags:
  #   - myproject-ee:latest
```

Every draft was checked with `ansible-builder create` (see "Implementation
notes").

## Code plan

- New package `internal/eeinit` (name tentative; update `Codemap.md`), plus
  `ee init` wiring in `internal/config/resolve.go` (`ParseVerb`, the "eleven
  verbs" count in `CLAUDE.md`) and a case in `session.Main` that doesn't
  build the live tree.
- **Pure core**: `Draft(inputs) (yaml string, report)`, where inputs are the
  found modules with locations, the module->collection map, installed and
  required versions, and the base-image facts. No I/O, so it's unit-testable
  as tables (project philosophy, `TESTING.md`).
- **Impure edges**, each feeding the core: playbook walk, `ansible-doc` /
  `ansible-galaxy` calls, `podman run` inspection, reading `requirements.yml`,
  writing the file.
- **Reuse** `internal/rolepath` (role/collection lookup) and the role
  walking in `internal/source` rather than writing a second walker. If
  `source`'s walking turns out to be bound to the task index or the event
  loop, extract the walker instead of copying it; nothing here may pull in
  `tview`.
- Host-side tool calls (`ansible-doc`, `ansible-galaxy`) stay on the host on
  purpose: the question is what the *host* knows, not what any EE contains
  (same exemption as the standalone verbs in `ExecutionEnvironments.md`).
- New config key `base_image` and the default constant are surfaced in `ConfigOptions.md`,
  `examples/config.toml`, `settingswarn.go` validation. The CLI change updates
  `completions/`, `README.md`, `man/`, `Keyboard-shortcuts.md` not needed
  (no TUI).

## Testing

- Unit: the pure core (golden YAML for: all-FQCN, short names with
  `collections:`, requirements.yml present/absent/partial, unresolvable
  items, unpinned), the `os-release` parser, the base-image lookup order,
  the refuse-to-overwrite check.
- Fixture playbooks under `testdata/` (own comments, as the others).
- Smoke with real podman gated on `TANGSIBLE_EE_IMAGE`, like
  `internal/ee`'s smoke test; optionally feed the result to
  `ansible-builder` manually while developing.

## Controller-side binaries (follow-up, `--resolve-packages`)

**Implemented** (see the notes at the end of this section). A `command`/`shell` task usually runs on the
*target*, so its binary is the target's business. When the task runs on the
**controller** - inside the EE - the binary must be in the image, and neither
the base image nor a collection's own requirements may have it. This feature
finds such binaries, checks the base image for them, and (opt-in) looks up the
package that provides each missing one. **It never adds anything on its own:**
the result is a commented block to review, like the interpreter stanza.

### Which tasks

Only `ansible.builtin.command` and `ansible.builtin.shell` (also their short
and `ansible.legacy` names), and only when the task is controller-side:

- the play has `hosts:` `localhost`, `127.0.0.1` or `::1` (literal; a template
  or a group name is not recognized), or `connection: local`;
- or the task (or an enclosing block) has `connection: local`, `delegate_to:`
  one of those three, `vars: ansible_connection: local`, or is `local_action:`.

Limitations, stated in the comment of the binaries block of the draft:
a role's task file is scanned on its own, so it has no play context (only
the task-level conditions above apply to it); an `ansible_connection` set in
the inventory, and `hosts:` given by variable, are invisible. There is no flag
to widen this to every task: a remote task's binary is not the EE's.

### Which binary

Per task, from the free-form string, `cmd:`, or the first element of `argv:`
(`shell`'s `executable:` is a binary too). `ScanBinaries` (pure) splits a
shell string into simple commands (at `|`, `||`, `&&`, `;`, `&`, newlines;
quotes respected) and takes the first word of each, skipping:

- leading `NAME=value` assignments and the wrappers `sudo`, `env`, `time`,
  `nohup`, `exec`, `command`;
- shell builtins and keywords (`cd`, `echo`, `test`, `[`, `export`, `set`,
  `read`, `source`, `.`, `if`/`then`/`fi`, `for`/`do`/`done`, `while`, `case`,
  `true`, `false`, `exit`, ...);
- paths inside the project (`./x`, anything under the playbook's directory).

An absolute path is kept as it is. A first word that is templated (`{{`),
`eval`, or a command substitution that can't be split becomes a *templated*
mark: one general `TODO`, counted like the templated includes ("detected N in
all"), not one per task.

### Checking the base image

The base-image probe takes the binary names as arguments (`sh -c <script>
probe name...`, so nothing is interpolated into the script; names that aren't
plain are dropped) and answers `missing=<name>` for each `command -v` that
fails. Same container run as the existing probe. An absolute path is tested
with `-x`.

### Resolving packages (`--resolve-packages`)

Without the flag, the missing binaries are only listed (see the output below),
with a hint about the flag. With it, tangsible runs a second, throw-away
container of the base image (the image itself is never changed) **with
network** - it honors `[execution_environment] network`, so `network = "host"`
fixes DNS trouble - and as root where the family needs it, and asks the
package manager which package provides each missing binary:

- **rpm family** (the image's package manager is microdnf, dnf or yum):
  `dnf -q repoquery --file '*/bin/<name>' --queryformat '%{name}\n'`, and the
  same with `*/sbin/` if that finds nothing, once per binary (a second
  argument is a package filter, not a second pattern). Checked on
  community-ee-base (dnf5): finds `jq`, `sshpass`; needs about 40 MB of
  repository metadata, so ~25-30 s for the first query. `microdnf` can't do it
  (its `provides` only refreshes metadata), `dnf` must exist in the image.
- **dpkg family** (apt-get): `apt-file`, which needs `apt-get update`,
  installing `apt-file` and `apt-file update` in the throw-away container
  first, then `apt-file -x search '^/(usr/)?s?bin/<name>$'` per binary.
  Measured on ubuntu:24.04: about 223 MB of Contents metadata and ~75 s in
  all, on a fast connection - hence the flag.
- Other families (apk, zypper, ...), no network, a failed update: no packages,
  the binaries stay listed with "package unknown", a note on stderr, exit
  code unaffected.

A binary with several distinct package names (after removing architecture
and repository duplicates) is listed with all of them as alternatives and is
not picked for the user. A progress line on stderr says that repository
metadata is being downloaded.

### Output

One block, grouped, in `dependencies`, with the platform selector from the
image's family (`[platform:rpm]`, `[platform:dpkg]`). Without the flag:

```yaml
  # Binaries used by tasks that run on the controller and missing from the
  # base image: jq (site.yml:40), sshpass (site.yml:52, roles/x/tasks/main.yml:7)
  # Look up their packages with --resolve-packages.
```

With it, in the same style as the interpreter stanza, to uncomment (bindep
syntax):

```yaml
  # Binaries used by tasks that run on the controller and missing from the base image:
  # system:
  #   - jq [platform:rpm]        # jq: site.yml:40
  #   - sshpass [platform:rpm]   # sshpass: site.yml:52, +1 more
  #   # TODO: "yq": no package found
  #   # TODO: "ip": one of iproute, iproute-tc
```

Nothing is written when no controller-side binary is missing. The summary
line gains "N binaries missing from the base image". Binaries are
referenced by file and line like the collections' evidence (at most two
locations, then "+N more").

### Code plan

- `scan.go`: `ScanYAML` also tracks the controller-side context (play
  `hosts`/`connection`, block and task keywords) and returns `Command`
  records (the task's command string/argv, `executable:`, location);
  `ScanBinaries(commands) (bins []Binary, templated int)` is the pure
  tokenizer, table-tested.
- `base.go`/`probe.go`: the probe script takes names and prints `missing=`;
  `BaseFacts` gains the os family (from `ID_LIKE`) and `Missing []string`.
- `packages.go`: `ParsePackages` (pure; per family) and `ResolvePackages(engine,
  image, network, family, names)` (the throw-away container).
- `draft.go`: the block above, pure like the rest.
- Flag parsing in `ParseArgs`; `completions/`, `README.md`, `man/`.

### Tests

Tokenizer table (pipes, `&&`, quotes, assignments, wrappers, builtins,
templated, `argv`, `executable`); controller-side detection table (play
`hosts`, `connection`, `delegate_to`, `local_action`, block inheritance,
role file without context); `ParsePackages` on captured `dnf`/`apt-file`
output (several providers, none, arch duplicates); golden `Draft` with and
without resolved packages. The container steps are checked by hand, like the
other podman edges.

### Implementation notes

- The family comes from the image's package manager (`BaseFacts.Family`), not
  from os-release.
- The probe takes the names as arguments (`sh -c <script> probe name...`);
  the resolve scripts likewise, in a throw-away `podman run --rm --user root
  [--network=<network>]` of the base image.
- The tokenizer (`binaries.go`) keeps `{{ ... }}`/`{% ... %}` whole, so a
  filter's `|` is not a shell pipe; a `(` or `{` glued to a word is dropped;
  heredoc bodies are data. `command` (no shell) takes only the first word.
- `dnf repoquery --file` takes one pattern per call (a second argument is a
  package filter), so `*/bin/<name>` and then `*/sbin/<name>`.
- Checked by hand on community-ee-base (rpm, `jq`->`jq`, `ip`->`iproute`) and
  ubuntu:24.04 (`jq`, `ip`->`iproute2`, `curl`, `sshpass`); a lookup that can't
  run (no network, no dnf) leaves the binaries listed with a `TODO`. Only the
  parsers and the draft are unit-tested.

### Open points

- Role task files have no play context; a play-aware walk (which play calls
  which role) is possible later, but is a larger change in `source`.
- Controller-side *modules* that need binaries (`synchronize` -> `rsync`)
  belong to the deferred implied-dependency rules table, not here.

## Lookups and implied dependencies (follow-up)

**Implemented** (see the notes at the end of this section). The module scan misses what is not a task's
module: a `lookup('community.general.dig', ...)` in a variable needs its
collection in the image and, often, a Python library nobody declared. Two
pieces, both feeding the draft's existing commented stanzas (nothing is ever
added on its own):

1. **Lookup detection** - `lookup()`, `query()` and `with_*` find the lookup
   plugins the playbook uses; their collections join the galaxy list.
2. **Implied-dependency rules** - a small embedded table of packages that a
   used module or lookup needs and its collection doesn't declare, plus the
   plugin's own documented requirements as hints.

Filters and tests are **not** part of this (see "Later"): a filter appears as
`| name` inside a template expression, which can't be told from a shell pipe
or a plain `|` reliably. Lookups are always called by name as a string.

### Detecting lookups

Lookups run on the controller whatever the task's host, so every one counts
(no controller-side test as for `command`/`shell`).

- **Where:** every string scalar of every YAML file `ReachableYAML` lists
  (task arguments, `vars`, `defaults`, `group_vars`/`host_vars`, vars files -
  not only task lists) and every `.j2`/`.jinja2` file under the playbook's
  directory (`{# ... #}` comments removed). YAML comments never reach the
  scan.
- **What:** `lookup('name', ...)`, `query('name', ...)` and `q('name', ...)`
  with a literal first argument, single or double quoted, short (`env`, `dig`)
  or fully qualified (`community.general.dig`); and a task key `with_<name>`
  (`with_items`, `with_fileglob`, `with_community.general.x`). The pattern is
  `\b(lookup|query|q)\(\s*['"]([A-Za-z0-9_.]+)['"]`.
- **Not detected, so a TODO:** a lookup whose name is not a literal
  (`lookup(var, ...)`), counted like the templated commands ("detected N in
  all", one general line). Not looked at at all: lookups in templates outside
  the playbook's directory (see "Open points"), filters, tests.
- **Collections:** the play's/task's `collections:` list applies to short
  names, as for modules; a collection role's own collection too.

### Resolving them

`ansible-doc -t lookup -j <names>` (one batch call per 80 names, like the
module lookup) on the host: the answer carries the owning `collection` and the
plugin's documented `requirements` (checked: `dig` -> `community.general`,
`dnspython (python library)`; `hashi_vault` -> `community.hashi_vault`, `hvac`;
`env`, `file`, `items` -> `ansible.builtin`, which is dropped). A fully
qualified name needs no lookup. A name the host doesn't know is a `TODO`
("no collection found on this host"), as for modules.

The collection joins the galaxy list with evidence `lookup dig (site.yml:12)`.
`LookupCollections` generalizes to a plugin type (`module`, `lookup`) and
returns the collection and the documented requirements.

### Documented requirements as hints

For each detected lookup whose documentation lists requirements, the
collection entry gets one more comment, markup removed (`ansibledoc.StripMarkup`):

```yaml
      # detected: lookup dig (site.yml:12)
      # documented requirements of dig: dnspython (python library)
      - name: community.general
```

Hints only: the text is free-form, so nothing is parsed out of it and nothing
is derived from it. Module documentation is not used (a module's requirements
are for its host, usually the target; "python >= 2.7" is noise).

### The rules table

An embedded Go table (`rules.go`), small on purpose: ansible-builder already
installs each collection's own `requirements.txt` and `bindep.txt`, so the
table only holds what collections forget to declare.

```go
type Rule struct {
    Plugin string   // fully qualified: "community.general.dig", "ansible.posix.synchronize"
    Kind   string   // "lookup" or "module"
    Pip    []string // Python requirements (pip syntax), "dnspython"
    System []string // system packages (bindep syntax), "rsync"
    Why    string   // one line, shown as the entry's comment
    ControllerOnly bool // a module rule only for controller-side tasks
}
```

Initial entries (each checked against the plugin's documentation before it is
added): `community.general.dig` -> `dnspython`; `community.hashi_vault.hashi_vault`
-> `hvac`; `ansible.posix.synchronize` -> `rsync`. More are added when someone
meets the gap; a table entry that a collection starts declaring itself just
becomes redundant.

- A **lookup** rule applies whenever the lookup is detected.
- A **module** rule marked `ControllerOnly` applies only when the task is
  **controller-side** (the detection of "Controller-side binaries":
  `hosts: localhost`, `delegate_to`, `connection: local`, ...); otherwise the
  module's host, not the EE, needs the package. `ModuleUse` therefore records
  whether its task is controller-side. A module whose action plugin runs on
  the controller whatever the task's host is not `ControllerOnly`:
  `synchronize` needs `rsync` on the controller for every task ("`rsync` must
  be installed on both the local and remote host").
- Names are matched after resolution, so `dig` and `community.general.dig`
  both hit the same rule.
- Not user-extensible in v1 (see "Later").

### Output

One commented-out block under `dependencies`, separated from its explanation
by a blank line, with a `python:` and a `system:` stanza. Entries carry their
evidence and `Why`; the `system:` stanza is **shared** with the one of the
controller-side binaries (one block, entries from both, so uncommenting never
gives two `system:` keys).

```yaml
  # Packages implied by what the playbook uses (the collections don't declare them).
  # To install them, uncomment:

  # python:
  #   - dnspython   # lookup dig (site.yml:12)
  # system:
  #   - rsync       # synchronize on the controller (site.yml:30)
```

`python:` is a list of pip requirements, `system:` bindep entries (a name
that differs per distribution gets `[platform:rpm]`/`[platform:dpkg]`
selectors in the table). `--resolve-packages` doesn't touch these: the table
already gives package names. Nothing is written when nothing applies. The
summary line gains a count.

### Code plan

- `lookups.go` (pure): `ScanLookups(path string, data []byte, own []string)`
  for a YAML file (walks every string scalar and `with_*` keys, keeping
  `collections:`) and `ScanTemplate(path string, text string)`; both return
  `LookupUse{Name, Collections, Loc}` and a templated count.
- `scan.go`: `ModuleUse` gains `Controller bool`.
- `host.go`: `LookupCollections` -> `LookupPlugins(kind, names)` returning
  collection and documented requirements per name.
- `rules.go` (pure): the table and `Implied(modules, lookups) (pip, system []Implied)`.
- `resolve.go`: lookups join `Resolve`'s needed collections.
- `draft.go`: the hint comment and the two stanzas; `run.go`: the template
  walk (`.j2` files under the playbook's directory, skipping `.git`/`.tangsible`).
- Docs: `man/tangsible-ee.1.scd`, `README.md`, `Changelog.md`, `Codemap.md`.

### Tests

Table tests: lookup patterns (quotes, short and FQCN, `query`/`q`, `with_*`,
nested in a longer expression, a non-literal name, a `{# ... #}` comment, a
string that only mentions the word `lookup`); `ScanLookups` on a vars file
and a task file; `Implied` with controller-side and remote module uses;
golden `Draft` for the two stanzas and the shared `system:` block. The
`ansible-doc` call is checked by hand like the other host edges.

### Implementation notes

- Lookups are scanned in every string scalar of every reachable YAML file and
  every `.j2`/`.jinja2` file under the playbook's directory (`TemplateFiles`);
  a non-literal first argument (`lookup(var, ...)`) is counted into one
  "detected N in all" TODO. A multi-line block scalar's locations are the
  lines inside it.
- A lookup written as `dig` and as `community.general.dig` is one plugin: one
  hint, one rule hit.
- Documented requirements that are cross-references (`R(...)`: "For detailed
  requirements, see ...") are left out of the hints.
- `synchronize` is **not** `ControllerOnly` (see above); the other two initial
  rules are lookups. The table is `Rules` in `rules.go`; a test checks every
  rule is well-formed.
- Where the binaries are only listed (no package lookup), that list stays a
  separate comment block; the shared stanza holds the entries that have a
  package, from the binaries (with the platform selector) and the rules.
- Checked by hand against this host's ansible-doc (`dig`, `hashi_vault`,
  `community.general.dig`, `env`, `items`) and with `ansible-builder create`.
  Only the scanners, resolution, rules and draft are unit-tested.

### Open points

- Templates outside the playbook's directory (an outside role's `templates/`,
  a `template:` `src` elsewhere) are not scanned. `ReachableYAML` lists only
  YAML; a template walk for referenced roles is possible later.
- Free-form documented requirements are shown, not interpreted.
- Which entries the table should hold beyond the three above.

## Verifying a definition (`ee verify`, follow-up)

**Implemented** (see the notes at the end of this section). The draft is built from what can be seen
statically; only a build shows what is still missing. `tangsible ee verify`
builds an `execution-environment.yml` into an image, runs checks inside it, and
reports what is wrong. It is a separate subcommand (not `ee init --verify`)
because the draft is meant to be edited first - stanzas uncommented, TODOs
resolved - and verifying the untouched draft would mostly repeat its own TODOs.

```
tangsible ee verify [playbook.yml] [-f file] [--keep] [ansible-playbook args ...]
```

- `-f file`: the definition, default `./execution-environment.yml` (missing:
  error, hinting at `ee init`).
- `playbook.yml`: resolved like `run`'s; the other arguments are passed to
  `ansible-playbook` for the syntax check (inventory, `-e`, ...), as for `run`.
  The credential prompts (`--ask-pass`, `--ask-become-pass`,
  `--ask-vault-pass`) are rejected in v1 with a usage error: a syntax check
  needs none, so pass a vault password *file* instead.
- `--keep`: don't remove the built image; print its name.
- `--no-cache`: build without podman's build cache (see step 1).
- Needs **podman** and **ansible-builder** on `PATH` (`tangsible version`
  reports ansible-builder); a missing one is an error naming it, with the
  usual not-found message. These are the only subcommand's requirements - the
  rest of tangsible never needs ansible-builder.

### Steps

1. **Build.** `ansible-builder build -f <file> -c <private temp context> -t
   <throw-away tag> --container-runtime podman
   --extra-build-cli-args="[--layers=false] [--network=<network>]"`. The throw-away tag
   (`localhost/tangsible-ee-verify:<random>`) overrides the file's own `tags`
   (the command line wins in ansible-builder), so a name the user chose is
   never created or overwritten. `--network` is `[execution_environment]
   network` when set (`host` fixes DNS trouble in a build, as it does for a
   run). By default podman's build cache is used and kept: the untagged stage
   images outlive the removed tag, and a second build of the same or a slightly
   changed definition takes about half as long (measured with one collection:
   37 s cold, 18 s again, 18 s with only the last stage changed; more with heavy
   dependencies). `--no-cache` passes `--layers=false`: podman keeps no
   intermediate images and so uses no cache, a fresh build that leaves nothing
   behind - the way to confirm a definition builds from scratch, since a cached
   `pip`/`dnf` layer can hide that unpinned packages would now resolve
   differently. Cached stage images accumulate (hundreds of MB each);
   `podman image prune` removes the unused ones. Not `--prune-images`: that runs `podman image prune --force`
   over *every* dangling image the user has. The output is written to `.tangsible/ee-verify.log`
   (0600); on failure its last lines are shown, with the path. A first build
   takes minutes and downloads packages: a progress line says so.
2. **Syntax check.** `ansible-playbook --syntax-check <playbook> <args>` in the
   built image, through tangsible's own execution-environment machinery
   (`runner.NewToolCommand` with the image active, `pull_policy = never`: the
   image is local), so mounts, environment and user are the ones a real `run`
   would have. Its exit status and output are reported. It stops at the first
   error, so step 3 does the exhaustive part.
3. **Plugin and binary check**, in one more container run of the image:
   - every module the scan found, and every lookup, asked of the image's
     `ansible-doc` (the same candidates as `ee init`: the `collections:` search
     list, then the bare name) - **all** that don't resolve are listed, not
     just the first;
   - every controller-side binary the scan found (the "Controller-side
     binaries" feature), checked with the probe's `command -v` /`-x` in the
     built image.
4. **Clean up.** The throw-away image is removed (`podman rmi`) unless
   `--keep`; the build context goes with the temp directory. Ctrl-C (and
   SIGTERM/SIGHUP) interrupts the build, then cleans up the same way, as the
   `template` verb does for its scratch directory. A successful build needs
   nothing more than removing the tag. One that fails or is killed leaves buildah
   "working containers" and a `-tmp` image behind (podman does not clean up after
   SIGINT or SIGTERM, to the process group or to `podman build` alone - measured),
   and the containers pin the images, so the next build can fail. Their names are
   not ours to know, so podman's containers (including the external ones) and
   images are listed before the build and, if it did not succeed, **only what is
   new since then** is removed: the containers, then the untagged and `-tmp`
   images (a new image with a real repository name is the base the build had to
   pull, and stays). Nothing that was already there is touched.

### Report

On stdout, one line per check, then a short list for each failure:

```
Build:           ok (4m12s)
Syntax check:    FAILED
  ERROR! couldn't resolve module/action 'community.docker.docker_container' ...
Modules:         11 of 12 found in the image
  not found: community.docker.docker_container (site.yml:5)
    -> add the collection that provides it to the definition
Lookups:         2 of 2 found
Binaries:        1 of 2 found
  missing: jq (site.yml:40)
    -> add it under dependencies.system (see ee init --resolve-packages)
```

Exit code: 0 when every check passed, 1 when the build or any check failed,
2 for usage errors. A failed build skips the checks that need the image.

### What it can't tell

Python libraries a plugin imports only at runtime, a task that fails when it
really runs, and everything a syntax check doesn't execute. It narrows the gaps
(missing collections, a broken build, missing ansible-core/ansible-runner - the
build's own `check_ansible` fails on that - missing binaries); it doesn't
promise a working EE.

### Code plan

- New package `internal/eeverify` (the subcommand), importing `internal/eeinit`.
  `eeinit` exports the project scan that `run.go` does inline today
  (`ScanProject`: modules, lookups, controller-side commands, templated
  counts), so both subcommands see the same things.
- Pure parts, unit-tested: `ParseArgs`, building the `ansible-builder` argv, the
  check script and its output parser, the report text and exit code.
- Impure edges: running `ansible-builder`, `podman rmi`, the temp context and
  log, the two container runs, signal handling.
- `session.Main` routes `ee init` and `ee verify`; the help-table synopsis
  becomes `tangsible ee <init|verify> ...` with each subcommand described in the
  man page; completions, `README.md`, `man/`, `Changelog.md`, `Codemap.md`.
- Tests: an e2e test (tmux is not needed; it runs the binary) behind an opt-in
  environment variable, since it needs podman, ansible-builder, network and
  minutes - like the other podman e2e tests.

### Open points

### Implementation notes

- The scan is `eeinit.ScanProject`, shared with `ee init`: it covers every YAML
  file under the **playbook's directory** (and referenced roles), not only the
  playbook, so a second playbook next to it counts too.
- `-f` is the definition here, though it is `--forks` for ansible-playbook; a
  syntax check has no use for forks.
- Podman must not run other builds at the same time: after a failed or
  interrupted build the cleanup removes whatever *appeared* since its snapshot,
  which would include a concurrent build's containers. `--prune-images` was
  dropped after an interrupted run left working containers that made it fail
  later ("image is in use by a container"), and because it would prune the
  user's own dangling images. After a failed or interrupted build the cleanup
  removes the new containers and `-tmp` images, never untagged stage images
  (finished stages are the build cache).
- The syntax check's own output is shown (last 15 lines), including ansible's
  warnings about having no inventory; pass `-i` to silence them.
- Checked by hand on community-ee-base: a build of a minimal definition takes
  ~30 s; a missing module/binary is reported (3 of 4 modules, 1 of 2 binaries
  found); a failed build (unknown base image) shows its log tail and exits 1;
  Ctrl-C mid-build exits 130, and so do a build that fails inside a `RUN` step
  (exit 1) and a successful one: in all three podman's containers and images are
  exactly as they were before. The e2e test (`e2e_ee_verify_test.go`,
  `TANGSIBLE_EE_VERIFY=1 TANGSIBLE_EE_IMAGE=... go test -tags e2e -run EEVerify`)
  covers the passing and the failing case.

### Open points

- Credentials for the syntax check (`--ask-*-pass`) are rejected in v1;
  `credfile` could answer them once, as for `run`.
- Implied-dependency rules (`synchronize` -> `rsync`) could be checked too, by
  `command -v` for the system entries; Python packages would need import names
  the table doesn't have.
- Build output is only logged, not streamed; streaming would need a progress
  view.
- The syntax check could default to `-i localhost,` when the user gave no
  inventory, to drop the no-inventory warnings; it would also hide an
  `ansible.cfg` inventory.

## Building a definition (`ee build`, follow-up)

**Implemented** (`internal/eebuild`; the build step moved out of `internal/eeverify`). `tangsible ee verify` builds into a throw-away tag, checks
the result and removes it. `tangsible ee build` is the other half: build the
definition into the image the definition names and keep it. It is a separate
verb, not `ee verify` with a switch, because the two have opposite side
effects (verify creates no name of yours and leaves nothing; build exists to
create one), different arguments (verify takes a playbook and
`ansible-playbook` arguments, build takes neither) and different exit
conditions (verify fails on a missing module or binary, build only on a failed
build). `ee build --verify` was considered and dropped: with the build cache
kept (see `ee verify`, step 1) a verify followed by a build costs little more
than the build alone.

```
tangsible ee build [-f file] [-t tag ...] [--no-cache]
```

- `-f file`: the definition, default `./execution-environment.yml` (missing:
  error, hinting at `ee init`), as for `verify`.
- `-t tag`: the image name; repeatable. Given, it replaces the definition's
  `options.tags` (the command line wins in `ansible-builder`, as it does for
  `verify`'s own tag).
- `--no-cache`: build without podman's build cache (`--layers=false`); the
  default uses and keeps it, as for `verify`.
- Needs **podman** and **ansible-builder**; the same not-found messages.

### What it does

1. **Name.** The tags come from `-t`, else the definition's `options.tags`.
   With neither it stops before building, naming both ways (and the commented
   `tags` stanza `ee init` writes): `ansible-builder` would otherwise create
   `ansible-execution-env:latest`, a name nobody chose. The definition is read
   with yaml.v3; a tag that is not a string is left for `ansible-builder` to
   reject.
2. **Build.** The same `ansible-builder build` as `verify` (private temporary
   context, removed afterwards, so nothing like `context/` appears in the
   project; `--network` from `[execution_environment] network`; output to
   `.tangsible/ee-build.log`, 0600; a progress line says a first build takes
   minutes), but with the real tags. Interrupted (Ctrl-C, SIGTERM, SIGHUP): the
   build is stopped through its process group, the cleanup below runs, exit
   130.
3. **Cleanup.** The throw-away machinery of `verify` applies unchanged: after a
   failed or interrupted build the containers and `-tmp` images that appeared
   since the snapshot are removed; the build cache (untagged stage images) and
   a pulled base image stay. A successful build removes nothing - the image is
   the point. A failed build never touches an existing tag (podman moves a tag
   only when a build succeeds).
4. **Report.** On success: each tag with the image id and size, and whether a
   tag moved (a rebuild of an existing name leaves the previous image untagged:
   said, with `podman image prune` to remove it). Then the line to use it:
   `image = "<first tag>"` under `[execution_environment]`, printed, never
   written (`config.toml` is the user's). On failure: the last lines of the log
   and its path, exit 1, as for `verify`.

Exit codes: 0 built, 1 build failed or a precondition failed (no tag, missing
file or tool), 2 usage, 130 interrupted - the same set as `verify`.

### Code plan

The build step moves out of `internal/eeverify` into a new package
`internal/eebuild`, which both verbs use: `BuildArgs` (tags instead of one tag;
`verify` passes its throw-away one), `Snapshot`/`TakeSnapshot`/`Cleanup`
(`Cleanup` gains no new case: its `tag` and `keepTag` arguments already cover
"remove the tag" and "keep it"), the process-group start and cancel, the temp
context and the log-tail helper. `internal/eebuild/run.go` is the verb
(`Run(args)`), `args.go` its parser, `definition.go` the pure tag extraction
(`Tags(yaml []byte) ([]string, error)`). `session/main.go` routes
`ee build` next to `ee verify`.

### Tests

Pure: argument parsing (repeated `-t`, `--no-cache`, `-f`, unknown options, a
missing value), `Tags` (a list, none, `options` absent, a null `options`, a
non-string entry), `BuildArgs` with several tags. `verify`'s existing tests move
with the code they cover. e2e (`TANGSIBLE_EE_VERIFY=1`, as for `verify`): build
a minimal definition with `-t localhost/tangsible-ee-build-test:1`, check the
image exists and no `context/` appeared in the project, then remove that image;
a definition with no tag fails before any podman call.

### Changes elsewhere

`config/help.go` (`tangsible ee <init | verify | build> ...`, summary), the
help/man consistency test, `man/tangsible-ee.1.scd`, `man/tangsible.1.scd`,
completions (bash, fish: `-f`, `-t`, `--no-cache` after `build`), `README.md`,
`Changelog.md`, `Codemap.md`, `CLAUDE.md`. `tangsible version` already reports
both tools.

### Implementation notes

- `ansible-builder build -t` is `-t TAG [TAG ...]` (one flag, several values),
  not repeatable: several names are passed after a single `-t`.
- A malformed `options.tags` (a scalar, a number in the list) is an error from
  `Tags`, naming the problem, rather than left to ansible-builder: the "no name
  given" message would otherwise mislead.
- The build step's temporary context is made and removed inside
  `eebuild.Execute`, so `ee verify` no longer manages one.
- `Cleanup("", built, true)` is `ee build`'s: it removes the new containers and
  `-tmp` images after a failure and never a tag.
- Measured: a minimal definition with one collection built in 31 s cold and in
  7 s after a definition change in the last stage (the cache); an interrupted
  build exits 130 and leaves no container, no `-tmp` image and no tag; a rebuilt
  name is reported as replaced.

### Not doing

`ee build --verify` (see above). Pushing to a registry. Writing the tag into
`config.toml`. Several definitions in one call. A `--keep-context` option: a
context to inspect is `ansible-builder create`'s job.

### Open points

- Whether a name that already exists should need a confirmation (it is
  replaced silently, as `podman build -t` does, and reported afterwards).
- Whether `-t` should also be accepted by `verify` (no: its tag is
  deliberately its own).

## Progress while building (`ee verify`, `ee build`, follow-up)

**Spec, not built.** A build takes minutes and today the terminal shows one
line ("building ... this can take several minutes") and then nothing: the build's
output goes only to `.tangsible/ee-verify.log` / `ee-build.log`. Two additions
to the build step the two verbs share (`internal/eebuild`):

1. **A live status line** (the default, on a terminal).
2. **`--show-output`**: stream the build's complete output.

### What the build prints

`ansible-builder` prints its own lines and then podman's build log. podman's
markers are what a progress indication can use (seen in a real log):

```
[2/4] STEP 11/13: RUN mkdir -p /usr/share/ansible
```

- `[n/m]` is the stage (the generated Containerfile has base, galaxy, builder
  and final: four), `STEP i/j` the step in that stage. A stage's step count is
  printed only when the stage starts, so there is **no overall percentage**:
  the total is not known up front, and one `pip install` or `dnf install` step
  can run for a minute without any marker. The line shows stage and step, not
  a bar.
- Cache hits print `--> Using cache ...`; pulls of a base image print
  `Copying blob ...` lines, rewritten in place with `\r`; pip and dnf print
  what they install. All of that is a better "is it alive" signal than the
  markers, so the status line also carries the latest line.

### The status line

On stderr, redrawn in place once a second (and on each new marker), only when
**stderr is a terminal** (`term.IsTerminal`); on a pipe or in CI nothing is
added (the existing "building ..." line stays the only one). Format:

    building  1m12s  [2/4] STEP 11/13  Collecting jmespath

- Elapsed time first, so a long step visibly ticks.
- The latest marker, as `[n/m] STEP i/j`; before the first one (the pull of the
  base image, ansible-builder's own start) it is left out.
- The latest non-empty output line (ANSI escapes stripped, split on `\n` and
  `\r`, so a progress bar updating itself is its latest state), truncated with
  an ellipsis to the terminal width (`term.GetSize`, else `$COLUMNS`, else 80;
  never wrapped, since a wrapped line cannot be redrawn).
- Drawn with `\r` and "erase to end of line"; cleared (the line erased) when
  the build ends, before the report is printed, and when interrupted; the
  existing "building ..." line above it stays.
- It is the build's concern only: the checks of `ee verify` that follow
  ("checking the image ...") keep their own plain lines.

The full output still goes to the log, and a failed build still shows the last
lines of it in the report.

### `--show-output`

Instead of the status line, the build's complete output is written to stderr as
it arrives (and to the log as before), terminal or not - for "show me
everything", and the way to see it in CI. Named so because `-v`/`--verbose` are
taken: `ee verify` passes `ansible-playbook`'s own arguments through, where
`-v` and `--verbose` mean its verbosity. Both verbs get it:

```
tangsible ee verify [playbook.yml] [-f file] [--keep] [--no-cache] [--show-output] [ansible-playbook args ...]
tangsible ee build [-f file] [-t tag ...] [--no-cache] [--show-output]
```

`ee verify` consumes `--show-output` as its own (like `--keep`), so it never
reaches `ansible-playbook`.

### Code plan

`internal/eebuild/progress.go`:
- **Pure:** `ParseStep(line) (stage, stages, step, steps int, ok bool)` (the
  marker, tolerant of a leading ANSI colour and of `STEP` lines without a
  stage, which older podman prints as `STEP 3/13: ...`); `Tracker`, fed lines
  (`Feed(line)`), holding the latest marker and latest line;
  `Tracker.Format(elapsed time.Duration, width int) string`, the status line
  (truncation by display width, not bytes: runes, and the elapsed time and
  marker are never cut off before the text is).
- **Impure:** `Progress`, an `io.Writer` that splits what it is given into
  lines on `\n` and `\r` (keeping a partial line until its end), feeds the
  Tracker, and redraws from a ticker goroutine; `Close()` stops the ticker and
  erases the line. A mutex guards the Tracker (the writer runs on exec's copy
  goroutine, the ticker on its own). With `--show-output` the writer is a plain
  tee to stderr instead.
- `Execute` is unchanged: its `log` argument becomes
  `io.MultiWriter(logFile, progress)` in both verbs' `run.go`.

### Tests

Pure: `ParseStep` (the real marker, a coloured one, a step without a stage,
lines that merely contain "STEP", `[0/0]`), `Tracker` over a recorded build log
excerpt (latest marker and line; `\r` progress lines; ANSI), `Format` (width
80 and 20, a very long line, no marker yet, elapsed over an hour, a multi-byte
character at the cut). The writer's line splitting with chunks that cut a line
in two. No test draws to a real terminal.

### Changes elsewhere

`--show-output` in both verbs' `ParseArgs`/`Usage`, `man/tangsible-ee.1.scd`,
completions (bash, fish), `README.md`, `Changelog.md`, `Codemap.md`,
`CLAUDE.md`.

### Not doing

A percentage or a bar (not derivable, see above). A TUI for the build. Pulling
the base image with its own progress before the build (podman prints its blob
lines, which the status line already shows). Colour in the status line.

### Open points

- Whether the status line should also redraw when the terminal is resized
  (the next tick redraws at the new width anyway).
- Whether `ee init`'s own slow steps (the base image pull, `--resolve-packages`,
  which "can take a minute") deserve the same treatment; they print one line
  today.

## Later (explicitly deferred)

- **Implied-dependency rules table**: specified above for lookups and
  controller-side modules. Later: filters (`json_query` -> `jmespath`,
  `password_hash` -> `passlib`), `sshpass` for password SSH, and whether the
  table is user-extensible (config or `.tangsible/`).
- Filters and tests (regex out of templates/vars, map via
  `ansible-doc -t filter --list`); lookups are specified above.
- `requires_ansible` check against the pinned ansible-core.
- Binaries used by `command`/`shell` (now specified above, as "Controller-side
  binaries").
- Smarter handling of an existing file (diff, second file, merge).
- Resolving collections not installed on the host (Galaxy lookup).
- `--verify`: now specified above as `ee verify`; building the real image is
  `ee build`, also above.
- An interactive review step.

## Implementation notes

Where the code decided something the spec left open:

- **Verb:** `ee` is a verb of its own (`config.VerbEE`) whose only subcommand
  is `init`; it never builds a TUI, so it skips the colour setup and the
  terminal background query. `internal/eeinit` holds everything;
  `internal/source.ReachableYAML` (extracted from the task index's role walk,
  which now shares `walkRoles` with it) supplies the files.
- **Order of refusals:** existing output file first (before any work), then
  playbook, then podman. The file is created with `O_EXCL`, so one that appears
  meanwhile is not overwritten either.
- **Image pull:** the base is pulled with `runner.EnsureImage` under the user's
  `pull_policy`/`pull_arguments`; inspection runs `podman run --rm
  --network=none --entrypoint /bin/sh <image> -c <probe>`.
- **Package manager choice:** `microdnf`, `dnf`, `apt-get`, `yum`, `zypper`, in
  that order (the community images are built with microdnf).
- **Module detection:** the first task key that is not a play/block/task
  keyword or `with_*`; `action:`/`local_action:` in string and mapping form.
  A fully qualified name (three or more parts) yields its collection without a
  lookup, so a collection missing on the host is still listed. `ansible.builtin`
  and `ansible.legacy` are dropped. Short names are looked up in one batch
  `ansible-doc -t module -j` call per 80 names, `collections:` search-list
  entries first.
- **Roles from collections:** a role that comes from a collection needs its
  collection even when none of its modules is resolved; it is listed as
  `role <name>`.
- **Files skipped by the scan:** `requirements.yml`/`.yaml` and the output
  file. Every other YAML file under the playbook's directory is classified by
  shape (a playbook or a task list), as the task index does; a vars file
  yields nothing.
- **requirements.yml:** looked up next to the playbook, then in the working
  directory (`requirements.yml`, `collections/requirements.yml`). Entries are
  emitted as written; a plain `name` with no version gets the host's version.
  Entries with a `source`/`type` (git, other servers) are never touched, and
  a detected collection is matched to an entry by name only, so a collection
  listed under a git URL is added a second time.
- **Templated includes:** a playbook can have dozens of `include_tasks`/
  `include_role`/... with a templated target, so they are reported as a single
  `TODO` without locations ("detected N in all"), after the other items; those
  stay one `TODO` each.
- **`tags`:** a commented `options.tags` stanza names the built image;
  tangsible can't know a good name, so it suggests `<project directory>-ee:latest`
  (made a valid image name, `my-ee:latest` if nothing is left). The other
  `options` keys (`container_init`, `skip_ansible_check`, ...) are not offered:
  `container_init`'s defaults suit running `ansible-playbook` in the container,
  and `skip_ansible_check` would only hide the build-time check that the image
  has ansible-core and ansible-runner (which the draft's own stanzas address).
  An unknown package manager writes ansible-builder's default
  (`/usr/bin/dnf`) with a `TODO`, so `options` is never null.
- **Empty dependencies:** `galaxy` is always written (`collections: []` when
  nothing was found), so `dependencies` is never null - which ansible-builder
  rejects - and the commented stanzas can be uncommented in place. Drafts were
  checked with `ansible-builder create`, also with the stanzas uncommented.
- **Not covered by automated tests:** the podman/ansible edges
  (`ProbeBase`, `LookupCollections`, `InstalledCollections`) beyond their
  output parsers; checked by hand against `community-ee-base`.
