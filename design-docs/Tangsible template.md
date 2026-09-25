# tangsible template

## Problem

Debugging Jinja 2 can be challenging sometimes. Using tools outside Ansible
can lead to different results. Within ansible requires crafting suitable
playbooks and roles just to test templates.

## Idea

Add a new verb

  tangsible template <path to template> [<hostSpec>] [-e...]

hostSpec is optional and, if given, must come immediately after the
template path, before any flags. Unlike a single hostname, it's a
comma-separated list of tokens, each one either a literal hostname, an
inventory group name, or the special keyword `all` (every host in the
inventory) - see Host/group resolution below for exactly how each token is
resolved and what happens once it expands past a configurable size. If
omitted entirely, tangsible picks the first host in the inventory for you.
-e works exactly as it does for run/role already - multiple occurrences
allowed, passed straight through, no new parsing needed for it.

This is a standalone, single-view program - it does not go through the
normal run/role tree UI at all. There's no tree to browse and nothing here
needs the live jsonl-streaming pipeline: each render is one synchronous
ansible-playbook invocation per host (sequential, one after another - see
Rendering below), run initially and again every time the template is
reprocessed (see Editing below). `q` quits the whole program - deliberately
just `q` (and Ctrl-C), not Esc too: Esc used to quit identically, but that
made it too easy to close the whole thing by reflex while just browsing the
tabs, so it's inert here instead.

## Host/group resolution

