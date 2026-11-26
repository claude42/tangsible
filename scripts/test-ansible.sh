#!/usr/bin/env bash
# Run tangsible's test suites against another ansible-core release.
#
#   scripts/test-ansible.sh 2.20          # newest 2.20.x
#   scripts/test-ansible.sh 2.20.3        # exactly that release
#   make test-ansible VERSION=2.20
#
# Installs ansible-core into a virtualenv of its own (cached, reused by the
# next run) and puts it first on PATH, which is all it takes: every test and
# every verb runs whichever ansible-playbook is first on PATH, and the Python
# helpers run under that ansible-playbook's own interpreter. The suites are
# those of design-docs/AnsibleInternals.md's checklist: the callback plugin's
# Python tests, the Go unit tests, the e2e tests (tmux, real ansible).
#
# Environment:
#   SUITES="callback unit e2e"   which suites to run (default: all three)
#   ISOLATE=0                    keep your own HOME, ~/.ansible and ANSIBLE_*
#                                variables (default 1: an empty HOME, no
#                                ANSIBLE_* variables, so the result says
#                                something about the version, not your setup)
#   REFRESH=1                    recreate the virtualenv
#   TANGSIBLE_ANSIBLE_VENVS=dir  where the virtualenvs live
#                                (default ~/.cache/tangsible/ansible-venvs)
#   PIP_ARGS="..."               extra arguments for pip install (a mirror, ...)
#
# The venv also gets the ansible.posix collection (the callback plugin's parity
# tests use ansible.posix.jsonl as the reference), installed where ansible finds
# it without any ANSIBLE_* variable.
#
# Not covered: the execution-environment e2e tests run inside an image that
# brings its own ansible - for those set TANGSIBLE_EE_IMAGE to an image built
# on the release you want to check.
set -u

version=${1:-}
if [ -z "$version" ] || [ "$version" = -h ] || [ "$version" = --help ]; then
	sed -n '2,/^set -u/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'
	[ -n "$version" ] && exit 0
	exit 2
fi
if ! [[ $version =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?([a-z0-9.]*)?$ ]]; then
	echo "test-ansible: '$version' is not a version like 2.20 or 2.20.3" >&2
	exit 2
fi
if [[ $version =~ ^[0-9]+\.[0-9]+$ ]]; then
	spec="ansible-core==$version.*"
else
	spec="ansible-core==$version"
fi

repo=$(cd "$(dirname "$0")/.." && pwd)
venvs=${TANGSIBLE_ANSIBLE_VENVS:-${XDG_CACHE_HOME:-$HOME/.cache}/tangsible/ansible-venvs}
venv=$venvs/$version
suites=${SUITES:-"callback unit e2e"}

command -v go >/dev/null || { echo "test-ansible: go not found" >&2; exit 2; }
command -v python3 >/dev/null || { echo "test-ansible: python3 not found" >&2; exit 2; }

# What the isolated run must still know after HOME changes.
export GOCACHE=$(go env GOCACHE) GOMODCACHE=$(go env GOMODCACHE) GOFLAGS=${GOFLAGS:-}

if [ "${REFRESH:-0}" = 1 ]; then
	rm -rf "$venv"
fi
if [ ! -x "$venv/bin/ansible-playbook" ]; then
	echo "==> creating $venv ($spec)"
	mkdir -p "$venvs"
	python3 -m venv "$venv" || exit 1
	# shellcheck disable=SC2086
	"$venv/bin/pip" install --quiet ${PIP_ARGS:-} "$spec" || {
		echo "test-ansible: could not install $spec" >&2
		rm -rf "$venv"
		exit 1
	}
fi

# ansible-core ships no collections; the callback plugin's parity tests run
# ansible.posix.jsonl as the reference. It goes into the venv's site-packages,
# where ansible finds it without any ANSIBLE_COLLECTIONS_PATH (the parity tests
# delete every ANSIBLE_* variable from their subprocesses).
site=$("$venv/bin/python3" -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')
if [ ! -d "$site/ansible_collections/ansible/posix" ]; then
	echo "==> installing the ansible.posix collection into $site"
	"$venv/bin/ansible-galaxy" collection install ansible.posix -p "$site" >/dev/null || {
		echo "test-ansible: could not install ansible.posix (the callback parity tests need it)" >&2
		exit 1
	}
fi

export PATH=$venv/bin:$PATH
if [ "${ISOLATE:-1}" = 1 ]; then
	home=$(mktemp -d)
	trap 'rm -rf "$home"' EXIT
	for name in $(env | sed -n 's/^\(ANSIBLE_[A-Za-z0-9_]*\)=.*/\1/p'); do
		unset "$name"
	done
	export HOME=$home XDG_CACHE_HOME=$home/.cache XDG_CONFIG_HOME=$home/.config XDG_DATA_HOME=$home/.local/share
fi

echo "==> $(ansible-playbook --version | head -1)"
echo "==> $(command -v ansible-playbook)"
cd "$repo" || exit 1

declare -a names results
run() { # name command...
	local name=$1
	shift
	echo
	echo "==> $name: $*"
	"$@"
	local code=$?
	names+=("$name")
	results+=("$code")
}

for suite in $suites; do
	case $suite in
	callback) run callback python3 -m unittest discover -s callback-tests ;;
	unit) run unit go test ./... ;;
	e2e)
		if command -v tmux >/dev/null; then
			run e2e go test -tags e2e -count=1 ./e2e
		else
			echo "==> e2e: skipped, tmux not found"
		fi
		;;
	*) echo "test-ansible: unknown suite '$suite' (callback, unit, e2e)" >&2; exit 2 ;;
	esac
done

echo
echo "==> summary for $(ansible-playbook --version | head -1)"
status=0
for i in "${!names[@]}"; do
	if [ "${results[$i]}" = 0 ]; then
		printf '    %-9s ok\n' "${names[$i]}"
	else
		printf '    %-9s FAILED (exit %s)\n' "${names[$i]}" "${results[$i]}"
		status=1
	fi
done
exit $status
