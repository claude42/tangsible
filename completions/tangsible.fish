# Fish completion for tangsible.
#
# Install: copy or symlink this file into fish's own completions
# directory, e.g.:
#   cp tangsible.fish ~/.config/fish/completions/tangsible.fish
# (or wherever `pkg-config --variable completionsdir fish` points, for a
# system-wide install) - picked up automatically, no sourcing needed.
#
# Covers tangsible's own verbs and positionals (resolve.go/role.go/
# template.go/host.go/revisit.go) plus every flag `ansible-playbook
# --help` lists, since tangsible passes those straight through unchanged.

# Disable fish's default path completion so every completion offered here
# is a deliberate choice below, not an accidental fallback.
complete -c tangsible -f

# __fish_is_nth_token counts tokens ignoring the command itself and any
# token that *looks* like a flag - the same "close enough" heuristic
# fish's own bundled git/docker completions rely on for positional
# tracking, not a real parse of which flags consume a following word.
function __tangsible_roles --description 'Role names tangsible could plausibly mean'
	# Only ./roles and ~/.ansible/roles - design-docs/Tangsible role.md's
	# own examples pull from these; a roles_path override in ansible.cfg
	# or a Galaxy collection's own roles aren't considered, a deliberately
	# narrow heuristic in the same spirit as tangsible's own "good enough
	# for the common case" choices elsewhere.
	for d in roles ~/.ansible/roles
		test -d $d; or continue
		for r in $d/*/
			basename $r
		end
	end
end

# --- Verb (the first positional) ---

complete -c tangsible -n '__fish_is_nth_token 1' -a run -d 'Run a playbook, streaming its output live'
complete -c tangsible -n '__fish_is_nth_token 1' -a rerun -d 'Re-run the last (or a given) playbook/role'
complete -c tangsible -n '__fish_is_nth_token 1' -a role -d 'Run a single role in isolation'
complete -c tangsible -n '__fish_is_nth_token 1' -a template -d 'Debug a Jinja2 template against a host'
complete -c tangsible -n '__fish_is_nth_token 1' -a host -d 'Inspect one inventory host'
complete -c tangsible -n '__fish_is_nth_token 1' -a hosts -d 'List inventory hosts, then inspect one'
complete -c tangsible -n '__fish_is_nth_token 1' -a revisit -d 'Browse and reopen a previous run'

# --- Verb-specific positionals ---

# run/rerun/hosts/revisit: tangsible <playbook.yml> [ansible-playbook args...]
# revisit's own positional is optional too (falls back to the most
# recently revisitable target), same shape as run/rerun/hosts.
complete -c tangsible -n '__fish_seen_subcommand_from run rerun hosts revisit; and __fish_is_nth_token 2' \
	-a '(__fish_complete_suffix .yml .yaml)' -d Playbook

# role: tangsible role <role_name> [ansible-playbook args...]
complete -c tangsible -n '__fish_seen_subcommand_from role; and __fish_is_nth_token 2' \
	-a '(__tangsible_roles)' -d Role

# template: tangsible template <path to template> [<hostname>] [-e...]
complete -c tangsible -n '__fish_seen_subcommand_from template; and __fish_is_nth_token 2' \
	-a '(__fish_complete_suffix "")' -d Template
# The hostname (2nd positional) has no offered completions - it would
# need a live `ansible-inventory` call, and shelling out during tab
# completion (possibly to a dynamic/networked inventory source) isn't
# worth the latency/hang risk for this.

# host: tangsible host <hostname> [<playbook>] [ansible-playbook args...]
# Same "no dynamic hostname completion" reasoning as template's 2nd arg,
# applied here to the 1st.
complete -c tangsible -n '__fish_seen_subcommand_from host; and __fish_is_nth_token 3' \
	-a '(__fish_complete_suffix .yml .yaml)' -d Playbook

# --- ansible-playbook's own flags, forwarded verbatim by every verb ---
# Shown once a verb is already chosen (any verb: tangsible passes these
# straight through regardless of which one), from ansible-playbook's own
# --help output.

set -l __tangsible_has_verb '__fish_seen_subcommand_from run rerun role template host hosts revisit'

complete -c tangsible -n $__tangsible_has_verb -l help -d 'Show ansible-playbook help and exit'
complete -c tangsible -n $__tangsible_has_verb -l version -d 'Show ansible-playbook version info and exit'
complete -c tangsible -n $__tangsible_has_verb -s v -l verbose -d 'Increase verbosity (repeatable, e.g. -vvv)'

complete -k -c tangsible -n $__tangsible_has_verb -l private-key -r -a '(__fish_complete_suffix "")' -d 'SSH private key file'
complete -k -c tangsible -n $__tangsible_has_verb -l key-file -r -a '(__fish_complete_suffix "")' -d 'SSH private key file'
complete -c tangsible -n $__tangsible_has_verb -s u -l user -x -d 'Connect as this user'
complete -c tangsible -n $__tangsible_has_verb -s c -l connection -x -a 'smart ssh local paramiko docker' -d 'Connection type to use'
complete -c tangsible -n $__tangsible_has_verb -s T -l timeout -x -d 'Connection timeout in seconds'
complete -c tangsible -n $__tangsible_has_verb -l ssh-common-args -x -d 'Common args for sftp/scp/ssh'
complete -c tangsible -n $__tangsible_has_verb -l sftp-extra-args -x -d 'Extra args for sftp only'
complete -c tangsible -n $__tangsible_has_verb -l scp-extra-args -x -d 'Extra args for scp only'
complete -c tangsible -n $__tangsible_has_verb -l ssh-extra-args -x -d 'Extra args for ssh only'
complete -c tangsible -n $__tangsible_has_verb -s k -l ask-pass -d 'Ask for connection password'
complete -k -c tangsible -n $__tangsible_has_verb -l connection-password-file -r -a '(__fish_complete_suffix "")' -d 'Connection password file'

complete -c tangsible -n $__tangsible_has_verb -l force-handlers -d 'Run handlers even if a task fails'
complete -c tangsible -n $__tangsible_has_verb -s b -l become -d 'Run operations with become'
complete -c tangsible -n $__tangsible_has_verb -l become-method -x -a 'sudo su pbrun pfexec doas dzdo ksu runas machinectl sesu pmrun enable' -d 'Privilege escalation method'
complete -c tangsible -n $__tangsible_has_verb -l become-user -x -d 'Run operations as this user (default root)'
complete -c tangsible -n $__tangsible_has_verb -s K -l ask-become-pass -d 'Ask for privilege escalation password'
complete -k -c tangsible -n $__tangsible_has_verb -l become-password-file -r -a '(__fish_complete_suffix "")' -d 'Become password file'
complete -k -c tangsible -n $__tangsible_has_verb -l become-pass-file -r -a '(__fish_complete_suffix "")' -d 'Become password file'

complete -c tangsible -n $__tangsible_has_verb -s t -l tags -x -d 'Only run plays/tasks tagged with these values'
complete -c tangsible -n $__tangsible_has_verb -l skip-tags -x -d 'Skip plays/tasks tagged with these values'
complete -c tangsible -n $__tangsible_has_verb -s C -l check -d "Don't make changes, just predict them"
complete -c tangsible -n $__tangsible_has_verb -s D -l diff -d 'Show file/template diffs when changing them'

complete -k -c tangsible -n $__tangsible_has_verb -s i -l inventory -r -a '(__fish_complete_suffix "")' -d 'Inventory host path or comma-separated host list'
complete -k -c tangsible -n $__tangsible_has_verb -l inventory-file -r -a '(__fish_complete_suffix "")' -d 'Inventory host path or comma-separated host list'
complete -c tangsible -n $__tangsible_has_verb -l list-hosts -d 'List matching hosts; run nothing'
complete -c tangsible -n $__tangsible_has_verb -s l -l limit -x -d 'Further limit selected hosts to this pattern'
complete -c tangsible -n $__tangsible_has_verb -l flush-cache -d 'Clear the fact cache for every inventory host'

complete -c tangsible -n $__tangsible_has_verb -s e -l extra-vars -x -d 'Extra vars as key=value or YAML/JSON (@file to load)'
complete -c tangsible -n $__tangsible_has_verb -l vault-id -x -d 'Vault identity to use'
complete -c tangsible -n $__tangsible_has_verb -s J -l ask-vault-password -d 'Ask for the vault password'
complete -c tangsible -n $__tangsible_has_verb -l ask-vault-pass -d 'Ask for the vault password'
complete -k -c tangsible -n $__tangsible_has_verb -l vault-password-file -r -a '(__fish_complete_suffix "")' -d 'Vault password file'
complete -k -c tangsible -n $__tangsible_has_verb -l vault-pass-file -r -a '(__fish_complete_suffix "")' -d 'Vault password file'

complete -c tangsible -n $__tangsible_has_verb -s f -l forks -x -d 'Number of parallel processes (default 5)'
complete -k -c tangsible -n $__tangsible_has_verb -s M -l module-path -r -a '(__fish_complete_suffix "")' -d 'Prepend to the module search path'

complete -c tangsible -n $__tangsible_has_verb -l syntax-check -d "Check syntax, don't execute"
complete -c tangsible -n $__tangsible_has_verb -l list-tasks -d 'List tasks that would be executed'
complete -c tangsible -n $__tangsible_has_verb -l list-tags -d 'List all available tags'
complete -c tangsible -n $__tangsible_has_verb -l step -d 'Confirm each task before running it'
complete -c tangsible -n $__tangsible_has_verb -l start-at-task -x -d 'Start the playbook at this task name'
