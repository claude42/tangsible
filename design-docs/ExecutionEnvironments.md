# Supporting execution environments

## Status

**Implemented** for `run`, `rerun` and `role` (and reruns started from
`revisit`) with podman on Linux. Not supported: docker, macOS, `debug`,
and the standalone verbs (`host`, `template`, ...). The sections after this
one record how we got here: "Decisions", the phase-by-phase findings
(including what live testing changed), and, last, the original
investigation, which predates the implementation and is partly superseded
(see the note before it).

Originated from a question about whether Tangsible should interface with
`ansible-runner` (`OwnCallbackPlugin.md` was the trigger). The real
motivation turned out to be narrower and more useful than "support
ansible-runner": when developing a playbook/role, verify it actually runs
correctly inside the target *execution environment* (EE) - the
`ansible-builder`-produced container image a project will actually be run
against in CI/AWX/Controller - not just against whatever `ansible-core`
happens to be on the developer's host.

## How it works

- **Selecting it:** `[execution_environment]` in `config.toml` (`image`,
  `engine`, `network`, `selinux`, `read_only`, `ssh_agent`,
  `ssh_known_hosts`, `ssh_dir`, `extra_mounts`; `ConfigOptions.md`),
  `--ee <image>` / `--no-ee` per invocation, the re-run dialog's
  "Execution environment" field per re-run. What a run used is recorded in
  history; `rerun` and reruns from `revisit` default to it (flag >
  recorded > config).
- **One command, wrapped:** `internal/ee.Build` (pure) turns the resolved
  inputs into a `podman run --rm -i ... <image> ansible-playbook ...`
  line. Tangsible still spawns one process and owns its stdio; Ctrl-C is
  forwarded to podman, which proxies it into the container.
- **Identity mounts:** the project root (where the working directory and
  the playbook meet) and every path an argument names are mounted at their
  own host path, never translated - event `task.path`, saved logs and the
  host-side source index must agree.
- **Events:** the callback plugin is mounted read-only and writes to a FIFO
  in a mounted scratch directory (`TANGSIBLE_EVENT_FILE`); an inherited fd
  can't cross a container boundary. stdout stays the user's own callback
  output.
- **Credentials:** the SSH agent socket is forwarded and `known_hosts`
  mounted read-only; private keys stay out unless `ssh_dir = true`.
- **Everything that runs ansible for display** (probe, `ansible-doc`,
  Docs/File/Resolved stubs, the condition resolver) goes through
  `runner.NewToolCommand`, so it uses the same ansible and collections as
  the run.
- **Code:** `internal/ee` (builder), `internal/runner/ee.go` (active
  setting, pull, FIFO, tool commands), `internal/config/ee.go`
  (settings, flags, history precedence), `internal/session/ee.go`
  (startup), the dialog field in `livesession_rerundialog.go`.
- **Tests:** unit tests per package; `TANGSIBLE_EE_IMAGE=<image> go test
  -run Smoke ./internal/ee` (real podman, builder output); `TANGSIBLE_EE_IMAGE=<image>
  go test -tags e2e -run EE .` (tmux: indicator, dialog switch to host and
  back, `rerun` reusing the recorded image, Ctrl-C and cleanup, flag
  errors). The plugin's `callback-tests/` cover the FIFO transport; it was
  also run by hand on Python 3.9 / ansible-core 2.15.

## Not verified, or open

- **SELinux:** `selinux = auto|disable|z|Z|none` is covered by builder unit
  tests only; the dev machines had no SELinux, so `--security-opt
  label=disable` and `:z`/`:Z` were never run against an enforcing host.
- **docker, macOS, `debug`:** deliberately out of scope (`debug` is
  designed for - see "Debug mode in an EE" - not built).
- **Real-host coverage** came from one target (Debian 13, ssh agent
  authentication, `become`); other connection types (password prompts,
  `ansible_connection=winrm`, jump hosts via `~/.ssh/config`, which the
  container does not see unless `ssh_dir = true`) were not tried.
- Findings that depend on the machine (default-network DNS failing behind
  WireGuard policy routing, `network = "host"`) are in the real-host notes
  below.

