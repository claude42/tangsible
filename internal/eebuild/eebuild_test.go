package eebuild

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildArgs(t *testing.T) {
	got := BuildArgs("ee.yml", "/tmp/ctx", []string{"localhost/x:1"}, "", false)
	want := []string{"build", "-f", "ee.yml", "-c", "/tmp/ctx", "-t", "localhost/x:1", "--container-runtime", "podman"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	got = BuildArgs("ee.yml", "/tmp/ctx", []string{"localhost/x:1"}, "host", false)
	if got[len(got)-1] != "--extra-build-cli-args=--network=host" {
		t.Errorf("network: %v", got)
	}
	got = BuildArgs("ee.yml", "/tmp/ctx", []string{"localhost/x:1"}, "host", true)
	if got[len(got)-1] != "--extra-build-cli-args=--layers=false --network=host" {
		t.Errorf("no cache: %v", got)
	}
	// Several names follow one -t; none leaves the definition's own.
	got = BuildArgs("ee.yml", "/tmp/ctx", []string{"a:1", "a:latest"}, "", false)
	want = []string{"build", "-f", "ee.yml", "-c", "/tmp/ctx", "-t", "a:1", "a:latest", "--container-runtime", "podman"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("two tags: %v", got)
	}
	got = BuildArgs("ee.yml", "/tmp/ctx", nil, "", false)
	want = []string{"build", "-f", "ee.yml", "-c", "/tmp/ctx", "--container-runtime", "podman"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("no tags: %v", got)
	}
	// --prune-images would prune the user's own dangling images
	for _, a := range got {
		if strings.Contains(a, "prune") {
			t.Errorf("%q in %v", a, got)
		}
	}
}

func TestNewSince(t *testing.T) {
	before := map[string]bool{"a": true, "b": true}
	after := map[string]bool{"b": true, "c": true, "a": true, "d": true}
	if got := NewSince(before, after); !reflect.DeepEqual(got, []string{"c", "d"}) {
		t.Errorf("got %v", got)
	}
	if got := NewSince(after, before); len(got) != 0 {
		t.Errorf("nothing new: %v", got)
	}
	var none map[string]bool
	if got := NewSince(none, none); len(got) != 0 {
		t.Errorf("empty: %v", got)
	}
}

func TestLeftoverImages(t *testing.T) {
	before := map[string]string{"old1": "ghcr.io/x/base", "old2": "<none>"}
	after := map[string]string{
		"old1": "ghcr.io/x/base", "old2": "<none>", // already there: never touched
		"n1": "<none>",                                      // a finished stage: build cache, kept
		"n2": "docker.io/library/e83c1b-tmp",                // buildah's temporary image
		"n3": "ghcr.io/ansible-community/community-ee-base", // pulled by the build: kept
		"n4": "localhost/tangsible-ee-verify",               // its own tag is removed by name
	}
	if got := leftoverImages(before, after); !reflect.DeepEqual(got, []string{"n2"}) {
		t.Errorf("got %v", got)
	}
}

func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want Options
		bad  string
	}{
		{nil, Options{File: DefaultFile}, ""},
		{[]string{"-f", "my.yml"}, Options{File: "my.yml"}, ""},
		{[]string{"--file=my.yml", "--no-cache"}, Options{File: "my.yml", NoCache: true}, ""},
		{[]string{"-t", "a:1", "-t", "a:latest"}, Options{File: DefaultFile, Tags: []string{"a:1", "a:latest"}}, ""},
		{[]string{"--tag", "a:1", "--tag=b:2"}, Options{File: DefaultFile, Tags: []string{"a:1", "b:2"}}, ""},
		{[]string{"-t"}, Options{}, "-t needs a value"},
		{[]string{"-t", "--no-cache"}, Options{}, "-t needs a value"},
		{[]string{"-f"}, Options{}, "-f needs a value"},
		{[]string{"--tag="}, Options{}, "--tag needs a value"},
		{[]string{"--keep"}, Options{}, "unknown option --keep"},
		{[]string{"site.yml"}, Options{}, "unexpected argument site.yml"},
	} {
		got, err := ParseArgs(c.args)
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("ParseArgs(%v) error = %v, want one containing %q", c.args, err, c.bad)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseArgs(%v) = %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
}

func TestTags(t *testing.T) {
	for _, c := range []struct {
		name, yaml string
		want       []string
		bad        string
	}{
		{"list", "version: 3\noptions:\n  tags:\n    - my-ee:1.0\n    - my-ee:latest\n", []string{"my-ee:1.0", "my-ee:latest"}, ""},
		{"no options", "version: 3\n", nil, ""},
		{"null options", "version: 3\noptions:\n", nil, ""},
		{"options without tags", "options:\n  package_manager_path: /usr/bin/dnf\n", nil, ""},
		{"commented tags", "options:\n  package_manager_path: /usr/bin/dnf\n  # tags:\n  #   - my-ee:1.0\n", nil, ""},
		{"scalar", "options:\n  tags: my-ee:1.0\n", nil, "must be a list"},
		{"number entry", "options:\n  tags:\n    - 1\n", nil, "entry 1 is not a name"},
		{"invalid yaml", "options: [\n", nil, "yaml"},
	} {
		got, err := Tags([]byte(c.yaml))
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("%s: error = %v, want one containing %q", c.name, err, c.bad)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

func TestRender(t *testing.T) {
	out, code := Render(Result{Duration: 72 * time.Second, Images: []Image{
		{Tag: "my-ee:1.0", ID: "3a9d50f4da0d", Size: 568 << 20},
		{Tag: "my-ee:latest", ID: "3a9d50f4da0d", Size: 568 << 20, Replaced: "d546e55a02a3"},
	}})
	for _, want := range []string{"Built in 1m12s", "my-ee:1.0  3a9d50f4da0d  568 MB", "(replaced d546e55a02a3)", "podman image prune", `image = "my-ee:1.0"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if code != 0 {
		t.Errorf("code = %d", code)
	}

	out, code = Render(Result{Duration: time.Second, Images: []Image{{Tag: "x:1", ID: "abc", Size: 1 << 30}}})
	if strings.Contains(out, "prune") || !strings.Contains(out, "1.0 GB") || code != 0 {
		t.Errorf("no replacement, no prune hint:\n%s", out)
	}

	out, code = Render(Result{BuildErr: "exit status 1", BuildTail: []string{"error one", "error two"}, LogPath: ".tangsible/ee-build.log"})
	if code != 1 || !strings.Contains(out, "Build: FAILED (exit status 1)") || !strings.Contains(out, "error two") || !strings.Contains(out, "full log: .tangsible/ee-build.log") {
		t.Errorf("failure (code %d):\n%s", code, out)
	}
	if strings.Contains(out, "image =") {
		t.Errorf("no config line after a failure:\n%s", out)
	}

	if out, code = Render(Result{Interrupted: true, BuildErr: "signal: interrupt"}); code != 130 || !strings.Contains(out, "Interrupted") {
		t.Errorf("interrupted (code %d): %s", code, out)
	}
}
