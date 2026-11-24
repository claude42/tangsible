package eeinit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var sampleTags = []string{"latest", "2.17-1", "2.18.9-1", "2.19.1-1", "2.19.3-1", "2.19.3-2", "2.19.5-1", "2.20.6-1", "2.21.5-1", "2.21.5-2", "2.21.6-1", "devel", "sha256-abcd", "2.22.0rc1-1"}

func TestPickBaseTag(t *testing.T) {
	for _, c := range []struct {
		core, want, how string
		latest          bool
	}{
		{"2.19.3", "2.19.3-2", "exact", false},  // highest build of the exact version
		{"2.19.11", "2.19.5-1", "minor", false}, // newest of the series
		{"2.17.0", "2.17-1", "exact", false},    // a tag without patch is .0
		{"2.17", "2.17-1", "exact", false},
		{"2.21.6rc1", "2.21.6-1", "exact", false}, // numeric prefix
		{"2.16.3", "", "", false},                 // no such series: default
		{"", "", "", false},
		{"2.19.3", "2.21.6-1", "newest", true}, // --latest ignores the host
		{"", "2.21.6-1", "newest", true},
	} {
		got, how := PickBaseTag(sampleTags, c.core, c.latest)
		if got != c.want || how != c.how {
			t.Errorf("PickBaseTag(%q, latest=%v) = %q, %q; want %q, %q", c.core, c.latest, got, how, c.want, c.how)
		}
	}
	if tag, _ := PickBaseTag([]string{"latest", "devel"}, "2.19.1", true); tag != "" {
		t.Errorf("no release tags: got %q", tag)
	}
}

func TestParseHostCore(t *testing.T) {
	out := "ansible-playbook [core 2.19.11]\n  config file = None\n"
	if got := parseHostCore(out); got != "2.19.11" {
		t.Errorf("got %q", got)
	}
	if got := parseHostCore("ansible-playbook 2.9.27\n"); got != "" {
		t.Errorf("old format: got %q", got)
	}
}

// fakeRegistry serves a token and two pages of tags.
func fakeRegistry(t *testing.T, tags []string) *RegistryClient {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if !strings.Contains(r.URL.Query().Get("scope"), "repository:"+BaseRepo+":pull") {
				t.Errorf("token scope %q", r.URL.Query().Get("scope"))
			}
			json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case r.URL.Path == "/v2/"+BaseRepo+"/tags/list":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			half := len(tags) / 2
			if r.URL.Query().Get("last") == "" {
				w.Header().Set("Link", `</v2/`+BaseRepo+`/tags/list?n=1000&last=x>; rel="next"`)
				json.NewEncoder(w).Encode(map[string]any{"tags": tags[:half]})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"tags": tags[half:]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return &RegistryClient{HTTP: srv.Client(), Base: srv.URL}
}

func TestListTagsFollowsPages(t *testing.T) {
	c := fakeRegistry(t, sampleTags)
	got, err := c.ListTags(context.Background(), BaseRepo)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string(nil), sampleTags...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestChooseDefaultBase(t *testing.T) {
	c := fakeRegistry(t, sampleTags)
	ctx := context.Background()

	got := ChooseDefaultBase(ctx, c, "2.19.3", false, false)
	if got.Image != BaseImageName+":2.19.3-2" || got.Warning != "" || len(got.Note) != 1 || !strings.Contains(got.Note[0], "2.19.3") {
		t.Errorf("exact: %+v", got)
	}
	got = ChooseDefaultBase(ctx, c, "2.19.3", true, false)
	if got.Image != BaseImageName+":2.21.6-1" || !strings.Contains(got.Note[0], "--latest") {
		t.Errorf("latest: %+v", got)
	}
	if got = ChooseDefaultBase(ctx, c, "2.16.3", false, false); got.Image != DefaultBaseImage || got.Warning == "" || !strings.Contains(got.Note[0], "built-in default") {
		t.Errorf("no fitting tag: %+v", got)
	}
	if got = ChooseDefaultBase(ctx, c, "", false, false); got.Image != DefaultBaseImage || got.Warning == "" {
		t.Errorf("unknown host version: %+v", got)
	}
	if got = ChooseDefaultBase(ctx, c, "2.19.3", false, true); got.Image != DefaultBaseImage || got.Warning != "" || got.Note != nil {
		t.Errorf("offline: %+v", got)
	}
	down := &RegistryClient{HTTP: http.DefaultClient, Base: "http://127.0.0.1:1"}
	if got = ChooseDefaultBase(ctx, down, "2.19.3", false, false); got.Image != DefaultBaseImage || !strings.Contains(got.Warning, "could not list") {
		t.Errorf("registry down: %+v", got)
	}
}