## Decisions

Settled in discussion after the investigation below. Where they conflict
with the investigation text, this section wins - the investigation was
written before the clean-room plugin (`OwnCallbackPlugin.md`) existed
and still assumes `ansible.posix.jsonl` on stdout. Today's plugin
(`callback/tangsible_events.py`) has *only* an fd sink
(`TANGSIBLE_EVENT_FD`), so there is no stdout-jsonl mode to fall back to
without building one.

- **Platform:** Linux only for now. macOS (engine in a VM, so FIFO and
  `SSH_AUTH_SOCK` mounts are unreliable) is out of scope.
- **Engine:** podman only in v1. The command builder takes the engine as
  a parameter so docker (`--user` instead of `--userns=keep-id`, other
  group/IPC flags) is a small later delta. `engine` accepts only
  `"podman"`; anything else warns and falls back, saying docker isn't
  supported yet.
- **Event transport:** a FIFO in a bind-mounted scratch dir, named to the
  plugin via a new `TANGSIBLE_EVENT_FILE=<container path>` (plugin opens
  it instead of using an fd). Same mechanism for any engine; keeps stdout
  free, so the `o` view and the user's own stdout callback keep working.
  Fallback if the spike shows FIFOs misbehaving: a plugin stdout-jsonl
  mode (costs the `o` view in EE mode). `--preserve-fds` (podman-only) was
  considered and rejected as non-portable to docker.
- **Verbs:** `run`, `rerun`, `role` in v1. `revisit` needs no EE support
  itself, but a rerun started from inside revisit must work. `debug`
  rejects EE mode with a clear message in v1 (like its `--step`
  rejection), but is wanted later - see below. `host`/`template` stay
  host-only.
- **Side processes** (progress probe, `ansible-doc`, File/Resolved tabs,
  start-with-play, `role` stub) run in the same EE, otherwise the tabs
  describe the host's collections rather than the EE's.
- **Rerun from revisit:** the EE image is recorded in the run's history
  entry; a rerun defaults to it, shown editable in the rerun dialog
  (clearable to run on the host).
- **Mounts (identity mounts - revised in phase 4, see below):** the
  project root (common ancestor of cwd and the playbook's directory) is
  mounted at *its own host path*, and so is every other path a known
  argument names (`-i`, `-e @file`, `--vault-password-file`,
  `--private-key`, `--limit @file`, ...; read-only, outside the root).
  Arguments are never rewritten. Anything else that looks like an
  absolute host path warns. (Originally the root was to be mounted at
  `/runner/project` with arguments translated.)
- **SELinux:** when enforcing (`/sys/fs/selinux/enforce`), default to
  `--security-opt label=disable` rather than `:Z` (which permanently
  relabels the host project dir); `selinux = "disable" | "z" | "Z" |
  "none"` overrides. Low priority if dev machines don't run SELinux.
- **Config/flags:** `[execution_environment]` in `config.toml` is the
  primary place (cascades like every key); `--ee <image>` overrides per
  run, `--no-ee` forces host. No env var for now. Update `completions/`,
  `README.md`, `man/`.
- **Smaller points:** the plugin runs on the EE's Python (possibly 3.9 or
  older - test for it); pull before the TUI starts with printed
  progress; map engine exit codes 125-127 apart from ansible's own (99,
  4); `--name` the container for fallback `kill`; `--userns=keep-id` on
  rootless podman; set `HOME` explicitly so `~/.ssh` resolves; forward
  `ANSIBLE_*` and proxy variables but not `ANSIBLE_CONFIG` or the host's
  `ansible.cfg`.

### Debug mode in an EE (later, not v1)

`debug` drives ansible over a second pipe pair (breakpoint notifications
out, commands in) next to the event stream. The FIFO transport extends to
it: two more FIFOs in the same scratch dir. No substantial blocker, but:
the debugger plugin/strategy code must be mounted and run on the EE's
ansible-core/Python; the hardlink shadow copy must stay on the same
filesystem as the project and is then mounted as the project root;
symlinks pointing outside the mount root break. The stdout-jsonl
fallback would rule `debug` out (no return channel).

