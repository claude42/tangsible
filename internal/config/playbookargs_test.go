package config

import (
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestSplitPlaybookArgsAmongOptions(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		playbook string
		rest     []string
		explicit bool
	}{
		{"none", nil, "", nil, false},
		{"playbook first", []string{"site.yml", "-i", "hosts"}, "site.yml", []string{"-i", "hosts"}, true},
		{"ee before playbook", []string{"--ee", "img", "site.yml"}, "site.yml", []string{"--ee", "img"}, true},
		{"ee= before playbook", []string{"--ee=img", "site.yml"}, "site.yml", []string{"--ee=img"}, true},
		{"switch before playbook", []string{"--check", "--diff", "site.yml"}, "site.yml", []string{"--check", "--diff"}, true},
		{"-i before playbook", []string{"-i", "hosts", "site.yml", "-vv"}, "site.yml", []string{"-i", "hosts", "-vv"}, true},
		{"value that looks like a playbook", []string{"-e", "site.yml"}, "", []string{"-e", "site.yml"}, false},
		{"cluster of switches", []string{"-vvv", "-Kk", "site.yml"}, "site.yml", []string{"-vvv", "-Kk"}, true},
		{"letters after a value flag are its value", []string{"-ei", "site.yml"}, "site.yml", []string{"-ei"}, true},
		{"cluster ending in value flag, playbook after value", []string{"-vi", "hosts", "site.yml"}, "site.yml", []string{"-vi", "hosts"}, true},
		{"attached value", []string{"-ihosts", "site.yml"}, "site.yml", []string{"-ihosts"}, true},
		{"double dash", []string{"-e", "@vars.yml", "--", "site.yml", "-v"}, "site.yml", []string{"-e", "@vars.yml", "-v"}, true},
		{"trailing double dash", []string{"-v", "--"}, "", []string{"-v"}, false},
		{"unknown flag is a switch", []string{"--newflag", "site.yml"}, "site.yml", []string{"--newflag"}, true},
		{"flags only", []string{"-i", "hosts", "-vv"}, "", []string{"-i", "hosts", "-vv"}, false},
		{"no-ee and start-at-play", []string{"--no-ee", "--start-at-play", "Web", "site.yml"}, "site.yml", []string{"--no-ee", "--start-at-play", "Web"}, true},
		{"extensionless playbook", []string{"-i", "hosts", "site"}, "site", []string{"-i", "hosts"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pb, rest, ex := SplitPlaybookArgsAmongOptions(c.args)
			if pb != c.playbook || !slices.Equal(rest, c.rest) || ex != c.explicit {
				t.Errorf("(%v) = (%q, %v, %v), want (%q, %v, %v)", c.args, pb, rest, ex, c.playbook, c.rest, c.explicit)
			}
		})
	}
}

// TestAnsiblePlaybookFlagTable fails when the table drifts from the installed
// ansible-playbook's --help. Skipped without ansible.
func TestAnsiblePlaybookFlagTable(t *testing.T) {
	out, err := exec.Command("ansible-playbook", "--help").Output()
	if err != nil {
		t.Skip("ansible-playbook not available")
	}
	re := regexp.MustCompile(`^  (-\S.*?)(?:\s{2,}.*)?$`)
	for _, line := range strings.Split(string(out), "\n") {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		aliases := strings.Split(m[1], ", ")
		takes := false
		for _, a := range aliases {
			if strings.Contains(a, " ") {
				takes = true
			}
		}
		for _, a := range aliases {
			name := strings.Fields(a)[0]
			var got bool
			if strings.HasPrefix(name, "--") {
				got = ansiblePlaybookValueFlags[name]
			} else {
				got = strings.Contains(ansiblePlaybookValueShorts, name[1:])
			}
			if got != takes {
				t.Errorf("%s: table says takes-value=%v, ansible-playbook --help says %v", name, got, takes)
			}
		}
	}
}
