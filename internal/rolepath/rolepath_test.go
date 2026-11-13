package rolepath

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseDirs(t *testing.T) {
	out := []byte(`[{"name":"OTHER","value":1},{"name":"DEFAULT_ROLES_PATH","origin":"default","value":["/a/roles"," /b ",""]}]`)
	if got, want := parseDirs(out), []string{"/a/roles", "/b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("parseDirs = %v, want %v", got, want)
	}
	for _, bad := range []string{`not json`, `[]`, `[{"name":"DEFAULT_ROLES_PATH","value":"x"}]`} {
		if got := parseDirs([]byte(bad)); got != nil {
			t.Errorf("parseDirs(%q) = %v, want nil", bad, got)
		}
	}
}

func TestFind(t *testing.T) {
	base, ext := t.TempDir(), t.TempDir()
	mk := func(p string) {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk(filepath.Join(base, "roles", "local"))
	mk(filepath.Join(ext, "both"))
	mk(filepath.Join(base, "roles", "both"))
	mk(filepath.Join(ext, "remote"))
	mk(filepath.Join(base, "adjacent")) // base itself is the last resort
	mk(filepath.Join(base, "remote"))   // ...behind roles_path
	if err := os.WriteFile(filepath.Join(ext, "afile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dirs := []string{ext}
	cases := map[string]string{
		"local":    filepath.Join(base, "roles", "local"),
		"remote":   filepath.Join(ext, "remote"),
		"both":     filepath.Join(base, "roles", "both"), // ./roles wins
		"adjacent": filepath.Join(base, "adjacent"),
		"afile":    "",
		"nope":     "",
		"../x":     "",
		"":         "",
	}
	for name, want := range cases {
		if got := Find(name, base, dirs); got != want {
			t.Errorf("Find(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFromPath(t *testing.T) {
	dirs := []string{"/opt/ansible-roles", "/etc/ansible/roles"}
	cases := []struct{ path, dir, name string }{
		{"/opt/ansible-roles/nginx/tasks/main.yml", "/opt/ansible-roles/nginx", "nginx"},
		{"/etc/ansible/roles/db/templates/a.j2", "/etc/ansible/roles/db", "db"},
		{"/opt/ansible-roles/stray.yml", "", ""},
		{"/opt/ansible-roles-other/x/tasks/main.yml", "", ""},
		{"/home/u/project/roles/x/tasks/main.yml", "", ""},
	}
	for _, c := range cases {
		dir, name := FromPath(c.path, dirs)
		if dir != c.dir || name != c.name {
			t.Errorf("FromPath(%q) = (%q, %q), want (%q, %q)", c.path, dir, name, c.dir, c.name)
		}
	}
}

func TestSplitFQCN(t *testing.T) {
	cases := map[string][3]string{
		"community.general.foo": {"community", "general", "foo"},
		"a.b.c":                 {"a", "b", "c"},
	}
	for name, want := range cases {
		ns, coll, role, ok := SplitFQCN(name)
		if !ok || [3]string{ns, coll, role} != want {
			t.Errorf("SplitFQCN(%q) = %q %q %q %v", name, ns, coll, role, ok)
		}
	}
	for _, name := range []string{"", "foo", "geerlingguy.docker", "a.b.c.d", "a..c", ".b.c", "a.b/c.d", `a.b\c`} {
		if _, _, _, ok := SplitFQCN(name); ok {
			t.Errorf("SplitFQCN(%q) ok, want not", name)
		}
	}
}

func TestLookup(t *testing.T) {
	base, rolesDir, collDir := t.TempDir(), t.TempDir(), t.TempDir()
	defer SetDirs([]string{rolesDir})()
	defer SetCollectionDirs([]string{collDir})()
	mk := func(p ...string) string {
		d := filepath.Join(p...)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	coll := func(root, ns, c, role string) string {
		return mk(root, "ansible_collections", ns, c, "roles", role)
	}
	inSystem := coll(collDir, "community", "general", "foo")
	inProject := coll(filepath.Join(base, "collections"), "acme", "tools", "bar")
	shadowed := coll(collDir, "acme", "tools", "bar") // ./collections wins
	_ = shadowed
	short := coll(collDir, "acme", "tools", "baz")
	legacy := mk(rolesDir, "galaxy.docker")
	plain := mk(rolesDir, "plain")
	both := mk(rolesDir, "baz") // plain role with the same name as a collection one
	mk(filepath.Join(base, "roles", "x"))

	cases := []struct {
		name        string
		collections []string
		dir, coll   string
	}{
		{"community.general.foo", nil, inSystem, "community.general"},
		{"acme.tools.bar", nil, inProject, "acme.tools"},
		{"baz", []string{"other.coll", "acme.tools"}, short, "acme.tools"}, // collections: list, in order
		{"baz", nil, both, ""},                       // no list: plain role
		{"galaxy.docker", nil, legacy, ""},           // two parts: not an FQCN
		{"plain", []string{"acme.tools"}, plain, ""}, // falls through to roles_path
		{"x", nil, filepath.Join(base, "roles", "x"), ""},
		{"community.general.missing", nil, "", ""},
		{"nope", []string{"acme.tools"}, "", ""},
	}
	for _, c := range cases {
		dir, coll := Lookup(c.name, c.collections, base)
		if dir != c.dir || coll != c.coll {
			t.Errorf("Lookup(%q, %v) = (%q, %q), want (%q, %q)", c.name, c.collections, dir, coll, c.dir, c.coll)
		}
	}
}