### Phasing (all phases done)

0. Spike by hand: `podman run` against
   `ghcr.io/ansible-community/community-ee-base`, plugin + FIFO mounted;
   verify events flow, SIGINT reaches the container, ownership under
   rootless podman. Findings go back into this doc; confirms FIFO vs
   stdout. (Needs rootless podman that works - not an unprivileged LXC
   without matching subuid ranges and nesting.)
1. Plugin: `TANGSIBLE_EVENT_FILE` mode, Python tests in
   `callback-tests/` incl. old-Python compatibility.
2. Pure command builder (`internal/ee`): engine detection, mount planner,
   path rewriting, env passthrough, flags; table-tested.
3. Wire into `SpawnGeneration`: config + `--ee`/`--no-ee`, pull step,
   exit-code mapping, container cleanup, Ctrl-C/exit-99 check.
4. Side processes in the EE (probe, `ansible-doc`, File/Resolved tabs,
   start-with-play, role stub).
5. UX: top-bar indicator, rerun-dialog field, history, help tables and
   bottom bars, `Keyboard-shortcuts.md`, completions, README, man,
   changelog, `Codemap.md`.
6. tmux e2e gated on podman; rewrite this doc as "implemented".

### Phase-by-phase findings (spike through real-host testing, 2026-10-31)

Hand-run `podman run` (podman 5.4.2, rootless, crun, Debian 13 VM, no
SELinux) against `ghcr.io/ansible-community/community-ee-base:latest`
(Python 3.14, ansible-core 2.21, `ansible.posix`/`utils`/`windows`
bundled, default `HOME=/runner`, entrypoint `dumb-init`).

- **FIFO transport works.** Host `mkfifo` in a scratch dir, bind-mounted
  rw; the container opened it as fd 3 (`exec 3>.../events`, then the
  existing `TANGSIBLE_EVENT_FD=3`) and all 17 events of
  `testdata/outcomes.yml` arrived on the host reader, stdout staying
  separate. The unmodified plugin ran fine on Python 3.14. So the
  stdout-jsonl fallback is not needed; phase 1 only has to let the
  plugin open a path (`TANGSIBLE_EVENT_FILE`) instead of an fd. Host side
  must have the FIFO open for reading before the plugin opens it for
  writing (open-for-write blocks otherwise), and keep a read end open to
  avoid a spurious EOF.
- **SIGINT:** `kill -INT` on the `podman run -i` process (no `-t`)
  reaches ansible in the container ("User interrupted execution"),
  podman exits **99** (ansible's own code passed through, matching the
  existing exit-99 contract), and `--rm` leaves no container behind.
- **Ownership:** with `--userns=keep-id`, a file the playbook wrote into a
  rw mount is owned by the host uid, but its *group* shows up as a
  subgid (100000), not the user's primary group - cosmetic, but worth
  knowing (`--userns=keep-id:uid=...,gid=...` is the knob if it matters).
- **Engine failures:** a missing image (`--pull=never`) gives exit **125**;
  a nonexistent command inside the container gave 2 here (not 127), so
  only 125 can be reliably attributed to the engine - the exit-code
  mapping must not assume 126/127.
- **Phase 1 done:** the plugin opens `TANGSIBLE_EVENT_FILE` itself (fd
  still wins if both are set); verified in the container (17/17 events
  through the FIFO) and covered by `callback-tests/`.
- **Phase 2 done:** `internal/ee` (`Build`) is table-tested, and a
  real-podman smoke test (`TANGSIBLE_EE_IMAGE=... go test -run Smoke
  ./internal/ee`) runs `testdata/outcomes.yml` through its output.
  Choices made there: the project root is the common ancestor of cwd and
  the playbook's directory (error if that is `/`); the playbook is passed
  as an absolute container path; forwarded env values travel as bare
  `-e NAME` plus the engine process's own environment, never on the
  command line; host-path-like `ANSIBLE_*` variables (`*PATH*`, `*FILE*`,
  `*CONFIG*`, `*HOME*`, `*DIR*`, `*PLUGINS*`, `LIBRARY`, `*TMP*`,
  `ANSIBLE_INVENTORY`) are dropped; `~/...` arguments can't be mapped and
  warn; the project mount is read-write unless `ReadOnly`.
