# Bash completion for tangsible.
#
# Install (pick one):
#   - Copy/symlink into bash-completion's own directory, usually
#     /usr/share/bash-completion/completions/tangsible or
#     /etc/bash_completion.d/tangsible - picked up automatically on the
#     next shell start.
#   - Or source it directly, e.g. from ~/.bashrc:
#       source /path/to/tangsible/completions/tangsible.bash
#
# Covers tangsible's own verbs and positionals (resolve.go/role.go/
# template.go/host.go/revisit.go) plus every flag `ansible-playbook
# --help` lists, since tangsible passes those straight through unchanged.
# Positional tracking below only recognizes the leading one or two
# arguments each verb itself gives meaning to (mirroring what tangsible's
# own arg parsers do) - anything after that falls back to plain filename
# completion, the same "good enough, not chased further" heuristic
# tangsible's own source uses for similar judgment calls.

_tangsible_verbs="run rerun role template host hosts revisit vault version"

# Every long/short flag ansible-playbook accepts, verbatim from its own
# --help - tangsible interprets none of these itself, it just forwards
# them, so completion offers the exact same surface `ansible-playbook`
# does.
_tangsible_ap_flags="-h --help --version -v --verbose \
--private-key --key-file -u --user -c --connection -T --timeout \
--ssh-common-args --sftp-extra-args --scp-extra-args --ssh-extra-args \
-k --ask-pass --connection-password-file --force-handlers -b --become \
--become-method --become-user -K --ask-become-pass --become-password-file \
--become-pass-file -t --tags --skip-tags -C --check -D --diff \
-i --inventory --inventory-file --list-hosts -l --limit --flush-cache \
-e --extra-vars --vault-id -J --ask-vault-password --ask-vault-pass \
--vault-password-file --vault-pass-file -f --forks -M --module-path \
--syntax-check --list-tasks --list-tags --step --start-at-task"

# Flags whose value is a path on disk.
_tangsible_ap_file_flags="-i --inventory --inventory-file \
--private-key --key-file --connection-password-file \
--become-password-file --become-pass-file \
--vault-password-file --vault-pass-file -M --module-path"

# compgen -X's extglob pattern needs the shell option on regardless of the
# user's own shopt state - saved/restored so this function has no lasting
# side effect on the interactive shell.
_tangsible_ymlfiles()
{
	local cur=$1 restore
	restore=$(shopt -p extglob)
	shopt -s extglob
	COMPREPLY=($(compgen -o plusdirs -f -X '!*.@(yml|yaml)' -- "$cur"))
	eval "$restore"
	compopt -o filenames 2>/dev/null
}

# Role names tangsible could plausibly mean for "tangsible role <name>" -
# a subdirectory name under ./roles or ~/.ansible/roles, the two places
# design-docs/Tangsible role.md's own examples pull from. Not aware of
# roles_path overrides in ansible.cfg or Galaxy-installed collections'
# own roles - a deliberately narrow heuristic, same spirit as tangsible's
# own "good enough for the common case" choices elsewhere.
_tangsible_roles()
{
	local d name
	for d in roles ~/.ansible/roles; do
		[ -d "$d" ] || continue
		for name in "$d"/*/; do
			[ -d "$name" ] || continue
			name=${name%/}
			printf '%s\n' "${name##*/}"
		done
	done | sort -u
}

_tangsible()
{
	local cur prev verb
	COMPREPLY=()
	cur=${COMP_WORDS[COMP_CWORD]}
	prev=${COMP_WORDS[COMP_CWORD-1]}

	if [ "$COMP_CWORD" -eq 1 ]; then
		COMPREPLY=($(compgen -W "$_tangsible_verbs" -- "$cur"))
		return
	fi

	verb=${COMP_WORDS[1]}

	# A fixed-choice ansible-playbook flag was just typed - complete its
	# value from the choices ansible-core itself documents, not a file.
	case "$prev" in
	--become-method)
		COMPREPLY=($(compgen -W "sudo su pbrun pfexec doas dzdo ksu runas machinectl sesu pmrun enable" -- "$cur"))
		return
		;;
	-c | --connection)
		COMPREPLY=($(compgen -W "local smart ssh paramiko docker" -- "$cur"))
		return
		;;
	esac

	# A file-valued flag was just typed.
	case " $_tangsible_ap_file_flags " in
	*" $prev "*)
		COMPREPLY=($(compgen -f -- "$cur"))
		compopt -o filenames 2>/dev/null
		return
		;;
	esac

	# A new flag being typed.
	if [[ $cur == -* ]]; then
		COMPREPLY=($(compgen -W "$_tangsible_ap_flags" -- "$cur"))
		return
	fi

	# A bare (non-flag) word: which positional this is depends on how many
	# bare words already appear between the verb and here, skipping any
	# flag and (for a value-taking flag) the word right after it - the
	# same shape splitPlaybookArgs/parseTemplateArgs/parseHostArgs give
	# meaning to on tangsible's own side.
	local pos=0 i w skip=0
	for ((i = 2; i < COMP_CWORD; i++)); do
		w=${COMP_WORDS[i]}
		if [ "$skip" -eq 1 ]; then
			skip=0
			continue
		fi
		case "$w" in
		-*)
			case " $_tangsible_ap_file_flags --become-method -c --connection " in
			*" $w "*) skip=1 ;;
			esac
			;;
		*)
			pos=$((pos + 1))
			;;
		esac
	done

	case "$verb" in
	run | rerun | hosts | revisit)
		# tangsible <playbook.yml> [ansible-playbook args...]
		# revisit's own positional is optional too (falls back to the most
		# recently revisitable target), same shape as run/rerun/hosts.
		if [ "$pos" -eq 0 ]; then
			_tangsible_ymlfiles "$cur"
		else
			COMPREPLY=($(compgen -f -- "$cur"))
			compopt -o filenames 2>/dev/null
		fi
		;;
	role)
		# tangsible role <role_name> [ansible-playbook args...]
		if [ "$pos" -eq 0 ]; then
			COMPREPLY=($(compgen -W "$(_tangsible_roles)" -- "$cur"))
		else
			COMPREPLY=($(compgen -f -- "$cur"))
			compopt -o filenames 2>/dev/null
		fi
		;;
	template)
		# tangsible template <path to template> [<hostname>] [-e...]
		# The hostname (2nd positional) has no offered completions here -
		# it would need a live ansible-inventory call, and shelling out
		# during tab completion (possibly to a dynamic/networked
		# inventory source) isn't worth the latency/hang risk for this.
		if [ "$pos" -eq 0 ]; then
			COMPREPLY=($(compgen -f -- "$cur"))
			compopt -o filenames 2>/dev/null
		fi
		;;
	host)
		# tangsible host <hostname> [<playbook>] [ansible-playbook args...]
		# Same "no dynamic hostname completion" reasoning as template above.
		if [ "$pos" -eq 1 ]; then
			_tangsible_ymlfiles "$cur"
		fi
		;;
	esac
}

complete -F _tangsible tangsible
