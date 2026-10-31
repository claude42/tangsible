package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestExtractEEFlags(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    EEOverride
		rest    []string
		wantErr string
	}{
		{"none", []string{"-i", "x"}, EEOverride{}, []string{"-i", "x"}, ""},
		{"ee space", []string{"-i", "x", "--ee", "img:1", "-v"}, EEOverride{Image: "img:1"}, []string{"-i", "x", "-v"}, ""},
		{"ee equals", []string{"--ee=img:2"}, EEOverride{Image: "img:2"}, []string{}, ""},
		{"last ee wins", []string{"--ee", "a", "--ee=b"}, EEOverride{Image: "b"}, []string{}, ""},
		{"no-ee", []string{"--no-ee", "-v"}, EEOverride{Disable: true}, []string{"-v"}, ""},
		{"missing value", []string{"--ee"}, EEOverride{}, []string{"--ee"}, "needs an image"},
		{"empty value", []string{"--ee="}, EEOverride{}, []string{"--ee="}, "needs an image"},
		{"both", []string{"--ee", "a", "--no-ee"}, EEOverride{}, []string{"--ee", "a", "--no-ee"}, "cannot be combined"},
	}
	for _, c := range cases {
		got, rest, err := ExtractEEFlags(c.args)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: err = %v, want %q", c.name, err, c.wantErr)
			}
		} else if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if got != c.want || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("%s: got (%+v, %q), want (%+v, %q)", c.name, got, rest, c.want, c.rest)
		}
	}
}

func TestHasEEFlag(t *testing.T) {
	for args, want := range map[string]bool{"--ee": true, "--ee=x": true, "--no-ee": true, "--eex": false, "-e": false} {
		if got := HasEEFlag([]string{"a", args}); got != want {
			t.Errorf("HasEEFlag(%q) = %v", args, got)
		}
	}
}

func TestEEFromSettingsDefaults(t *testing.T) {
	got := EEFromSettings(SettingsConfig{})
	want := EESettings{Engine: "podman", SSHAgent: true, SSHKnownHosts: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("defaults = %+v, want %+v", got, want)
	}
}

