# What ansible-navigator and ansible-runner teach us about execution environments

Written 2026-11-17, after a run of edge cases hit in real use of the EE
support (`ExecutionEnvironments.md`): a silent hang on a new image, short host
names, hosts recorded only in the system-wide `known_hosts`, a collection the
image lacked. The idea: ansible-navigator has been through this for years, so
read how it (and ansible-runner, which builds its container command line)
handles the same ground, and use that to decide what to build next.

## Method and limits

- Sources: `ansible/ansible-navigator` at `d0735bd6` (2026-11-12, 1,444
  commits) and `ansible/ansible-runner` at `db7e91d` (2026-11-12, 1,692
  commits), both Apache-2.0, cloned and **only read, never run**. Read in
  full: runner's `config/_base.py` (container command construction,
  automounts), navigator's `navigator_post_processor.py` (EE settings),
  `runner/base.py`, `image_manager/puller.py`, `docs/faq.md`; the git history
  of the container code (`git log --grep`, `-S`), several commits in detail.
- Behaviours were learned, no code copied (the licence would allow it with
  notices; the project rule is to write our own).
- Many commits have no message beyond the title, so *why* is sometimes
  inference; those places say so. **GitHub issues were not read** - a second
  pass over navigator's and runner's issue trackers (search "execution
  environment") would likely add more real-world cases.
- "Verified" below means reproduced on tangsible on the user's workstation
  during this analysis, not only read in code.

## Where we already agree with them

| Topic | navigator / runner | tangsible |
|---|---|---|
| Project directory mounted, found by ansible | FAQ: playbook dir auto-mounted, `ansible.cfg` and `collections/` beside the playbook "just work" | same (identity mount) |
| SSH agent | mounts the socket, sets `SSH_AUTH_SOCK` | same |
| System-wide known hosts | runner's automounts include `/etc/ssh/ssh_known_hosts` | same since `ddb299b` (we found it the hard way: 5 of 12 hosts failed) |
| keep-id + HOME | navigator #2079: with `--userns=keep-id` podman puts HOME at the workdir, "which causes the EE's ssh setup to fail"; fix is `--passwd-entry` plus a writable home | same finding on our side; our fix (a tmpfs home) needs no host directory |
| Sockets under SELinux | their own example uses `--security-opt=label=disable` for the DBus socket | `selinux = auto` picks it when enforcing |
| Pull before running | `ImagePuller` assesses and pulls before the run | `EnsureImage` before the TUI |
| Inventory host lists | an `-i` value ending in `,` is a host list, not a path | we test existence |
| No secrets on the command line | env file | bare `-e NAME` |

Things neither project does that we do: pass the host `resolv.conf` `domain`
line as `--dns-search` (no DNS handling exists in either), a tmpfs home, event
paths identical on both sides (their artifacts model needs translation),
SELinux auto-detection.

## Gaps, with evidence

### Verified on tangsible

1. **(Fixed: `mapper.translateInventory`, see `ExecutionEnvironments.md`.)
   An inventory outside the project loses its `group_vars/` and
   `host_vars/`, silently.** Ansible loads them from the inventory file's
   directory; we mount only the file. Test: group var in `inv/group_vars/`
   beside `inv/inventory.ini`, project elsewhere: host run prints it, EE run
   prints `UNDEFINED`, no error anywhere. Runner mounts the *parent
   directory* of playbook, inventory, vault and key files (`_update_volume_mount_paths`:
   "ensure each is a directory not file"), made deliberate by #1304 "Pass
   inventory directory path instead of file path when using containerization"
   and #1408 ("if the user tried to pass a file as the source path;
   ansible-runner should resolve the file's parent directory").
   Care needed: mounting the parent of an inventory in `$HOME` would expose
   `~/.ssh`. A narrower design: mount the file plus its sibling `group_vars`
   and `host_vars` directories, nothing else; an inventory *directory* whole.
2. **Fact cache and `log_path` outside the project are not shared.** Test
   with an `ansible.cfg` like the user's (`gathering = smart`,
   `fact_caching = jsonfile`, absolute `fact_caching_connection`, absolute
   `log_path`): after two container runs the configured directory held no
   fact cache and no run log; a host run wrote the cache. So an EE run
   re-gathers every time and its log is lost. Runner remaps the fact cache
   into its artifacts dir (`ANSIBLE_CACHE_PLUGIN_CONNECTION`); navigator's FAQ
   tells users to mount paths by hand. Nobody solves it automatically.
