package eeinit

import (
	"reflect"
	"testing"
)

func TestParseDocs(t *testing.T) {
	raw := `{"dig": {"doc": {"collection": "community.general", "requirements": ["dnspython (python library, http://www.dnspython.org/)"]}},
	         "copy": {"doc": {"collection": "ansible.builtin"}},
	         "one": {"doc": {"collection": "a.b", "requirements": ["C(hvac) (L(Python library,https://hvac.readthedocs.io))", "For detailed requirements, see R(the page,ansible_collections.x.y.docsite.z)."]}},
	         "odd": {"doc": {}}}`
	got := parseDocs([]byte(raw))
	want := map[string]PluginDoc{
		"dig":  {Collection: "community.general", Requirements: []string{"dnspython (python library, http://www.dnspython.org/)"}},
		"copy": {Collection: "ansible.builtin"},
		"one":  {Collection: "a.b", Requirements: []string{"hvac (Python library)"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if got := parseDocs([]byte("not json")); len(got) != 0 {
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
