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

// Named vars for every plain tview tag-name color string this app uses,
// one per semantic meaning listed in design-docs/Colors.md's own Quick
// reference table - previously these were bare string literals
// (`"green"`, `"maroon"`, ...) repeated at each call site, then named
// `const`s once design-docs/Colors.md's own naming pass finished. Now
// `var`s instead, initialized with those exact same defaults, so
// ApplyColorConfig (below) can overwrite them from design-docs/
// ColorConfig.md's own `[colors]` config once, at startup, before any of
// them is ever read - every existing color-emitting function (ColorTag,
// TaskLabel, BuildOutputTabs, RevisitStatusColor, ...) needed no
// signature change at all for this: only the literal each one referenced
// moved into a shared var, twice now (const -> var, string -> string).
//
// NothingReportedColor/SelectedRowTextColor/ProgressFillColor/
// WarningColor/IgnoredColor moved here from tui_style.go (where they
// predated this file and its own naming pass) now that ApplyColorConfig
// needs to touch every one of them uniformly - no longer worth keeping
// two files' full complement of color vars apart from each other.
// diff.DiffChromeColor (internal/diff) and search.go's own unexported
// searchMatchForegroundColor/searchMatchBackgroundColor/
// searchCurrentMatchForegroundColor/searchCurrentMatchBackgroundColor
// still stay where they are - the former belongs to a different package
// entirely (see diff.ApplyColorConfig), the latter are already right
// next to the one thing (search.go) that reads them besides this file's
// own ApplyColorConfig.
//
// Deliberately one var per row even where several already share today's
// default value (e.g. OutcomeOKColor/DiffAddedColor are both "green"
// right now) - each names an independently meaningful color design-docs/
// Colors.md already lists as its own row, kept apart unless a genuine,
// structural reason to merge them turns up (see design-docs/Colors.md's
// own "Merged so far" list for the ones that already have).
package uikit

