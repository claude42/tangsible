package eeinit

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPipeTerms(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       []string
	}{
		{"one literal", `, 'hostname') }}`, []string{"hostname"}},
		{"double quotes, spaces", `,  "date +%s" )`, []string{"date +%s"}},
		{"several terms", `, 'a', 'b -x') }}`, []string{"a", "b -x"}},
		{"a variable is unknown", `, cmd) }}`, []string{"{{ unknown }}"}},
		{"a list is unknown", `, ['a']) }}`, []string{"{{ unknown }}"}},
		{"no arguments", `) }}`, nil},
	} {
		if got := pipeTerms(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: pipeTerms(%q) = %v, want %v", c.name, c.text, got, c.want)
		}
	}
}

// A pipe lookup in group_vars runs `hostname` on the controller, so the
// binary has to be in the image, the same as for a shell task there.
func TestScanProjectPipeLookupNeedsItsBinary(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("site.yml", "- hosts: all\n  tasks:\n    - debug: msg=\"{{ lookup('ansible.builtin.pipe', 'date | sort') }}\"\n")
	write("group_vars/all/main.yml", "controller_hostname: \"{{ lookup('pipe', 'hostname') }}\"\n")
	write("templates/t.j2", "{{ query('pipe', 'whoami') }}\n{{ lookup('pipe', cmd) }}\n")
	proj, err := ScanProject(filepath.Join(dir, "site.yml"), dir)
	if err != nil {
		t.Fatal(err)
	}
	bins, templated := ScanBinaries(proj.Commands, dir)
	var names []string
	for _, b := range bins {
		names = append(names, b.Name)
	}
	if want := []string{"date", "hostname", "sort", "whoami"}; !reflect.DeepEqual(names, want) {
		t.Errorf("binaries = %v, want %v", names, want)
	}
	if templated != 1 {
		t.Errorf("templated = %d, want 1 (lookup('pipe', cmd))", templated)
	}
}
