tangsible(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible - a live terminal UI for ansible-playbook

# SYNOPSIS

*tangsible* _verb_ [_args_ ...]

# DESCRIPTION

*tangsible* wraps *ansible-playbook*(1) in a live, navigable tree of plays → tasks → hosts. It comes up immediately and updates live while the playbook is still running, and lets you drill into any host's result — task source, output, stderr, *--diff* output, full JSON — without losing your place in the run.

It is a development and troubleshooting interface for *ansible-playbook*, not a replacement for it or an orchestration platform. It is aimed at someone running playbooks by hand, repeatedly, against a live-ish environment — not at CI pipelines, scheduled automation, or fleet-scale inventories.

In addition, tangsible provides useful utilities to rerun / analyze previous playbook runs, debug Jinja2 template, get host information and easily edit individually encrypted variables.

Every _verb_ below is documented in its own manual page. This page covers what all of them have in common: configuration, environment variables, and files.

# VERBS

*tangsible-run*(1)
	Run a playbook and show its live progress in the tree UI.

*tangsible-rerun*(1)
	Run a previous playbook again. Hosts, tags, starting play and other parameters can be modified compared to the previous run.

*tangsible-role*(1)
	Run a single role in isolation, without hand-writing a wrapper playbook.

*tangsible-template*(1)
	Render one Jinja2 template through Ansible's own templating engine, against an existing inventory host.

*tangsible-host*(1)
	Show everything Tangsible can determine about one host: live facts, inventory group membership, which plays would run, host_vars, and the raw *ansible-inventory --host* dump.

*tangsible-hosts*(1)
	Browse all inventory host, then drill into any one of them with the same view *tangsible host* shows.

*tangsible-revisit*(1)
	Browse previous runs and reopen one, then analyze all information exactly as it looked when it finished.

*tangsible-vault*(1)
	Edit individually vault-encrypted variables in a file in place, without hand-running *ansible-vault encrypt_string* and pasting ciphertext back yourself.

*tangsible version*
	Print version information about tangsible and all components it's using.

# REQUIREMENTS

*tangsible* requires *ansible-playbook* and the jsonl plugin from the *ansible.posix* collection to be installed and on *PATH*:

```
ansible-galaxy collection install ansible.posix
```

# CONFIGURATION

When a _verb_ that needs a playbook is not given one explicitly, Tangsible resolves one in order from:

. *$TANGSIBLE_PLAYBOOK*
. *.tangsible/config.toml* in the current directory
. *$XDG_CONFIG_HOME/tangsible/config.toml* (default *~/.config/tangsible/config.toml*)
. *./site.yml*, if present

Both config files share the same TOML shape:

```
[general]
default_playbook = "myplaybook.yml"
default_tree_state = "collapsed"   # "expanded" or "collapsed"
two_pane_layout = true             # default: true
color = true                       # default: true
```

# FILES

*$XDG_CONFIG_HOME/tangsible/config.toml* (default *~/.config/tangsible/config.toml*)
	Global user settings.

*.tangsible/config.toml*
	Project-local, user settings.

# EXIT STATUS

*0*
	The playbook completed successfully; completed with only unreachable hosts and no other failure; or the run was interrupted by the user (*q* or *Ctrl-C*). None of these are treated as a Tangsible failure.

*1*
	The playbook genuinely failed (any exit code other than the two benign cases above), or the terminal UI itself errored.

*2*
	Usage error: a required argument was missing, or no playbook could be resolved.

# LIMITATIONS

- *strategy: free* is not supported
- Interactive playbook elements (e.g. a bare *pause* task or *--step*) are not supported
- Execution environments are not supported
- Tangsible is developed and tested against inventories on the order of ten hosts, not fleet scale.

# SEE ALSO

*ansible-playbook*(1), *tangsible-run*(1), *tangsible-rerun*(1), *tangsible-role*(1), *tangsible-template*(1), *tangsible-host*(1), *tangsible-hosts*(1), *tangsible-revisit*(1), *tangsible-vault*(1)
