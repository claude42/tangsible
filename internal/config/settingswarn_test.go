package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadSettingsConfigWarnings(t *testing.T) {
	pendingWarnings = nil
	t.Cleanup(func() { pendingWarnings = nil })
	path := writeConfig(t, `[general]
terminal_progress = "ok"
color_scheme = "Light"
terminal_progres = "on"
notify_task_failed = "desktop"
template_hosts_max = 3

[colors]
standard_text = "red"
bogus_color = "red"
`)

	cfg := ReadSettingsConfig(path)
	ReadSettingsConfig(path) // re-reads (MergedSettingsConfig runs several times per process): no duplicates

	if cfg.General.ColorScheme != "Light" || ColorSchemePreference(cfg) != "light" {
		t.Errorf("values must be unchanged and still parse case-insensitively: %+v", cfg.General)
	}
	if len(pendingWarnings) != 4 {
		t.Fatalf("got %d warnings, want 4: %q", len(pendingWarnings), pendingWarnings)
	}
	for _, want := range []string{
		path + `: unknown setting general.terminal_progres, ignored`,
		path + `: unknown setting colors.bogus_color, ignored`,
		`invalid value "ok" for [general] terminal_progress (allowed: auto, on, off), using default "auto"`,
		`invalid value "desktop" for [general] notify_task_failed (allowed: off, osc9, osc777, osc99, bell), using default "off"`,
	} {
		if !slices.ContainsFunc(pendingWarnings, func(w string) bool { return strings.Contains(w, want) }) {
			t.Errorf("missing warning containing %q in %q", want, pendingWarnings)
		}
	}
}

func TestReadSettingsConfigNoWarningsForValidFile(t *testing.T) {
	pendingWarnings = nil
	t.Cleanup(func() { pendingWarnings = nil })
	path := writeConfig(t, "[general]\nterminal_progress = \"AUTO\"\nrun_dialog = \"never\"\n[colors]\nstandard_text = \"red\"\n[colors.dark]\nanything = \"red\"\n")
	ReadSettingsConfig(path)
	if len(pendingWarnings) != 0 {
		t.Errorf("unexpected warnings: %q", pendingWarnings)
	}
}

// The shipped example documents every key; it must itself be warning-free.
func TestExampleConfigIsWarningFree(t *testing.T) {
	pendingWarnings = nil
	t.Cleanup(func() { pendingWarnings = nil })
	ReadSettingsConfig(filepath.Join("..", "..", "examples", "config.toml"))
	if len(pendingWarnings) != 0 {
		t.Errorf("examples/config.toml triggers warnings: %q", pendingWarnings)
	}
}
