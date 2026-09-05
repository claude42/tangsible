tangsible-rerun(1) "Tangsible" "Tangsible Manual"

# NAME

tangsible-rerun - repeat the last playbook run

# SYNOPSIS

*tangsible rerun* [_playbook.yml or role name_] [--start-at-play _name_] [_ansible-playbook args_ ...]

# DESCRIPTION

*rerun* will repeat the last playbook run. Before the rerun is started, the
user can modify certain settings (e.g. hosts, tags, etc.) of the previous
run. Confirming the dialog starts the run exactly as *tangsible-run*(1)
would have.

- With no target (_playbook.yml_ or _role_) given at all, *rerun* falls
  back to whichever playbook or role was invoked most recently, by any
  verb.
- Otherwise the last invocation of the specified _playbook.yml_ or _role_
  will be rerun.
- Explicit *--tags*/*-t*, *--skip-tags*, *--limit*/*-l*, or
  *--start-at-play* if given, win over that target's own history — otherwise the
  history entry's own tags/skip-tags/hosts pre-fill the dialog.
- Any _other_ passthrough argument given on the command line (inventory,
  extra-vars, verbosity, ...) replaces the history entry's own remembered
  arguments outright, rather than merging with them.

A target with no recorded history at all (never run before) still works —
every dialog field simply starts empty.

The rerun dialog itself offers options for *Start with play*, *Start with
task*, *Limit tags*, *Skip tags*, and *Limit hosts* - each corresponding to
the respective options.

# EXIT STATUS

Same as *tangsible-run*(1); see *tangsible*(1). Resolving no target at all
(see above) is a usage error.

# SEE ALSO

*tangsible*(1), *tangsible-run*(1), *tangsible-role*(1)
