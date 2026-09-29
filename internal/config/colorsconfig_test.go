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

import "testing"

func strPtr(s string) *string { return &s }

func TestMergeColorMap(t *testing.T) {
	t.Run("per-key fallthrough, not whole-map", func(t *testing.T) {
		project := map[string]string{"outcome_ok": "from-project"}
		global := map[string]string{"outcome_ok": "from-global", "outcome_failed": "from-global"}
		system := map[string]string{"outcome_ok": "from-system", "outcome_failed": "from-system", "outcome_skipped": "from-system"}

		got := mergeColorMap(project, global, system)
		want := map[string]string{
			"outcome_ok":      "from-project", // project wins
			"outcome_failed":  "from-global",  // project doesn't set it, global wins
			"outcome_skipped": "from-system",  // only system sets it
		}
		if len(got) != len(want) {
			t.Fatalf("mergeColorMap() = %+v, want %+v", got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("mergeColorMap()[%q] = %q, want %q", k, got[k], v)
			}
		}
	})

	t.Run("all three empty returns nil, not an allocated empty map", func(t *testing.T) {
		if got := mergeColorMap(nil, nil, nil); got != nil {
			t.Errorf("mergeColorMap(nil, nil, nil) = %#v, want nil", got)
		}
	})
}

// TestResolveEffectiveColors covers design-docs/ColorConfig.md's own "Full
// precedence chain": config.toml's [colors.<scheme>] beats its own flat
// [colors], which beats a loaded theme's [<scheme>], which beats the
// theme's own flat keys, which beats leaving the field nil (uikit.
// ApplyColorConfig's own cue to keep the hardcoded default - not this
// function's concern, so a still-nil result is exactly what "nothing set
// anywhere" should look like here).
func TestResolveEffectiveColors(t *testing.T) {
	cfg := SettingsConfig{}
	cfg.General.ColorScheme = "dark"
	cfg.Colors.OutcomeOK = strPtr("from-flat-colors")
	cfg.Colors.OutcomeChanged = strPtr("from-flat-colors-beaten-by-dark")
	cfg.Colors.Dark = map[string]string{
		"outcome_changed": "from-colors-dark",
	}
	// outcome_skipped set only by the theme's own flat keys.
	// outcome_failed set only by the theme's own [dark] table.
	// outcome_unreachable set nowhere at all - should stay nil.

	theme := ThemeFile{
		Flat: map[string]string{"outcome_skipped": "from-theme-flat"},
		Dark: map[string]string{"outcome_failed": "from-theme-dark"},
	}

	got := resolveEffectiveColorsWithTheme(cfg, theme)

	if got.OutcomeOK == nil || *got.OutcomeOK != "from-flat-colors" {
		t.Errorf("OutcomeOK = %v, want a pointer to %q (flat [colors] wins with no [colors.dark] entry)", got.OutcomeOK, "from-flat-colors")
	}
	if got.OutcomeChanged == nil || *got.OutcomeChanged != "from-colors-dark" {
		t.Errorf("OutcomeChanged = %v, want a pointer to %q ([colors.dark] beats flat [colors])", got.OutcomeChanged, "from-colors-dark")
	}
	if got.OutcomeSkipped == nil || *got.OutcomeSkipped != "from-theme-flat" {
		t.Errorf("OutcomeSkipped = %v, want a pointer to %q (theme's own flat key, nothing in config.toml)", got.OutcomeSkipped, "from-theme-flat")
	}
	if got.OutcomeFailed == nil || *got.OutcomeFailed != "from-theme-dark" {
		t.Errorf("OutcomeFailed = %v, want a pointer to %q (theme's own [dark], nothing in config.toml)", got.OutcomeFailed, "from-theme-dark")
	}
	if got.OutcomeUnreachable != nil {
		t.Errorf("OutcomeUnreachable = %v, want nil (unset everywhere - ApplyColorConfig's own job to fall back)", got.OutcomeUnreachable)
	}
}

// TestResolveEffectiveColorsLightScheme confirms the [colors.light]/theme
// [light] tables are actually consulted when color_scheme = "light", not
// just [colors.dark]/theme [dark] regardless of the setting.
func TestResolveEffectiveColorsLightScheme(t *testing.T) {
	cfg := SettingsConfig{}
	cfg.General.ColorScheme = "light"
	cfg.Colors.Dark = map[string]string{"outcome_ok": "from-dark-should-not-apply"}
	cfg.Colors.Light = map[string]string{"outcome_ok": "from-light"}

	got := resolveEffectiveColorsWithTheme(cfg, ThemeFile{})
	if got.OutcomeOK == nil || *got.OutcomeOK != "from-light" {
		t.Errorf("OutcomeOK = %v, want a pointer to %q (color_scheme=light must use [colors.light], not [colors.dark])", got.OutcomeOK, "from-light")
	}
}
