tangsible-host(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-host - everything Tangsible can determine about one host

# SYNOPSIS

*tangsible host* _hostname_ [_playbook.yml_] [_ansible-playbook args_ ...]

# DESCRIPTION

*host* shows all available information about a host from the inventory. If
_playbook.yml_ is omitted, it is resolved the same way *tangsible-run*(1)
resolves one (see *tangsible*(1)), though a playbook is only strictly
required for the Plays tab; the others work without one.

Standard *ansible-playbook*(1) arguments can be used, e.g. *--inventory* or
*--tags* can be provided.

# TABS

*Summary*
	Live-gathered facts (FQDN, IP addresses, OS, architecture, RAM,
	virtualization, and more), fetched from the host.

*Groups*
	Every inventory group this host belongs to, direct or transitive —
	a host-centric view of the group membership in contrast to the
	group-centric format of the inventory file.

*Plays*
	Every play in _playbook.yml_ that would actually run against this
	host.

*host_vars*
	The host's own *host_vars* files, if any are found alongside the
	playbook or inventory.

*Everything known*
	Raw output from *ansible-inventory --host* _hostname_.

# KEY BINDINGS

*Tab / Shift-Tab*
	Switch tabs.

*Up / Down, Page Up / Down, CTRL-B / F, Home/End, CTRL-A / E*
	Scroll the active tab.

*/*
	Search the active tab's text.

*n / N*
	Jump to the next / previous match when search is active.

*Esc*
	Close search dialog.

*q / Ctrl-C*
	Quit.

# EXIT STATUS

*0*
	Normal exit, however the view was closed.

*1*
	Unable to write the stub playbook to create the Plays tab.

*2*
	Usage error: no _hostname_ was given.

# SEE ALSO

*tangsible*(1), *tangsible-hosts*(1), *tangsible-template*(1)
