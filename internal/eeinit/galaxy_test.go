package eeinit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdmits(t *testing.T) {
	for _, c := range []struct {
		spec, core string
		want       bool
	}{
		{">=2.19.0", "2.21.6", true},
		{">=2.19.0", "2.18.2", false},
		{">=2.19.0", "2.19.0", true},
		{">=2.9", "2.9.10", true},
		{">=2.9.10", "2.9.9", false},
		{">=2.9,<2.11", "2.10.5", true},
		{">=2.9,<2.11", "2.11.0", false},
		{">=2.9.10,<2.11", "2.21.6", false},
		{">=2.17.0", "2.21.0rc1", true},
		{">=2.21.1", "2.21.0rc1", false},
		{"", "2.21.6", true},      // null: no constraint
		{">=2.19.0", "", true},    // unknown core: nothing excluded
		{"~=2.9", "2.21.6", true}, // a shape not read: admitted
		{">=two", "2.21.6", true}, // unparseable: admitted
		{">=2.19.0, <2.22", "2.21.6", true},
		{"==2.21.6", "2.21.6", true},
		{"!=2.21.6", "2.21.6", false},
	} {
		if got := Admits(c.spec, c.core); got != c.want {
			t.Errorf("Admits(%q, %q) = %v, want %v", c.spec, c.core, got, c.want)
		}
	}
}

// versions as galaxy.ansible.com listed community.general, newest first, with
// pre-releases among them.
var communityGeneral = []GalaxyVersion{
	{"14.0.1", ">=2.19.0"},
	{"14.0.0", ">=2.19.0"},
	{"13.5.1", ">=2.18.0"},
	{"13.5.0", ">=2.18.0"},
	{"6.0.0-a1", ">=2.11.0"},
	{"5.9.0", ">=2.11.0"},
	{"0.3.0-experimental.meta.redirects", ""},
	{"0.1.0", ""},
}

func TestNewest(t *testing.T) {
	for core, want := range map[string]string{
		"2.21.6": "14.0.1",
		"2.19.0": "14.0.1",
		"2.18.2": "13.5.1",
		"2.12.0": "5.9.0", // 6.0.0-a1 is a pre-release
		"2.11.0": "5.9.0",
		"2.10.0": "0.1.0", // null requires_ansible admits everything
		"":       "14.0.1",
	} {
		if got := Newest(communityGeneral, core); got != want {
			t.Errorf("Newest(core %q) = %q, want %q", core, got, want)
		}
	}
	if got := Newest([]GalaxyVersion{{"1.0.0", ">=2.30.0"}, {"1.0.0-rc1", ""}}, "2.21.6"); got != "" {
		t.Errorf("nothing admitted: %q", got)
	}
}

func TestParseGalaxyServerList(t *testing.T) {
	for dump, want := range map[string]bool{
		"GALAXY_SERVER_LIST(default) = None\nGALAXY_SERVER_TIMEOUT(default) = 60": false,
		"GALAXY_SERVER(default) = https://galaxy.ansible.com":                     false,
		"GALAXY_SERVER_LIST(/etc/ansible/ansible.cfg) = ['hub', 'galaxy']":        true,
		"GALAXY_SERVER_LIST(env: ANSIBLE_GALAXY_SERVER_LIST) = ['hub']":           true,
		"GALAXY_SERVER_LIST(default) = []":                                        false,
		"":                                                                        false,
	} {
		if got := parseGalaxyServerList(dump); got != want {
			t.Errorf("parseGalaxyServerList(%q) = %v, want %v", dump, got, want)
		}
	}
}

