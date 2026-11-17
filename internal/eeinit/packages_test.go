package eeinit

import (
	"reflect"
	"testing"
)

func TestParsePackages(t *testing.T) {
	out := "pkg=jq=jq\npkg=ip=iproute\npkg=ip=iproute-tc\npkg=ip=iproute\npkg=weird=$(x)\nnoise\npkg=/usr/bin/foo=foo-bin\n"
	got, failed := ParsePackages(out)
	want := map[string][]string{
		"jq":           {"jq"},
		"ip":           {"iproute", "iproute-tc"},
		"/usr/bin/foo": {"foo-bin"},
	}
	if !reflect.DeepEqual(got, want) || failed != "" {
		t.Errorf("got %v, %q; want %v", got, failed, want)
	}
	if _, failed := ParsePackages("failed=couldn't load the repository metadata\n"); failed == "" {
		t.Errorf("a failure must be reported")
	}
}

func TestFamilyAndMissing(t *testing.T) {
	for pm, want := range map[string]string{"/usr/bin/microdnf": "rpm", "/usr/bin/dnf": "rpm", "/usr/bin/apt-get": "dpkg", "/usr/bin/zypper": "", "": ""} {
		if got := (BaseFacts{PackageManager: pm}).Family(); got != want {
			t.Errorf("Family(%q) = %q, want %q", pm, got, want)
		}
	}
	got := ParseProbe("python=/usr/bin/python3\nmissing=jq\nmissing=/usr/bin/ip\n")
	if !reflect.DeepEqual(got.Missing, []string{"jq", "/usr/bin/ip"}) {
		t.Errorf("Missing = %v", got.Missing)
	}
}
