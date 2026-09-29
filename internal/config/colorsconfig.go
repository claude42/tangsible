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

// Implements design-docs/ColorConfig.md's `[colors]` table: one field per
// uikit.*Color/diff.DiffChromeColor constant (design-docs/Colors.md), the
// toml tag being that constant's own name with the trailing "Color"
// dropped and snake_cased - see ColorConfig.md's own "Key naming rule".
// The struct itself carries no meaning about any individual color -
// that's Colors.md's job; this is purely the TOML shape and its merge
// rule. Resolving a field's final string value (validating it, falling
// back to the hardcoded default on an invalid override) is
// uikit.ApplyColorConfig/diff.ApplyColorConfig's job, not this package's
// - internal/config never imports a rendering library.
package config

// ColorsConfig is SettingsConfig.Colors' own shape - every field a
// *string, not string, for the same reason General's own *bool/*int
// fields are pointers (TwoPaneLayout, NotifyTaskFailedMax): a zero-value
// "" can't be told apart from "key genuinely absent," and MergedSettingsConfig's
// firstNonNil needs that distinction to fall through to the next tier
// correctly.
type ColorsConfig struct {
	// Background is the whole TUI's own base background - every unstyled
	// cell, not a specific row/element the way every other key here is.
	// Wired to tview.Styles.PrimitiveBackgroundColor (uikit.ApplyColorConfig),
	// which every tview.Box reads at construction and actively paints
	// across its own rect - tview's own hardcoded default is
	// tcell.ColorBlack, which this app never overrode before this field
	// existed, so an unconfigured background was always black regardless
	// of General.ColorScheme. No built-in light-mode value is set here
	// deliberately - see design-docs/ColorConfig.md's own note on why
	// this doesn't make color_scheme = "light" produce a light UI by
	// itself.
	Background *string `toml:"background"`

	OutcomeOK          *string `toml:"outcome_ok"`
	OutcomeChanged     *string `toml:"outcome_changed"`
	OutcomeSkipped     *string `toml:"outcome_skipped"`
	OutcomeFailed      *string `toml:"outcome_failed"`
	OutcomeUnreachable *string `toml:"outcome_unreachable"`

	NothingReported *string `toml:"nothing_reported"`
	Annotation      *string `toml:"annotation"`

	TaskTitle    *string `toml:"task_title"`
	PlayTitle    *string `toml:"play_title"`
	StandardText *string `toml:"standard_text"`

	ProgressText *string `toml:"progress_text"`
	TabText      *string `toml:"tab_text"`

	ChromeLive    *string `toml:"chrome_live"`
	ChromeRevisit *string `toml:"chrome_revisit"`
	ChromeCheck   *string `toml:"chrome_check"`
	DiffChrome    *string `toml:"diff_chrome"`

	SelectedRowText       *string `toml:"selected_row_text"`
	SelectedRowBackground *string `toml:"selected_row_background"`

	ProgressFill *string `toml:"progress_fill"`
	Warning      *string `toml:"warning"`
	Ignored      *string `toml:"ignored"`

	Selection *string `toml:"selection"`

	AsyncError *string `toml:"async_error"`

	DiffAdded         *string `toml:"diff_added"`
	DiffRemoved       *string `toml:"diff_removed"`
	DiffHunk          *string `toml:"diff_hunk"`
	DiffUnmatchedTask *string `toml:"diff_unmatched_task"`

	StatusBenign *string `toml:"status_benign"`

	KeyHighlight   *string `toml:"key_highlight"`
	ValueHighlight *string `toml:"value_highlight"`
	Heading        *string `toml:"heading"`

	SummaryFailed *string `toml:"summary_failed"`

	SearchMatchForeground        *string `toml:"search_match_foreground"`
	SearchMatchBackground        *string `toml:"search_match_background"`
	SearchCurrentMatchForeground *string `toml:"search_current_match_foreground"`
	SearchCurrentMatchBackground *string `toml:"search_current_match_background"`

	// Dark/Light are design-docs/ColorConfig.md's own addendum -
	// [colors.dark]/[colors.light] sub-tables, holding only the keys
	// that actually need to differ by background brightness (see
	// General.ColorScheme). A map, not 34 more *string fields each: a
	// key that's absent from the map (not merely "" - a genuinely
	// missing map entry) means "no override at this tier," resolved by
	// ResolveEffectiveColors below, not by this file's own
	// firstNonNil-per-field convention - keyed by the exact same
	// snake_case names as the flat fields above (toml tags on this
	// struct), so ResolveEffectiveColors can look either up by the same
	// string.
	Dark  map[string]string `toml:"dark"`
	Light map[string]string `toml:"light"`
}