3. **Path-valued `ANSIBLE_*` variables are dropped silently** by our filter
   (`ANSIBLE_CONFIG`, `ANSIBLE_ROLES_PATH`, `ANSIBLE_COLLECTIONS_PATH(S)`,
   `ANSIBLE_VAULT_PASSWORD_FILE`, `ANSIBLE_INVENTORY`,
   `ANSIBLE_PRIVATE_KEY_FILE`, `ANSIBLE_LIBRARY`, plugin paths; `TestEnvForwarding`).
   Navigator forwards *every* `ANSIBLE_*` unchanged (plus `--penv`/`--senv`
   for others) and works around the path problem by moving the file: its FAQ's
   vault-password examples hard-link the password file (or put a `vault.sh`
   script) into the project directory, which is already mounted, and set
   `ANSIBLE_VAULT_PASSWORD_FILE` to the relative name. With identity mounts we
   can do better than either: forward the variable **and** mount the existing
   paths it names (read-only), and say once what was dropped or mounted.
4. **(Fixed: `pass_env` / `set_env`, see `ExecutionEnvironments.md`.) No way to pass other environment variables** (only `ANSIBLE_*`, proxies,
   `NO_COLOR`). Cloud credentials and tool settings (`AWS_*`, `AZURE_*`,
   `GOOGLE_*`, `KUBECONFIG`, `VAULT_ADDR`, ...) are the normal reason to need
   it. Navigator: `--penv NAME` (pass; warns "not currently set, discarded")
   and `--senv NAME=value` (set), config keys
   `execution-environment.environment-variables.pass/set`.
5. **(Fixed: `expandPath`, see `ExecutionEnvironments.md`; it also expands
   `$VAR`.) `~` in path arguments** (e.g. `-e @~/vars.yml`, `--flag=~/x`): navigator
   expands `~` and `$VAR` (`expand_path`) before mounting; we warn and pass it
   unchanged, so inside the container `~` is the container's home and the file
   isn't found.
