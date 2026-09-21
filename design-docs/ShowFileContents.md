# Show File Contents

## Situation

Drill down view already provides a lot of information. But when debugging
specific actions there is more information that are of interest. It would be
especially nice to directly see the contents of the file a specific action
(e.g. ansible.builtin.template) has modified or created.

## Why a separate tab, not just the existing Diff tab

The Diff tab only shows Ansible's own diff output, which for line-oriented
modules (`lineinfile`, `blockinfile`, ...) is just the differing lines/hunks,
not the full file. It's also not clean to copy: `y` on the Diff tab picks up
diff markup (headers, +/- markers, context) along with the content. A
dedicated File tab gives a clean, fully-copyable view of the file as it
currently stands, independent of whether/how a module reported a diff.

## Approach

### Actions that modify a file on the remote host

Pull the file via a throwaway, single-task playbook using
`ansible.builtin.fetch`:

```yaml
- hosts: <hostname>
  gather_facts: false
  ignore_unreachable: true
  tasks:
    - name: fetch file contents
      ansible.builtin.fetch:
        src: <path>
        dest: <local temp path>
        flat: true
```

Run this playbook as a new `ansible-playbook` invocation, reusing the
*exact* passthrough args (`Rest`) of the original invocation — same
inventory, connection settings, become settings, vault password source,
extra vars. Unlike an earlier sketch of this doc, the target host is named
directly via the stub's own `hosts: <hostname>` line rather than by
appending `-l <hostname>` to `Rest` — `Rest` never contains `--limit`/`-l`
in the first place (Tangsible's own passthrough-arg parsing already
extracts that separately for the re-run dialog), and how a repeated
`--limit`/`-l` would actually interact with one it might already appended
is unverified against real `ansible-playbook` argparse behavior, so
naming the host directly sidesteps the question entirely. This is the same
approach the drill-down's existing "Resolved" tab
(`internal/session/resolved.go`) already uses for the identical reason.
`gather_facts: false` keeps it fast since a fetch needs no facts;
`ignore_unreachable: true` matches "Resolved"'s own stub, so a host that's
gone unreachable since the main run fails this one task cleanly rather than
aborting the whole invocation.

**A real limitation found while validating this**: connection settings
that live in the *original playbook's own YAML* (e.g. a play-level
`connection: local`, common in this project's own `testdata/*.yml`
fixtures for zero-infrastructure local testing) do **not** carry over to
the stub playbook, since the stub is a separate play with no knowledge of
the original one. Only connection settings that live in *inventory*
(`ansible_connection`, `ansible_host`, etc., host/group vars) or in `Rest`
itself (a CLI-level `-c`/`--connection` override) are reused. For a real
remote host managed via SSH through inventory — the feature's actual target
use case — this is a non-issue, since connection info naturally lives in
inventory already. It only bites a playbook that relies on a play-level
connection override instead; a fetch against such a host will simply fail
(and the File tab stays hidden, per the caching/errors section below) even
though the main run itself succeeded.

**Interactive-credential guard.** `--ask-become-pass`/`-K` and
`--ask-vault-pass`/`--ask-vault-password` are not cached to disk — they're
only good for the process that prompted for them. The original run's prompt
(if any) happens before the TUI takes over the terminal, which is why it's a
non-issue there. But the throwaway fetch playbook is a *second* process,
spawned *while the TUI already owns the terminal* — replaying those flags
verbatim would trigger a fresh bootstrap prompt fighting the raw terminal,
the same class of problem `Purpose.md` documents for mid-run prompts. So:
before spawning the fetch playbook, `Rest` is scanned for those
interactive-prompt flags; if present, the fetch is skipped entirely (never
spawned) and the File tab simply stays hidden, same as any other fetch
failure. Non-interactive credential sources (private key files,
`--vault-password-file`, env vars) replay with no prompt risk and are fine.

The fetched file is written to a scratch temp path, read into memory
immediately, then the temp file is deleted right away — nothing is left on
disk to clean up later.

