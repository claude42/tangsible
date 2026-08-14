# tangsible template

## Problem

Debugging Jinja 2 can be challenging sometimes. Using tools outside Ansible
can lead to different results. Within ansible requires crafting suitable
playbooks and roles just to test templates.

## Idea

Add a new verb

  tangsible template <path to template> [<hostname>] [-e...]

hostname is optional and, if given, must come immediately after the
template path, before any flags - it's a single host, not a comma list
(unlike ansible-playbook's own -l). If omitted, tangsible picks the first
host in the inventory for you (see Host resolution below). -e works
exactly as it does for run/role already - multiple occurrences allowed,
passed straight through, no new parsing needed for it.

This is a standalone, single-view program - it does not go through the
normal run/role tree UI at all. There's no tree to browse and nothing here
needs the live jsonl-streaming pipeline: each render is one synchronous
ansible-playbook invocation (the initial one, and again every time the
template is reprocessed - see Editing below), and the result is shown
directly in the one view this verb ever shows. `q` quits the whole program
from there - deliberately just `q` (and Ctrl-C), not Esc too: Esc used to
quit identically, but that made it too easy to close the whole thing by
reflex while just browsing the Rendered/Source tabs, so it's inert here
instead.

## Host resolution

Before generating anything, tangsible shells out to `ansible-inventory
--list` (passing through whatever -i/inventory-related args were given)
and picks the first resolved host from that - deterministically, not by
racing ansible-playbook's own event stream. The stub is then generated
already scoped to exactly that one host (or whichever one was given
explicitly). This matters beyond convenience: without it, "run against
`hosts: all` and take whichever host reports first" would mean the
template task actually executes - and writes its output file - on every
host in the inventory just to show one result, which is more than wasted
work, it's touching hosts the user never asked to touch.

## The stub playbook and rendered output

Tangsible creates a small, temporary stub playbook with an
`ansible.builtin.template` action referencing the template, and runs it
with ansible-playbook - which then automatically picks up any available
host or group vars for the resolved host, same as always.

Unlike the `role` verb's own stub, this one doesn't need to live anywhere
special - there's no tree/drill-down source lookup happening here, so
nothing needs to discover it by walking a directory. Both the stub
playbook and its `template` task's own `dest:` (the rendered output,
which is what gets read and displayed) are pure scratch: one stable path
per interactive session, overwritten on every reprocess, both removed
once at the end when tangsible exits.

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

  * A thin header line above the content: the template's own path and the
    hostname currently being rendered against (same pattern as the
    drill-down view's own `outputTopBar`) - so after switching hosts (see
    below), it's always visible which host produced what's on screen.
  * The body is one of exactly two states:
    * The template rendered successfully: show the rendered file's own
      content, verbatim.
    * It didn't: show the task's own error message (`msg` - for a
      template failure this is the Jinja traceback; `stderr` too if
      that's ever non-empty) in its place.
  * A bottom keybinding-hint bar, matching every other view in the app
    (`e`: edit, `h`: change host, `q`: quit).

## Editing

Pressing `e` opens the user's preferred editor ($VISUAL, falling back to
$EDITOR) on the template file itself - the real source file, not a scratch
copy, so changes are the changes the user actually wanted to make. This is
the first real use case for `tcell.Screen.Suspend()`/`Resume()` - handing
the terminal to the editor as a normal foreground process, then resuming
once it exits. Once it exits, tangsible always reprocesses unconditionally
- no attempt to detect whether anything was actually saved (matching how
e.g. `git commit`/`crontab -e` behave) - and displays the new result the
same way as the first run.

## Changing hosts

Pressing `h` lets the user enter a different hostname, which is then used
for the next reprocess - first iteration is a plain text entry; a picker
list gleaned from the inventory (the same `ansible-inventory --list` call
Host resolution above already needs) is a natural later improvement, not
required for a first version.

## Cleanup

Both the stub playbook and the rendered-output file are removed once
tangsible exits - one stable pair of paths for the whole interactive
session (edit/reprocess/host-switch cycles all reuse them), not
regenerated fresh on every keypress.
