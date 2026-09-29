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
	"path/filepath"
	"reflect"
	"testing"
)

func TestPrefixRelativeThemePath(t *testing.T) {
	got := prefixRelativeThemePath("/home/user/.local/bin/tangsible", "solarized")
	want := filepath.Join("/home/user/.local/share/tangsible/themes/solarized.toml")
	if got != want {
		t.Errorf("prefixRelativeThemePath() = %q, want %q", got, want)
	}
}

func TestDataHomeThemePath(t *testing.T) {
	got := dataHomeThemePath("/home/user/.local/share", "solarized")
	want := filepath.Join("/home/user/.local/share/tangsible/themes/solarized.toml")
	if got != want {
		t.Errorf("dataHomeThemePath() = %q, want %q", got, want)
	}
}

// TestResolveThemePath only exercises the $XDG_DATA_HOME tier for real -
// the <prefix>/share/tangsible tier depends on a real os.Executable()
// (the go test binary's own path in a test), not something this can
// redirect, same reasoning TestPrefixRelativeShareDir's own doc comment
// already gives for the identical callback-plugin-side split.
func TestResolveThemePath(t *testing.T) {
	dir := t.TempDir()
	xdgData := filepath.Join(dir, "xdg-data")
	t.Setenv("XDG_DATA_HOME", xdgData)

	t.Run("not found anywhere", func(t *testing.T) {
		if _, ok := ResolveThemePath("nonexistent"); ok {
			t.Error("ResolveThemePath(\"nonexistent\") = ok, want !ok")
		}
	})

	t.Run("found under $XDG_DATA_HOME/tangsible/themes", func(t *testing.T) {
		themePath := filepath.Join(xdgData, "tangsible", "themes", "solarized.toml")
		mustWriteFile(t, themePath, "outcome_ok = \"green\"\n")

		got, ok := ResolveThemePath("solarized")
		if !ok || got != themePath {
			t.Errorf("ResolveThemePath(\"solarized\") = (%q, %v), want (%q, true)", got, ok, themePath)
		}
	})
}

func TestLoadTheme(t *testing.T) {
	dir := t.TempDir()
	xdgData := filepath.Join(dir, "xdg-data")
	t.Setenv("XDG_DATA_HOME", xdgData)

	t.Run("empty name means no theme, no warning", func(t *testing.T) {
		theme, ok := LoadTheme("")
		if ok {
			t.Error("LoadTheme(\"\") = ok, want !ok")
		}
		if !reflect.DeepEqual(theme, ThemeFile{}) {
			t.Errorf("LoadTheme(\"\") = %+v, want the zero value", theme)
		}
	})

	t.Run("nonexistent theme falls back to no theme", func(t *testing.T) {
		theme, ok := LoadTheme("does-not-exist")
		if ok {
			t.Error("LoadTheme(\"does-not-exist\") = ok, want !ok")
		}
		if !reflect.DeepEqual(theme, ThemeFile{}) {
			t.Errorf("LoadTheme(\"does-not-exist\") = %+v, want the zero value", theme)
		}
	})

	t.Run("malformed TOML falls back to no theme", func(t *testing.T) {
		mustWriteFile(t, filepath.Join(xdgData, "tangsible", "themes", "broken.toml"), "not valid toml [[[")

		theme, ok := LoadTheme("broken")
		if ok {
			t.Error("LoadTheme(\"broken\") = ok, want !ok")
		}
		if !reflect.DeepEqual(theme, ThemeFile{}) {
			t.Errorf("LoadTheme(\"broken\") = %+v, want the zero value", theme)
		}
	})

	t.Run("flat keys plus dark/light sub-tables parse correctly", func(t *testing.T) {
		mustWriteFile(t, filepath.Join(xdgData, "tangsible", "themes", "solarized.toml"), `
outcome_ok = "green"
outcome_failed = "red"

[dark]
selected_row_text = "#001100"

[light]
selected_row_text = "#eeeeee"
`)

		theme, ok := LoadTheme("solarized")
		if !ok {
			t.Fatal("LoadTheme(\"solarized\") = !ok, want ok")
		}
		if theme.Flat["outcome_ok"] != "green" || theme.Flat["outcome_failed"] != "red" {
			t.Errorf("theme.Flat = %+v, want outcome_ok=green, outcome_failed=red", theme.Flat)
		}
		if theme.Dark["selected_row_text"] != "#001100" {
			t.Errorf("theme.Dark[selected_row_text] = %q, want %q", theme.Dark["selected_row_text"], "#001100")
		}
		if theme.Light["selected_row_text"] != "#eeeeee" {
			t.Errorf("theme.Light[selected_row_text] = %q, want %q", theme.Light["selected_row_text"], "#eeeeee")
		}
		// "dark"/"light" themselves must not leak into Flat as bare keys.
		if _, ok := theme.Flat["dark"]; ok {
			t.Error("theme.Flat contains \"dark\" - the [dark] sub-table should not appear as a flat key")
		}
	})
}