// galaxy is a stand-in server: versions per collection (newest first, served
// in pages of two), a deprecated set, and a failure per collection.
func galaxy(t *testing.T, versions map[string][]GalaxyVersion, deprecated map[string]bool, fail map[string]int) *GalaxyClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		const prefix = "/api/v3/plugin/ansible/content/published/collections/index/"
		if !strings.HasPrefix(path, prefix) {
			http.NotFound(w, r)
			return
		}
		rest := strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/")
		isVersions := strings.HasSuffix(rest, "/versions")
		coll := strings.Replace(strings.TrimSuffix(rest, "/versions"), "/", ".", 1)
		if code := fail[coll]; code != 0 {
			http.Error(w, "boom", code)
			return
		}
		if !isVersions {
			fmt.Fprintf(w, `{"deprecated": %v}`, deprecated[coll])
			return
		}
		all := versions[coll]
		offset := 0
		fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
		end := min(offset+2, len(all))
		page := all[min(offset, len(all)):end]
		next := "null"
		if end < len(all) {
			next = fmt.Sprintf("%q", fmt.Sprintf("%s%s/versions/?limit=100&ordering=-version&offset=%d", prefix, strings.Replace(coll, ".", "/", 1), end))
		}
		var data []string
		for _, v := range page {
			spec := "null"
			if v.RequiresAnsible != "" {
				spec = fmt.Sprintf("%q", v.RequiresAnsible)
			}
			data = append(data, fmt.Sprintf(`{"version": %q, "requires_ansible": %s, "marks": []}`, v.Version, spec))
		}
		fmt.Fprintf(w, `{"meta": {"count": %d}, "links": {"next": %s}, "data": [%s]}`, len(all), next, strings.Join(data, ","))
	}))
	t.Cleanup(srv.Close)
	return &GalaxyClient{HTTP: &http.Client{Timeout: time.Second}, Base: srv.URL}
}

func TestLookup(t *testing.T) {
	c := galaxy(t,
		map[string][]GalaxyVersion{
			"community.general": communityGeneral,
			"ns.tooNew":         {{"2.0.0", ">=2.30.0"}},
		},
		map[string]bool{"community.general": false, "ns.tooNew": true},
		map[string]int{"ns.down": 500},
	)
	ctx := context.Background()

	// the answer is on the second page (pages of two), and is marked
	got := c.Lookup(ctx, "community.general", "2.18.2")
	if got.Version != "13.5.1" || got.Note != "from Galaxy: newest compatible with ansible-core 2.18.2" || got.Deprecated || got.Err != nil {
		t.Errorf("paged: %+v", got)
	}
	// unknown core
	if got := c.Lookup(ctx, "community.general", ""); got.Version != "14.0.1" || !strings.Contains(got.Note, "unknown") {
		t.Errorf("unknown core: %+v", got)
	}
	// no version admitted; deprecated is still reported
	got = c.Lookup(ctx, "ns.tooNew", "2.21.6")
	if got.Version != "" || got.Note != "unpinned: no version on Galaxy admits ansible-core 2.21.6" || !got.Deprecated {
		t.Errorf("none admitted: %+v", got)
	}
	// Galaxy answers 200 with an empty list for a collection it does not have
	got = c.Lookup(ctx, "no.such", "2.21.6")
	if got.Version != "" || got.Note != "unpinned: not found on Galaxy" || got.Err != nil {
		t.Errorf("not found: %+v", got)
	}
	// an error status
	got = c.Lookup(ctx, "ns.down", "2.21.6")
	if got.Version != "" || got.Note != "unpinned: Galaxy answered HTTP 500" || got.Err == nil {
		t.Errorf("status: %+v", got)
	}
}

func TestLookupUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // nothing listens any more
	c := &GalaxyClient{HTTP: &http.Client{Timeout: time.Second}, Base: base}
	got := c.Lookup(context.Background(), "a.b", "2.21.6")
	if got.Version != "" || got.Note != "unpinned: Galaxy unreachable" || got.Err == nil {
		t.Errorf("%+v", got)
	}
}

func TestLookupSlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	c := &GalaxyClient{HTTP: &http.Client{Timeout: 50 * time.Millisecond}, Base: srv.URL}
	if got := c.Lookup(context.Background(), "a.b", "2.21.6"); got.Note != "unpinned: Galaxy unreachable" {
		t.Errorf("%+v", got)
	}
}

