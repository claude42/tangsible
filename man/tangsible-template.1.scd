tangsible-template(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-template - debug a Jinja2 template against a real inventory host

# SYNOPSIS

*tangsible template* _path_ [_hostname_] [_ansible-playbook args_ ...]

# DESCRIPTION

*template* renders _path_ through Ansible's templating engine by generating a playbook that templates it via a *ansible.builtin.template* task.

If _hostname_ is omitted, the first host reported by *ansible-inventory --list* is used. Extra variables can be supplied with *-e*, same as any *ansible-playbook* invocation. If _path_ lives in a role's conventional *templates/* directory, that role's own *defaults/main.yml* and *vars/main.yml* are made available automatically.

# KEY BINDINGS

*Tab / Shift-Tab*
	Switch tabs.

*Up / Down, Page Up / Down, CTRL-B / F, Home/End, CTRL-A / E*
	Scroll the active tab.

*e*
	Edit the Jinja2 template file.

*h*
	Open the "change host" dialog: type a new hostname, Enter to re-render against it, Esc to cancel.

*/*
	Search the active tab's text.

*n / N*
	Jump to the next / previous match when search is active.

*Esc*
	Close search or host dialog.

*q / Ctrl-C*
	Quit.

# EXIT STATUS

*0*
	Normal exit, however the view was closed.

*1*
	_path_ couldn't be read, no host could be resolved from the inventory, or a scratch file this view depends on internally couldn't be created.

*2*
	Usage error: no _path_ was given.

# SEE ALSO

*tangsible*(1), *tangsible-host*(1)
