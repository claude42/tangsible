package eeinit

import (
	"reflect"
	"testing"
)

func TestResolveBase(t *testing.T) {
	for _, c := range []struct {
		flag, cfg, want string
		def             bool
	}{
		{"f", "c", "f", false},
		{"", "c", "c", false},
		{" ", "  ", DefaultBaseImage, true},
		{"", "", DefaultBaseImage, true},
	} {
		got, def := ResolveBase(c.flag, c.cfg)
		if got != c.want || def != c.def {
			t.Errorf("ResolveBase(%q, %q) = %q, %v; want %q, %v", c.flag, c.cfg, got, def, c.want, c.def)
		}
	}
}

func TestParseProbe(t *testing.T) {
	out := "os=Fedora Linux 44\npkg=/usr/bin/dnf\npkg=/usr/bin/microdnf\npython=/usr/bin/python3\npython_real=/usr/bin/python3.14\npython_pkg=python3\npython_version=3.14.8\npip=yes\nansible_core=2.21.5\n"
	want := BaseFacts{OS: "Fedora Linux 44", PackageManager: "/usr/bin/microdnf", Python: "/usr/bin/python3", PythonReal: "/usr/bin/python3.14", PythonPackage: "python3", PythonVersion: "3.14.8", Pip: true, AnsibleCore: "2.21.5"}
	if got := ParseProbe(out); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got := ParseProbe("os=Debian\npkg=/usr/bin/apt-get\nansible_core=\n"); got.PackageManager != "/usr/bin/apt-get" || got.Python != "" || got.Pip {
		t.Errorf("debian without python: %+v", got)
	}
}

func TestParseProbeIgnoresOddPackageNames(t *testing.T) {
	// the package name ends up in a comment of the draft: only a plausible
	// name is taken
	for _, pkg := range []string{"a b", "x\ty", "$(rm)", "-flag", ""} {
		if got := ParseProbe("python=/usr/bin/python3\npython_pkg=" + pkg + "\n"); got.PythonPackage != "" {
			t.Errorf("python_pkg=%q was accepted as %q", pkg, got.PythonPackage)
		}
	}
	if got := ParseProbe("python_pkg=python3.12-minimal\n"); got.PythonPackage != "python3.12-minimal" {
		t.Errorf("got %q", got.PythonPackage)
	}
}
