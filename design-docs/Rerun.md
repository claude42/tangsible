# Rerun functionality

Especially developing new playbooks or roles frequently requires rerunning the same
playbook. But this can also be the case in other scenarios, e.g.

* one host not online, rerun whole playbook for this host again
* Failed task because of something unforeseen - fix this, re-run
* ...

As these use cases have quite different requirements, the implementation will
tackle different areas of the app

## Interactive re-run

* After a playbook has run (successfully or failed), the user can initiate a
  re-run by pressing the r key.
* This will open the re-run dialog Which will present the following options
  * Where to start the rerun
  * Tags
    * User can input tags to limit or skip the tasks that should (not) be re-run
    * If tags were already specified in the previous run (e.g. by --tags ...)
      then this text field should be pre-filled with these tags
  * Hosts
    * User can limit on which hosts the tasks should be re-run
    * If hosts were already specified in the previous run (e.g. by --l ...)
      then this text field should be pre-filled with these tags
  * Re-run shall be initiated by pressing return, dialog will be closed
    (without re-runnig) by ESC or q (only if no text field is active in that
    moment

Please see also section "Extend rerun dialog" below for more options.

## Re-run from the command line

Prerequisite: we have to change the way tangsible is invoked to a command
verb syntax, i.e instead of calling

   tangsible site.yml -l somehost --tags sometag

this should in the future be

   tangsible run site.yml -l somehost --tags sometag

Rationale: for this feature (and some more I have in mind) we'll need
different verbs.

* Tangsible shall save the history of its previous invocations in the local
  .tangsible file
* When tangsible is started with the verb "rerun" instead of "run"
  * it shall bring up the same Re-run dialog as describe above
  * where sensible the elements of the dialog shall be filled with known
    information from the previous run (read from the .tangsible file) (i.e.
    hosts, tags)
  * if -l or --tags is specified on the command line, these should have
    precedence over the data from the last run
* The user has to confirm the dialog by pressing enter before the playbook is
  run
* So tangsible could be called like
  * tangsible rerun
    -> would run the same playbook with the same arguments as last time
  * tangsible rerun someplaybook.yml
    -> would run someplaybook.yml with the same arguments as the last time
    tangsible was run for someplaybook.yml (or no arguments if never run for
    this playbook
  * tangsible rerun -l somehost
    -> would run tangsible with the same playbook and arguments as in the
    last invocation but only for host somehost

## Extend rerun dialog

I would like to propose two (actually three) additions to the rerun dialog

1. Get rid of "Start with task" in the rerun dialog at all.

Rationale: --start-at-task has alwqys been questionable because task names
are not unique. So it IMHO cannot be used sensibly at all. So for tangsible
let's not clutter our UI with options that make no sense and rather focus on
"Start with play" that actually makes sense.

2. Only failed hosts

Behavior: the playbook will be rerun only for those hosts which failed at
some time during the last run. Using this option has the same effect as
entering all failed hosts in the "Limit hosts" input field.

3. Only unreachable hosts

Behavior: the playbook will be rerun only for those hosts which were
unreachable during the last run. Using this option has the same effect as
entering all unreachable hosts in the "Limit hosts" input field.

4. Resume where failed

Behavior: tangsible will start executing the playbook at the first play where
any host task failed and only for failed hosts.

The information regarding failed / unreachable hosts should come from the
jsonl information. If the information is missing (because jsonl data is not
available) or in case there were no failures or no unreachable hosts, the
respective checkboxes should simply be omitted.

### User Interface

My initial idea is to give some more structure to the current rerun dialog,
like so

╔══════════ Re-run (enter: run, esc: cancel) ══════╗
║ Start:                                           ║
║                                                  ║
║ Start with play ________________________________ ║
║                                                  ║
║ [ ] Resume where failed                          ║
║                                                  ║
║                                                  ║
║ Tags                                             ║
║                                                  ║
║ Limit tags to:  ________________________________ ║
║                                                  ║
║ Skip tags:      ________________________________ ║
║                                                  ║
║                                                  ║
║ Hosts                                            ║
║                                                  ║
║ Limit hosts to: ________________________________ ║
║                                                  ║
║ [ ] Only failed                                  ║
║                                                  ║
║ [ ] Only unreachable                             ║
║                                                  ║
║                                    Cancel Re-run ║
╚══════════════════════════════════════════════════╝

"Resume where failed" is a checkbox, if it's checked, the play from which the 
playbook will be re-run will be automatically entered into "Start with play".
If the user later on modifies "Start with play" again, the "Resume where
failed checkbox shall be unchecked. Conversely, if "Resume where failed" is
unchecked again (by the user, via the checkbox itself), "Start with play"
shall be cleared.

When "Resume where failed" is checked, "Only failed" shall also automatically
be checked; when "Resume where failed" is unchecked again - whether by
clicking the checkbox directly, or as the side effect of editing "Start with
play" above - "Only failed" shall likewise be unchecked again. This coupling
only goes one way, though: if the user subsequently unchecks "Only failed" by
itself, or additionally checks "Only unreachable", "Resume where failed"
stays checked regardless - the user is assumed to know what they're doing,
and "Resume where failed" is not forced back off to keep its name literally
accurate to whatever "Limit hosts to" ends up containing.

"Only failed" and "Only unreachable" are check boxes. If either is checked,
the resulting host names will be entered into "Limit hosts to". If the user
later on modifies "Limit hosts to" by hand, both the "Only failed" and "Only
unreachable" checkboxes shall be unchecked.

If both "Only failed" and "Only unreachable" are checked, the behavior shall
be that the playbook is run for both hosts that failed as well as hosts that
were unreachable in the previous run - "Limit hosts to" always reflects
whichever of the two checkboxes are *currently* checked, recomputed fresh on
every toggle, rather than being incrementally appended to or edited in
place. Unchecking one of the two follows from that same rule:

* If only one of them was checked and it is now unchecked, "Limit hosts to"
  is cleared.
* If both were checked and one of them is now unchecked, "Limit hosts to" is
  recomputed to reflect only the one that is still checked.

### Command line flags

To match the three new checkboxes, the "rerun" verb gains three new
passthrough flags: `--only-failed`, `--only-unreachable` and
`--resume-where-failed`.

* These flags only ever pre-fill the rerun dialog's checkboxes (same as
  `-l`/`--tags` already do for the existing text fields) - the dialog is
  still shown and still has to be confirmed with return before anything
  actually runs.
* `--resume-where-failed` combined with an explicit `--start-at-play` on the
  command line is a usage error: tangsible shall fail with an explanatory
  error message rather than silently picking one over the other.
* If a flag asks for data that isn't available - e.g. `--only-failed` when
  the last run had no failed hosts, or no jsonl data can be found for the
  last run at all - tangsible shall likewise fail with an explanatory error
  message rather than silently ignoring the flag.

### Session:

claude --resume 3af8b38c-2078-4fc4-aa2c-d81e1be0becf