func TestEEFromSettingsValues(t *testing.T) {
	f := false
	var cfg SettingsConfig
	e := &cfg.ExecutionEnvironment
	e.Image, e.Engine, e.Network, e.SELinux = " img:1 ", "Podman", "host", "Z"
	tr := true
	e.SSHAgent, e.SSHDir, e.SSHKnownHosts = &f, &tr, &f
	e.ExtraMounts = []string{"/a:/b"}
	got := EEFromSettings(cfg)
	want := EESettings{Image: "img:1", Engine: "podman", Network: "host", SELinux: "Z",
		SSHAgent: false, SSHDir: true, SSHKnownHosts: false, ExtraMounts: []string{"/a:/b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// Invalid choices fall back; "auto" means unset.
	e.Engine, e.SELinux = "docker", "bogus"
	if got := EEFromSettings(cfg); got.Engine != "podman" || got.SELinux != "" {
		t.Errorf("fallbacks: %+v", got)
	}
	e.SELinux = "auto"
	if got := EEFromSettings(cfg); got.SELinux != "" {
		t.Errorf("auto: %+v", got)
	}
}

func TestResolveEE(t *testing.T) {
	var cfg SettingsConfig
	if ResolveEE(cfg, EEOverride{}) != nil {
		t.Error("no image anywhere must mean a host run")
	}
	if s := ResolveEE(cfg, EEOverride{Image: "cli:1"}); s == nil || s.Image != "cli:1" {
		t.Errorf("--ee alone: %+v", s)
	}
	cfg.ExecutionEnvironment.Image = "cfg:1"
	if s := ResolveEE(cfg, EEOverride{}); s == nil || s.Image != "cfg:1" {
		t.Errorf("config image: %+v", s)
	}
	if s := ResolveEE(cfg, EEOverride{Image: "cli:2"}); s == nil || s.Image != "cli:2" {
		t.Errorf("--ee must beat config: %+v", s)
	}
	if ResolveEE(cfg, EEOverride{Disable: true}) != nil {
		t.Error("--no-ee must beat config")
	}
}

func TestEEMergeCascade(t *testing.T) {
	tr, fl := true, false
	var project, global, system, merged SettingsConfig
	project.ExecutionEnvironment.Network = "host"
	global.ExecutionEnvironment.Image = "global:1"
	global.ExecutionEnvironment.Network = "none"
	global.ExecutionEnvironment.SSHAgent = &fl
	system.ExecutionEnvironment.Image = "system:1"
	system.ExecutionEnvironment.SSHAgent = &tr
	system.ExecutionEnvironment.ExtraMounts = []string{"/s:/s"}
	mergeEE(&merged, project, global, system)
	e := merged.ExecutionEnvironment
	if e.Image != "global:1" || e.Network != "host" || e.SSHAgent == nil || *e.SSHAgent ||
		!reflect.DeepEqual(e.ExtraMounts, []string{"/s:/s"}) {
		t.Errorf("merged = %+v", e)
	}
}

func TestEESettingsWarnings(t *testing.T) {
	pendingWarnings = nil
	t.Cleanup(func() { pendingWarnings = nil })
	path := writeConfig(t, `[execution_environment]
image = "img"
engine = "docker"
selinux = "maybe"
netwrk = "host"
`)
	ReadSettingsConfig(path)
	for _, want := range []string{
		`unsupported value "docker" for [execution_environment] engine`,
		`invalid value "maybe" for [execution_environment] selinux`,
		`unknown setting execution_environment.netwrk, ignored`,
	} {
		if !slices.ContainsFunc(pendingWarnings, func(w string) bool { return strings.Contains(w, want) }) {
			t.Errorf("missing warning containing %q in %q", want, pendingWarnings)
		}
	}
	if len(pendingWarnings) != 3 {
		t.Errorf("warnings = %q", pendingWarnings)
	}

	pendingWarnings = nil
	ReadSettingsConfig(writeConfig(t, "[execution_environment]\nimage = \"i\"\nengine = \"Podman\"\nselinux = \"Z\"\nextra_mounts = [\"/a:/b\"]\nssh_agent = false\n"))
	if len(pendingWarnings) != 0 {
		t.Errorf("valid table warned: %q", pendingWarnings)
	}
}

func TestEEHistoryValue(t *testing.T) {
	var none, configured SettingsConfig
	configured.ExecutionEnvironment.Image = "cfg:1"
	active := &EESettings{Image: "run:1"}
	cases := []struct {
		name   string
		cfg    SettingsConfig
		active *EESettings
		want   string
	}{
		{"ee run", configured, active, "run:1"},
		{"ee run without config", none, active, "run:1"},
		{"host run while configured", configured, nil, EEHostRecord},
		{"plain host run", none, nil, ""},
	}
	for _, c := range cases {
		if got := EEHistoryValue(c.cfg, c.active); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestResolveEEWithHistory(t *testing.T) {
	var cfg SettingsConfig
	cfg.ExecutionEnvironment.Image = "cfg:1"
	cfg.ExecutionEnvironment.Network = "host"
	cases := []struct {
		name     string
		o        EEOverride
		recorded string
		want     string // "" means a host run
	}{
		{"nothing recorded: config", EEOverride{}, "", "cfg:1"},
		{"recorded image beats config", EEOverride{}, "old:1", "old:1"},
		{"recorded host beats config", EEOverride{}, EEHostRecord, ""},
		{"--ee beats recorded", EEOverride{Image: "cli:1"}, "old:1", "cli:1"},
		{"--ee beats recorded host", EEOverride{Image: "cli:1"}, EEHostRecord, "cli:1"},
		{"--no-ee beats recorded", EEOverride{Disable: true}, "old:1", ""},
	}
	for _, c := range cases {
		got := ResolveEEWithHistory(cfg, c.o, c.recorded)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: got %+v, want host run", c.name, got)
		case c.want != "" && (got == nil || got.Image != c.want):
			t.Errorf("%s: got %+v, want image %s", c.name, got, c.want)
		case got != nil && got.Network != "host":
			t.Errorf("%s: the rest of the configuration must still apply: %+v", c.name, got)
		}
	}
	// Without a configured image, a recorded one still stands on its own.
	if got := ResolveEEWithHistory(SettingsConfig{}, EEOverride{}, "old:1"); got == nil || got.Image != "old:1" {
		t.Errorf("recorded image with no config: %+v", got)
	}
}
