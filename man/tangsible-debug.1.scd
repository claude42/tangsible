tangsible-debug(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-debug - set breakpoints and step through a playbook

# SYNOPSIS

*tangsible debug* [_playbook.yml_] [_ansible-playbook args_ ...]

# DESCRIPTION

*debug* lets you pause a playbook run at specific tasks, step through it
task by task, and inspect each paused task's own variables - built on
ansible-core's own built-in task debugger, not a separate interactive
protocol of its own.

It never touches the real playbook or role files on disk: *debug* first
builds a disposable, hardlinked copy of the whole playbook's directory tree
(under *.tangsible/debug/* next to the playbook, removed again on exit),
splices ansible-core's *debugger: always* onto every task in that copy, and
runs the copy instead. Every task therefore pauses; a breakpoint you set is
purely a client-side choice of which of those pauses to actually show you -
*debug* resumes silently past every other one on your behalf, which is also
why breakpoints can be toggled live, mid-run, with no re-editing of
anything.

On startup, *debug* shows every play and task the run will touch, predicted
the same way the live tree's own progress indicator is (a throwaway
*--list-tasks --list-hosts* probe) - a notified handler, or a dynamically
resolved *include_tasks:*, won't appear in this predicted list even though
it will genuinely run; see *tangsible*(1)'s own *--list-tasks* caveats.

This is a separate, standalone screen - not *tangsible-run*(1)'s own live
tree - with no rerun dialog, no two-pane drill-down, and no recorded run
history; a debug session is not something a later *tangsible rerun* can
replay.

# CONTROLS

Before the run starts, move the cursor and press *t* to arm or disarm a
breakpoint on the selected task, then *enter* to run (stopping at the first
armed breakpoint, or running to completion if none are set) or *s* to step
- stopping at the very first task regardless of any breakpoint.

Once paused, the same *enter*/*s* continue the run the same way, *t* keeps
arming/disarming breakpoints (including the one you're currently stopped
on), and *v* shows the paused task's own variables - the state they held
*before* this task ran, which is what ansible-core's own debugger scope
actually exposes; this task's own effect on them isn't visible until the
*next* task's own pause.

*q*/*Ctrl-C* interrupts the child while it's running or paused, and quits
once it's finished (or immediately, before any run has started) - the same
two-stage behavior *tangsible-run*(1) already has.

# LIMITATIONS

Deliberately out of scope for now: Step Out (ansible-core has no real call
stack to step out of), modifying a variable's value, and *--step* (rejected
outright - it would pause the child for a second, unrelated reason *debug*
doesn't know how to drive; *debug*'s own breakpoints are what pause a run).
Two tasks sharing the exact same name alias to the same breakpoint.

# EXIT STATUS

Always *0* once the TUI itself exits normally, regardless of whether any
task actually failed - the tree's own per-host coloring already shows
that. *2* on a usage error (no playbook could be resolved, or *--step* was
given).

# SEE ALSO

*tangsible*(1), *tangsible-run*(1)
