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
	OutcomeOK          *string `toml:"outcome_ok"`
	OutcomeChanged     *string `toml:"outcome_changed"`
	OutcomeSkipped     *string `toml:"outcome_skipped"`
	OutcomeFailed      *string `toml:"outcome_failed"`
	OutcomeUnreachable *string `toml:"outcome_unreachable"`

	NothingReported *string `toml:"nothing_reported"`
	Annotation      *string `toml:"annotation"`

	TaskTitle *string `toml:"task_title"`
	PlayTitle *string `toml:"play_title"`

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

	SummaryFailed *string `toml:"summary_failed"`

	SearchMatchForeground        *string `toml:"search_match_foreground"`
	SearchMatchBackground        *string `toml:"search_match_background"`
	SearchCurrentMatchForeground *string `toml:"search_current_match_foreground"`
	SearchCurrentMatchBackground *string `toml:"search_current_match_background"`
}

// mergeColors is MergedSettingsConfig's own per-field cascade, pulled out
// purely to keep that function from being one unbroken 34-line stretch on
// top of General's own 9 - same firstNonNil rule, one call per field.
func mergeColors(project, global, system ColorsConfig) ColorsConfig {
	return ColorsConfig{
		OutcomeOK:          firstNonNil(project.OutcomeOK, global.OutcomeOK, system.OutcomeOK),
		OutcomeChanged:     firstNonNil(project.OutcomeChanged, global.OutcomeChanged, system.OutcomeChanged),
		OutcomeSkipped:     firstNonNil(project.OutcomeSkipped, global.OutcomeSkipped, system.OutcomeSkipped),
		OutcomeFailed:      firstNonNil(project.OutcomeFailed, global.OutcomeFailed, system.OutcomeFailed),
		OutcomeUnreachable: firstNonNil(project.OutcomeUnreachable, global.OutcomeUnreachable, system.OutcomeUnreachable),

		NothingReported: firstNonNil(project.NothingReported, global.NothingReported, system.NothingReported),
		Annotation:      firstNonNil(project.Annotation, global.Annotation, system.Annotation),

		TaskTitle: firstNonNil(project.TaskTitle, global.TaskTitle, system.TaskTitle),
		PlayTitle: firstNonNil(project.PlayTitle, global.PlayTitle, system.PlayTitle),

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

		SummaryFailed: firstNonNil(project.SummaryFailed, global.SummaryFailed, system.SummaryFailed),

		SearchMatchForeground:        firstNonNil(project.SearchMatchForeground, global.SearchMatchForeground, system.SearchMatchForeground),
		SearchMatchBackground:        firstNonNil(project.SearchMatchBackground, global.SearchMatchBackground, system.SearchMatchBackground),
		SearchCurrentMatchForeground: firstNonNil(project.SearchCurrentMatchForeground, global.SearchCurrentMatchForeground, system.SearchCurrentMatchForeground),
		SearchCurrentMatchBackground: firstNonNil(project.SearchCurrentMatchBackground, global.SearchCurrentMatchBackground, system.SearchCurrentMatchBackground),
	}
}
