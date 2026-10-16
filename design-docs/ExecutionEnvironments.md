# Supporting execution environments

## Status

**Investigated, not decided, nothing built.** Originated from a question
about whether Tangsible should interface with `ansible-runner`
(`OwnCallbackPlugin.md` was the trigger - see "Why this isn't
`ansible-runner` support" below for how the two got untangled). The real
motivation turned out to be narrower and more useful than "support
ansible-runner": when developing a playbook/role, verify it actually runs
correctly inside the target *execution environment* (EE) - the
`ansible-builder`-produced container image a project will actually be run
against in CI/AWX/Controller - not just against whatever `ansible-core`
happens to be on the developer's host.

Findings below come from reading `ansible-runner`'s actual container
invocation code (`ansible_runner/config/_base.py`,
`wrap_args_for_containerization`), not from memory - the same
live-spike-before-writing-conclusions discipline `OwnCallbackPlugin.md`
uses.

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
