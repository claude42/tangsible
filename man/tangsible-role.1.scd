tangsible-role(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-role - run a single role in isolation

# SYNOPSIS

*tangsible role* _role_name_ [_ansible-playbook args_ ...]

# DESCRIPTION

Testing a single role normally means hand-writing a temporary playbook
around it first. *role* does that for you: it generates a minimal stub
playbook (*hosts: all*, *roles: [*_role_name_*]*) in the current directory,
runs it exactly as *tangsible-run*(1) would, and removes the stub again once
Tangsible exits.

The stub always targets *hosts: all*; narrow the actual target the normal
way, with *-i*/*-l* passthrough arguments. If a *./roles/*_role_name_
directory isn't found relative to the current directory, a warning is
printed up front — the run proceeds regardless (Ansible may still resolve
the role from *$ANSIBLE_ROLES_PATH*, a configured roles path, or a
collection), but the drill-down view's Task definition section won't be
available for it, since the source lookup only ever walks the stub's own
directory tree.

Everything else — the live tree, filtering, the rerun dialog, the diff
picker — works exactly as in *tangsible-run*(1).

# EXIT STATUS

Same as *tangsible-run*(1); see *tangsible*(1).

# SEE ALSO

*tangsible*(1), *tangsible-run*(1), *tangsible-rerun*(1)
