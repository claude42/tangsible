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

// Named constants for every plain tview tag-name color string this app
// uses, one per semantic meaning listed in design-docs/Colors.md's own
// Quick reference table - previously these were bare string literals
// (`"green"`, `"maroon"`, ...) repeated at each call site. Kept apart from
// tui_style.go's own pre-existing NothingReportedColor/SelectedRowTextColor/ProgressFillColor/
// WarningColor/IgnoredColor (already named, unchanged here) purely to
// avoid growing that already-large file further - not a deliberate split
// by kind. diff.DiffChromeStyle/DiffChromeColor (internal/diff) and
// search.go's own unexported searchMatchForegroundColor/searchMatchBackgroundColor/
// searchCurrentMatchForegroundColor/searchCurrentMatchBackgroundColor stay where
// they are too, for the same reason.
//
// Deliberately one constant per row even where several already share
// today's default value (e.g. OutcomeOKColor/DiffAddedColor are both
// "green" right now) - each names an independently meaningful color
// design-docs/Colors.md already lists as its own row, kept apart unless
// a genuine, structural reason to merge them turns up (see design-docs/
// Colors.md's own "Merged so far" list for the ones that already have).
// See design-docs/ColorConfig.md for the per-key configurability this is
// meant to set up.
package uikit

// Outcome colors - see ColorTag, the single place that actually reads
// these (playbook.Outcome -> tag name).
const (
	OutcomeOKColor          = "green"
	OutcomeChangedColor     = "yellow"
	OutcomeSkippedColor     = "teal"
	OutcomeFailedColor      = "red"
	OutcomeUnreachableColor = "maroon"
)

// AnnotationColor marks a supplementary annotation (a task's own source
// location in the drill-down, host.go's "(from fact cache)" note, the
// search dialog's own hint text) - kept distinct from
// NothingReportedColor (tui_style.go) even though today's default value
// is identical: an annotation is present and simply secondary, not a
// "nothing to show yet" placeholder.
const AnnotationColor = "gray"

// Row-title text colors.
const (
	TaskTitleColor = "silver" // a task row's own title text.
	PlayTitleColor = "white"  // a play row's own title text.
)

// Chrome text/backgrounds that can't be one tcell.Style the way
// BarStyle/ReplayBarStyle/CheckBarStyle/diff.DiffChromeStyle are - these
// are baked into plain tag strings instead, since a progress-sweep fill
// or a tab bar's own per-tab coloring varies per-column within one line
// (see ProgressFillLineAt, TabbedPane.renderHeader).
const (
	ProgressTextColor = "white" // top bar's progress-sweep text.

	// TabTextColor is the tab bar's own "light" color, used both as an
	// inactive tab's own label text (chrome-color background) and,
	// swapped, as the active tab's own background (chrome-color text) -
	// see TabbedPane.renderHeader. One constant, not two: these are
	// always the same two halves of one fg/bg swap, never independently
	// meaningful - an inactive tab and the active tab are each other's
	// color-inverse by design.
	TabTextColor = "white"
)

// Chrome colors' own plain tag-name equivalents - BarStyle/ReplayBarStyle/
// CheckBarStyle (tui_style.go) bake the same colors into a tcell.Style for
// the chrome bars themselves; these are for the places that need the bare
// name instead (a tab bar's own headerColorName, a progress-fill line's
// unfilled background - see TabbedPane.renderHeader,
// liveSession.chromeColorName). diff.DiffChromeColor ("fuchsia",
// internal/diff) is this same idea for the fourth, diff-mode chrome color.
const (
	ChromeLiveColor    = "navy"
	ChromeRevisitColor = "purple"
	ChromeCheckColor   = "olive"
)

// SelectedRowBackgroundColor is the light background every selected row's
// SelectedRowTextColor (tui_style.go) text sits on, across the tree, recap, host/
// hosts, diff, and revisit views.
const SelectedRowBackgroundColor = "lightgray"

// SelectionColor marks the filter dialog's currently-selected filter
// mode's own "*" marker.
const SelectionColor = "aqua"

// AsyncErrorColor marks an async fetch/render failure heading (host/hosts,
// template) - kept separate from OutcomeFailedColor: it's Tangsible's own
// Go code failing to get a result at all, not a playbook.Outcome. The
// drill-down Output tab's own Error section (a task's stderr) used to
// have its own OutputErrorColor for the same default value, but that one
// merged into OutcomeFailedColor directly - stderr from a task that
// actually ran genuinely is that task's own Failed-outcome content.
const AsyncErrorColor = "red"

// Diff tab colors (ColorizedUnifiedDiff, within the drill-down).
const (
	DiffAddedColor   = "green"
	DiffRemovedColor = "red"
	DiffHunkColor    = "teal"
)

// DiffUnmatchedTaskColor marks the `tangsible diff` verb's own "Task only
// present in the X run" note (internal/diff).
const DiffUnmatchedTaskColor = "yellow"

// StatusBenignColor is the run-completion status line's own "exit 4, but
// a real unreachable host, not a parser error" case (StatusRowText) -
// its success/failure cases reuse OutcomeOKColor/OutcomeFailedColor
// directly instead, but this one stays its own constant: it isn't the
// same thing as OutcomeChangedColor (also yellow) just because they
// share a default value - "some hosts were unreachable" and "a task
// reported changed" are unrelated facts that happen to get the same
// attention-color today.
const StatusBenignColor = "yellow"

// KeyHighlightColor/ValueHighlightColor are the shared green-key/
// lightsteelblue-value convention - one implementation
// (HighlightYAMLKeys/HighlightJSONKeys/HighlightKeyValueLines), used by
// both `host`/`hosts` and the run drill-down's own tabs.
const (
	KeyHighlightColor   = "green"
	ValueHighlightColor = "lightsteelblue"
)

// SummaryFailedColor is host/hosts' own Summary tab color for a
// genuinely failed live result - kept separate from OutcomeFailedColor
// for now. Its own sibling cases (a real live Unreachable result, and
// the "Host unavailable." skip-the-attempt-entirely placeholder) were
// merged into OutcomeUnreachableColor instead - both read the same way
// as the tree's own Unreachable outcome - see design-docs/Colors.md's
// "Merged so far" list.
const SummaryFailedColor = "red"
