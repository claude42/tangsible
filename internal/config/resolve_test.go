// Copyright 2026 Klaus Wissmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

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
		wantVerb Verb
		wantRest []string
		wantOK   bool
	}{
		{
			name:     "run verb with a playbook and args",
			args:     []string{"run", "site.yml", "-v"},
			wantVerb: VerbRun,
			wantRest: []string{"site.yml", "-v"},
			wantOK:   true,
		},
		{
			name:     "rerun verb alone",
			args:     []string{"rerun"},
			wantVerb: VerbRerun,
			wantRest: nil,
			wantOK:   true,
		},
		{
			name:     "role verb with a role name and args",
			args:     []string{"role", "myrole", "-l", "somehost"},
			wantVerb: VerbRole,
			wantRest: []string{"myrole", "-l", "somehost"},
			wantOK:   true,
		},
		{
			name:     "version verb alone",
			args:     []string{"version"},
			wantVerb: VerbVersion,
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
			v, rest, ok := ParseVerb(c.args)
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
			playbook, rest, explicit := SplitPlaybookArgs(c.args)
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
		if got := ConfigHome(); got != "/custom/xdg/config" {
			t.Errorf("configHome() = %q, want /custom/xdg/config", got)
		}
	})

	t.Run("falls back to $HOME/.config when unset", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "/home/someone")
		if got := ConfigHome(); got != filepath.Join("/home/someone", ".config") {
			t.Errorf("configHome() = %q, want $HOME/.config", got)
		}
	})
}

// TestDataHome mirrors TestConfigHome above - DataHome is ConfigHome's own
// XDG_DATA_HOME/.local/share sibling, same precedence shape.
func TestDataHome(t *testing.T) {
	t.Run("XDG_DATA_HOME set wins", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/custom/xdg/data")
		if got := DataHome(); got != "/custom/xdg/data" {
			t.Errorf("DataHome() = %q, want /custom/xdg/data", got)
		}
	})

	t.Run("falls back to $HOME/.local/share when unset", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		t.Setenv("HOME", "/home/someone")
		if got := DataHome(); got != filepath.Join("/home/someone", ".local", "share") {
			t.Errorf("DataHome() = %q, want $HOME/.local/share", got)
		}
	})
}

func TestReadDefaultPlaybook(t *testing.T) {
	t.Run("nonexistent file returns empty string silently", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nope.toml")
		if got := ReadDefaultPlaybook(path); got != "" {
			t.Errorf("readDefaultPlaybook(%q) = %q, want \"\"", path, got)
		}
	})

	t.Run("valid TOML returns the configured playbook", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		content := "[general]\ndefault_playbook = \"foo.yml\"\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ReadDefaultPlaybook(path); got != "foo.yml" {
			t.Errorf("readDefaultPlaybook(%q) = %q, want foo.yml", path, got)
		}
	})

	t.Run("malformed TOML returns empty string, not an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte("this is not [valid toml"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ReadDefaultPlaybook(path); got != "" {
			t.Errorf("readDefaultPlaybook(%q) = %q, want \"\"", path, got)
		}
	})
}

