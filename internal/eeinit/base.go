package eeinit

import (
	"regexp"
	"strings"
)

// DefaultBaseImage is the base used when neither --base nor
// [execution_environment] base_image names one. Its tag is pinned on
// purpose (a moving tag would change the inspected facts and the draft's
// pins between runs); bump it now and then, and check the new one still
// has Python and pip, which ansible-builder needs. community-ee-base tags
// are <ansible-core version>-<n>.
const DefaultBaseImage = "ghcr.io/ansible-community/community-ee-base:2.21.6-1"

// ResolveBase picks the base image: the --base flag, then the configured
// base_image, then DefaultBaseImage. [execution_environment] image is never
// consulted - it is what tangsible runs, often the output of the very build
// this draft is for (design-docs/EEInit.md).
func ResolveBase(flag, configured string) (image string, isDefault bool) {
	for _, c := range []string{flag, configured} {
		if c = strings.TrimSpace(c); c != "" {
			return c, false
		}
	}
	return DefaultBaseImage, true
}

// BaseFacts is what inspecting the base image found; an empty field means
// "not found".
type BaseFacts struct {
	OS             string // pretty name from os-release
	PackageManager string // path
	Python         string // path
	// PythonReal is Python with symlinks resolved; PythonPackage the
	// package that owns it, from the image's own package database (rpm or
	// dpkg). Either may be empty.
	PythonReal    string
	PythonPackage string
	PythonVersion string
	Pip           bool
	AnsibleCore   string
}

// ProbeScript runs inside the base image (sh) and prints key=value lines
// for ParseProbe.
const ProbeScript = `
. /etc/os-release 2>/dev/null
echo "os=$PRETTY_NAME"
for c in microdnf dnf apt-get yum zypper; do
  p=$(command -v $c 2>/dev/null) && echo "pkg=$p"
done
py=$(command -v python3 2>/dev/null)
if [ -n "$py" ]; then
  echo "python=$py"
  real=$(readlink -f "$py" 2>/dev/null)
  if [ -n "$real" ]; then
    echo "python_real=$real"
    pkg=
    if command -v rpm >/dev/null 2>&1; then
      pkg=$(rpm -qf --qf '%{NAME}\n' "$real" 2>/dev/null | head -1) || pkg=
    elif command -v dpkg >/dev/null 2>&1; then
      pkg=$(dpkg -S "$real" 2>/dev/null | head -1 | sed -e 's/: .*//' -e 's/,.*//' -e 's/:.*//') || pkg=
    fi
    [ -n "$pkg" ] && echo "python_pkg=$pkg"
  fi
  echo "python_version=$($py -c 'import platform; print(platform.python_version())' 2>/dev/null)"
  $py -m pip --version >/dev/null 2>&1 && echo "pip=yes"
  echo "ansible_core=$($py -c 'import ansible.release as r; print(r.__version__)' 2>/dev/null)"
fi
`

// packageName is what a package name can look like; the probe's output ends
// up in the draft, so anything else is ignored.
var packageName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]*$`)

// packageManagerOrder is the preference when an image has several: the
// community images are built with microdnf.
var packageManagerOrder = []string{"microdnf", "dnf", "apt-get", "yum", "zypper"}

// ParseProbe reads ProbeScript's output.
func ParseProbe(out string) BaseFacts {
	var f BaseFacts
	var pkgs []string
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "os":
			f.OS = v
		case "pkg":
			pkgs = append(pkgs, v)
		case "python":
			f.Python = v
		case "python_real":
			f.PythonReal = v
		case "python_pkg":
			if packageName.MatchString(v) {
				f.PythonPackage = v
			}
		case "python_version":
			f.PythonVersion = v
		case "pip":
			f.Pip = v == "yes"
		case "ansible_core":
			f.AnsibleCore = v
		}
	}
	for _, name := range packageManagerOrder {
		for _, p := range pkgs {
			if p == name || strings.HasSuffix(p, "/"+name) {
				f.PackageManager = p
				return f
			}
		}
	}
	return f
}