func TestLookupAll(t *testing.T) {
	c := galaxy(t,
		map[string][]GalaxyVersion{"a.one": {{"1.0.0", ""}}, "b.two": {{"2.0.0", ""}}, "c.three": {{"3.0.0", ""}}},
		nil, map[string]int{"c.three": 503})
	got := c.LookupAll(context.Background(), []string{"a.one", "b.two", "c.three"}, "2.21.6")
	if got["a.one"].Version != "1.0.0" || got["b.two"].Version != "2.0.0" || got["c.three"].Err == nil || len(got) != 3 {
		t.Errorf("%+v", got)
	}
}

func TestPinTargets(t *testing.T) {
	req, err := ParseRequirements([]byte("collections:\n  - community.general\n  - name: ns.fixed\n    version: 1.2.3\n  - name: ns.range\n    version: \">=1.0\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	in := Inputs{
		Requirements: req,
		Needed: []Needed{
			{Collection: "ns.docker"}, {Collection: "ns.fixed"}, {Collection: "community.general"}, {Collection: "ns.docker"}, {Collection: "ns.local"},
		},
		Installed: map[string]string{"ns.local": "1.0.0", "community.general": "11.0.0"},
	}
	// what the draft pins itself and the host lacks: not the versioned
	// requirements entries, not what is installed
	if got := strings.Join(PinTargets(in, false), " "); got != "ns.docker" {
		t.Errorf("targets = %q", got)
	}
	// --latest: also the installed ones, still not the ones with a version
	if got := strings.Join(PinTargets(in, true), " "); got != "community.general ns.docker ns.local" {
		t.Errorf("latest targets = %q", got)
	}
}

func TestDraftPinOrder(t *testing.T) {
	in := Inputs{
		Playbook: "site.yml", Base: "img:1", Facts: microFacts,
		Needed: []Needed{
			{Collection: "ns.host"}, {Collection: "ns.gal"}, {Collection: "ns.gone"}, {Collection: "ns.dep"},
		},
		Installed: map[string]string{"ns.host": "1.0.0"},
		Galaxy: map[string]GalaxyInfo{
			"ns.host": {Version: "9.0.0", Note: "from Galaxy: newest compatible with ansible-core 2.21.6"},
			"ns.gal":  {Version: "2.0.0", Note: "from Galaxy: newest compatible with ansible-core 2.21.6"},
			"ns.gone": {Note: "unpinned: Galaxy unreachable"},
			"ns.dep":  {Version: "3.0.0", Note: "from Galaxy: newest compatible with ansible-core 2.21.6", Deprecated: true},
		},
	}
	got, rep := Draft(in)
	for _, want := range []string{
		"      - name: ns.host\n        version: 1.0.0\n", // the host's, when not --latest
		"      # from Galaxy: newest compatible with ansible-core 2.21.6\n      - name: ns.gal\n        version: 2.0.0\n",
		"      # unpinned: Galaxy unreachable\n      - name: ns.gone\n",
		"      # from Galaxy: newest compatible with ansible-core 2.21.6\n      # deprecated on Galaxy\n      - name: ns.dep\n        version: 3.0.0\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if rep.Pinned != 1 || rep.FromGalaxy != 2 || rep.Deprecated != 1 {
		t.Errorf("report = %+v", rep)
	}
	assertValidYAML(t, got)

	// --latest: Galaxy's version replaces the host's; a failed lookup keeps it
	in.Latest = true
	in.Galaxy["ns.gone"] = GalaxyInfo{Note: "unpinned: Galaxy unreachable"}
	in.Installed["ns.gone"] = "4.0.0"
	got, rep = Draft(in)
	for _, want := range []string{
		"      - name: ns.host\n        version: 9.0.0\n",
		"      - name: ns.gone\n        version: 4.0.0\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("--latest: missing %q in:\n%s", want, got)
		}
	}
	if rep.Pinned != 1 || rep.FromGalaxy != 3 {
		t.Errorf("--latest report = %+v", rep)
	}
}
