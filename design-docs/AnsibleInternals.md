# Ansible internals we rely on

Status: audit of 2026-11-26, against the code at that date. Re-run the audit
(see "Keeping this current") when a verb gains a new way of talking to
Ansible.

Tangsible wraps `ansible-playbook`; most of what it does uses documented
command lines (`ansible-playbook`, `ansible-inventory --list`,
`ansible-config dump --format json`, the callback and action plugin base
classes, the `debugger:` keyword). This document lists the places where it
goes beyond that: private Python APIs, output that is only meant for humans,
behavior nobody promised, and formats we re-implemented. Each is a place a new
ansible-core release can break us, so each says **what we assume**, **where it
lives**, and **what happens when the assumption fails**.

Everything here was developed and probed against **ansible-core 2.19.x**
(mostly 2.19.11); the research notes name the probes (`VariableOrigins.md`,
`Data Tagging.md`, `DebugMode.md`). Nothing has been verified against another
series unless a line says so; "Tested releases" at the end has the results of
`make test-ansible`.

## How to read the risk column

| Class | Meaning |
|---|---|
| **A: private API** | an underscore name, an `_internal` module, or an object attribute no documentation lists |
| **B: source-only contract** | public-looking but defined only by reading the source (hook signatures, class attributes, a plugin module's registered name) |
| **C: text scraping** | the format is meant for humans; a reworded message breaks the parser |
| **D: observed behavior** | something ansible does today that no document promises |
| **E: re-implemented** | we copied a file format or a rule instead of asking ansible |

## Overview

| # | What | Class | Where | If it breaks |
|---|---|---|---|---|
| 1 | Callback plugin hooks and the objects they receive | B/A | `callback/tangsible_events.py` | no events: the TUI shows nothing (the pre-flight gate reports it) |
| 2 | Wrapping `Display.display` to catch warnings | A | `tangsible_events.py` (`_hook_display`) | warnings missing from the drill-down, nothing else |
| 3 | Data tagging (`Origin`, `VaultedValue`, `EncryptedString`) | A | `tangsible_events.py` (`tangsible_snapshot`) | no origins, vault values not masked (see below: this one matters) |
| 4 | Plugin module name in `sys.modules` | B | `callback/action/tangsible_vars.py`, `internal/session/vaultmask.go` | Variables tab (host) fails with a message; debug snapshot falls back |
| 5 | Playbook parser classes | A | `internal/staticplay/loader.py` | `tangsible view` exits with the error |
| 6 | The task debugger's REPL: prompt, scope objects, commands | B/A | `internal/debugrun/session.go`, `internal/session/livesession_debug.go` | `debug` hangs or fails (prompt never matches) |
| 7 | The debugger and strategies: forced linear, quiet stdout | D | `internal/debugrun/session.go`, `internal/debugcopy/strategy.go` | prompts garbled, or free plays reorder |
| 8 | Which YAML mappings are tasks, and where to splice `debugger: always` | E | `internal/source`, `internal/debugcopy` | tasks without a breakpoint, or a spliced copy ansible rejects |
| 9 | `--list-tasks --list-hosts` text | C | `internal/runner/progress.go` | no progress percentage; `debug` has no tree (fatal there) |
| 10 | Warning and deprecation message text, the `Origin:` line | C | `tangsible_events.py`, `internal/playbook/notice.go` | warnings attributed to the wrong task, or dropped |
| 11 | Become-failure message text | C | `internal/ee/hint.go` | the container hint is not shown |
| 12 | `[core X.Y.Z]` in the version banner | C | `internal/eeinit/registry.go`, `internal/eeverify/script.go` | `ee init` picks the default base image |
| 13 | `ansible-doc`: plain text and the `-j` schema, markup in descriptions | C/D | `internal/ansibledoc`, `internal/eeinit/host.go` | Docs tab / context help empty or wrong |
| 14 | `ansible-inventory --host` merging cached facts; the memory cache trick | D | `internal/host/host.go` | host Summary shows a live gather instead of the cache |
| 15 | Fact-gathering details (`ansible_architecture`, `mount_facts`) | D | `internal/host/host.go` | Summary incomplete |
| 16 | Default and effective role/collection paths | D/E | `internal/rolepath`, `internal/ansibleconfig` | roles not found for the drill-down |
| 17 | Vault file format 1.1/1.2 (AES256, HMAC) | E | `internal/vaultcrypto`, `internal/vaultfile` | `tangsible vault` fails to decrypt (never silently corrupts, see below) |
| 18 | Variable names ansible provides itself | E | `tangsible_events.py` (`_BUILTIN_VARS`) | a variable labelled wrongly |
| 19 | Exit codes and the `ansible-playbook` flag table | B/E | `internal/runner/process.go`, `internal/config/playbookargs.go` | wrong status line; a flag with a value misread as the playbook |
| 20 | Execution-environment image conventions, Galaxy API | D | `internal/ee`, `internal/runner/ee.go`, `internal/eeinit/galaxy.go` | EE runs fail to start; collection versions unresolved |

## 1. The callback plugin: hooks and objects (class B, A)

`callback/tangsible_events.py` is a clean-room reimplementation of
`ansible.posix.jsonl` (`OwnCallbackPlugin.md`). What it assumes:

* **Hook names and signatures** (`v2_playbook_on_play_start(play)`,
  `v2_playbook_on_task_start(task, is_conditional)`,
  `v2_playbook_on_handler_task_start(task)`, `v2_runner_on_start(host,
  task)`, `v2_playbook_on_notify(handler, host)`, `v2_runner_on_ok|failed|
  skipped|unreachable(result, ...)`, `v2_playbook_on_stats(stats)`;
  `v2_runner_on_failed(result, ignore_errors=...)`). The callback developer
  guide names `CallbackBase` but not these signatures; they are the base
  class's.
* **Attributes of the objects passed in**, none documented: `result._result`,
  `result._task`, `result._host`; `task._uuid`, `task.get_name()`,
  `task.get_path()`, `task.action`; `play._uuid`, `play.get_name()`,
  `play.get_path()`, `play.strategy`; `handler._uuid`; `stats.processed`,
  `stats.summarize(host)`, `stats.custom`.
* **Plugin plumbing**: `CALLBACK_TYPE = "aggregate"` and
  `CALLBACK_NEEDS_ENABLED = False` make the plugin load whenever its
  directory is on `ANSIBLE_CALLBACK_PLUGINS`, without touching
  `callbacks_enabled` (`CLAUDE.md`, "Callback plugin"); `set_options()` /
  `get_option()` with options declared in the plugin's own `DOCUMENTATION`;
  `self._display`.
* **Strategy behavior** that decides when events arrive: under `linear` and
  `debug` a task is announced once (`v2_playbook_on_task_start`) and
  `v2_runner_on_start` follows per host; under `free` and `host_pinned`
  only `v2_runner_on_start` exists. `_LOCKSTEP_STRATEGIES` encodes that
  (`StrategyFree.md`).
* **Task identity**: `task._uuid` is stable across all events of one task,
  which is how `findOrCreateTask` (`internal/playbook/aggregate.go`) matches
  results to rows. The `path` (`file:line`) of a role task is the role's
  file, of an include's tasks the included file; the source index is keyed by
  exactly that string.

**Failure mode**: an `AttributeError` inside a hook would reach ansible; most
hooks are small enough that the first event fails loudly. The pre-flight gate
(`StartFirstGeneration`) turns "no event at all" into a printed error.
`callback-tests/test_events.py` exercises the plugin against real ansible.

## 2. Wrapping `Display.display` (class A)

`_hook_display` replaces `display.display` on the shared `Display` object to
see every `[WARNING]: ...` / `[DEPRECATION WARNING]: ...` ansible prints
itself (a deprecated module, a reserved variable name) because those never
reach a task result (`Warnings` in the drill-down).

* Assumes `Display.display(msg, ...)` is the single funnel in the main
  process, that worker processes proxy their messages to it (we ignore
  anything where `os.getpid()` differs from the main pid), and that the text
  starts with exactly `[DEPRECATION WARNING]: ` or `[WARNING]: `.
* Skips the line starting `Deprecation warnings can be disabled` (the
  boilerplate ansible appends).
* **Failure mode**: swallowed (`except Exception: pass`), the original
  `display` is always called; warnings just don't appear as task notices.

## 3. Data tagging: origins and vault values (class A)

ansible-core 2.19 tags values as they flow; we read three tag classes in
`tangsible_snapshot` (also used by the host verb's Variables tab):

* `ansible._internal._datatag._tags.Origin` (`path`, `line_num`,
  `col_num`, `description`), via `Origin.get_tag(value)`: where a variable
  was defined (`VariableOrigins.md`). `-e` has only a `description`
  (`<CLI option '-e'>`).
* `ansible._internal._datatag._tags.VaultedValue` (`is_tagged_on`): an
  inline vault value.
* `ansible.parsing.vault.EncryptedString`: the type of an inline `!vault`
  value.

**Why this one matters most.** Beyond labelling, `tangsible_snapshot` is how a
vault secret is kept off the screen: it collects the decrypted strings so
`internal/session/vaultmask.go` (and the plugin's own `_mask`) can replace
them, also inside derived values (`"pre-{{ secret }}"`). The wholly encrypted
`vars_files` case has no tag at all, so we detect it by reading the file's
first bytes (`$ANSIBLE_VAULT`) and the value's `Origin.path`. If the private
import fails (older ansible, a refactor), `Origin`/`VaultedValue` are `None`,
the lists stay empty and **nothing is masked by tag** - the Resolved,
Variables and watch views would show vaulted values in clear. Treat a failing
import as a release blocker for the debug and host verbs
(`Data Tagging.md`, "Spike findings", has the cases the e2e tests pin:
`e2e/debug_test.go`, `e2e/vault_test.go`).

Also assumed: an origin's `path` and `line_num` refer to the file ansible
loaded (the shadow copy in `debug`, mapped back by `internal/origin`, with
the lines `debugger: always` inserted subtracted).

## 4. The plugin's registered module name (class B)

The action plugin (`tangsible_vars.py`) reaches the callback plugin's helper
with `sys.modules["ansible.plugins.callback.tangsible_events"]`, and the debug
snapshot expression does the same
(`internal/session/vaultmask.go`: `debugSnapshotExpr`). The name is what
ansible's plugin loader registers a callback plugin's module under - an
implementation detail of the loader. If the loader names modules differently
the action plugin reports "needs tangsible's callback plugin to be loaded"
and the debug snapshot query fails (the views show unmasked, unlabelled
`task_vars`; see #3).

## 5. The playbook parser (class A) - `view`

`internal/staticplay/loader.py` (run as `python3 -c`, so the process has
ansible importable) uses `ansible.playbook.Playbook.load`, `Play.compile()`,
`Block` (`block`, `rescue`, `always`), `Task` (`action`, `get_name()`,
`get_path()`, `tags`, `when`, `_role`), `ansible.plugins.loader.
init_plugin_loader()` (needed so `ansible.builtin.*` resolves),
`ansible.vars.manager.VariableManager(loader=...)` with no inventory, and
`ansible.parsing.dataloader.DataLoader`. All are the classes
`ansible-playbook` itself uses, none is a supported API.

* Implicit `meta` tasks (`action == "meta"`) are dropped to match
  `--list-tasks`.
* Without an inventory, a `hosts:` or name that needs variables stays
  unexpanded (shown as written).
* The interpreter is read from `ansible-playbook`'s shebang line
  (`staticplay.interpreterOf`), because `python3` need not be the one ansible
  is installed in (pipx). Inside an execution environment: plain `python3`.
* **Failure mode**: any exception prints its message on stderr, exit 1;
  `view` prints `couldn't read <playbook>: ...` and never starts the TUI
  (`e2e/view_test.go`). A reload (`R`) keeps the old tree.

## 6. The task debugger's REPL (class B/A) - `debug`

`debugger: always` and the debugger's commands are documented ("Playbook
debugger"). What goes beyond the documentation (`internal/debugrun/
session.go`, `DebugMode.md`):

* **The prompt**: `[<host>] TASK: <name> (debug)> ` (or `HANDLER:`), from
  ansible's `Debugger` class in `plugins/strategy/__init__.py`. `promptRe`
  matches it, and everything is read until it appears. A reworded prompt
  makes every read block forever.
* **Scope objects**: `task_vars`, `task`, `host`, `result`, `play_context`.
  `task_vars`, `task` and `host` are in the docs; we also read
  `task.register` (the register name) and `result._return_data` (the value a
  failed/ok task registered) - private (`livesession_debug.go`). Both are
  asked for through one expression that handles the two shapes `result` has
  had (`registeredValuesExpr`, "Tested releases" below).
* **Evaluating expressions**: queries are sent as `p print(__import__
  ('json').dumps(<expr>, default=str)) or 0`: `p` runs a Python expression,
  and the `or 0` keeps the debugger from printing the `None`. Responses
  hundreds of KB long (a host's gathered facts) are read with a bounded
  prompt scan (`promptCheckTail`), after one version took ~20 s on a
  100 KB reply.
* **Repairing `register:` after `update_task`** (2.22): see "Tested
  releases"; `Session.Redo` sends one more line after `update_task`.
* **Assigning**: `expr = value` in the watch panel is sent as a bare
  `task_vars[...] = value` line, which the debugger executes as Python
  because no command matches - relies on `Debugger`'s fall-through to
  `exec`. `update_task` (documented) then re-creates the task from
  `task._ds` and templates it with the changed `task_vars`; the next `redo`
  sees them. They never reach later tasks (those get variables fresh).
* **`redo` is not a re-pause promise**: the redone task goes back in the
  queue and the next pause may be another host or task; `Session.Redo`
  documents the cases.
* `--step` is rejected up front (a second, unhandled pause mechanism).

## 7. Strategies and stdout under the debugger (class D)

A debug run starts `ansible-playbook` with `ANSIBLE_NOCOLOR=1`,
`ANSIBLE_STRATEGY=linear` and `ANSIBLE_STDOUT_CALLBACK=minimal`, plus our
callback plugin (events on an extra fd). Assumed: the minimal callback's
output cannot be confused with the prompt, colors would split the prompt's
text, and a play-level `strategy:` is overridden by what
`debugcopy.ForceLinearStrategy` rewrites in the copy (a `strategy:` line is
found with a regexp and a small YAML walk). `strategy: free` therefore runs
as linear in `debug` (`DebugMode.md`).

## 8. Re-implemented task discovery (class E)

Two packages decide, from the YAML text, what a "task" is:

* `internal/source` (the Task definition tab, tag autocomplete) and
  `internal/debugcopy` (where to splice `debugger: always`) share
  `ListTaskLocations`/`BuildTaskSourceIndex`: play task lists (`tasks`,
  `pre_tasks`, `post_tasks`, `handlers`), a task's `block`/`rescue`/`always`
  lists, the role layout (`roles/*/tasks|handlers`), and the "top-level
  sequence items" of an included task file. The key lists are literals
  (`playTaskListKeys`, `blockTaskListKeys`); `ReservedTagNames` is a copy of
  ansible's built-in tags.
* The index is keyed by the `file:line` ansible reports, which assumes our
  scan and ansible agree on the line a task starts at (the first key's line
  of its mapping, not of a `- ` on an earlier line).
* The splice inserts a `debugger: always` sibling key textually, at the
  task's own indentation, into a hardlink copy of the project (so the user's
  files are never touched). A construct we don't recognize as a task gets no
  breakpoint; a wrongly detected one produces a playbook ansible rejects
  (shown as the error of the first `--list-tasks`).

`staticplay` is the exception: it asks ansible (#5) instead of scanning, and
is the better base for anything new.

## 9. `--list-tasks --list-hosts` (class C)

`internal/runner/progress.go` parses the combined output
(`play #N (name): title\tTAGS: [...]`, `pattern:`, `hosts (N):`, indented
task lines `name\tTAGS: [...]`) with three regexps, for the progress
percentage and, in `debug`, for the whole predicted tree. It relies on:
a role task's name in the listing equalling the event's `task.name`
(`role : name`); `--list-hosts` honoring `--limit` while `--list-tasks`
alone does not; both flags merging into one block; a play with `hosts (0)`
firing no task event. An unrecognized line is skipped silently. In `run`
this only costs the percentage; in `debug` a probe that yields nothing is
fatal (`PrepareSession`).

## 10. Warning text and the `Origin:` line (class C)

* The `[WARNING]: ` / `[DEPRECATION WARNING]: ` prefixes (#2).
* From 2.19 a message that has a source says so on its own line,
  `Origin: <file>:<line>:<col>`; `internal/playbook/notice.go` extracts it
  (`originLine`) and attaches the message to the task defined closest above
  that line in the file, within `noticeReach` (25) lines, whichever started
  first. A message without one goes to the task started last.
* `FilterRedundantWarnings` drops stderr lines starting `[WARNING]:` from the
  post-TUI dump (the drill-down already shows them).

## 11. Become failures (class C)

`internal/ee/hint.go` recognizes "Duplicate become password prompt" and
"Premature end of stream waiting for become success" in the output to offer a
one-time hint for `become` on localhost inside a rootless container. Worded
as observed on ansible-core 2.21 (per the file's comment); a rewording only
loses the hint.

## 12. The version banner (class C)

`[core X.Y.Z]` in the first line of `ansible --version` /
`ansible-playbook --version` (`hostCoreRe`, `coreVersion`): `ee init` uses it
to pick the community base image matching the host, `ee verify` to report
the image's ansible-core. No match: the built-in default image.

## 13. `ansible-doc` (class C/D)

* `FetchAnsibleDoc` shows `ansible-doc <action>`'s plain text in the Docs tab
  after stripping ANSI sequences (ansible-doc writes some even when piped).
* Context help (`internal/ansibledoc/contexthelp.go`) parses `ansible-doc -t
  module -j <action>` and `-t keyword -l` / `-j`: the JSON's `doc` /
  `options` / `suboptions` / `type` / `description` shape (a documented flag,
  not a documented schema), the module-description markup
  (`I() B() C() U() M() L() O() V() P() E() RV()` - `markupRe`), and the
  fact that an unknown module only warns and prints `{}`.
* `internal/eeinit/host.go` asks `ansible-doc -t <kind> -j <names...>` in
  chunks to map module and lookup names to collections; unknown names make
  ansible-doc exit non-zero, which is tolerated.
* The set of task keywords and which keyword is also a module option
  (`name:`) is decided from this data (`taskkeys.go`).

## 14-15. Facts and the fact cache (class D) - `host`

* `ansible-inventory --host <name>` includes the **cached facts** of the host
  when `fact_caching` is configured: the Summary tab uses that as a fast
  path, treating a result that contains `ansible_architecture`
  (`FactCacheCanaryKey`, part of the minimal gather subset) as "a gather has
  happened". To tell facts from inventory variables a second dump runs with
  `ANSIBLE_CACHE_PLUGIN=memory` and `ANSIBLE_CACHE_PLUGIN_CONNECTION=` empty,
  and the difference is the facts.
* An explicit `setup:` task is used for the live gather, because the
  implicit one of `gather_facts:` is skipped with a warm cache under smart
  gathering (and does not consult the cache when it does run).
* `ansible.builtin.mount_facts` (ansible-core 2.18+) is probed for and used
  if present; the extra mounts it stores as `mount_points` in the cache are
  how a cache we completed is recognized.

## 16. Role and collection paths (class D/E)

`internal/rolepath` asks `ansible-config dump --format json` for
`DEFAULT_ROLES_PATH` and `COLLECTIONS_PATHS` (config names, in the config
reference) and falls back to the built-in defaults when that fails:
`~/.ansible/roles`, `/usr/share/ansible/roles`, `/etc/ansible/roles`,
`/usr/share/ansible/collections`. The project's own `./roles` (next to the
playbook) is searched first, as ansible does. The dump's entry shape
(`internal/ansibleconfig`) is assumed; the effective value of a setting
depends on its source, which we don't interpret.

## 17. The vault format (class E)

`internal/vaultcrypto` implements what `ansible-vault` writes and reads: the
`$ANSIBLE_VAULT;1.1;AES256` header (and `;1.2;AES256;<label>` for a vault
id), the hex-encoded salt/HMAC/ciphertext body, PBKDF2 key derivation
(10000 iterations, "matches Ansible's own VaultAES256"), AES-CTR with PKCS7
padding and an HMAC-SHA256 check. Nothing here is a public specification; it
was written from the source. Tests pin it two ways: a golden fixture made
by a real `ansible-vault encrypt_string` (`TestDecrypt_GoldenFixture`) and a
check that the real `ansible-vault` decrypts our output (skipped when it
isn't installed); also `vaultfile_test.go` and `e2e/vault_test.go`.
`internal/vaultfile` additionally assumes how `!vault |` blocks are
indented in YAML (it splices the original bytes of unchanged values because
yaml.v3 cannot reproduce ansible-vault's indentation).

Failure mode is conservative: an unreadable header or a failed HMAC stops
with an error and the file is not rewritten; a value no password opens is
kept byte for byte (`<vaulted: label>`).

## 18. Ansible's own variable names (class E)

`_BUILTIN_VARS` in the callback plugin is a literal copy of the special
variables (`inventory_hostname`, `groups`, `ansible_play_hosts`, ...) so they
are labelled `builtin` and left out of the Variables tab. A new special
variable is shown as a user variable until the list is updated.

## 19. Exit codes and the flag table (class B/E)

* `ansible-playbook`'s exit codes: 4 for unreachable (also used for a parse
  error, told apart by whether any host was unreachable) and 99 for
  "interrupted", which is also what tangsible reports for its own Ctrl-C
  (the only source of SIGINT to the child; `CLAUDE.md`, "Exit codes").
* `internal/config/playbookargs.go` is a static table of which
  `ansible-playbook` flags take a value, to find the playbook among options
  (`ImplicitRun.md`). `TestAnsiblePlaybookFlagTable` compares it with the
  installed `ansible-playbook --help` (skipped without ansible); a flag
  missing from the table is treated as a switch, so the one misreading is a
  *new* value-taking flag placed before the playbook.
* `--ask-pass` style flags are swapped for password files before the TUI
  (`internal/credfile`), which assumes the password-file flags are equivalent.

## 20. Adjacent conventions: execution environments, Galaxy (class D)

Not ansible-core, but the same kind of risk:

* The EE runner assumes the layout of the community execution-environment
  images (ansible-runner conventions): a writable `/runner`, a usable home
  (we provide `/runner/home` as a tmpfs), `ansible-playbook` on `PATH`, and
  `ANSIBLE_CALLBACK_PLUGINS` pointing at our mounted plugin
  (`internal/ee`, `ExecutionEnvironments.md`). Events cross the container
  boundary through a FIFO because an inherited fd cannot.
* `ee init` asks Galaxy (`/api/v3/plugin/ansible/content/published/
  collections/index/<ns>/<name>/versions/`) for `requires_ansible`, and the
  registry (ghcr.io) for image tags. Best effort; never fails the command
  (`EEInit.md`).

## Keeping this current

* **When a new ansible-core series appears**, `make test-ansible VERSION=2.20`
  (`scripts/test-ansible.sh`) runs all three suites below against it in a
  virtualenv of its own, with an empty HOME and no `ANSIBLE_*` variables so
  the result is about the release and not your setup. By hand, the same, in
  this order:
  `python3 -m unittest discover -s callback-tests` (callback plugin and
  helpers), `go test ./...` (the flag-table test, parsers), then
  `go test -tags e2e ./e2e` - especially `TestE2E_Debug_*` (REPL, prompt,
  snapshot, masking), `TestE2E_Vault_*`, `TestE2E_View_*` (loader),
  `TestE2E_EE_*` (needs podman) - and check by hand that a vaulted value
  never shows in clear in the debug Resolved and Variables tabs (#3).
* **When adding code that talks to Ansible**, add a row here and say which
  class it is. Prefer, in order: a documented command line; asking ansible
  from inside a process it runs (as `staticplay` and the plugins do) over
  scraping text; a regexp over human output last. Make a failed assumption
  fall back to "show less", never to "show a secret".
* The research documents behind the rows: `OwnCallbackPlugin.md`,
  `DebugMode.md`, `Data Tagging.md`, `VariableOrigins.md`, `Vault.md`,
  `ExecutionEnvironments.md`, `HostVerb.md`, `View.md`.

## Tested releases

Results of `make test-ansible VERSION=...` (all three suites, a venv with
`ansible.posix` for the callback parity tests):

| ansible-core | callback (Python) | unit | e2e | Notes |
|---|---|---|---|---|
| 2.19.11 | ok | ok | ok | the series everything was developed on |
| 2.22.0 (2026-11-26) | ok | ok | ok | needed a port of the debug mode, see below |

**2.22.0** (first run: callback ok; 5 unit failures, 6 e2e failures; all
fixed below, then all three suites green):

* The callback plugin passes, including byte-for-byte parity with
  `ansible.posix.jsonl`: #1 and #2 hold.
* **The debugger's `result` object changed (#6).** It is now a
  `HostTaskResult(host, task, utr)` (`ansible/_internal/_task.py`) holding a
  `UnifiedTaskResult`; `result.is_failed()` and `result._return_data` are
  gone, and `task.register` is a mapping `{name: expression}` instead of the
  name. Ported without a version switch in Go: one debugger expression
  (`registeredValuesExpr`, `livesession_debug.go`) returns "what the task
  registered" as a name -> value mapping on both shapes -
  `result.utr.registered_values` from 2.22, `{task.register:
  result._return_data}` before - and the tests ask `result.utr.failed` /
  `result.utr.as_result_dict()` the same way (`session_test.go`).
  `mergeRegisteredResult` now merges every registered name (a 2.22 task can
  register several) and reports the first.
* **`update_task` breaks tasks with `register:` (an ansible-core 2.22
  defect, as far as we can tell).** The `register:` string is translated
  into a projection by `Task._validate_register` when a task is first loaded;
  the debugger's `update_task` reloads `task._ds`, leaves the raw string, and
  the redo then dies in the worker (`'_AnsibleTaggedStr' object has no
  attribute 'items'`, "A worker was found in a dead state"). `Session.Redo`
  therefore sends one more line after `update_task` that calls
  `task._validate_register(None, 'register', task.register)` when
  `task.register` is still a string and the method exists - a no-op before
  2.22 (no such method) and on a release that fixes it (not a string any
  more). Private API (class A), pinned by `TestSession_Set*`.
* **Vaulted values print as `$REDACTED$`** in an ad-hoc `ansible ... -m debug
  -a var=x` (also `msg={{ x }}`); a filter's result (`| upper`, `| b64encode`)
  is plain text again. Only the e2e tests' way of reading a value back was
  affected, and now decodes `b64encode`'s output; tangsible's own handling of
  vaulted values is unaffected. (Worth knowing for #3: the marker survives
  direct use of the value, not a filter's result.)
