tangsible-run(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-run - run a playbook with a live, navigable tree UI

# SYNOPSIS

*tangsible run* [_playbook.yml_] [--start-at-play _name_] [_ansible-playbook args_ ...]

# DESCRIPTION

*run* starts _playbook.yml_ under *ansible-playbook*(1) and shows its progress
as a live tree of plays → tasks → hosts, updating incrementally as the run
proceeds. Any trailing arguments are passed straight through to
*ansible-playbook* unchanged.

# ADDITIONAL OPTIONS

*--start-at-play* _name_
	Starts executing the playbook at the given play. Will only work with the playbook's top-level plays, not with play pulled in via *import_playbook*.

# KEY BINDINGS

*Up/Down, Page Up/Down, CTRL-B/F, Home/End, CTRL-A/E*
	Move the cursor (stops autoscrolling). Or scroll the active tab when
	showing a host's full results.

*F*
	Resume autoscrolling to the newest row.

*Left / Right*
	Collapse / expand the task under the cursor.

*E / C*
	Expand / collapse every task at once.

*n / N*
	Jump to the next / previous task.

*f*
	Open the filter dialog: All / Interesting / Changed / Failed.

*/*
	Search filter (task name, source, and output).

*Enter*
	Toggle a task's host rows, or open a host's full result if the cursor is on one.

*Tab / Shift-Tab*
	Switch tabs.

*r*
	Open the rerun dialog, once the run has finished. See *tangsible-rerun*(1) for what it does.

*d*
	Open the diff picker: choose a previous run to compare this one against. Requires at least one other completed run recorded in _.tangsible/state.toml_; otherwise this key does nothing. See *tangsible-revisit*(1), whose own list picker this reuses.

*Esc*
	Close dialog or host's full results.

*q / Ctrl-C*
	While the playbook is still running, interrupt it (forwarded as *SIGINT*, exactly as pressing Ctrl-C on a plain *ansible-playbook* invocation would). Once it has finished, quit.

# EXIT STATUS

See *tangsible*(1).

# SEE ALSO

*tangsible*(1), *ansible-playbook*(1), *tangsible-rerun*(1), *tangsible-role*(1), *tangsible-revisit*(1)
