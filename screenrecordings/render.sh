#!/bin/bash
# Render every tape (or the ones named: ./render.sh stats hosts) from a freshly
# built tangsible, then write the poster frames.
#
# Besides vhs, ttyd and ffmpeg this needs podman, ansible-builder and network
# access for the ee tape, which pushes code.aw.net/claude/tangsible-demo-ee
# (needs a `podman login code.aw.net`). Container builds run with the host
# network (CONTAINERS_CONF), only for this script.
#
# demo-project/.fact-cache holds canned facts for the hosts recording; if a
# cache entry lacks something (mount_points) `tangsible host` gathers live and
# overwrites it with the machine's own facts, so the canned files are put back
# before every tape and at the end.
set -u
cd "$(dirname "$0")/.."
work=$(mktemp -d)
facts=screenrecordings/demo-project/.fact-cache
mkdir "$work/facts" && cp "$facts"/* "$work/facts/"
restore_facts() { cp "$work/facts"/* "$facts/"; }
trap 'restore_facts; rm -rf "$work"' EXIT

go build -o "$work/tangsible" . || exit 1
export PATH="$work:$PATH"

printf '[containers]\nnetns="host"\n' >"$work/containers.conf"
export CONTAINERS_CONF="$work/containers.conf"

if [ $# -gt 0 ]; then tapes=("$@"); else
	tapes=(demo stats hosts revisit template vault debug ee)
fi

failed=()
for t in "${tapes[@]}"; do
	restore_facts
	if [ "$t" = ee ]; then
		podman rmi code.aw.net/claude/tangsible-demo-ee:1.0 code.aw.net/claude/tangsible-demo-ee:latest >/dev/null 2>&1
	fi
	echo "==> $t"
	vhs "screenrecordings/$t.tape" || failed+=("$t")
done

screenrecordings/posters.sh
if [ ${#failed[@]} -gt 0 ]; then echo "FAILED: ${failed[*]}"; exit 1; fi
