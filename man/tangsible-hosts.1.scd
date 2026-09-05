tangsible-hosts(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-hosts - browse every inventory host

# SYNOPSIS

*tangsible hosts* [_playbook.yml_] [_ansible-playbook args_ ...]

# DESCRIPTION

*hosts* lists every host reported by *ansible-inventory --list*, and opens
the same five-tab detail view *tangsible-host*(1) shows for whichever one
you select. If _playbook.yml_ is omitted, it is resolved the same way as
*tangsible-run*(1) (see *tangsible*(1)).

Standard *ansible-playbook*(1) arguments can be used, e.g. *--inventory* or
*--tags* can be provided.

# KEY BINDINGS

*Up/Down, Page Up/Down, CTRL-B/F, Home/End, CTRL-A/E*
	Move the selection in the list.

*Enter*
	Open the selected host's detail view.

*Esc*
	From the detail view, clears an active search if there is one;
	otherwise goes back to the list.

*Tab / Shift-Tab*
	Switch tabs.

*/*
	Search the active tab's text.

*n / N*
	Jump to the next / previous match when search is active.

*q / Ctrl-C*
	Quit.

# EXIT STATUS

*0*
	Normal exit, however the view was closed.

*1*
	The inventory couldn't be listed, it reported no hosts at all, or
	unable to write the stub playbook to create the Plays tab.

# SEE ALSO

*tangsible*(1), *tangsible-host*(1)
