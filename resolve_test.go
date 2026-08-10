package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParseVerb(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantVerb verb
		wantRest []string
		wantOK   bool
	}{
		{
			name:     "run verb with a playbook and args",
			args:     []string{"run", "site.yml", "-v"},
			wantVerb: verbRun,
			wantRest: []string{"site.yml", "-v"},
			wantOK:   true,
		},
		{
			name:     "rerun verb alone",
			args:     []string{"rerun"},
			wantVerb: verbRerun,
			wantRest: nil,
			wantOK:   true,
		},
		{
			name:   "unrecognized verb",
			args:   []string{"site.yml", "-v"},
			wantOK: false,
		},
		{
			name:   "no args at all",
			args:   nil,
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, rest, ok := parseVerb(c.args)
			if ok != c.wantOK {
				t.Fatalf("parseVerb(%v) ok = %v, want %v", c.args, ok, c.wantOK)
			}
			if !ok {
				return
			}
			if v != c.wantVerb || !slices.Equal(rest, c.wantRest) {
				t.Errorf("parseVerb(%v) = (%q, %v), want (%q, %v)", c.args, v, rest, c.wantVerb, c.wantRest)
			}
		})
	}
}

func TestSplitPlaybookArgs(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		wantPlaybook string
		wantRest     []string
		wantExplicit bool
	}{
		{
			name:         "playbook followed by passthrough args",
			args:         []string{"site.yml", "-v"},
			wantPlaybook: "site.yml",
			wantRest:     []string{"-v"},
			wantExplicit: true,
		},
		{
			name:         "first arg looks like a flag - no playbook given",
			args:         []string{"-v"},
			wantPlaybook: "",
			wantRest:     []string{"-v"},
			wantExplicit: false,
		},
		{
			name:         "no args at all",
			args:         nil,
			wantPlaybook: "",
			wantRest:     nil,
			wantExplicit: false,
		},
		{
			name:         "playbook with multiple passthrough args",
			args:         []string{"myplaybook.yml", "-i", "hosts"},
			wantPlaybook: "myplaybook.yml",
			wantRest:     []string{"-i", "hosts"},
			wantExplicit: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			playbook, rest, explicit := splitPlaybookArgs(c.args)
			if playbook != c.wantPlaybook || !slices.Equal(rest, c.wantRest) || explicit != c.wantExplicit {
				t.Errorf("splitPlaybookArgs(%v) = (%q, %v, %v), want (%q, %v, %v)",
					c.args, playbook, rest, explicit, c.wantPlaybook, c.wantRest, c.wantExplicit)
			}
		})
	}
}

func TestConfigHome(t *testing.T) {
	t.Run("XDG_CONFIG_HOME set wins", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/custom/xdg/config")
		if got := configHome(); got != "/custom/xdg/config" {
			t.Errorf("configHome() = %q, want /custom/xdg/config", got)
		}
	})

	t.Run("falls back to $HOME/.config when unset", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "/home/someone")
		if got := configHome(); got != filepath.Join("/home/someone", ".config") {
			t.Errorf("configHome() = %q, want $HOME/.config", got)
		}
	})
}

func TestReadDefaultPlaybook(t *testing.T) {
	t.Run("nonexistent file returns empty string silently", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nope.toml")
		if got := readDefaultPlaybook(path); got != "" {
			t.Errorf("readDefaultPlaybook(%q) = %q, want \"\"", path, got)
		}
	})

	t.Run("valid TOML returns the configured playbook", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		content := "[general]\ndefault_playbook = \"foo.yml\"\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readDefaultPlaybook(path); got != "foo.yml" {
			t.Errorf("readDefaultPlaybook(%q) = %q, want foo.yml", path, got)
		}
	})

	t.Run("malformed TOML returns empty string, not an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte("this is not [valid toml"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readDefaultPlaybook(path); got != "" {
			t.Errorf("readDefaultPlaybook(%q) = %q, want \"\"", path, got)
		}
	})
}

// resolvePlaybook reads .tangsible/site.yml relative to the process's own
// cwd, so each case below runs in its own isolated temp directory via
// t.Chdir rather than sharing one.
func TestResolvePlaybook(t *testing.T) {
	t.Run("TANGSIBLE_PLAYBOOK wins even when .tangsible also exists", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeDefaultPlaybookConfig(t, ".tangsible", "from-dot-tangsible.yml")
		t.Setenv("TANGSIBLE_PLAYBOOK", "from-env.yml")

		path, source := resolvePlaybook()
		if path != "from-env.yml" || source != "TANGSIBLE_PLAYBOOK" {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"from-env.yml\", \"TANGSIBLE_PLAYBOOK\")", path, source)
		}
	})

	t.Run(".tangsible wins over site.yml when no env var is set", func(t *testing.T) {
		t.Chdir(t.TempDir())
		t.Setenv("TANGSIBLE_PLAYBOOK", "")
		writeDefaultPlaybookConfig(t, ".tangsible", "from-dot-tangsible.yml")
		mustWriteFile(t, "site.yml", "")

		path, source := resolvePlaybook()
		if path != "from-dot-tangsible.yml" || source != "./.tangsible" {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"from-dot-tangsible.yml\", \"./.tangsible\")", path, source)
		}
	})

	t.Run("site.yml is the last resort", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv("TANGSIBLE_PLAYBOOK", "")
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "empty-xdg-config"))
		mustWriteFile(t, "site.yml", "")

		path, source := resolvePlaybook()
		if path != "site.yml" || source != "./site.yml" {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"site.yml\", \"./site.yml\")", path, source)
		}
	})

	t.Run("nothing found returns empty strings", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv("TANGSIBLE_PLAYBOOK", "")
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "empty-xdg-config"))

		path, source := resolvePlaybook()
		if path != "" || source != "" {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"\", \"\")", path, source)
		}
	})
}

func writeDefaultPlaybookConfig(t *testing.T, path, defaultPlaybook string) {
	t.Helper()
	content := "[general]\ndefault_playbook = \"" + defaultPlaybook + "\"\n"
	mustWriteFile(t, path, content)
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultTreeExpanded(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"unset - defaults to collapsed", "", false},
		{"expanded", "expanded", true},
		{"mixed case still matches", "ExPaNdEd", true},
		{"collapsed", "collapsed", false},
		{"unrecognized value falls back to collapsed", "sideways", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var cfg playbookConfig
			cfg.General.DefaultTreeState = c.value
			if got := defaultTreeExpanded(cfg); got != c.want {
				t.Errorf("defaultTreeExpanded(%q) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}