import (
	"fmt"
	"os"

	"code.aw.net/claude/tangsible/internal/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// BackgroundColor is the whole TUI's own base background - every unstyled
// cell, not one specific row/element the way every other var here is.
// Defaults to "black", matching tview's own hardcoded
// tview.Styles.PrimitiveBackgroundColor default (tcell.ColorBlack) - this
// app never overrode that before this var existed, so the background was
// always black regardless of General.ColorScheme. ApplyColorConfig below
// is what actually makes an override reach the screen: setting this var
// alone changes nothing by itself, since nothing reads BackgroundColor
// directly the way TaskTitleColor etc. are read per-row - every
// tview.Box (essentially every primitive in this app) reads
// tview.Styles.PrimitiveBackgroundColor once, at construction, and
// paints it across its own rect.
var BackgroundColor = "black"

// Outcome colors - see ColorTag, the single place that actually reads
// these (playbook.Outcome -> tag name).
var (
	OutcomeOKColor          = "green"
	OutcomeChangedColor     = "yellow"
	OutcomeSkippedColor     = "teal"
	OutcomeFailedColor      = "red"
	OutcomeUnreachableColor = "maroon"
)

// NothingReportedColor covers two meanings that read the same way -
// "nothing to show here (yet)": a host AllHosts knows about run-wide but
// that hasn't reported for *this* task yet (tui_layout.go), and recap.go's
// own zero-count de-emphasis (a field whose count is 0) - plus, by the
// same reasoning, host/hosts' own ping-still-in-flight dot and the
// revisit list's own aborted-run color (design-docs/Colors.md's own
// "Merged so far" list).
var NothingReportedColor = "gray"

// AnnotationColor marks a supplementary annotation (a task's own source
// location in the drill-down, host.go's "(from fact cache)" note, the
// search dialog's own hint text) - kept distinct from
// NothingReportedColor even though today's default value is identical:
// an annotation is present and simply secondary, not a "nothing to show
// yet" placeholder.
var AnnotationColor = "gray"

// Row-title text colors.
var (
	TaskTitleColor = "silver" // a task row's own title text.
	PlayTitleColor = "white"  // a play row's own title text.
)

// StandardTextColor is a shared, plain "just text" color, for content
// that isn't itself an outcome/heading/title but was previously
// borrowing one of those colors anyway (host.go's own host-list
// hostnames, the revisit list's timestamp/command-line text, and the
// recap's own per-host hostname+duration prefix - all originally
// PlayTitleColor or KeyHighlightColor by coincidence, not because any of
// them are actually a play title or a key/heading). Also the default for
// every piece of text that would otherwise fall back to tview's own
// white: the body of every tab/view (drill-down, diff drill-down, host
// tabs, the 'o' console-output view), untagged tree rows (TreeList - the
// error-output lines, the recap's summary sentence), and every dialog's
// title, field/checkbox labels and plain text, the help dialog's
// descriptions included.
var StandardTextColor = "silver"

// DialogBorderColor is every modal dialog's own border (the re-run form,
// the search/filter dialogs, template's "Change host" dialog, the '?'
// help dialog) - these
// previously had no configurable color at all, just tview's own hardcoded
// tview.Styles.BorderColor default (white), the same kind of gap
// BackgroundColor used to leave for the whole TUI's base background
// before it existed. Defaults to StandardTextColor's own default value
// rather than tview's "white", so an unconfigured border reads as plain
// chrome, not an emphasized element. Read directly via tcell.GetColor at
// each dialog's own construction (Box.SetBorderColor takes a tcell.Color,
// not a tag string), same "resolved once, at startup, before any dialog
// exists" timing every other var here already relies on - just a plain
// tcell.Color reader rather than a tag-string one.
var DialogBorderColor = "silver"

// ControlTextColor/ControlBackgroundColor are every input control's own
// colors: text input fields, buttons and checkbox boxes (the re-run form,
// the search/filter dialogs, template's "Change host" dialog) - applied
// via StyleInputField/StyleButton/StyleForm. A focused button or checkbox
// shows the two swapped, tview's own convention for "this one has focus".
// Default to exactly what tview itself used before these were
// configurable (its Styles.PrimaryTextColor white on
// Styles.ContrastBackgroundColor blue). Not used by the yellow in-tab
// search prompt, which deliberately looks the same everywhere.
var (
	ControlTextColor       = "white"
	ControlBackgroundColor = "blue"
)

// Chrome text/backgrounds that can't be one tcell.Style the way
// BarStyle/ReplayBarStyle/CheckBarStyle/diff.DiffChromeStyle are - these
// are baked into plain tag strings instead, since a progress-sweep fill
// or a tab bar's own per-tab coloring varies per-column within one line
// (see ProgressFillLineAt, TabbedPane.renderHeader).
var (
	ProgressTextColor = "white" // top bar's progress-sweep text.

	// TabTextColor is the tab bar's own "light" color, used both as an
	// inactive tab's own label text (chrome-color background) and,
	// swapped, as the active tab's own background (chrome-color text) -
	// see TabbedPane.renderHeader. One var, not two: these are always
	// the same two halves of one fg/bg swap, never independently
	// meaningful - an inactive tab and the active tab are each other's
	// color-inverse by design.
	TabTextColor = "white"
)

// Chrome colors' own plain tag-name equivalents - BarStyle/ReplayBarStyle/
// CheckBarStyle (tui_style.go) are built from these same vars (see
// buildChromeStyle) for the chrome bars themselves; these are for the
// places that additionally need the bare name instead (a tab bar's own
// headerColorName, a progress-fill line's unfilled background - see
// TabbedPane.renderHeader, liveSession.chromeColorName).
// diff.DiffChromeColor ("fuchsia", internal/diff) is this same idea for
// the fourth, diff-mode chrome color, resolved by diff.ApplyColorConfig
// instead of this file's own ApplyColorConfig below.
var (
	ChromeLiveColor    = "navy"
	ChromeRevisitColor = "purple"
	ChromeCheckColor   = "olive"
)

// SelectedRowTextColor is every selected row's own text color, app-wide -
// needs to read as unambiguously black against the light backgrounds
// those rows use, so it defaults to a fixed hex value rather than
// tcell's named "black": some terminal themes remap the base-16 ANSI
// "black" slot to a dark gray rather than true black (the same
// base-16-vs-fixed-value trap already documented for red/maroon, see
// ColorTag).
var SelectedRowTextColor = "#1a1a1a"

// SelectedRowBackgroundColor is the light background every selected
// row's SelectedRowTextColor text sits on, across the tree, recap,
// host/hosts, diff, and revisit views - and the highlighted entry of a
// field's completion drop-down (StyleAutocomplete).
var SelectedRowBackgroundColor = "lightgray"

// ProgressFillColor is topBarText's own "headline as a progress bar"
// fill - defaults to a fixed hex value, not tcell's named "green", for
// the same reason SelectedRowTextColor isn't named "black": tcell's
// nearest-color search prefers an EXACT match to a base-16 slot's own
// nominal RGB even when given as a hex value (confirmed against tcell's
// colorfit.go), and some terminal themes remap that slot to something
// that reads poorly under white text. The default is deliberately not
// the nominal xterm green (#008000) for exactly that reason - nudged
// dark enough to keep white bold text readable while still landing on a
// fixed, non-remappable extended-256 slot.
var ProgressFillColor = "#146414"

// WarningColor marks a ⚠ indicator wherever one appears (taskLabel's own
// collapsed-row aggregate marker, hostLabel's own per-host marker,
// recap.go's own "warnings" category) - a pinkish shade by default,
// matching real ansible-playbook's own default [WARNING] color.
var WarningColor = "hotpink"

// IgnoredColor marks recap.go's own "ignored" category (design-docs/
// OwnCallbackPlugin.md) - deliberately distinct from both WarningColor
// (an unrelated cross-cutting concept, a task's own "warnings" field)
// and every outcome color in ColorTag (an ignored failure still records
// as OutcomeFailed - see TaskNode.Ignored's own doc comment - so reusing
// red there would blur the very distinction this color exists to draw).
var IgnoredColor = "orange"

// SelectionColor marks the filter dialog's currently-selected filter
// mode's own "*" marker.
var SelectionColor = "aqua"

// AsyncErrorColor marks an async fetch/render failure heading (host/hosts,
// template) - kept separate from OutcomeFailedColor: it's Tangsible's own
// Go code failing to get a result at all, not a playbook.Outcome.
var AsyncErrorColor = "red"

// Diff tab colors (ColorizedUnifiedDiff, within the drill-down).
var (
	DiffAddedColor   = "green"
	DiffRemovedColor = "red"
	DiffHunkColor    = "teal"
)

// DiffUnmatchedTaskColor marks the `tangsible diff` verb's own "Task only
// present in the X run" note (internal/diff).
var DiffUnmatchedTaskColor = "yellow"

// StatusBenignColor is the run-completion status line's own "exit 4, but
// a real unreachable host, not a parser error" case (StatusRowText) -
// its success/failure cases reuse OutcomeOKColor/OutcomeFailedColor
// directly instead, but this one stays its own var: it isn't the same
// thing as OutcomeChangedColor (also yellow by default) just because
// they share a default value - "some hosts were unreachable" and "a
// task reported changed" are unrelated facts that happen to get the
// same attention-color today.
var StatusBenignColor = "yellow"

// KeyHighlightColor/ValueHighlightColor are the shared green-key/
// lightsteelblue-value convention - one implementation
// (HighlightYAMLKeys/HighlightJSONKeys/HighlightKeyValueLines), used by
// both `host`/`hosts` and the run drill-down's own tabs for the "key"
// half of an actual key:value pair (a highlighted YAML/JSON line, or a
// synthesized "Label: value" line like Role:/Task:/Status:/Group:), and
// for the key column of the '?' help dialog - deliberately distinct from HeadingColor below even though both
// default to the same green, since a section heading isn't a key with
// no value, and the two were found to want independently overridable
// config keys.
var (
	KeyHighlightColor   = "green"
	ValueHighlightColor = "lightsteelblue"
)

// HeadingColor is a plain section-heading color - `uikit.SectionLabel`'s
// Output/Items headings, host.go's own hostSectionHeading, the recap's
// "Summary" heading, the error-output block's "Error output:" heading,
// and the '?' help dialog's section titles. Kept as its own var, not folded into KeyHighlightColor, even
// though both default to the same green: a heading has no accompanying
// value the way a key does, and splitting them lets a config override
// change one without the other.
var HeadingColor = "green"

// SummaryFailedColor is host/hosts' own Summary tab color for a
// genuinely failed live result - kept separate from OutcomeFailedColor
// for now. Its own sibling cases (a real live Unreachable result, and
// the "Host unavailable." skip-the-attempt-entirely placeholder) were
// merged into OutcomeUnreachableColor instead - both read the same way
// as the tree's own Unreachable outcome - see design-docs/Colors.md's
// "Merged so far" list.
var SummaryFailedColor = "red"

// TerminalDefaultColor is the config value meaning "the terminal's own
// default color" (tcell.ColorDefault: nothing is sent, so the terminal's
// configured default foreground/background shows) - for using the
// terminal's own colors as Tangsible's base pair. Works both via
// tcell.GetColor and as a tview tag ("[default]"), the two ways colors
// are consumed here.
const TerminalDefaultColor = "default"

// terminalDefaultKeys are the only keys TerminalDefaultColor is valid
// for: the base pair, plain text on the plain background. Every other key
// is either an accent meant to differ from plain text, or drawn as a
// background somewhere (selected-row blocks, the active tab, chrome,
// search highlights), where "the terminal's default" would make the
// highlight vanish.
var terminalDefaultKeys = map[string]bool{"background": true, "standard_text": true}

// ResolveColorOverride validates override against tcell.GetColor (the
// same resolution tcell.Style construction below already needs, and a
// reasonable proxy for "will tview's own tag parser accept this too" -
// see design-docs/ColorConfig.md's own Validation section) and returns
// its dereferenced value if valid, fallback otherwise - covering both
// "key absent" (override == nil) and "key set to something neither a
// recognized color name nor a #rrggbb hex string" with the exact same
// one-key-at-a-time tolerance ReadTOMLFile's own parse-failure path
// already has elsewhere in this app: one bad value costs only that one
// key, warned once, never the whole config. Exported so
// diff.ApplyColorConfig can reuse it for its own single key
// (diff_chrome) - internal/diff already imports this package, so both
// packages agree on exactly what counts as a valid override and warn
// identically, rather than diff.go hand-rolling a second copy of this
// logic for one field.
//
// "default" - the terminal's own default color, tcell.ColorDefault - is
// accepted only for the keys in terminalDefaultKeys. It has to be checked
// by name: tcell.GetColor returns that same ColorDefault for an unknown
// name, which is what the validity check below relies on.
func ResolveColorOverride(key string, override *string, fallback string) string {
	if override == nil {
		return fallback
	}
	if *override == TerminalDefaultColor {
		if terminalDefaultKeys[key] {
			return *override
		}
		pendingColorWarnings = append(pendingColorWarnings, fmt.Sprintf("tangsible: %q for [colors] %s is only allowed for background and standard_text, using default %q", TerminalDefaultColor, key, fallback))
		return fallback
	}
	if tcell.GetColor(*override) == tcell.ColorDefault {
		pendingColorWarnings = append(pendingColorWarnings, fmt.Sprintf("tangsible: invalid color %q for [colors] %s, using default %q", *override, key, fallback))
		return fallback
	}
	return *override
}

// pendingColorWarnings collects ResolveColorOverride's own invalid-color
// messages instead of printing them immediately - a real user report:
// ApplyColorConfig runs before any TUI-showing Verb's own tview.
// Application starts, so an immediate print sits on the terminal's
// normal screen for well under a second before tview's alternate-screen
// swap visually covers it, and isn't reliably restored on quit either
// (terminal-dependent - some clear scrollback on entering the alternate
// screen). PrintColorWarnings, called once a Verb's own TUI (if any) has
// actually closed, is the fix - the same "collect while the alternate
// screen might be up, print for real only once it's gone" reasoning
// runner.StreamStderr/lastStderr already apply to a generation's own
// stderr (design-docs/ErrorOutput.md), applied here to config-validation
// warnings instead of ansible-playbook's own stderr.
var pendingColorWarnings []string

// PrintColorWarnings prints (to stderr) and clears every warning
// ResolveColorOverride has collected since the last call - see
// pendingColorWarnings' own doc comment for why this is deferred rather
// than printed immediately. A no-op when nothing's pending (the common
// case), so every caller can call this unconditionally right after its
// own Verb function returns, TUI or not.
func PrintColorWarnings() {
	for _, w := range pendingColorWarnings {
		fmt.Fprintln(os.Stderr, w)
	}
	pendingColorWarnings = nil
}

// buildChromeStyle builds a bold tcell.Style from fg/bg color-name
// strings - shared by BarStyle/ReplayBarStyle/CheckBarStyle/
// SearchBarStyle's own initial computation (tui_style.go) and
// ApplyColorConfig's later recomputation below, so both always agree on
// exactly how a chrome bar's Style is derived from its own plain-string
// color vars. fg is a literal "white"/"black", not one of this file's
// own vars, for the chrome bars specifically - design-docs/
// ColorConfig.md's own Open questions #8: the chrome bars' shared
// foreground has no backing var at all (deliberately, for now), so
// there's nothing for a config key to override there yet.
func buildChromeStyle(fg, bg string) tcell.Style {
	return tcell.StyleDefault.Foreground(tcell.GetColor(fg)).Background(tcell.GetColor(bg)).Bold(true)
}

// ApplyColorConfig overwrites every color var in this package (and, via
// buildChromeStyle, the derived BarStyle/ReplayBarStyle/CheckBarStyle/
// SearchBarStyle - tui_style.go) from cfg, design-docs/ColorConfig.md's
// own resolved `[colors]` config - called exactly once, from main.go,
// before any TUI is constructed (same point colorEnabled/useColor are
// already resolved today). A cfg field left nil (the key was absent
// everywhere in the cascade - see config.MergedSettingsConfig) leaves
// that var at its own hardcoded default, ResolveColorOverride's own nil
// branch; every other case is validated the same way.
//
// diff.DiffChromeColor/DiffChromeStyle are NOT touched here - internal/
// diff owns its own ApplyColorConfig for exactly that one field, since
// this package can't reach into another package's own vars, and main.go
// calls both.
//
// BackgroundColor is the one field this function does something with
// beyond "overwrite a var" - it additionally pushes the resolved value
// into tview.Styles.PrimitiveBackgroundColor, the actual mechanism that
// makes it reach the screen (see BackgroundColor's own doc comment).
func ApplyColorConfig(cfg config.ColorsConfig) {
	BackgroundColor = ResolveColorOverride("background", cfg.Background, BackgroundColor)
	tview.Styles.PrimitiveBackgroundColor = tcell.GetColor(BackgroundColor)

	OutcomeOKColor = ResolveColorOverride("outcome_ok", cfg.OutcomeOK, OutcomeOKColor)
	OutcomeChangedColor = ResolveColorOverride("outcome_changed", cfg.OutcomeChanged, OutcomeChangedColor)
	OutcomeSkippedColor = ResolveColorOverride("outcome_skipped", cfg.OutcomeSkipped, OutcomeSkippedColor)
	OutcomeFailedColor = ResolveColorOverride("outcome_failed", cfg.OutcomeFailed, OutcomeFailedColor)
	OutcomeUnreachableColor = ResolveColorOverride("outcome_unreachable", cfg.OutcomeUnreachable, OutcomeUnreachableColor)

	NothingReportedColor = ResolveColorOverride("nothing_reported", cfg.NothingReported, NothingReportedColor)
	AnnotationColor = ResolveColorOverride("annotation", cfg.Annotation, AnnotationColor)

	TaskTitleColor = ResolveColorOverride("task_title", cfg.TaskTitle, TaskTitleColor)
	PlayTitleColor = ResolveColorOverride("play_title", cfg.PlayTitle, PlayTitleColor)
	StandardTextColor = ResolveColorOverride("standard_text", cfg.StandardText, StandardTextColor)
	DialogBorderColor = ResolveColorOverride("dialog_border", cfg.DialogBorder, DialogBorderColor)
	ControlTextColor = ResolveColorOverride("control_text", cfg.ControlText, ControlTextColor)
	ControlBackgroundColor = ResolveColorOverride("control_background", cfg.ControlBackground, ControlBackgroundColor)

	ProgressTextColor = ResolveColorOverride("progress_text", cfg.ProgressText, ProgressTextColor)
	TabTextColor = ResolveColorOverride("tab_text", cfg.TabText, TabTextColor)

	ChromeLiveColor = ResolveColorOverride("chrome_live", cfg.ChromeLive, ChromeLiveColor)
	ChromeRevisitColor = ResolveColorOverride("chrome_revisit", cfg.ChromeRevisit, ChromeRevisitColor)
	ChromeCheckColor = ResolveColorOverride("chrome_check", cfg.ChromeCheck, ChromeCheckColor)

	SelectedRowTextColor = ResolveColorOverride("selected_row_text", cfg.SelectedRowText, SelectedRowTextColor)
	SelectedRowBackgroundColor = ResolveColorOverride("selected_row_background", cfg.SelectedRowBackground, SelectedRowBackgroundColor)

	ProgressFillColor = ResolveColorOverride("progress_fill", cfg.ProgressFill, ProgressFillColor)
	WarningColor = ResolveColorOverride("warning", cfg.Warning, WarningColor)
	IgnoredColor = ResolveColorOverride("ignored", cfg.Ignored, IgnoredColor)

	SelectionColor = ResolveColorOverride("selection", cfg.Selection, SelectionColor)

	AsyncErrorColor = ResolveColorOverride("async_error", cfg.AsyncError, AsyncErrorColor)

	DiffAddedColor = ResolveColorOverride("diff_added", cfg.DiffAdded, DiffAddedColor)
	DiffRemovedColor = ResolveColorOverride("diff_removed", cfg.DiffRemoved, DiffRemovedColor)
	DiffHunkColor = ResolveColorOverride("diff_hunk", cfg.DiffHunk, DiffHunkColor)
	DiffUnmatchedTaskColor = ResolveColorOverride("diff_unmatched_task", cfg.DiffUnmatchedTask, DiffUnmatchedTaskColor)

	StatusBenignColor = ResolveColorOverride("status_benign", cfg.StatusBenign, StatusBenignColor)

	KeyHighlightColor = ResolveColorOverride("key_highlight", cfg.KeyHighlight, KeyHighlightColor)
	ValueHighlightColor = ResolveColorOverride("value_highlight", cfg.ValueHighlight, ValueHighlightColor)
	HeadingColor = ResolveColorOverride("heading", cfg.Heading, HeadingColor)

	SummaryFailedColor = ResolveColorOverride("summary_failed", cfg.SummaryFailed, SummaryFailedColor)

	searchMatchForegroundColor = ResolveColorOverride("search_match_foreground", cfg.SearchMatchForeground, searchMatchForegroundColor)
	searchMatchBackgroundColor = ResolveColorOverride("search_match_background", cfg.SearchMatchBackground, searchMatchBackgroundColor)
	searchCurrentMatchForegroundColor = ResolveColorOverride("search_current_match_foreground", cfg.SearchCurrentMatchForeground, searchCurrentMatchForegroundColor)
	searchCurrentMatchBackgroundColor = ResolveColorOverride("search_current_match_background", cfg.SearchCurrentMatchBackground, searchCurrentMatchBackgroundColor)

	// Every chrome-bar/search-bar Style var is rebuilt from the vars just
	// resolved above, via the exact same formula tui_style.go's own
	// package-init expressions use - buildChromeStyle - so a config
	// override actually reaches the real chrome bars, not just their own
	// plain-tag-string equivalents (ChromeLiveColor etc.) elsewhere.
	BarStyle = buildChromeStyle("white", ChromeLiveColor)
	ReplayBarStyle = buildChromeStyle("white", ChromeRevisitColor)
	CheckBarStyle = buildChromeStyle("white", ChromeCheckColor)
	SearchBarStyle = buildChromeStyle(searchMatchForegroundColor, searchMatchBackgroundColor)
}
