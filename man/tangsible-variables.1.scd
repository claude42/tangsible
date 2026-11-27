tangsible-variables(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-variables - where each variable of a host is defined, and which definition wins

# SYNOPSIS

*tangsible variables* [_ansible-playbook options_ ...] _hostname_ [_playbook.yml_]

# DESCRIPTION

*variables* lists the variables of one inventory host, one line each: the value
of the definition that wins, and the file and line it is written in. *Enter*
(or a click) on a variable shows all its definitions, the winning one first,
in Ansible's own order of precedence, each with its level (for example
_inventory group_vars (web)_ or _role defaults (app)_), its value and its place.
Nothing is run on any host, and nothing is gathered: only what is written
down is looked at.

The definitions come from the inventory (variables written in the inventory
file, and its _group_vars_ and _host_vars_), from the _group_vars_ and
_host_vars_ next to the playbook, and from the _defaults_ and _vars_ of the
roles of the plays that apply to the host. If _playbook.yml_ is omitted it is
resolved as for *tangsible-run*(1) (see *tangsible*(1)); without a playbook
only the inventory side is listed. Several plays applying to the host are
treated as one (their roles together).

When the inventory is in the same directory as the playbook, their shared
_group_vars_ and _host_vars_ are listed once, as the playbook's (the higher
level).

The files are read by Ansible's own code, one source at a time, in the order
of precedence Ansible uses (including a changed *VARIABLE_PRECEDENCE*). Ansible
is also asked for its own answer, and that decides the winner: when it names a
definition other than the one our reading of the order puts first, the
variable is marked with a *?*, Ansible's definition is shown first, and the
order of the other definitions is to be taken with care.

A value written in a vault is shown as _<vaulted>_. A vaulted value that no
given password opens (an unused leftover, say) is shown the same way and does
not stop the list.

Not covered yet: variables of the play itself (*vars:*, *vars_files:*) and of
blocks and tasks, gathered facts, and what *set_fact* and *register* create
while a playbook runs. *-e* values given on the command line are listed. A
play whose host pattern is templated, and roles included with a computed name,
are not looked at; what was skipped is named under the list.

Standard *ansible-playbook*(1) arguments are passed through (*-i*,
*-e*, *--vault-password-file*, ...). *--ask-vault-pass* is not supported here:
use a password file. Execution environments are not used; the files are read
on the host.

# KEY BINDINGS

*Up/Down, Page Up/Down, CTRL-B/F, Home/End, CTRL-A/E*
	Move the cursor.

*Enter*
	On a variable: show or hide its definitions. On a definition: show the
	file it is in, with the definition marked and in view. The file view is
	beside the list on a wide terminal and over it on a narrow one; in it,
	*Up/Down*, *Page Up/Down* scroll, */* and *n*/*N* search, *y* copies the
	file, *e* edits it (and everything is read again afterwards), *Esc* or *q*
	go back.

*E*, *C*
	Show / hide the definitions of every variable.

*Right/Left*
	The next / previous host of the inventory (the choice of definitions
	you opened stays).

*h*
	Pick a host from a list.

*e*
	Open the file of the definition under the cursor in *$VISUAL* / *$EDITOR*.
	When the editor returns, everything is read again.

*R*
	Read the files again.

*?*, *F1*
	Help.

*q*, *CTRL-C*
	Quit.

# SEE ALSO

*tangsible*(1), *tangsible-host*(1), *tangsible-view*(1), *tangsible-vault*(1)
