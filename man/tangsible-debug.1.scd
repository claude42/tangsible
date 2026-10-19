tangsible-debug(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-debug - set breakpoints and step through a playbook

# SYNOPSIS

*tangsible debug* [_playbook.yml_] [--vars-at-stops-only] [_ansible-playbook args_ ...]

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
it will genuinely run; see *tangsible*(1)'s own *--list-tasks* caveats. The
whole predicted tree is visible up front, before anything has run, so
breakpoints can be armed before the first *F5*/*F10*/*F11*.

*debug* is the exact same live tree UI *tangsible-run*(1) uses, with
breakpoint/pause state layered on top - not a second, separate screen.
Every *tangsible-run*(1) key binding (navigation, Enter-to-expand, the
two-pane drill-down and its tabs, filter, search, recap, *q*/Ctrl-C) works
identically here; see that page for all of those. There is no rerun
dialog and no recorded run history, though - a debug session is not
something a later *tangsible rerun* can replay.

# ADDITIONAL OPTIONS

*--vars-at-stops-only*
	By default, *debug* captures each paused task's own variables for
	*every* task reached, including one silently resumed past with no
	breakpoint armed on it, so its Resolved tab works no matter which
	task you later navigate back to - at the cost of paying that
	capture's own real time on every single task, whether or not the run
	ever actually stops there. This flag captures only at a pause
	actually shown to you (an armed breakpoint, a step, or a task
	failure), trading that for a noticeably faster run through everything
	else.

# CONTROLS

Four keys exist only for this verb, on top of everything *tangsible-run*(1)
already offers:

*F5*
	Run: before a run starts, or while paused, resumes past every pause
	whose task isn't armed, surfacing only an armed one - or runs to
	completion outright if nothing is armed at all.

*F9*
	Arm or disarm a breakpoint on the selected task row (a no-op on a play
	header or a host row) - shown as a red *●* before the task's name. A
	left click on that same first column does the same thing, instead of
	the row's usual expand/collapse.

*F10*
	Next task: before a run starts, or while paused, skips the rest of
	the current task's remaining hosts and stops unconditionally at the
	next task's first pause, armed or not.

*F11*
	Step: before a run starts, or while paused, stops at the very next
	pause unconditionally, armed or not - one host at a time, including
	every other host still left on the current task.

Whenever a pause surfaces (an armed breakpoint, a step, or a next-task
stop), the drill-down
opens automatically for that exact paused task and host, in whichever of
full-screen or two-pane mode the terminal's current width calls for. Its
*Resolved* tab works for *any* task's drill-down, not just the one
currently paused at - every task's own variables are captured as the run
reaches it (see *--vars-at-stops-only* above to trade that for speed
instead) - showing the task's source with every variable reference filled
in from its own captured state, including *when:*/*failed_when:*/
*changed_when:*/*until:* conditions and a *register:* target's own result.
The raw-output (*o*) view has nothing to show in a debug session, since the
debugger itself owns the child's stdout for its own prompt text.

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