**Binary content.** A short heuristic (a NUL byte in the first ~8000
bytes, git's own long-standing test for the same question) treats the
content as binary — in that case the tab is hidden, exactly like any other
"nothing safe to show" case below, not shown with a placeholder.

**Errors.** Any failure — host unreachable, permission denied, file
missing, an interactive-credential prompt that would otherwise be needed,
non-zero exit from the fetch playbook — hides the File tab entirely, never
a crash and never a visible error/placeholder message (confirmed: unlike
the Docs/Resolved tabs' own "Could not fetch: ..." convention, a File-tab
failure should read as "nothing to show," not "something went wrong").

### Actions that modify a file on the control host

No Ansible invocation needed — read the file directly from the local
filesystem (it's already local to the machine running Tangsible).

### `delegate_to: localhost`

A task delegated to `localhost` also never touches the named host at all —
its `dest`/`path`/`creates` value is a control-host path, exactly like the
control-host actions above, even though the task is still one of the
remote-host-modifying modules in the table below and reports its result
under the original inventory host's name. Ansible surfaces the delegation
on the result itself as `_ansible_delegated_vars.ansible_host ==
"localhost"` (confirmed empirically — there's no field literally named
`delegate_to`). When detected, the File tab reads the path directly off
local disk instead of spawning a fetch playbook against the named host,
which would be pointless at best (fetching a path that was never actually
written there) and could fail outright at worst (a named host that's
otherwise unreachable, since the real work always ran locally). Only the
exact `"localhost"` spelling is recognized, not `127.0.0.1` or some other
loopback address/inventory alias — narrow on purpose, matching what was
actually asked for.

## Caching / refetch semantics

- The file is fetched (or read, for control-host actions) once per (task,
  host) the first time the File tab is displayed after opening the
  drill-down view for that task.
- Cycling between tabs within the same drill-down visit reuses the cached
  content — no refetch on every tab click.
- Closing the drill-down and reopening it (even for the same task) triggers
  a fresh fetch. This bounds staleness — a later task in the same run may
  have modified the same file again — without paying the fetch cost on every
  tab switch.
- This cache has a different lifetime than the Docs tab's `docsCache`, which
  persists for the whole process (including across reruns) since module docs
  don't depend on run state. The File tab's cache should be cleared whenever
  the drill-down view closes.

## Implementation status

The mechanism above is implemented for the modules marked `*` in the tables
below (`lineinfile`, `assemble`, `blockinfile`, `command`, `copy`, `fetch`,
`get_url`, `known_hosts`, `replace`, `shell`, `template`, `uri`, `patch`,
`archive`, `htpasswd`, `ini_file`, `authorized_key`, `openssh_cert`,
`openssh_keypair`, `apt_repository`, `deb822_repository`), gated by
`internal/uikit.FileTabSupportedModules` (a package-level set — extending
support to another module, once `FilenameField` knows how to extract that
module's own path/dest field, is meant to be a one-line addition there,
nothing more):

- `internal/session/fetchfile.go` — `fetchRemoteFileContents` (the stub
  playbook + fetch mechanism above), `readLocalFileContents` (the
  `delegate_to: localhost` case above), and `isBinaryContent`.
- `internal/config/rerunargs.go` — `HasInteractiveCredentialFlag` (the
  interactive-credential guard).
- `internal/uikit/tui_drilldown.go` — `RemoteFilePath` (module/path
  detection, plus the `local` flag backing the `delegate_to: localhost`
  case and `fetch`'s own always-local `dest`), `DelegatedToLocalhost`,
  `createsField` (`command`'s and `shell`'s shared `creates:` extraction),
  `FileTabHidden`/`BuildFileTab`, and `BuildOutputTabs`'s new "File" tab,
  positioned right after "Diff".
- `internal/session/tui.go` — `fileCache` (keyed like the existing
  `resolveCache`, by `(task, host)`), the async fetch-kickoff inside
  `showOutputWithOrigin` mirroring the existing Resolved/Docs pattern
  (branching on `local` to call `readLocalFileContents` instead of
  `fetchRemoteFileContents`), and the cache-clear points in `closeOutput`
  (every close, per the caching rule above) and `submitRerun` (a new
  generation).

Verified live against a real `ansible.builtin.lineinfile` task (over a
`connection: local` inventory host — see the connection-settings limitation
above): a successful fetch returns the exact file content; a missing
remote file and an `--ask-become-pass`-guarded invocation both fail cleanly
with no crash. `lineinfile`'s own result JSON turns out to report `path`
only under `invocation.module_args.path`, never at the top level - exactly
the fallback case `FilenameField`'s doc comment already called out for
`stat`/`git`.

`ansible.builtin.fetch` turned out simpler than expected despite its own
`flat:`/directory-destination branching (see its own table row below,
originally marked "more complicated algorithm needed"): its top-level
`dest` field already reports the fully-resolved final local path in every
case — `flat: true` against an exact file path, `flat: true` against a
directory (appends the source's basename), and the `flat: false` default's
`dest/<hostname>/<src>` layout — confirmed empirically across all three,
including on an idempotent (`changed: false`) second run. So no
flat-aware resolution logic was needed at all: `fetch` reuses
`FilenameField`'s existing top-level `dest` lookup like `copy`/`template`,
with `RemoteFilePath` simply treating it as always-local (its `dest` is
never on the named host, in any configuration, regardless of
`delegate_to`).

`get_url`/`uri` turned out similarly simple, and — unlike `fetch` — genuinely
do run on whichever host the task targets (remote, or the control host via
`delegate_to: localhost`), so they need no forced-local treatment;
`DelegatedToLocalhost` already covers them generically. Their own
directory-dest resolution (the doc's "dest + basename if dest is a
directory" note below) is already done for us too, confirmed empirically,
just reported under two different fields: `get_url` echoes the resolved
path (basename of the URL appended, when `dest` was a directory) under
top-level `dest`, same as `copy`/`template`; `uri` echoes the equivalent
resolved path under top-level `path` instead — it never sets `dest` at
all, in either the exact-path or directory-`dest` case. `FilenameField`'s
existing `dest`-then-`path` fallback chain already covers both without any
change.

Most of the rest of the remote-host table turned out to already be covered
by `FilenameField`'s existing fallback chain with zero new code — confirmed
empirically for `patch` (`dest` only under `invocation.module_args`),
`archive` (top-level `dest`, fully resolved even when the task didn't
specify one at all — same "Ansible resolves it for us" pattern as
`fetch`/`get_url`), `htpasswd` (`path` only under `invocation.module_args`),
`ini_file` (top-level `path`), and `authorized_key` (top-level `path`,
already fully resolved whether the task gave an explicit `path:` or relied
on the default `~<user>/.ssh/authorized_keys`). `apt_repository` and
`deb822_repository` weren't live-tested — both always write under
`/etc/apt/sources.list.d/`, requiring root and modifying real
package-manager state, which didn't seem worth doing just to confirm a
result shape — so they're implemented from `ansible-doc`'s own RETURN
documentation instead: `apt_repository` reports no singular dest/path field
at all, only a `sources_added` list (a new `sourcesAddedField` helper,
supporting only the common single-source case — a task that added more
than one source in one run has no single obvious file to show, so that's
treated as unsupported); `deb822_repository` reports a top-level `dest`,
already covered by `FilenameField`.

`community.crypto.openssh_cert`/`openssh_keypair` needed one small addition:
both report their output file under a top-level `filename` field, not
`dest`/`path` — confirmed empirically — so `FilenameField` gained a third
fallback for it. `openssh_cert`'s `filename` is shown as-is (an SSH
certificate is meant to be public). `openssh_keypair`'s `filename` is the
*private* key, though — `RemoteFilePath` appends `.pub` to it instead, since
the module always writes the public half alongside it at exactly that
suffixed path (standard OpenSSH keypair convention, confirmed empirically);
the private key itself is never shown.

Three modules from the table are deliberately **not** implemented:
- `ansible.builtin.script` — confirmed empirically that its result carries
  no `invocation`/`dest`/`path`/`creates` field at all, unlike
  `command`/`shell`, so a `creates:` path can't be recovered after the
  fact.
- `ansible.builtin.expect` — couldn't be verified live in this environment
  (`pexpect` isn't installed), and since `script` — `expect`'s sibling in
  the "command family" of action plugins — turned out *not* to share
  `command`/`shell`'s `invocation` echoing, it isn't safe to assume `expect`
  does either without checking.
- `ansible.builtin.user` — the only file-shaped output is either the
  *private* generated SSH key file (`ssh_key_file`) or the public key's raw
  text inline (`ssh_public_key`, already shown as literal text by
  `AdditionalOutputLines`' own `user` case, not a path) — neither fits a tab
  whose whole job is resolving a *path* to fetch, and showing a generated
  private key in a debugging UI would be the wrong default regardless. This
  is the same "probably too broad" instinct the original `/etc/passwd` idea
  already flagged, just confirmed against the actual result shape.

## Potential actions

Once the mechanism above is extended to other actions, see below for ones
that might make sense (still TBD for some) — this table is a brainstorm,
not all lines will necessarily make sense once looked at closer (e.g.
dumping the whole `/etc/passwd` file for a single `user` task is probably
too broad, see below).

### Actions which modify a file on the remote host

| action | parameter | comment |
| --- | --- | --- | 
| ansible.builtin.apt_repository* | sources_added | only if exactly one source was added; not live-tested, see Implementation status |
| ansible.builtin.deb822_repository* | dest | not live-tested, see Implementation status |
| ansible.builtin.assemble* | dest | |
| ansible.builtin.blockinfile* | path | | 
| ansible.builtin.command* | creates | only if creates is specified |
| ansible.builtin.copy* | dest | only if dest is not a directory |
| ansible.builtin.expect | creates | not implemented — couldn't confirm it echoes creates: the way command/shell do, see Implementation status |
| ansible.builtin.known_hosts* | path | |
| ansible.builtin.lineinfile* | path | |
| ansible.builtin.replace* | path | |
| ansible.builtin.script | creates | not implemented — confirmed empirically the result has no recoverable field at all, see Implementation status |
| ansible.builtin.shell* | creates | only if creates is specified, same as command |
| ansible.builtin.template* | dest | |
| ansible.builtin.user | | not implemented — only file-shaped output is a private key file or inline public-key text, see Implementation status |
| ansible.posix.authorized_key* | path | already fully resolved whether path: was given explicitly or defaulted |
| ansible.posix.patch* | dest | only under invocation.module_args, see Implementation status |
| community.crypto.openssh_cert* | filename | not path — shown as-is (public data) |
| community.crypto.openssh_keypair* | filename | not path — .pub appended to only show the public key, never the private key itself |
| community.general.archive* | dest | fully resolved even when dest wasn't specified |
| community.general.htpasswd* | path | only under invocation.module_args |
| community.general.ini_file* | path | |

### Actions that modify a file on the control host

| action | parameter | comment |
| --- | --- | --- |
| ansible.builtin.fetch* | dest | turned out not to need the "more complicated algorithm" — dest is already fully resolved, see Implementation status |
| ansible.builtin.get_url* | dest | dest + basename if dest is a directory — already resolved for us, see Implementation status |
| ansible.builtin.uri* | path | not dest — uri never sets dest at all, only the already-resolved path (dest + basename if dest was a directory), see Implementation status |


### More commands that might be interesting but I have not made up my mind what I could do.

ansible.builtin.get_ent:

ansible.builtin.git:
    List directory?

ansible.builtin.find:
    register:

ansible.posix.patch:
    dest

systemd
service

## Resume

claude --resume 090d9a7d-fe0e-44fb-a411-06c2c78d387d