// resolvePlaybook reads .tangsible/config.toml/site.yml relative to the
// process's own cwd, so each case below runs in its own isolated temp
// directory via t.Chdir rather than sharing one. SystemConfigPath is
// pointed at a nonexistent path by default so these cases don't
// accidentally depend on (or get broken by) a real /etc/tangsible/
// config.toml on whatever machine the tests run on; the subtests that
// actually exercise the system tier point it at a real file themselves.
func TestResolvePlaybook(t *testing.T) {
	origSystemConfigPath := SystemConfigPath
	SystemConfigPath = filepath.Join(t.TempDir(), "unused-etc-tangsible", "config.toml")
	t.Cleanup(func() { SystemConfigPath = origSystemConfigPath })

	t.Run("TANGSIBLE_PLAYBOOK wins even when .tangsible/config.toml also exists", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeDefaultPlaybookConfig(t, ".tangsible/config.toml", "from-dot-tangsible.yml")
		t.Setenv("TANGSIBLE_PLAYBOOK", "from-env.yml")

		path, source := ResolvePlaybook()
		if path != "from-env.yml" || source != "TANGSIBLE_PLAYBOOK" {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"from-env.yml\", \"TANGSIBLE_PLAYBOOK\")", path, source)
		}
	})

	t.Run(".tangsible/config.toml wins over site.yml when no env var is set", func(t *testing.T) {
		t.Chdir(t.TempDir())
		t.Setenv("TANGSIBLE_PLAYBOOK", "")
		writeDefaultPlaybookConfig(t, ".tangsible/config.toml", "from-dot-tangsible.yml")
		mustWriteFile(t, "site.yml", "")

		path, source := ResolvePlaybook()
		if path != "from-dot-tangsible.yml" || source != "./.tangsible/config.toml" {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"from-dot-tangsible.yml\", \"./.tangsible/config.toml\")", path, source)
		}
	})

	t.Run("site.yml is the last resort", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv("TANGSIBLE_PLAYBOOK", "")
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "empty-xdg-config"))
		mustWriteFile(t, "site.yml", "")

		path, source := ResolvePlaybook()
		if path != "site.yml" || source != "./site.yml" {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"site.yml\", \"./site.yml\")", path, source)
		}
	})

	t.Run("system config wins over site.yml", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv("TANGSIBLE_PLAYBOOK", "")
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "empty-xdg-config"))
		systemConfig := filepath.Join(dir, "etc-tangsible", "config.toml")
		writeDefaultPlaybookConfig(t, systemConfig, "from-system.yml")
		origSystemConfigPath := SystemConfigPath
		SystemConfigPath = systemConfig
		t.Cleanup(func() { SystemConfigPath = origSystemConfigPath })
		mustWriteFile(t, "site.yml", "")

		path, source := ResolvePlaybook()
		if path != "from-system.yml" || source != systemConfig {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"from-system.yml\", %q)", path, source, systemConfig)
		}
	})

	t.Run("global XDG config wins over system config", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv("TANGSIBLE_PLAYBOOK", "")
		xdgHome := filepath.Join(dir, "xdg-config")
		t.Setenv("XDG_CONFIG_HOME", xdgHome)
		writeDefaultPlaybookConfig(t, filepath.Join(xdgHome, "tangsible", "config.toml"), "from-xdg.yml")
		systemConfig := filepath.Join(dir, "etc-tangsible", "config.toml")
		writeDefaultPlaybookConfig(t, systemConfig, "from-system.yml")
		origSystemConfigPath := SystemConfigPath
		SystemConfigPath = systemConfig
		t.Cleanup(func() { SystemConfigPath = origSystemConfigPath })

		path, source := ResolvePlaybook()
		wantSource := filepath.Join(xdgHome, "tangsible", "config.toml")
		if path != "from-xdg.yml" || source != wantSource {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"from-xdg.yml\", %q)", path, source, wantSource)
		}
	})

	t.Run("nothing found returns empty strings", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv("TANGSIBLE_PLAYBOOK", "")
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "empty-xdg-config"))

		path, source := ResolvePlaybook()
		if path != "" || source != "" {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"\", \"\")", path, source)
		}
	})

	// A stale, pre-upgrade flat .tangsible file (see
	// design-docs/Dottangsible-directory.md) sitting where .tangsible/
	// should now be a directory must not error out on the read side - only
	// appendInvocation's write path is meant to surface a loud, actionable
	// error (see history_test.go's
	// TestAppendInvocationCreatesTangsibleDirCollision); reads degrade
	// gracefully, same as any other missing/unreadable source, and the
	// cascade simply falls through to the next one.
	t.Run("stale flat .tangsible file falls through to the next source", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv("TANGSIBLE_PLAYBOOK", "")
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "empty-xdg-config"))
		mustWriteFile(t, ".tangsible", "[general]\ndefault_playbook = \"stale.yml\"\n")
		mustWriteFile(t, "site.yml", "")

		path, source := ResolvePlaybook()
		if path != "site.yml" || source != "./site.yml" {
			t.Errorf("resolvePlaybook() = (%q, %q), want (\"site.yml\", \"./site.yml\")", path, source)
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
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
			var cfg SettingsConfig
			cfg.General.DefaultTreeState = c.value
			if got := DefaultTreeExpanded(cfg); got != c.want {
				t.Errorf("defaultTreeExpanded(%q) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}

func TestRunDialogPreference(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  DialogPreference
	}{
		{"unset - defaults to default", "", DialogPreferenceDefault},
		{"default", "default", DialogPreferenceDefault},
		{"never", "never", DialogPreferenceNever},
		{"mixed case still matches", "NeVeR", DialogPreferenceNever},
		{"always", "always", DialogPreferenceAlways},
		{"unrecognized value falls back to default", "sideways", DialogPreferenceDefault},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var cfg SettingsConfig
			cfg.General.RunDialog = c.value
			if got := RunDialogPreference(cfg); got != c.want {
				t.Errorf("RunDialogPreference(%q) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}

func TestParseNotificationKind(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  NotificationKind
	}{
		{"unset - defaults to off", "", NotificationOff},
		{"off", "off", NotificationOff},
		{"osc9", "osc9", NotificationOSC9},
		{"osc777", "osc777", NotificationOSC777},
		{"osc99", "osc99", NotificationOSC99},
		{"bell", "bell", NotificationBell},
		{"mixed case still matches", "OsC99", NotificationOSC99},
		{"unrecognized value falls back to off", "desktop", NotificationOff},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseNotificationKind(c.value); got != c.want {
				t.Errorf("ParseNotificationKind(%q) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}

func TestNotifyPlaybookFinishedAndTaskFailedKind(t *testing.T) {
	var cfg SettingsConfig
	cfg.General.NotifyPlaybookFinished = "osc9"
	cfg.General.NotifyTaskFailed = "bell"
	if got := NotifyPlaybookFinishedKind(cfg); got != NotificationOSC9 {
		t.Errorf("NotifyPlaybookFinishedKind = %v, want NotificationOSC9", got)
	}
	if got := NotifyTaskFailedKind(cfg); got != NotificationBell {
		t.Errorf("NotifyTaskFailedKind = %v, want NotificationBell", got)
	}
}

func TestNotifyTaskFailedMax(t *testing.T) {
	var cfg SettingsConfig
	if got := NotifyTaskFailedMax(cfg); got != defaultNotifyTaskFailedMax {
		t.Errorf("NotifyTaskFailedMax with unset config = %d, want default %d", got, defaultNotifyTaskFailedMax)
	}
	explicit := 10
	cfg.General.NotifyTaskFailedMax = &explicit
	if got := NotifyTaskFailedMax(cfg); got != 10 {
		t.Errorf("NotifyTaskFailedMax with explicit config = %d, want 10", got)
	}
}

func TestTemplateHostsMax(t *testing.T) {
	var cfg SettingsConfig
	if got := TemplateHostsMax(cfg); got != defaultTemplateHostsMax {
		t.Errorf("TemplateHostsMax with unset config = %d, want default %d", got, defaultTemplateHostsMax)
	}
	explicit := 25
	cfg.General.TemplateHostsMax = &explicit
	if got := TemplateHostsMax(cfg); got != 25 {
		t.Errorf("TemplateHostsMax with explicit config = %d, want 25", got)
	}
}

func TestTwoPaneLayoutEnabled(t *testing.T) {
	trueVal, falseVal := true, false
	cases := []struct {
		name  string
		value *bool
		want  bool
	}{
		{"unset - defaults to enabled", nil, true},
		{"explicit true", &trueVal, true},
		{"explicit false", &falseVal, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var cfg SettingsConfig
			cfg.General.TwoPaneLayout = c.value
			if got := TwoPaneLayoutEnabled(cfg); got != c.want {
				t.Errorf("TwoPaneLayoutEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestColorEnabledByUser(t *testing.T) {
	trueVal, falseVal := true, false
	cases := []struct {
		name  string
		value *bool
		want  bool
	}{
		{"unset - defaults to enabled", nil, true},
		{"explicit true", &trueVal, true},
		{"explicit false", &falseVal, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var cfg SettingsConfig
			cfg.General.Color = c.value
			if got := ColorEnabledByUser(cfg); got != c.want {
				t.Errorf("ColorEnabledByUser() = %v, want %v", got, c.want)
			}
		})
	}
}
