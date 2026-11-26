tangsible-view(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-view - browse a playbook's plays and tasks without running anything

# SYNOPSIS

*tangsible view* [--ee _image_ | --no-ee] [_ansible-playbook options_ ...] [_playbook.yml_]

# DESCRIPTION

*view* shows what a playbook would do, as the tree *tangsible-run*(1) and
*tangsible-debug*(1) show while it runs - plays and their tasks, without any
host lines - but runs nothing. If _playbook.yml_ is omitted, it is resolved
the same way as *tangsible-run*(1) (see *tangsible*(1)).

The playbook is read the way *ansible-playbook*(1) reads it: roles, blocks,
imports and the order of *pre_tasks*, roles, *tasks* and *post_tasks* are as
Ansible resolves them. Only what is known without running can be shown: a
task pulled in by *include_tasks* is not expanded (the include itself is a
row), and a templated host pattern or task name is shown as written.
Handlers are not listed.

*Enter* (or a click) on a task opens a view with three tabs:

*Task*
	The task's name, its role (if any), its action, the *Hosts:* of its play
	as written (for example _web:!db1_), and its *when:* conditions and tags
	if it has any.

*Task definition*
	The task's source from its file; click a key in it for what it means.

*Docs*
	*ansible-doc*(1) for the task's action.

Standard *ansible-playbook*(1) arguments can be given; they are kept for
*r*. *--ee* and *--no-ee* are understood as for *tangsible-run*(1): the
playbook is then read inside the execution environment.

# KEY BINDINGS

*Up/Down, Page Up/Down, CTRL-B/F, Home/End, CTRL-A/E*
	Move the selection in the tree.

*Enter*
	Open the selected task.

*Right/Left*
	Nothing to expand in the tree: there are no host lines.

*/*
	Search the tree.

*r*
	Open the re-run dialog; confirming it runs the playbook (*tangsible-run*(1))
	in this same session, with the tags and limit entered there.

*e*
	In the task view, open the task's source file in *$VISUAL* / *$EDITOR*.
	When the editor returns the playbook is read again (see *R*).

*R*
	Read the playbook again, in the tree and in the task view, and show the
	new version: the cursor and the open task stay on the same task (found by
	play, name and position among tasks of that name), and say so if it is
	gone. A playbook that no longer loads leaves the old version in place and
	shows the reason in the bottom bar.

*Tab / Shift-Tab*
	In the task view, switch tabs.

*Right/Left, n / N*
	In the task view, the next / previous task (*n* / *N* step through matches
	instead while a search has matches).

*Esc*
	Back from the task view.

*q / Ctrl-C*
	Quit.

The keys *f* (filter), *o* (raw output) and *s* (statistics) belong to a run
and do nothing here.

# EXIT STATUS

*0*
	Normal exit.

*1*
	The playbook couldn't be read.

*2*
	Usage error.

# SEE ALSO

*tangsible*(1), *tangsible-run*(1), *tangsible-debug*(1)
