#!/bin/bash
# Render every tape (or the ones named: ./render.sh stats hosts) from a freshly
# built tangsible, then write the poster frames.
#
# Besides vhs, ttyd and ffmpeg this needs podman, ansible-builder and network
# access for the ee-* tapes. It starts a throw-away registry on localhost:5000
# (docker.io/library/registry:2) for ee-build and removes it again. Container
# builds run with the host network (CONTAINERS_CONF) and localhost:5000 is
# marked insecure (CONTAINERS_REGISTRIES_CONF), both only for this script.
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
trap 'podman rm -f tangsible-demo-registry >/dev/null 2>&1; restore_facts; rm -rf "$work"' EXIT

go build -o "$work/tangsible" . || exit 1
export PATH="$work:$PATH"

printf '[containers]\nnetns="host"\n' >"$work/containers.conf"
printf '[[registry]]\nlocation = "localhost:5000"\ninsecure = true\n' >"$work/registries.conf"
export CONTAINERS_CONF="$work/containers.conf" CONTAINERS_REGISTRIES_CONF="$work/registries.conf"

if [ $# -gt 0 ]; then tapes=("$@"); else
	tapes=(demo stats hosts revisit template vault debug debug-help ee-draft ee-build ee-run)
fi

failed=()
for t in "${tapes[@]}"; do
	restore_facts
	if [ "$t" = ee-build ]; then
		podman rm -f tangsible-demo-registry >/dev/null 2>&1
		podman rmi localhost:5000/demo/service-ee:1.0 localhost:5000/demo/service-ee:latest >/dev/null 2>&1
		podman run -d --rm --name tangsible-demo-registry -p 127.0.0.1:5000:5000 docker.io/library/registry:2 >/dev/null || { failed+=("$t"); continue; }
		sleep 3
	fi
	echo "==> $t"
	vhs "screenrecordings/$t.tape" || failed+=("$t")
done

screenrecordings/posters.sh
if [ ${#failed[@]} -gt 0 ]; then echo "FAILED: ${failed[*]}"; exit 1; fi
