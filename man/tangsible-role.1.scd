tangsible-role(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-role - run a single role in isolation

# SYNOPSIS

*tangsible role* _role_name_ [--dialog | --no-dialog] [_ansible-playbook args_ ...]

# DESCRIPTION

Testing a single role normally means hand-writing a temporary playbook
around it first. *role* does that for you: it generates a minimal stub
playbook (*hosts: all*, *roles: [*_role_name_*]*) in the current directory,
runs it exactly as *tangsible-run*(1) would, and removes the stub again once
Tangsible exits.

The stub always targets *hosts: all*; narrow the actual target the normal
way, with *-i*/*-l* passthrough arguments. The role is looked up as Ansible
would: *./roles/*_role_name_, then each entry of the effective roles path
(*$ANSIBLE_ROLES_PATH*, *roles_path* in *ansible.cfg*, or the defaults
*~/.ansible/roles*, */usr/share/ansible/roles*, */etc/ansible/roles*). If
it isn't found, a warning is printed up front — the run proceeds regardless
(Ansible may still resolve the role some other way, e.g. from a collection),
but the drill-down view's Task definition section won't be available for it.

Everything else — the live tree, filtering, the rerun dialog, the diff
picker — works exactly as in *tangsible-run*(1).

# ADDITIONAL OPTIONS

*--dialog*
	Show the run dialog before starting, instead of running immediately — see *tangsible-rerun*(1) for what it offers. 

*--no-dialog*
	Don't show run dialog, potentially overrides the *run_dialog* setting (*tangsible*(1)) for this invocation.

# EXIT STATUS

Same as *tangsible-run*(1); see *tangsible*(1).

# SEE ALSO

*tangsible*(1), *tangsible-run*(1), *tangsible-rerun*(1)
