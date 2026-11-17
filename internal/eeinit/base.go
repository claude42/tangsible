package eeinit

import (
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
	PythonVersion  string
	Pip            bool
	AnsibleCore    string
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
  echo "python_version=$($py -c 'import platform; print(platform.python_version())' 2>/dev/null)"
  $py -m pip --version >/dev/null 2>&1 && echo "pip=yes"
  echo "ansible_core=$($py -c 'import ansible.release as r; print(r.__version__)' 2>/dev/null)"
fi
`

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
