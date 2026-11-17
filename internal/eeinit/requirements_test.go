package eeinit

import "testing"

func TestParseRequirements(t *testing.T) {
	src := `collections:
  - community.general
  - name: community.docker
    version: ">=3.0.0"
  - name: https://example.com/x.git
    type: git
  - name: ns.pinned
    version: 1.2.3
    source: https://hub.example.com
roles:
  - src: geerlingguy.nginx
`
	r, err := ParseRequirements([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Collections) != 4 || len(r.Roles) != 1 {
		t.Fatalf("got %d collections, %d roles", len(r.Collections), len(r.Roles))
	}
	type exp struct {
		name, version string
		plain         bool
	}
	for i, w := range []exp{
		{"community.general", "", true},
		{"community.docker", ">=3.0.0", true},
		{"https://example.com/x.git", "", false},
		{"ns.pinned", "1.2.3", false},
	} {
		c := r.Collections[i]
		if c.Name != w.name || c.Version != w.version || c.Plain != w.plain {
			t.Errorf("entry %d = {%q %q %v}, want %+v", i, c.Name, c.Version, c.Plain, w)
		}
	}
}

func TestParseRequirementsLegacyRolesList(t *testing.T) {
	r, err := ParseRequirements([]byte("- src: a.b\n- src: c.d\n"))
	if err != nil || len(r.Roles) != 2 || len(r.Collections) != 0 {
		t.Errorf("r = %+v, err = %v", r, err)
	}
}
