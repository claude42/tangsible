package eeinit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"code.aw.net/claude/tangsible/internal/execerr"
)

// resolveScripts look up which package provides each binary (the script's
// arguments), once per family. They print pkg=<binary>=<package> lines, or
// failed=<why> when the package manager couldn't be used at all. They run in
// a throw-away container with network (see ResolvePackages).
var resolveScripts = map[string]string{
	// dnf only: microdnf can't answer "which package provides this file".
	// A second argument to repoquery is a package filter, so one pattern per
	// call; /sbin is tried when /bin finds nothing.
	"rpm": `
command -v dnf >/dev/null 2>&1 || { echo "failed=no dnf in the image"; exit 0; }
dnf -q makecache >/dev/null 2>&1 || { echo "failed=couldn't load the repository metadata"; exit 0; }
for n in "$@"; do
  case "$n" in
    /*) pats="$n" ;;
    *) pats="*/bin/$n */sbin/$n" ;;
  esac
  out=
  for pat in $pats; do
    out=$(dnf -q repoquery --file "$pat" --queryformat '%{name}\n' 2>/dev/null | sort -u)
    [ -n "$out" ] && break
  done
  for p in $out; do echo "pkg=$n=$p"; done
done
`,
	"dpkg": `
export DEBIAN_FRONTEND=noninteractive
{ apt-get update -qq && apt-get install -y -qq apt-file && apt-file update; } >/dev/null 2>&1 \
  || { echo "failed=couldn't set up apt-file (no network?)"; exit 0; }
for n in "$@"; do
  esc=$(printf '%s' "$n" | sed 's/[.+]/\\&/g')
  case "$n" in
    /*) re="^$esc\$" ;;
    *) re="^/(usr/)?s?bin/$esc\$" ;;
  esac
  for p in $(apt-file -x search "$re" 2>/dev/null | cut -d: -f1 | sort -u); do
    echo "pkg=$n=$p"
  done
done
`,
}

// ParsePackages reads a resolve script's output: binary -> the distinct
// packages that provide it (a binary the script found nothing for is
// absent). failed is the script's own reason when it couldn't look anything
// up.
func ParsePackages(out string) (pkgs map[string][]string, failed string) {
	pkgs = map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "failed":
			failed = v
		case "pkg":
			bin, pkg, ok := strings.Cut(v, "=")
			if !ok || !packageName.MatchString(pkg) {
				continue
			}
			if !contains(pkgs[bin], pkg) {
				pkgs[bin] = append(pkgs[bin], pkg)
			}
		}
	}
	for _, l := range pkgs {
		sort.Strings(l)
	}
	return pkgs, failed
}

// resolveTimeout bounds the whole lookup: it downloads repository metadata.
const resolveTimeout = 10 * time.Minute

// ResolvePackages asks the image's package manager which packages provide
// binaries, in a throw-away container of image (never changed) that has
// network - network is the [execution_environment] network value, "" for the
// engine's default - and runs as root so the package manager may refresh its
// metadata. family is BaseFacts.Family.
func ResolvePackages(engine, image, network, family string, binaries []string) (map[string][]string, error) {
	script, ok := resolveScripts[family]
	if !ok {
		return nil, errors.New("the base image's package manager is not one tangsible can query (dnf and apt are supported)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()
	args := []string{"run", "--rm", "--user", "root"}
	if network != "" {
		args = append(args, "--network="+network)
	}
	args = append(args, "--entrypoint", "/bin/sh", image, "-c", script, "resolve")
	cmd := exec.CommandContext(ctx, engine, append(args, binaries...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := execerr.NotFoundMessage(engine, err); msg != "" {
			return nil, errors.New(msg)
		}
		return nil, fmt.Errorf("%s run failed: %v: %s", engine, err, strings.TrimSpace(stderr.String()))
	}
	pkgs, failed := ParsePackages(stdout.String())
	if failed != "" {
		return nil, errors.New(failed)
	}
	return pkgs, nil
}