Before generating anything, tangsible shells out to `ansible-inventory
--list` once (passing through whatever -i/inventory-related args were
given) and resolves hostSpec against that raw group tree:

  * No hostSpec at all: the first host in the inventory, deterministically
    (not by racing ansible-playbook's own event stream) - same as before
    this verb supported more than one host.
  * Each comma-separated token is resolved as a group name first (a real
    top-level key in `ansible-inventory --list`'s own JSON, `_meta`
    excluded) - including `all`, which needs no special-casing at all,
    since every inventory already has a real, always-present `all` group
    (the same one `ansible-playbook -l all` already addresses) - falling
    back to a literal hostname if it isn't a group. A token that's neither
    is a usage error, reported immediately. Mixing hosts, groups, and
    `all` freely in one list is harmless, not an error - the result is
    just their deduplicated union, in first-appearance order.

This matters beyond convenience: without resolving explicitly first and
narrowing the actual play to just the resolved hosts, "run against `hosts:
all` and take whichever host reports first" would mean the template task
actually executes - and writes its output file - on every host in the
inventory just to show one result, which is more than wasted work, it's
touching hosts the user never asked to touch.

**Confirmation above template_hosts_max.** If resolution (from an explicit
host list, a group, or `all`) ends up with more hosts than
`.tangsible/config.toml`'s `[general] template_hosts_max` (default 10,
project-local only, same `*int`/nil-means-default shape as
`notify_task_failed_max`), tangsible asks for confirmation in the
terminal's own normal cooked mode before rendering against all of them -
this runs before the TUI (and its raw-mode screen) exists at all, so a
plain "This will render the template against N hosts - continue? [y/N]"
prompt on stdin/stderr is enough; anything but an explicit y/yes exits
immediately, printing nothing further.

## The stub playbook and rendered output

Tangsible creates a small, temporary stub playbook with an
`ansible.builtin.template` action referencing the template, and runs it
with ansible-playbook - which then automatically picks up any available
host or group vars for whichever host is currently being rendered against.

Unlike the `role` verb's own stub, this one doesn't need to live anywhere
special - there's no tree/drill-down source lookup happening here, so
nothing needs to discover it by walking a directory. Both the stub
playbook and its `template` task's own `dest:` (the rendered output, which
is what gets read and displayed) are pure scratch: **one stable pair of
paths for the whole interactive session**, reused - not regenerated -
across every reprocess and every host, and removed once at the end when
tangsible exits. This single shared `dest:` is exactly why rendering stays
sequential (see Rendering below): two `ansible-playbook` invocations
racing the same output file would be a real bug, not a hypothetical one.

## Role-owned templates

If the template's own path matches the standard role layout
(`roles/<name>/templates/...`), tangsible generates the stub with
`roles: [<name>]` instead of a bare template task, so variables defined
by that role are available the same way they would be for a real
`include_role`/`import_role`. Detected automatically from the path alone
- no separate argument needed - by extending `tui.go`'s existing
`rolePathPattern` (today `/roles/([^/]+)/(?:tasks|handlers)/`, used to
derive the drill-down view's own `Role:` line) to also match
`/templates/`.

## The view

A full-screen view, built the same way the existing output drill-down
view is (a `TextView` in its own small `Flex`, dynamic colors on) - but
with template-specific content, not a reuse of `formatHostOutput`'s own
Task/Output/Warnings/Error section layout:

  * A thin header line above the content: just the template's own path -
    unlike an earlier single-host version of this view, the current host
    is *not* shown here, since each host now gets its own tab (see below)
    and repeating it in the header would be redundant.
  * A tab per resolved host (see Host/group resolution above), titled with
    that host's own name, each showing exactly one of two states:
    * The template rendered successfully against that host: the rendered
      file's own content, verbatim.
    * It didn't: the task's own error message (`msg` - for a template
      failure this is the Jinja traceback; `stderr` too if that's ever
      non-empty) in its place - and that host's own tab label renders in
      the same red used for a genuinely failed task everywhere else in the
      app, active or inactive, clearing back to normal the moment that
      host reprocesses successfully (an `e`/`h` reprocess, or a rename via
      `h` landing on a previously-fine host).
  * One further, shared "Source" tab (the template file's own raw
    content) - host-independent, so it's never duplicated per host.
  * A bottom keybinding-hint bar, matching every other view in the app
    (`e`: edit, `h`: change host, `q`: quit, plus the usual tab/search/copy
    shortcuts every tabbed view shares).

## Rendering

Each host's own render is one synchronous `ansible-playbook` invocation
against the shared stub, narrowed to that host via `--limit`. Multiple
hosts render **sequentially**, one after another, not concurrently -
tried first as the simplest option (given this app's own ~10-host target
scale) before reaching for anything more complex, and it also happens to
be what the single shared `dest:` file requires (see above) without extra
per-host scratch paths. Each host's own tab updates incrementally as its
own render finishes, rather than waiting for every host to complete.

Every render this view ever runs - the initial load, `e`'s own
reprocess-every-host, and `h`'s own single-host reprocess - is serialized
against that same shared stub/`dest:` pair: a render requested while one
is already in flight is never silently dropped, but coalesced into a
single trailing "reprocess everything" once the current one finishes
(simple and always safe, even if occasionally slightly more work than
strictly necessary).

## Editing

Pressing `e` opens the user's preferred editor ($VISUAL, falling back to
$EDITOR) on the template file itself - the real source file, not a scratch
copy, so changes are the changes the user actually wanted to make. This is
the first real use case for `tcell.Screen.Suspend()`/`Resume()` - handing
the terminal to the editor as a normal foreground process, then resuming
once it exits. Once it exits, tangsible always reprocesses **every open
host tab** unconditionally - no attempt to detect whether anything was
actually saved (matching how e.g. `git commit`/`crontab -e` behave), and
every tab needs it regardless since the template file itself just
changed, not just whichever tab happened to be active.

## Changing hosts

Pressing `h` opens a single-field dialog targeting whichever host tab was
most recently active (if the shared Source tab is active instead, the
last-active host tab is still remembered and used) - typing a different
hostname and confirming renames that one tab in place and reprocesses just
that host, leaving every other open tab untouched. If the typed hostname
is already open under a different tab, tangsible switches to that
existing tab instead of creating a duplicate. The field autocompletes
against every known literal hostname (not group names - unlike the
command line's own comma-separated hostSpec, this dialog only ever swaps
in one literal host, so a group name here wouldn't actually do anything
useful).

## Cleanup

Both the stub playbook and the rendered-output file are removed once
tangsible exits - one stable pair of paths for the whole interactive
session (edit/reprocess/host-switch cycles all reuse them), not
regenerated fresh on every keypress or per host.
