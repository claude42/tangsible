# Data Tagging

## Situation

Ansible 2.19 used data tagging which provides some opportunities for
Tangsible.

### Special treatement of vaulted variables

Variables saved in a vault usually contain values that should not carelessly
be displayed on screen. Data tagging should allow us to identify such values.

#### Idea

* In the "resolved" tab, do not show vaulted variables but only placeholder
  to signal that this is such a value
* In the variable explorer, when a user explicitely wants to view such a
  variable, its content should still be displayed as usual.

#### Answers
1. Yes, use a common placeholder, e.g. "<vaulted>"
2. Yes, I meant when the variable is added to the watch pannel then its real
   value should be displayed there as usual. So no change to how the
variables are displayed there.
3. Good point, yeah show the placeholder in the autocomplete.
4. Yes
5. Keep current behavior.
6. Let's keep no_log separate
7. I have ideas for the origin tag, let's tackle this in a second step

#### Spike findings (2026-11-14, ansible-core 2.19.11, real debugger)

| Case | In `task_vars` | Detectable via |
|---|---|---|
| Inline `!vault` variable | `ansible.parsing.vault.EncryptedString`, tags: `Origin` only | `isinstance(v, EncryptedString)` (not the `VaultedValue` tag) |
| Variable from a wholly encrypted `vars_files` file | plain tagged `str`/`dict`, tags: `Origin`, `TrustedAsTemplate`; no `SourceWasEncrypted`/`VaultedValue` | `Origin.path` file starts with `$ANSIBLE_VAULT` |
| `{{ secret \| upper }}`, `"pre-{{ secret }}"`, `\| length` | plain values, `Origin` only - the vault marker is lost | not by tag; by value (below) |
| `[secret, 'x']` | element 0 carries `VaultedValue`, element 1 none | tag, but only this shape |

`Query("task_vars")` (`json.dumps(default=str)`) currently emits the
*decrypted* text of an `EncryptedString`, so today's snapshot - Resolved tab,
autocomplete - leaks vaulted values in clear. `v`'s own values are meant to.

#### Design: Python-side snapshot serializer

**Principle: capture once, mask at the consumers.** One round trip per
pause, as today. The child returns the snapshot *plus* a description of what
is secret; Go keeps the real values (the watch panel shows them, as decided)
and masks only on the way into the Resolved tab and the autocomplete.

**Helper location.** `tangsible_snapshot(task_vars)` in
`callback/tangsible_events.py`. Debug already loads that plugin into the
child (`debugrun` uses `CallbackPluginEnv`) and its module is reachable from
the debugger REPL as `sys.modules['ansible.plugins.callback.tangsible_events']`
(confirmed), so the query is one line - no multi-line REPL definitions that
would not survive a pause. Written clean-room, like the rest of the file.

**Output** (JSON via the same `dumps(..., default=str)`):

```
{"vars":    { ...task_vars as today... },
 "vaulted": [["db_password"], ["cfg", "token"], ["items", 0]],
 "secrets": ["a top secret db password", ...]}
```

`vaulted` is a list of key/index paths (ints for list indices) of values
that are vaulted *as a whole*; `secrets` the distinct decrypted strings of
the `EncryptedString` leaves.

**Walk** (iterative, one pass over `task_vars`, containers recursed):
1. `EncryptedString` leaf, or a `VaultedValue`-tagged one: record the path;
   `str(v)` for `secrets`. A decrypt failure (no secret available) records
   the path only.
2. At every node, `Origin.get_tag(node)`: if its `path` is a vault-encrypted
   file, record the path and *do not descend* (the subtree is wholly
   secret; its leaf strings still go to `secrets`). Encrypted-or-not is
   cached per file path for the walk: read the first 14 bytes, compare to
   `$ANSIBLE_VAULT`.
3. Everything else is passed through. Every `ansible._internal` import is
   in a `try`: on any failure (older ansible, API drift) the helper returns
   `vaulted`/`secrets` empty - today's behavior, as decided for pre-2.19.

Cost is linear in `task_vars`; fine at the target scale.

