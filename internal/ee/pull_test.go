package ee

import (
	"strings"
	"testing"
)

func TestImageTag(t *testing.T) {
	for image, want := range map[string]string{
		"ghcr.io/ansible-community/community-ee-base:latest": "latest",
		"ghcr.io/ansible-community/community-ee-base":        "latest",
		"localhost/my-ee:1.0":                                "1.0",
		"my-ee":                                              "latest",
		"my-ee:2":                                            "2",
		"registry.example:5000/team/ee":                      "latest", // a port is not a tag
		"registry.example:5000/team/ee:3":                    "3",
		"quay.io/x/ee@sha256:0123456789abcdef":               "", // pinned by digest
		"quay.io/x/ee:5@sha256:0123456789abcdef":             "",
	} {
		if got := ImageTag(image); got != want {
			t.Errorf("ImageTag(%q) = %q, want %q", image, got, want)
		}
	}
}

func TestLocalOnly(t *testing.T) {
	for image, want := range map[string]bool{
		"localhost/my-ee:1.0":      true,
		"localhost/x":              true,
		"localhost:5000/my-ee":     false, // a registry on a port
		"ghcr.io/localhost/x":      false,
		"my-ee":                    false,
		"docker.io/library/alpine": false,
	} {
		if got := LocalOnly(image); got != want {
			t.Errorf("LocalOnly(%q) = %v, want %v", image, got, want)
		}
	}
}

func TestDecidePull(t *testing.T) {
	const remote = "ghcr.io/x/ee:latest"
	const pinned = "ghcr.io/x/ee:1.2"
	cases := []struct {
		name            string
		policy, image   string
		present, before bool
		pull, optional  bool
		errPart         string
	}{
		{"missing, absent", "missing", remote, false, false, true, false, ""},
		{"missing, present", "missing", remote, true, false, false, false, ""},
		{"empty policy means missing, absent", "", remote, false, false, true, false, ""},
		{"empty policy means missing, present", "", remote, true, false, false, false, ""},
		{"unknown policy means missing", "sometimes", remote, true, false, false, false, ""},

		{"always, absent", "always", remote, false, false, true, false, ""},
		{"always, present: refresh, optional", "always", remote, true, false, true, true, ""},
		{"always, present, refreshed already", "always", remote, true, true, false, false, ""},
		{"always, absent, refreshed already (pulling failed once)", "always", remote, false, true, true, false, ""},

		{"never, present", "never", remote, true, false, false, false, ""},
		{"never, absent", "never", remote, false, false, false, false, `pull_policy is "never"`},

		{"tag latest, present: refresh", "tag", remote, true, false, true, true, ""},
		{"tag latest, present, refreshed already", "tag", remote, true, true, false, false, ""},
		{"tag untagged is latest", "tag", "ghcr.io/x/ee", true, false, true, true, ""},
		{"tag 1.2, present", "tag", pinned, true, false, false, false, ""},
		{"tag 1.2, absent", "tag", pinned, false, false, true, false, ""},
		{"tag digest-pinned, present", "tag", "ghcr.io/x/ee@sha256:abc", true, false, false, false, ""},

		{"localhost image, present, any policy", "always", "localhost/my-ee:1.0", true, false, false, false, ""},
		{"localhost image, absent", "missing", "localhost/my-ee:1.0", false, false, false, false, "images under localhost/ exist only locally"},
		{"localhost image, absent, never", "never", "localhost/my-ee:1.0", false, false, false, false, "images under localhost/ exist only locally"},
	}
	for _, c := range cases {
		d := DecidePull(c.policy, c.image, c.present, c.before)
		if c.errPart != "" {
			if d.Err == nil || !strings.Contains(d.Err.Error(), c.errPart) || d.Pull {
				t.Errorf("%s: %+v, want an error containing %q and no pull", c.name, d, c.errPart)
			}
			continue
		}
		if d.Err != nil || d.Pull != c.pull || d.Optional != c.optional {
			t.Errorf("%s: %+v, want pull=%v optional=%v", c.name, d, c.pull, c.optional)
		}
		if d.Pull && d.Reason == "" {
			t.Errorf("%s: a pull should come with a reason", c.name)
		}
	}
}

func TestDecidePullHintsAreActionable(t *testing.T) {
	d := DecidePull("never", "ghcr.io/x/ee:1", false, false)
	for _, part := range []string{"podman pull ghcr.io/x/ee:1", `pull_policy = "missing"`} {
		if !strings.Contains(d.Err.Error(), part) {
			t.Errorf("never: the hint should mention %q: %v", part, d.Err)
		}
	}
	d = DecidePull("missing", "localhost/my-ee:1.0", false, false)
	for _, part := range []string{"podman build -t my-ee:1.0", "podman load"} {
		if !strings.Contains(d.Err.Error(), part) {
			t.Errorf("localhost: the hint should mention %q: %v", part, d.Err)
		}
	}
}

func TestExpandPathExported(t *testing.T) {
	if got := ExpandPath("$A/x", "/home/u", []string{"A=/srv"}); got != "/srv/x" {
		t.Errorf("ExpandPath = %q", got)
	}
}
