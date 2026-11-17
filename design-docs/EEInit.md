# `tangsible ee init`: draft an execution-environment.yml

## Status

**Spec, not implemented.** Decisions come from the discussion recorded in
`EETemplate.md` (the raw input: original idea, Q&A, earlier brainstorm).
This document is the settled v1 scope; `EETemplate.md` stays as history.

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
not a dependency); `--verify` (a later idea: build + `--syntax-check` inside
the image); filters, lookups and tests (modules only); merging into or
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
| python version/path | `python_interpreter` |
| pip present? | note/`TODO` if absent |
| ansible-core version | pin / comment; informs `ansible_core` in the file |

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

Terse, reference-bearing comments (e.g. `# community.docker: docker_container,
roles/web/tasks/main.yml:14`); we'll see after use whether that's too much.
Shape (illustrative):

```yaml
---
# Draft generated by tangsible ee init. Review before building:
# versions below are pinned from this host; keep or change them.
version: 3
images:
  base_image:
    name: <base>
dependencies:
  ansible_core:
    package_pip: ansible-core==<from base image>
  galaxy:
    collections:
      - name: community.docker      # docker_container (roles/web/tasks/main.yml:14)
        version: 3.4.0              # host
      # TODO: unknown module "frobnicate" (site.yml:22): no collection found on host
options:
  package_manager_path: /usr/bin/microdnf
```

Exact keys follow the ansible-builder v3 schema; verify against its docs and
a real `ansible-builder build` while implementing.

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

## Later (explicitly deferred)

- **Implied-dependency rules table**: a small editable mapping from a module
  (and later filter) to extra packages the collection's own requirements
  don't cover, e.g. `synchronize` -> `rsync`, `json_query` -> `jmespath`,
  `password_hash` -> `passlib`, `sshpass` for password SSH. Whether it's
  user-extensible (config or `.tangsible/`) is open.
- Filters, lookups and tests (regex out of templates/vars, map via
  `ansible-doc -t filter --list`).
- `requires_ansible` check against the pinned ansible-core.
- Binaries used by `command`/`shell` as commented suggestions (can't tell
  EE vs target host, so never added automatically).
- Smarter handling of an existing file (diff, second file, merge).
- Resolving collections not installed on the host (Galaxy lookup).
- `--verify`: build and run `--syntax-check` inside the image.
- An interactive review step.