6. **(Fixed with item 8's cousin: `parseExtraMount`, see `ExecutionEnvironments.md`.)
   Mount validation**: navigator's `VolumeMount` rejects an empty or
   non-string source/destination, a source that does not exist, and unknown
   options *before* starting, and expands `~`. Our `extra_mounts` check only
   the shape; a typo surfaces as podman's own error after exit.
7. **(Fixed: `imagePresent`, see `ExecutionEnvironments.md`.) Image presence check**: `EnsureImage` treats *any* failure of
   `podman image exists` as "missing" and pulls (exit 1 means absent, 125 an
   error). Navigator uses `image inspect`, tells "no such image" from other
   failures ("image assumed to be corrupted or missing") and prints the
   outcome.

### Read in their code, not (yet) reproduced here

8. **(Fixed: `pull_policy` / `pull_arguments`; registry credentials via `pull_arguments`.) Pull policy**: `always`, `missing`, `never`, `tag` (pull when the tag is
   `latest` or the image is absent; navigator's default), with a clear error
   and hint for `never` + absent ("Try again with --pp missing or pull
   manually"). Plus `--pull-arguments` (e.g. `--tls-verify=false`, with
   environment-variable expansion, #1590). We have "pull if absent" only, and
   for a `localhost/...` image that is absent we would try to pull from nowhere.
   Runner also supports registry credentials (`--authfile`, docker `--config`,
   an insecure-registry `registries.conf`).
9. **A raw engine-options escape hatch**: `--container-options` /
   `execution-environment.container-options`, a flat list appended unvalidated
   (`--co`). Discussed separately: needs guarding against flags the builder
   owns (`--userns`, `--security-opt`, `-v`/`--mount` over our mounts, `-w`,
   `--name`, our tmpfs home).
10. **Observability**: `--log-level debug` writes the exact container command
    and engine messages to navigator's log; it prints an overview of image,
    tag, pull policy and "pull needed" before pulling. We had to read `ps` to
    see our own command when a new image stalled for 34 s (the first
    `--userns=keep-id` start of an image re-owns its layers, silently).
11. **Mount-safety**: runner refuses to mount `/`, `/home`, `/usr`
    (`_ensure_path_safe_to_mount`). We refuse `/` and a list of system
    directories but not `/home` - a playbook in `/home/a` run from `/home/b`
    makes `/home` the root - nor `$HOME` itself when the project *is* the home
    directory (the whole home, keys included, mounted read-write). Neither
    project guards `$HOME`; worth a warning at least.
12. **fuse-overlayfs bug**: runner sets `ANSIBLE_UNSAFE_WRITES=1` for every
    podman run (extended-attribute failures when the storage driver is
    fuse-overlayfs, originally a RHEL < 8.5 kernel bug; ansible#73310). This
    machine uses native overlay, so it can't be tested here; a precise fix
    would detect the driver (`podman info`) instead of always setting it
    (the variable makes writes non-atomic).

### Different choice, no action

13. **User inside the container**: navigator forces `--user=root` for podman
    unless the user passes a user option (#1539, no stated reason; likely so
    bind-mounted files are always writable, since rootless root is the host
    user - an inference). Runner *removed* `--userns=keep-id` in 2021 (42d091a):
    for an image with `USER root` the files created ended up owned by a mapped
    sub-UID. **Checked on podman 5.4 here: keep-id overrides the image's
    `USER root`, the container runs as the host uid and created files are
    owned by it** (not re-checked on 5.7/5.8). `--user=root` would also avoid
    the slow first start of a new image, at the price of `whoami` being root,
    unlike a host run; we keep keep-id for parity.
14. **`/dev/mqueue` pre-flight check** (navigator #610): exists only because
    runner passes `--ipc=host`; we don't, so nothing to do.
15. **Engines and platforms**: docker (`--user=<uid>`, `--config` for auth),
    Apple's `container` engine (`docs/apple-container.md`), macOS specifics
    (Docker Desktop's `/run/host-services/ssh-auth.sock`, resolving a symlinked
    `SSH_AUTH_SOCK`, #2055/#2114). Out of scope while we are podman/Linux only;
    the list to start from if that changes.
16. **Signal and tty handling, prompts**: they have `--enable-prompts` and a
    stdout mode for `--ask-pass`/`vars_prompt`; we answer prompts once at
    startup (`credfile`). Not a gap.
17. **Time zone**: navigator's `time_zone` setting only formats its *own log
    timestamps*; it is not passed into the container. Not a parity lesson.

## Suggested order

Small and clearly worth it (each verified or well evidenced):

- (1) inventory siblings, (5) `~` expansion, (6) mount validation,
  (7) image check, (4) `pass_env` / `set_env`, (10) a "still waiting" line,
  the exact engine command in a log, podman's stderr when it stalls.

Medium, needs a design decision:

- (3) forward-and-mount path-valued `ANSIBLE_*`, (2) fact cache and log
  directories (ask `ansible-config dump` which paths the effective config
  names, mount the ones outside the project), (8) `pull_policy` and pull
  arguments, (9) the options escape hatch, (11) the `$HOME` / `/home` warning.

Later or only if it bites: (12) fuse-overlayfs, (13) `--user=root`, (15).
