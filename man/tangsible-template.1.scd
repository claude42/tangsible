tangsible-template(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-template - debug a Jinja2 template against real inventory hosts

# SYNOPSIS

*tangsible template* _path_ [_hostSpec_] [_ansible-playbook args_ ...]

# DESCRIPTION

*template* renders _path_ through Ansible's templating engine by generating a playbook that templates it via a *ansible.builtin.template* task, showing one tab per rendered host plus a shared *Source* tab.

_hostSpec_ is a comma-separated list of tokens, each either a literal hostname, an inventory group name, or the special keyword *all* (every host in the inventory) - mixing them freely is fine, the result is just their deduplicated union. If _hostSpec_ is omitted entirely, the first host reported by *ansible-inventory --list* is used. If resolution produces more hosts than the *template_hosts_max* config setting (default 10, see *tangsible*(1)), you're asked to confirm before rendering against all of them. Extra variables can be supplied with *-e*, same as any *ansible-playbook* invocation. If _path_ lives in a role's conventional *templates/* directory, that role's own *defaults/main.yml* and *vars/main.yml* are made available automatically.

# KEY BINDINGS

*Tab / Shift-Tab*
	Switch tabs.

*Up / Down, Page Up / Down, CTRL-B / F, Home/End, CTRL-A / E*
	Scroll the active tab.

*e*
	Edit the Jinja2 template file, then re-render every open host tab.

*h*
	Open the "change host" dialog: type a new hostname, Enter to re-render just that tab against it - or switch to it if already open - Esc to cancel.

*/*
	Search the active tab's text.

*y*
	Copy the active tab's content to the clipboard.

*n / N*
	Jump to the next / previous match when search is active.

*Esc*
	Close search or host dialog.

*q / Ctrl-C*
	Quit.

# EXIT STATUS

*0*
	Normal exit, however the view was closed - also returned if you decline the "render against N hosts?" confirmation prompt.

*1*
	_path_ couldn't be read, a token in _hostSpec_ wasn't a known host or group, no host could be resolved from the inventory, or a scratch file this view depends on internally couldn't be created.

*2*
	Usage error: no _path_ was given.

# SEE ALSO

*tangsible*(1), *tangsible-host*(1)
