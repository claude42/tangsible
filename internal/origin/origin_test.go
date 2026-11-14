package origin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLabel(t *testing.T) {
	base := "/proj"
	cases := []struct {
		name string
		o    Origin
		want string
	}{
		{"file and line, project-relative", Origin{Path: "/proj/group_vars/all.yml", Line: 3}, "group_vars/all.yml:3"},
		{"no line", Origin{Path: "/proj/inv.ini"}, "inv.ini"},
		{"outside the project stays absolute", Origin{Path: "/etc/ansible/hosts", Line: 1}, "/etc/ansible/hosts:1"},
		{"-e", Origin{Desc: "<CLI option '-e'>"}, "-e"},
		{"other description", Origin{Desc: "<something>"}, "something"},
		{"fact", Origin{Kind: "fact"}, "fact"},
		{"built-in", Origin{Kind: "builtin"}, "built-in"},
		{"registered", Origin{Kind: "registered"}, "registered"},
		{"nothing known", Origin{}, ""},
	}
	for _, c := range cases {
		if got := Label(c.o, "", "", base); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLabelShadowCopy(t *testing.T) {
	root := t.TempDir()
	copyDir := filepath.Join(root, "proj", ".tangsible", "debug", "r1")
	if err := os.MkdirAll(copyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two spliced tasks; the set_fact key sits on copy line 7, original line 6.
	content := "- hosts: all\n  tasks:\n    - name: a\n      debug:\n      debugger: always\n    - name: b\n      set_fact: {x: 1}\n"
	if err := os.WriteFile(filepath.Join(copyDir, "p.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	playbookDir := filepath.Join(root, "proj")
	got := Label(Origin{Path: filepath.Join(copyDir, "p.yml"), Line: 7}, copyDir, playbookDir, playbookDir)
	if got != "p.yml:6" {
		t.Errorf("got %q, want p.yml:6", got)
	}
	if got := UnspliceLine(filepath.Join(copyDir, "p.yml"), 4); got != 4 {
		t.Errorf("a line above every splice must not shift, got %d", got)
	}
	if got := UnspliceLine(filepath.Join(copyDir, "missing.yml"), 9); got != 9 {
		t.Errorf("unreadable file keeps its line, got %d", got)
	}
}
