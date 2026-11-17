package eeinit

import "testing"

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
	out := "os=Fedora Linux 44\npkg=/usr/bin/dnf\npkg=/usr/bin/microdnf\npython=/usr/bin/python3\npython_version=3.14.8\npip=yes\nansible_core=2.21.5\n"
	want := BaseFacts{OS: "Fedora Linux 44", PackageManager: "/usr/bin/microdnf", Python: "/usr/bin/python3", PythonVersion: "3.14.8", Pip: true, AnsibleCore: "2.21.5"}
	if got := ParseProbe(out); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got := ParseProbe("os=Debian\npkg=/usr/bin/apt-get\nansible_core=\n"); got.PackageManager != "/usr/bin/apt-get" || got.Python != "" || got.Pip {
		t.Errorf("debian without python: %+v", got)
	}
}
