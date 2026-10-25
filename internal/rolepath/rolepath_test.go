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
	if err := os.WriteFile(filepath.Join(ext, "afile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dirs := []string{ext}
	cases := map[string]string{
		"local":  filepath.Join(base, "roles", "local"),
		"remote": filepath.Join(ext, "remote"),
		"both":   filepath.Join(base, "roles", "both"), // ./roles wins
		"afile":  "",
		"nope":   "",
		"../x":   "",
		"":       "",
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
