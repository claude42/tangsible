tangsible-inventory(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-inventory - browse the whole inventory as a tree

# SYNOPSIS

*tangsible inventory* [_playbook.yml_] [_ansible-playbook args_ ...]

# DESCRIPTION

*inventory* shows an interactive tree of the whole inventory, similar to
*ansible-inventory --graph*. Opening a host shows the same five-tab detail view
*tangsible-host*(1) shows. If _playbook.yml_ is omitted, it is resolved the same
way as *tangsible-run*(1) (see *tangsible*(1)), though a playbook is only
strictly required for the detail view's own Plays tab.

Standard *ansible-playbook*(1) arguments can be used, e.g. *--inventory* or
*--tags* can be provided.

# KEY BINDINGS

*Up/Down, Page Up/Down, CTRL-B/F, Home/End, CTRL-A/E*
	Move the selection in the tree.

*Enter*
	Toggle a group, or open the selected host's detail view.

*Right*
	Expand a collapsed group.

*Left*
	Collapse an expanded group - or, if it's already collapsed, move
	up to its parent group.

*E / C*
	Expand / collapse every group.

*Esc*
	From the detail view, clears an active search if there is one;
	otherwise goes back to the tree.

*Tab / Shift-Tab*
	Switch tabs.

*/*
	Search the active tab's text.

*y*
	Copy the active tab's content to the clipboard.

*n / N*
	Jump to the next / previous host, or the next / previous search match if
	a search is active.

*q / Ctrl-C*
	Quit.

# EXIT STATUS

*0*
	Normal exit, however the view was closed.

*1*
	The inventory couldn't be listed, it reported no hosts at all, or
	unable to write the stub playbook to create the Plays tab.

# SEE ALSO

*tangsible*(1), *tangsible-host*(1), *tangsible-hosts*(1)