- **Phase 3 done:** `[execution_environment]` config (`config/ee.go`:
  image, engine, network, selinux, read_only, ssh_agent, ssh_dir,
  extra_mounts; cascades like `[general]`), `--ee`/`--no-ee` (stripped like
  `--dialog`; other verbs reject them, `debug` rejects with its own
  message unless `--no-ee`), `runner/ee.go` (process-wide active setting,
  `EnsureImage` before any TUI, per-generation FIFO, `KillActiveContainer`
  fallback after Ctrl-C's grace period), `SpawnGeneration` branching on
  host vs. EE. Verified live in tmux: run, `o` view, Ctrl-C (exit path,
  no container or scratch dir left), rerun from the dialog, and the
  agent socket (`ssh-add -l` inside the container lists the host's key).
  The FIFO's EOF is driven by a held write end that closes when the engine
  process's stderr ends (`eeRun.finish`). Engine failures need no special
  mapping: podman's own stderr already surfaces as the failure text.
  Warnings from the builder are queued (`config.QueueWarning`) and printed
  after the TUI closes. Still open from phase 3's list: nothing, except
  that the side processes (phase 4) and the history/rerun-dialog EE
  field (phase 5) are not done - until then a rerun uses the session's EE.
- **keep-id and HOME (found after phase 3's first live run):**
  `--userns=keep-id` makes podman inject a passwd entry whose home is the
  *working directory*, and that overrides `-e HOME=...` - so ansible
  created `.ansible/tmp` inside the user's project and `~/.ssh` would have
  resolved there instead of at the `/runner/.ssh` mount. The builder now
  passes `--passwd-entry='$USERNAME:*:$UID:$GID:tangsible:/runner:/bin/sh'`
  (podman substitutes the variables), giving `HOME=/runner`, with
  `.ansible` in the container's own filesystem.
- **Identity mounts (revised in phase 4):** the first live run showed
  events carrying `task.path` as `/runner/project/...`, while everything
  host-side keys on host paths - the task source index (Task definition
  tab), the File tab, role detection, and `revisit`, which replays saved
  logs - so a translated layout would silently break all of them unless
  paths were also mapped back in the event stream, the saved logs and
  revisit. Mounting the project at its own path avoids the whole class:
  event paths, `playbook_dir`, stub playbooks' absolute references and
  saved logs mean the same thing on both sides. Cost: the root can't lie
  inside a directory the image needs (`/usr`, `/etc`, `/bin`, `/sbin`,
  `/lib*`, `/proc`, `/sys`, `/dev`, `/run`, `/runner` - rejected with a
  message). Only `HOME` (`/runner`), the plugin (`/runner/plugin`) and
  the event dir (`/runner/events`) use fixed container paths.
- **Phase 4 done:** every side process goes through
  `runner.NewToolCommand` (host or EE): the progress probe
  (`--list-tasks`), the Docs tab (`ansible-doc`), the Resolved tab's stub
  render, the File tab's fetch stub, and the debug condition resolver
  (`python3`). Stubs and their outputs live in one scratch dir mounted
  read-write at its own path (`ee.Inputs.Scratch`; a stub playbook inside
  one doesn't pull the project root towards `/`). One-shot runs have no
  event fd/FIFO: the plugin's display fallback puts events on stdout, as
  the host path always did. `fetchfile.go` used
  `ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl` (a leftover from before
  the own plugin; an EE image needn't ship that collection) and now uses
  our plugin like the other scrapes. Verified live: the Docs tab in EE mode
  shows the module path inside the image's Python 3.14; Task definition
  works; a throwaway test fetched the *image's* `/etc/os-release` through
  the fetch stub. Start-with-play's trimmed copy and the role stub sit in
  the project directory, so they need nothing extra (not exercised live).
  Caveat inherent to EEs: with `localhost` as a target, what a run changes
  lives in a container that is gone afterwards, so the File tab (already
  skipped for localhost) and anything inspecting local state afterwards
  see the host, not the run.
- **Phase 5 done:** (1) *History:* `InvocationRecord.EE` holds the image,
  `"host"` for a host run made while an image was configured (so a rerun
  doesn't quietly switch to the configured image), or nothing;
  `config.ResolveEEWithHistory` applies flag > recorded > config.
  `rerun` activates the environment only after `ResolveRerun` (which now
  also returns the recorded one); `OpenRevisitEntry` sets it per opened
  entry, so a rerun from revisit uses that run's environment (imported
  runs record none: config applies). (2) *Dialog:* an "Execution
  environment" text field (last field, after the checkboxes that belong with "Limit hosts to"; omitted in debug), pre-filled once
  from the active image; on confirm it sets the process-wide setting
  (`ResolveEE` with the typed image, or host if empty) before
  `requestRerun`, so no signature changed. A rerun that names an
  unpulled image pulls it inside the generation goroutine, with the
  engine's output going to the `o` view (the TUI owns the terminal).
  (3) *Top bar:* `Playbook: site.yml (EE: community-ee-base:latest)`,
  read fresh on each rebuild; not shown for a frozen revisit replay or in
  debug. (4) Help table, man page, README, Keyboard-shortcuts.md.
  Verified live: indicator; clearing the field switches a rerun to the
  host (indicator gone, history shows image then nothing); a fresh
  `rerun --no-dialog` reuses the recorded image and `--no-ee` overrides it.
  Not exercised live: rerun from `revisit` (unit-level only: the
  precedence is covered by `TestResolveEEWithHistory`).
- **Real remote host (shunya.aw.net, Debian 13, ssh as `ansible` with
  passwordless sudo, key only in the agent) - findings:**
  - Host baseline and EE with `network = "host"` both complete the same
    4-task playbook (ping, command, `become`, copy): agent-only
    authentication works from inside the container (no private key
    mounted), and so does `become`.
  - **Default container network: the hostname did not resolve** here
    (`Could not resolve hostname ...: Temporary failure in name
    resolution`) while the same run worked on the host - but the cause
    is this VM's routing, not a general weakness of the default network
    and not the local resolver (unbound on `127.0.0.1`, which podman's
    pasta backend handles by handing the container a forwarder at
    `169.254.1.1`). The VM has no IPv4 default route in its main table
    (WireGuard full-tunnel policy routing, `ip rule ... lookup 51820`);
    pasta copies the main table, so inside the container every IPv4
    address - the `169.254.1.1` forwarder and the `8.8.8.8` fallbacks -
    is "Network is unreachable", while IPv6 works. Verified: default
    network + only a reachable IPv6 resolver (`--dns`) resolves, and so
    does `network = "host"` (the host's own stack, routes and resolver).
    (An earlier version of this note blamed the loopback resolver and
    recommended defaulting `network` to `host`; both were wrong or
    overstated.) So `network = "host"` stays an opt-in workaround for
    DNS/routing oddities and for the odd target only reachable through
    host-side routing, not a default.
  - **Host-key checking:** with no `known_hosts` in the container,
    `Host key verification failed` (ansible's message gives no hint it is
    the container's missing file). Fixes verified: mount all of `~/.ssh`,
    mount only `known_hosts`, or `ANSIBLE_HOST_KEY_CHECKING=False`. Added
    `ssh_known_hosts` (default true): only `~/.ssh/known_hosts` is
    mounted read-only - public host keys, no private keys - unless
    `ssh_dir` already covers it. Verified with defaults + `network =
    "host"`: works (host networking was needed only because of the
    routing issue above).
  - **File tab on a remote host** works through the container (fetch stub:
    agent, `known_hosts`, scratch dir, our plugin on stdout); the fetched
    text was the remote file's. Unreachable hosts are reported as on the
    host. No container or scratch directory is left over after any of the
    runs.
  - Decided: `network` stays unset by default (isolation); the docs
    point to `network = "host"` for DNS/routing problems.
- **Phase 6 done:** `e2e_ee_test.go` (tag `e2e`, needs
  `TANGSIBLE_EE_IMAGE`; skipped otherwise) covers the indicator and a full
  run, the dialog switching to the host and back, `rerun` reusing the
  recorded image and `--no-ee` overriding it, Ctrl-C reaching ansible and
  the container disappearing, and the flags' usage errors. The tests work
  in a temp dir holding a copy of the playbook (a playbook elsewhere would
  put the mount root at `/`, which is rejected). Writing them found
  two things: (1) my phase 5 had put the new dialog field between "Limit
  hosts to" and the two checkboxes that write into it, which broke two
  existing e2e tests' Tab counts - the field now comes last, after the
  checkboxes (and a unit test pins that); (2) a test must not trust text
  already on screen: the previous generation's banner, or the typed command
  line, satisfies `waitFor` too early. **Older Python:** the plugin was run
  by hand on Python 3.9.25 with ansible-core 2.15.13 (`python:3.9-slim`
  with `pip install "ansible-core<2.16"`): syntax fine, all 17 events of
  `outcomes.yml` arrived over `TANGSIBLE_EVENT_FILE`.

> **Everything from here on is the original investigation**, written before
> anything was built. Where it disagrees with the sections above, the
> sections above win. Superseded: the `/runner/project` layout and
> translated arguments (now identity mounts), the stdout-jsonl fallback
> (the FIFO transport needs none), `ansible.posix.jsonl` and the "GPL
> fork" (the plugin is now our own clean-room one), the config sketch (the
> real table is in `ConfigOptions.md`), and the effort estimate and
> phasing at the very end.

## Motivation

A dev iterating on a role wants to know: "does this actually work on the
EE my CI/AWX will run it with?" - not just "does this work with whatever
`ansible-core` + collections happen to be installed on my laptop." An EE
image can differ in Python version, collection versions, or ship a
narrower FQCN surface than the dev's host. Running against it locally,
interactively, with the same live TUI Tangsible already provides, closes
that gap without needing an actual AWX/Controller instance.

## Why this isn't `ansible-runner` support

The original framing ("interface with `ansible-runner` for EE support,
since that's what `ansible-navigator` does") doesn't survive contact with
what `ansible-runner` actually does for containers. Its
`wrap_args_for_containerization()` is not deep magic - it constructs a
plain `podman run` / `docker run` command line wrapping the same
`ansible-playbook` invocation: mount `private_data_dir` at `/runner`,
`--workdir /runner/project`, `--env-file` for env vars, `--group-add=root
--ipc=host` for podman, explicit `--user=$(id -u)` for docker. There is
no ansible-runner-the-library capability happening *inside* the container
that Tangsible couldn't construct itself.

Depending on the actual `ansible-runner` package/CLI to get this would
also directly cut against `OwnCallbackPlugin.md` motivation 3 ("one less
dependency") - trading the `ansible.posix` collection requirement for a
much heavier pip package with its own dependency tree, private-data-dir
directory conventions, and a job-artifact model (`artifacts/<ident>/
job_events/*.json`) Tangsible has no other use for. See the prior
conversation's conclusion: mine `ansible-runner`'s ideas, don't adopt its
runtime.

**The one idea worth keeping from it is the container-invocation shape
itself** - and one important negative finding about its transport choice.

## The transport finding (why this touches `OwnCallbackPlugin.md`)

`grep`ing the entire `ansible-runner` source turns up zero uses of
`--preserve-fds` (podman's mechanism for passing extra file descriptors
into a container) or any docker equivalent (docker has none). This isn't
an oversight - **there is no portable way to hand a container process an
arbitrary inherited fd** the way `cmd.ExtraFiles` hands one to a plain
child process on the host. That is almost certainly *why* `awx_display.py`
(`ansible-runner`'s stdout callback) multiplexes structured JSON events
into the same stdout stream as the human-readable output via ANSI
cursor-movement escape sequences, instead of using a side channel: stdout
(and stdin, for `--interactive`) are the only channels guaranteed to
survive a `podman run` / `docker run` boundary uniformly.

**Consequence for `OwnCallbackPlugin.md` decision 3:** that doc's planned
transport - an `aggregate`-type callback writing to a dedicated pipe fd
via `cmd.ExtraFiles`, freeing stdout for the user's own callback - works
well for a plain child process, but **would not survive a container
boundary**. If EE support is ever wanted, the container-mode code path
would need to fall back to plain stdout-jsonl (today's actual mechanism,
unchanged) rather than the fd transport, since the fd simply can't be
delivered across the boundary without adopting something like
`awx_display`'s escape-code multiplexing scheme (itself GPL, itself
another Risk-A-shaped fork question, and adding real parsing complexity
for a benefit - "free the stdout slot for the user's own callback while
containerized" - that matters less here anyway, since verifying a
playbook against an EE is a different workflow than watching your normal
human-readable console output).

**Practical takeaway:** don't over-invest in the fd-based transport as
the *only* transport. Either keep today's plain-stdout-jsonl scan
available as a permanent fallback mode (used whenever containerized,
`aggregate`+fd used otherwise), or reconsider whether the fd transport's
benefit (motivation 2, capturing the user's real stdout callback) is
worth pursuing at all given it can't extend to the EE case. This is a
noted interaction, not a blocker on either doc - the two features can
still ship independently and in either order.

## Proposed design: wrap the existing invocation, don't replace it

Tangsible already builds one child-process command
(`ansible-playbook <playbook> <passthrough args>`) and scans its stdout.
EE support changes *what* that command is, not the scanning:

- **Non-EE mode (default, unchanged):** `ansible-playbook ...` directly,
  exactly as today.
- **EE mode:** `podman run --rm -i -v ... -w <workdir> <ee-image>
  ansible-playbook ...` - the same argument list, just wrapped. Tangsible
  still spawns one process, still owns its stdin/stdout/stderr, still
  forwards SIGINT the same way (`procHandle` doesn't care whether the
  process it holds is `ansible-playbook` or `podman` - the container
  engine forwards the signal into the container on `--rm -i`, matching
  today's Ctrl-C contract from `CLAUDE.md`).

### Callback plugin delivery, generalized

`OwnCallbackPlugin.md`'s plan already writes the forked `.py` to a host
temp dir and points `ANSIBLE_CALLBACK_PLUGINS` at it via env var. For EE
mode this needs one addition, not a redesign: bind-mount that same temp
dir into the container (read-only) and set the **container-side** env var
to the container-side path. This works regardless of what the target EE
image ships - no dependency on the image already having `ansible.posix`
or any bundled plugin pre-installed, which matters specifically for
"verify against an EE I don't control." Same logic applies whether the
plugin in play is stock `ansible.posix.jsonl` (today) or the eventual GPL
fork - EE support does not require the fork to exist first, and the fork
does not require EE support to exist first. Genuinely orthogonal, sharing
only this one mechanism.

### Mount list

- The project directory (playbook, roles, files) - read-only unless a
  role is known to write locally.
- The inventory file/directory.
- The vault password file, if `--vault-password-file` is in play.
- `~/.ssh`, if targeting real remote hosts from inside the container -
  worth stealing directly from `ansible-runner`'s `_handle_automounts`,
  which does exactly this, since a container has its own filesystem
  namespace and won't otherwise see the host's SSH config/keys.
- The callback-plugin temp dir (above).

No `/runner`-shaped layout is needed. That path convention is
`ansible-runner`'s own choice (for AWX/Controller compatibility) - reading
`wrap_args_for_containerization` makes clear it's not something
`ansible-builder` bakes into the image itself. A bare EE image doesn't
care what the mount point is called; Tangsible can pick something
simpler (e.g. a single `/workdir`).

### Engine choice

Default to `podman` (matches `ansible-builder`/`ansible-navigator`'s own
default - confirmed `default_process_isolation_executable = 'podman'` in
`ansible_runner/defaults.py` - and it's rootless-friendlier). Detect on
`$PATH`; support `docker` as a fallback, with the explicit
`--user=$(id -u)` docker needs that podman's own user-namespace handling
(`--group-add=root --ipc=host`) doesn't.

### Networking

Containers get their own network namespace by default - reaching real
remote inventory hosts over SSH from inside the container needs
`--network=host` (or explicit routing) or it silently fails to connect.
Worth surfacing as a config option / documented gotcha up front, not
discovered after a confusing unreachable-host failure.

### TTY

No new requirement here: Tangsible's existing "needs a real TTY"
constraint (`CLAUDE.md`) is about its *own* terminal for `tview`, not
about the child process's stdio - today's stdout-jsonl scanning already
works over a plain pipe, no pty needed, since `ansible.posix.jsonl`
doesn't care whether stdout is a tty. `podman/docker run -i` (without
`-t`) preserves that; no pty allocation needed in the container either.

## Config surface (sketch, not finalized)

Plausible shape for `.tangsible/config.toml`, following the existing
`[general]` conventions from `ConfigOptions.md`:

```toml
[execution_environment]
image = "quay.io/example/ee-minimal:latest"
engine = "podman"           # or "docker"; default: auto-detect, prefer podman
network = "host"            # only if targeting real remote hosts
extra_mounts = []           # additional host:container[:opts] bind mounts
```

Not designed in detail here - this is a placeholder for whoever picks
this up, sized to match how small the actual container-wrapping logic is.

## Roadblocks and open questions

| # | Concern | Finding / mitigation |
|---|---------|----------------------|
| 1 | Fd-based transport can't cross a container boundary | ✅ confirmed - no `--preserve-fds` anywhere in `ansible-runner`; container mode must use plain stdout-jsonl regardless of what `OwnCallbackPlugin.md` eventually ships for the non-container case |
| 2 | Does the target EE image need our callback plugin baked in | No - bind-mount + container-side `ANSIBLE_CALLBACK_PLUGINS` works against an unmodified image |
| 3 | `/runner`-style mount layout - is it required by EE images | No - confirmed it's `ansible-runner`'s own convention, not baked into images by `ansible-builder` |
| 4 | Reaching real remote hosts from inside the container | Needs `--network=host` or explicit routing - container has its own netns by default |
| 5 | podman vs docker non-root handling differs | podman: `--group-add=root --ipc=host`; docker: explicit `--user=$(id -u)` - both confirmed in `ansible-runner`'s own code, safe to copy the pattern |
| 6 | SSH keys / known_hosts inside the container | Bind-mount `~/.ssh`, same idea as `ansible-runner`'s `_handle_automounts` |
| 7 | Dependency on `ansible-runner` itself | Not needed - the container invocation is simple enough to construct directly; avoids the exact dependency weight `OwnCallbackPlugin.md` motivation 3 is trying to shed |
| 8 | Interaction with the GPL callback fork | Orthogonal - EE support works with stock `ansible.posix.jsonl` today or the eventual fork, unchanged either way, aside from the transport-fallback point in #1 |

## Relationship to `OwnCallbackPlugin.md`

Mostly independent efforts that can ship in either order or in parallel.
The one real coupling is the transport question (#1 above) - worth
deciding before finishing the fd-based transport design, since it
constrains whether Tangsible ends up with one event-transport code path
or two (fd for direct exec, stdout-jsonl for containerized). A
cross-reference note has been added to `OwnCallbackPlugin.md` pointing
back here.

## Rough effort

Smaller than a full `ansible-runner` backend (which would need a new
private-data-dir builder, a file-watching event source, and a new
dependency) - this is closer in size to a bounded "wrap the existing
subprocess launch in a container command" feature:

- Container command construction (mount list, engine detection, image
  config): ~1 day.
- Callback-plugin bind-mount + container-side env var wiring: ~half a
  day (mostly extending the existing temp-dir mechanism).
- Networking/UID edge cases + docs: ~half a day.
- Testing against a real EE image (manual + maybe a CI job): unestimated,
  depends on what EE images are available to test against.

Total ≈ 2-3 days for a first working version, excluding the config-surface
polish sketched above.

## Suggested phasing

Not scheduled - like `OwnCallbackPlugin.md`, this waits behind
`TODOsBeforeLaunch.md`. No dependency ordering forces one before the
other; the transport-fallback interaction (#1) is the only thing to keep
in mind if both are picked up around the same time.