// mergeColors is MergedSettingsConfig's own per-field cascade, pulled out
// purely to keep that function from being one unbroken 34-line stretch on
// top of General's own 9 - same firstNonNil rule, one call per field.
func mergeColors(project, global, system ColorsConfig) ColorsConfig {
	return ColorsConfig{
		Background: firstNonNil(project.Background, global.Background, system.Background),

		OutcomeOK:          firstNonNil(project.OutcomeOK, global.OutcomeOK, system.OutcomeOK),
		OutcomeChanged:     firstNonNil(project.OutcomeChanged, global.OutcomeChanged, system.OutcomeChanged),
		OutcomeSkipped:     firstNonNil(project.OutcomeSkipped, global.OutcomeSkipped, system.OutcomeSkipped),
		OutcomeFailed:      firstNonNil(project.OutcomeFailed, global.OutcomeFailed, system.OutcomeFailed),
		OutcomeUnreachable: firstNonNil(project.OutcomeUnreachable, global.OutcomeUnreachable, system.OutcomeUnreachable),

		NothingReported: firstNonNil(project.NothingReported, global.NothingReported, system.NothingReported),
		Annotation:      firstNonNil(project.Annotation, global.Annotation, system.Annotation),

		TaskTitle:    firstNonNil(project.TaskTitle, global.TaskTitle, system.TaskTitle),
		PlayTitle:    firstNonNil(project.PlayTitle, global.PlayTitle, system.PlayTitle),
		StandardText: firstNonNil(project.StandardText, global.StandardText, system.StandardText),

		ProgressText: firstNonNil(project.ProgressText, global.ProgressText, system.ProgressText),
		TabText:      firstNonNil(project.TabText, global.TabText, system.TabText),

		ChromeLive:    firstNonNil(project.ChromeLive, global.ChromeLive, system.ChromeLive),
		ChromeRevisit: firstNonNil(project.ChromeRevisit, global.ChromeRevisit, system.ChromeRevisit),
		ChromeCheck:   firstNonNil(project.ChromeCheck, global.ChromeCheck, system.ChromeCheck),
		DiffChrome:    firstNonNil(project.DiffChrome, global.DiffChrome, system.DiffChrome),

		SelectedRowText:       firstNonNil(project.SelectedRowText, global.SelectedRowText, system.SelectedRowText),
		SelectedRowBackground: firstNonNil(project.SelectedRowBackground, global.SelectedRowBackground, system.SelectedRowBackground),

		ProgressFill: firstNonNil(project.ProgressFill, global.ProgressFill, system.ProgressFill),
		Warning:      firstNonNil(project.Warning, global.Warning, system.Warning),
		Ignored:      firstNonNil(project.Ignored, global.Ignored, system.Ignored),

		Selection: firstNonNil(project.Selection, global.Selection, system.Selection),

		AsyncError: firstNonNil(project.AsyncError, global.AsyncError, system.AsyncError),

		DiffAdded:         firstNonNil(project.DiffAdded, global.DiffAdded, system.DiffAdded),
		DiffRemoved:       firstNonNil(project.DiffRemoved, global.DiffRemoved, system.DiffRemoved),
		DiffHunk:          firstNonNil(project.DiffHunk, global.DiffHunk, system.DiffHunk),
		DiffUnmatchedTask: firstNonNil(project.DiffUnmatchedTask, global.DiffUnmatchedTask, system.DiffUnmatchedTask),

		StatusBenign: firstNonNil(project.StatusBenign, global.StatusBenign, system.StatusBenign),

		KeyHighlight:   firstNonNil(project.KeyHighlight, global.KeyHighlight, system.KeyHighlight),
		ValueHighlight: firstNonNil(project.ValueHighlight, global.ValueHighlight, system.ValueHighlight),
		Heading:        firstNonNil(project.Heading, global.Heading, system.Heading),

		SummaryFailed: firstNonNil(project.SummaryFailed, global.SummaryFailed, system.SummaryFailed),

		SearchMatchForeground:        firstNonNil(project.SearchMatchForeground, global.SearchMatchForeground, system.SearchMatchForeground),
		SearchMatchBackground:        firstNonNil(project.SearchMatchBackground, global.SearchMatchBackground, system.SearchMatchBackground),
		SearchCurrentMatchForeground: firstNonNil(project.SearchCurrentMatchForeground, global.SearchCurrentMatchForeground, system.SearchCurrentMatchForeground),
		SearchCurrentMatchBackground: firstNonNil(project.SearchCurrentMatchBackground, global.SearchCurrentMatchBackground, system.SearchCurrentMatchBackground),

		Dark:  mergeColorMap(project.Dark, global.Dark, system.Dark),
		Light: mergeColorMap(project.Light, global.Light, system.Light),
	}
}

