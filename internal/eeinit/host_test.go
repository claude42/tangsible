package eeinit

import (
	"reflect"
	"testing"
)

func TestParseDocCollections(t *testing.T) {
	raw := `{"docker_container": {"doc": {"collection": "community.docker"}}, "copy": {"doc": {"collection": "ansible.builtin"}}, "odd": {"doc": {}}}`
	want := map[string]string{"docker_container": "community.docker", "copy": "ansible.builtin"}
	if got := parseDocCollections([]byte(raw)); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := parseDocCollections([]byte("not json")); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestParseInstalledFirstPathWins(t *testing.T) {
	raw := `{"/b/ansible_collections": {"x.y": {"version": "2.0.0"}, "p.q": {"version": "*"}},
	         "/a/ansible_collections": {"x.y": {"version": "1.0.0"}}}`
	got, err := parseInstalled([]byte(raw), []string{"/a", "/b"})
	if err != nil {
		t.Fatal(err)
	}
	if got["x.y"] != "1.0.0" {
		t.Errorf("x.y = %q, want the version on the first path", got["x.y"])
	}
	if _, ok := got["p.q"]; ok {
		t.Errorf("a collection without a real version should stay unpinned")
	}
}
