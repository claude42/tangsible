# Rerund dialog options

## Situation

I have a small feature idea. Currently tangsible run starts the run directly
while tangsible rerun always opens the rerun dialog.  Sometimes that's not
what you want. Sometimes a dialog after tangsible run would be nice,
sometimes I know what I want with tangsible rerun and don't need the dialog.

## Proposal

Add the following command line option for the verbs run, rerun and role

--dialog - will always show the rerun dialog - potentially pre-filled with
values from other command line options, potentially pre-filled from the last
run

--no-dialog - will not show the rerun dialog at all, just use whatever
information tangsible has.


In addtion add a configuration option run_dialog. Potential values are

Default - keep the default behavior (show for rerun, don't show for run and
role)

Never - never show the dialog

Always - always show the dialog

Command line options will have priority over this configuration option.

### Questions

1. What does --no-dialog actually skip on rerun?

tangsible shall behave the same as right now. --no-dialog just skips
rendering the dialog, it doesn't change anything else.

2. What pre-fills the dialog for run/role --dialog?

It only pre-fills from CLI flags given on this invocation

3. Scope: does this touch revisit?

Out of scope for now

4. --dialog and --no-dialog together?

Should result in an error.

5. Naming nits, to match existing convention:

- "default", "never", "always" and case insensitive matching is fine

- I haven't found a better name then run_dialog yet. Just "dialog" seems a
  bit too general :-)

- Yeah, [general] in .tangsible/config.toml sounds good

6. Does --dialog/--no-dialog get persisted into invocation history?

No. Extracted and discarded before Rest is recorded, same as
--start-at-play/--only-failed/--only-unreachable/--resume-where-failed -
never sticky across a later bare rerun.

7. --dialog/--no-dialog on a verb that doesn't support it (template,
   host/hosts, vault, version, revisit)?

Usage error.