// ResolveEffectiveColors combines cfg's own [colors] (flat keys plus
// General.ColorScheme's active [colors.dark]/[colors.light] sub-table)
// with cfg.General.Theme's own loaded ThemeFile (design-docs/
// ColorConfig.md's own addendum) into one ColorsConfig whose 34 flat
// fields already carry the fully-resolved winning override for that
// scheme+theme+config combination - Dark/Light left nil on the result,
// unused from here on. This is the one place the addendum's own "full
// precedence chain" (config.toml's [colors.<scheme>] -> flat [colors] ->
// theme's own [<scheme>] -> theme's own flat keys -> hardcoded default)
// is actually implemented; everything downstream (uikit.ApplyColorConfig/
// diff.ApplyColorConfig) needs no changes at all to consume it, since a
// ColorsConfig with every field already resolved is exactly what those
// two already expected before this existed - "hardcoded default" itself
// isn't decided here at all, it's ApplyColorConfig's own existing
// fallback for a still-nil field.
func ResolveEffectiveColors(cfg SettingsConfig) ColorsConfig {
	theme, _ := LoadTheme(cfg.General.Theme)
	return resolveEffectiveColorsWithTheme(cfg, theme)
}

// resolveEffectiveColorsWithTheme is ResolveEffectiveColors' own pure
// core, theme passed in rather than loaded from disk - split out purely
// so it's directly testable without real theme files on disk, the same
// "pure function taking already-resolved inputs" split this package
// already uses for ResolveThemePath's own two candidate-path helpers.
func resolveEffectiveColorsWithTheme(cfg SettingsConfig, theme ThemeFile) ColorsConfig {
	scheme := ColorSchemePreference(cfg)

	cfgScheme, themeScheme := cfg.Colors.Dark, theme.Dark
	if scheme == "light" {
		cfgScheme, themeScheme = cfg.Colors.Light, theme.Light
	}

	// pick resolves one key's own winning override, in the exact order
	// design-docs/ColorConfig.md's own "Full precedence chain" documents:
	// config.toml's own scheme-specific entry, then its flat field, then
	// the theme's own scheme-specific entry, then the theme's own flat
	// entry, then nil (ApplyColorConfig's own cue to keep the hardcoded
	// default).
	pick := func(key string, flat *string) *string {
		if v, ok := cfgScheme[key]; ok {
			return &v
		}
		if flat != nil {
			return flat
		}
		if v, ok := themeScheme[key]; ok {
			return &v
		}
		if v, ok := theme.Flat[key]; ok {
			return &v
		}
		return nil
	}

	return ColorsConfig{
		Background: pick("background", cfg.Colors.Background),

		OutcomeOK:          pick("outcome_ok", cfg.Colors.OutcomeOK),
		OutcomeChanged:     pick("outcome_changed", cfg.Colors.OutcomeChanged),
		OutcomeSkipped:     pick("outcome_skipped", cfg.Colors.OutcomeSkipped),
		OutcomeFailed:      pick("outcome_failed", cfg.Colors.OutcomeFailed),
		OutcomeUnreachable: pick("outcome_unreachable", cfg.Colors.OutcomeUnreachable),

		NothingReported: pick("nothing_reported", cfg.Colors.NothingReported),
		Annotation:      pick("annotation", cfg.Colors.Annotation),

		TaskTitle:    pick("task_title", cfg.Colors.TaskTitle),
		PlayTitle:    pick("play_title", cfg.Colors.PlayTitle),
		StandardText: pick("standard_text", cfg.Colors.StandardText),

		ProgressText: pick("progress_text", cfg.Colors.ProgressText),
		TabText:      pick("tab_text", cfg.Colors.TabText),

		ChromeLive:    pick("chrome_live", cfg.Colors.ChromeLive),
		ChromeRevisit: pick("chrome_revisit", cfg.Colors.ChromeRevisit),
		ChromeCheck:   pick("chrome_check", cfg.Colors.ChromeCheck),
		DiffChrome:    pick("diff_chrome", cfg.Colors.DiffChrome),

		SelectedRowText:       pick("selected_row_text", cfg.Colors.SelectedRowText),
		SelectedRowBackground: pick("selected_row_background", cfg.Colors.SelectedRowBackground),

		ProgressFill: pick("progress_fill", cfg.Colors.ProgressFill),
		Warning:      pick("warning", cfg.Colors.Warning),
		Ignored:      pick("ignored", cfg.Colors.Ignored),

		Selection: pick("selection", cfg.Colors.Selection),

		AsyncError: pick("async_error", cfg.Colors.AsyncError),

		DiffAdded:         pick("diff_added", cfg.Colors.DiffAdded),
		DiffRemoved:       pick("diff_removed", cfg.Colors.DiffRemoved),
		DiffHunk:          pick("diff_hunk", cfg.Colors.DiffHunk),
		DiffUnmatchedTask: pick("diff_unmatched_task", cfg.Colors.DiffUnmatchedTask),

		StatusBenign: pick("status_benign", cfg.Colors.StatusBenign),

		KeyHighlight:   pick("key_highlight", cfg.Colors.KeyHighlight),
		ValueHighlight: pick("value_highlight", cfg.Colors.ValueHighlight),
		Heading:        pick("heading", cfg.Colors.Heading),

		SummaryFailed: pick("summary_failed", cfg.Colors.SummaryFailed),

		SearchMatchForeground:        pick("search_match_foreground", cfg.Colors.SearchMatchForeground),
		SearchMatchBackground:        pick("search_match_background", cfg.Colors.SearchMatchBackground),
		SearchCurrentMatchForeground: pick("search_current_match_foreground", cfg.Colors.SearchCurrentMatchForeground),
		SearchCurrentMatchBackground: pick("search_current_match_background", cfg.Colors.SearchCurrentMatchBackground),
	}
}

// mergeColorMap merges project/global/system [colors.dark]/[colors.light]
// sub-tables the same per-key rule mergeColors' own firstNonNil calls
// apply to the flat fields: a key present in a higher-priority tier's map
// wins outright; a key that tier's map simply doesn't have falls through
// to the next one, rather than the whole map winning or losing as a unit.
// Returns nil, not an allocated-but-empty map, when all three tiers are
// empty - keeps a fully-unconfigured SettingsConfig equal to its own
// zero value (reflect.DeepEqual, used in tests), and ResolveEffectiveColors'
// own map lookups treat a nil map identically to an empty one regardless.
func mergeColorMap(project, global, system map[string]string) map[string]string {
	if len(project) == 0 && len(global) == 0 && len(system) == 0 {
		return nil
	}
	merged := make(map[string]string, len(system)+len(global)+len(project))
	for k, v := range system {
		merged[k] = v
	}
	for k, v := range global {
		merged[k] = v
	}
	for k, v := range project {
		merged[k] = v
	}
	return merged
}
