tangsible-debug(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-debug - step through a playbook, set breakpoints, watch variables.

# SYNOPSIS

*tangsible debug* [_playbook.yml_] [--all-vars|-a] [_ansible-playbook args_ ...]

# DESCRIPTION

*debug* lets you pause a playbook run at specific tasks, step through it
task by task, and inspect each paused task's own variables.

*tangsible debug* creates a copy of the playbook's directory tree 
nd addes *debugger: always* to every task in that copy to achieve its
interactive debugging functionality.

On startup, *debug* shows every play and task the run will touch, so breakpoints
can be armed before the first *F5*/*F10*/*F11*/*F12*.

*debug* is the exact same live tree UI *tangsible-run*(1) uses, with
breakpoint/single step functionality, variable explorer and Resolved tab
layered on top.

# ADDITIONAL OPTIONS

*--all-vars*, *-a*
	By default, *debug* captures a paused task's own variables only at a
	pause actually shown to you (an armed breakpoint, a step, or a task
	failure) - its Resolved tab works for that task, but not yet for one
	you haven't stopped at. This flag captures every task's own
	variables instead, including one silently resumed past with no
	breakpoint armed on it, so the Resolved tab works no matter which
	task you later navigate back to - at the cost of significantly reduced
	performance.

# CONTROLS

Six keys exist only for this verb, on top of everything *tangsible-run*(1)
already offers:

*F5*
	Run / continue: start / resume execution until the  next breakpoint or
	the end of the playbook is reached.

*F9*
	Arm or disarm a breakpoint on the selected task row (a no-op on a play
	header or a host row) - shown as a red *●* before the task's name. A
	left click on that same first column does the same thing, instead of
	the row's usual expand/collapse.

*F10*
	Next task: before a run starts, or while paused, executes the rest of
	the current task's remaining hosts and stops unconditionally at the
	next task's first host.

*F11*
	Step: before a run starts, or while paused, stops at the very next
	host (of the current or next task).

*F12*
	Redo: only meaningful while genuinely paused (a no-op otherwise).
	Re-runs the exact task/host currently paused at from scratch. Will take
	any modifications made in the variable watch explorer into account.

*v*
	Add new variables to be watched. Will autocomplete known variables.
	Currently undefined variables can be watched as well. Currently only
	supports scalars - elements of sequences or mappings can be accessed
	e.g. with *foo.bar* or *foo[0]*.

	Use cursor up/down to select a variable and press *Delete* or
	*Backspace* to stop watching it.

	If no watched variables are available for a specific task (because
	execution has not stopped at this task and *-a* has not been specified,
	the header will read "Watched variables (unavailable)". If a variable is
	not defined for a specific task it will be listed as *<undefined>*.

	Change a variables value by enterint *var_name = new_value*. Changed
	values will only be used when the current task is repeated with *F12*

Whenever execution stops, the drill-down view opens automatically for that exact
paused task and host, in whichever of full-screen or two-pane mode the
terminal's current width calls for. Its *Resolved* tab works for the task
currently paused at (or all tasks with *-a*) - showing the task's source with every
variable reference filled in from its own captured state.

# EXIT STATUS

Always *0* once the TUI itself exits normally, regardless of whether any
task actually failed - the tree's own per-host coloring already shows
that. *2* on a usage error (no playbook could be resolved, or *--step* was
given).

# SEE ALSO

*tangsible*(1), *tangsible-run*(1)
