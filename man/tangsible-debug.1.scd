tangsible-debug(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-debug - set breakpoints and step through a playbook

# SYNOPSIS

*tangsible debug* [_playbook.yml_] [--all-vars|-a] [_ansible-playbook args_ ...]

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
breakpoints can be armed before the first *F5*/*F10*/*F11*/*F12*.

*debug* is the exact same live tree UI *tangsible-run*(1) uses, with
breakpoint/pause state layered on top - not a second, separate screen.
Every *tangsible-run*(1) key binding (navigation, Enter-to-expand, the
two-pane drill-down and its tabs, filter, search, recap, *q*/Ctrl-C) works
identically here; see that page for all of those. There is no rerun
dialog and no recorded run history, though - a debug session is not
something a later *tangsible rerun* can replay.

# ADDITIONAL OPTIONS

*--all-vars*, *-a*
	By default, *debug* captures a paused task's own variables only at a
	pause actually shown to you (an armed breakpoint, a step, or a task
	failure) - its Resolved tab works for that task, but not yet for one
	you haven't stopped at. This flag captures every task's own
	variables instead, including one silently resumed past with no
	breakpoint armed on it, so the Resolved tab works no matter which
	task you later navigate back to - at the cost of paying that
	capture's own real time on every single task, whether or not the run
	ever actually stops there.

# CONTROLS

Six keys exist only for this verb, on top of everything *tangsible-run*(1)
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

*F12*
	Redo: only meaningful while genuinely paused (a no-op otherwise).
	Re-runs the exact task/host currently paused at from scratch, and
	waits for that exact result - even if another host's own pending
	result for the same task would otherwise surface first, it's drained
	in the background rather than shown, so the view never jumps away
	from the host you asked to redo. That other host's own result, its
	outcome overwritten in place rather than duplicated, still shows up
	normally the next time you navigate to or step/continue onto it.

*v*
	Open the variable watch explorer: a text field with live autocomplete
	(name and current value, from whichever host/task is currently
	selected) for adding a watched expression - a bare name, or a
	dotted/bracket-index path into one, like *foo.bar* or *foo[0]*. Enter
	adds the typed or highlighted expression; *Up*/*Down* with an empty
	field selects a watched row instead (jumping straight to the last
	entry on *Up*, the first on *Down*, from no selection); *Delete* or
	*Backspace* removes the selected one. *Esc* is staged - closes a
	suggestion list, then clears the field, then closes the explorer (or
	deselects a row, then closes). Watched values re-evaluate live as you
	navigate to a different host/task, and stay visible, greyed out, once
	you close the explorer; one that can't currently be resolved shows as
	*<undefined>* rather than as an error, and the headline itself reads
	"Watched variables (unavailable)" instead whenever nothing was
	captured for the current host/task at all. While open, the
	explorer's own key hints replace the usual ones at the bottom of the
	screen, reverting the moment it closes; its own "Watched variables"
	headline (not the entries or the input field) is colored like the
	rest of the session's own chrome. *Page Up*/*Page Down*, *Home*/*End*,
	and *Tab* still reach the screen/tabs behind the explorer while it's
	open - only the plain arrow keys are claimed by the explorer itself.

Whenever a pause surfaces (an armed breakpoint, a step, or a next-task
stop), the drill-down
opens automatically for that exact paused task and host, in whichever of
full-screen or two-pane mode the terminal's current width calls for. Its
*Resolved* tab works for the task currently paused at; with *--all-vars*
(above), every task's own variables are captured as the run reaches it,
so the tab works for *any* task's drill-down, not just the one currently
paused at - showing the task's source with every variable reference filled
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