**Go side.** `debugCaptureSnapshot` queries the helper instead of bare
`task_vars`, splits the result: `vars` goes into `debugTaskVarsCache`
exactly as now (so `mergeRegisteredResult`, `debugUnfakeShadowPaths`,
`debugSetWatch`'s refresh and the watch evaluator are untouched), the mask
(`vaulted`, `secrets`) into a new `debugVaultMaskCache` under the same key.
A pure `maskSnapshot(vars, mask)`:
- replaces each `vaulted` path's value with `"<vaulted>"`;
- in every remaining string, replaces each occurrence of a `secret`
  (case-insensitive) with `<vaulted>` - this covers derived values:
  `pre-<vaulted>-post`, an uppercased copy, a registered `stdout` containing
  the secret. `secrets` shorter than 4 characters are skipped (a vaulted
  `"1"` or `"yes"` would otherwise shred every value). Not covered: values
  transformed beyond recognition (`length`, hashes, base64).

Consumers:
- **Resolved tab:** `resolveTaskValuesFromSnapshot` renders from
  `maskSnapshot(...)`. Masking the *input* means a filter applied in the task
  (`{{ secret | b64encode }}`) renders the encoding of `<vaulted>`, never of
  the secret. The `register: name = value` line and `when:` resolution use
  the same masked copy.
- **Autocomplete:** shows the masked value; the watch panel's own evaluation
  of a typed expression keeps using the real cache.
- **Watch panel:** unchanged.

**Tests.** `callback-tests/` (needs ansible): the helper against a fixture
with inline vault, a wholly encrypted vars file, a derived value and a
nested structure. `maskSnapshot`: plain Go tests (paths, substring,
case-insensitivity, the length floor). tmux e2e: Resolved tab shows
`<vaulted>`, `v` shows the real value.

**Open points.**
1. The 4-character floor for substring masking: right trade-off?
2. A secret that cannot be decrypted (no vault password): the path is
   masked, but there is no string to match derived values against. Accept?
3. Tasks using `no_log` stay separate (answer 6); the `secrets` set is a
   natural place for them to join later.

### Next idea

#### Situation

Variables can be defined in lots of different places and sometimes it's not
clear where a value comes from.

#### Ideas Show where a variable originated

1. In the variable explorer, show the variable's origin after its value
   (maybe in brackets).

2. I might have more but let's do #1 first

#### Answers
1. Top-level variables only.
2. Project-relative path and line.
3. Show a label where we can ("magic variables" are the ones Ansible itself
   provides, e.g. `inventory_hostname`, `groups`, `playbook_dir`).
4. Not in the autocomplete.
5. `(set in debugger)` for a variable assigned in the watch panel.
6. Only the winning definition.

#### Spike findings (2026-11-14)

`Origin.get_tag(value)` carries `path`/`line_num`/`col_num` for play `vars:`,
`vars_files`, `include_vars`, `group_vars`, `host_vars`, inventory host vars
and `set_fact` (a direct `"{{ other }}"` reference traces through to the
source; a computed value reports the `set_fact` line). `-e` has only a
description (`<CLI option '-e'>`). Gathered facts, ansible's own variables
and registered results carry no origin tag at all.

#### Design

`tangsible_snapshot` also returns `origins`: per top-level variable
`{path, line}`, `{desc}`, or `{kind}` where `kind` is `fact` (name, or
`ansible_<name>`, is a key of `ansible_facts`), `builtin` (fixed list of
ansible's special variables) or `registered` (a dict with `changed`). Go
(`internal/origin`) formats labels: paths relative to the working directory
(absolute when outside it), `-e` for the command-line option. The debug run
uses a shadow copy, so a path under it is mapped back to the real tree, and
the line has the inserted `debugger: always` lines above it subtracted. The
current task's own `register:` name is labelled `registered`. The watch panel
appends `  (label)` after a value, using the root variable of the expression;
`(just set)` takes its place until the pause changes, and a variable assigned
in the panel reads `(set in debugger)` for that pause.

### Variables tab on the host verb

Built on the same `Origin` data: the host verb's Variables tab (see
`HostVerb.md`) runs a stub play whose task is an embedded action plugin,
because origins exist only inside a real ansible run, and renders the
variables as YAML with the origin as a trailing comment. Scope decisions:
inventory level only, facts left out, vaulted values as `<vaulted>`,
alphabetical. The origin-label code moved to `internal/origin`, shared with
the debug watch panel. Only variables with a file (or `-e`) origin are shown.

