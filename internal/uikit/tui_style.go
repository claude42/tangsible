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

package uikit

import (
	"time"
)

// BarStyle is used for every non-list chrome bar (top bar, bottom bar, and
// the output drill-down page's own top/bottom bars) - white on blue, bold
// by default. Built via buildChromeStyle (colors.go) from ChromeLiveColor
// rather than a hardcoded tcell.ColorNavy literal, so design-docs/
// ColorConfig.md's own chrome_live config key actually reaches this bar,
// not just ChromeLiveColor's own plain-tag-string uses elsewhere -
// ApplyColorConfig (colors.go) rebuilds this var the same way after
// resolving any config override.
var BarStyle = buildChromeStyle("white", ChromeLiveColor)

// ReplayBarStyle is design-docs/Revisit.md's own chrome for a revisited
// (historical) run - the same bars BarStyle normally covers, plus the
// two-pane divider's own background (see NewLiveTUI's chromeStyle/chromeBg
// locals), switch to this instead for as long as the session is showing
// replayed data rather than a live/finished one. Built from
// ChromeRevisitColor the same way BarStyle is built from ChromeLiveColor
// - see its own doc comment.
var ReplayBarStyle = buildChromeStyle("white", ChromeRevisitColor)

// CheckBarStyle is the chrome for a session whose current generation was
// invoked with --check (a dry run: ansible-playbook still runs for real,
// but most modules only report what they *would* do rather than doing
// it) - the same bars BarStyle normally covers switch to this instead for
// as long as the running/finished generation is a --check one, so "this
// didn't actually change anything" is visible in the chrome itself rather
// than only discoverable by reading --check off the command line after
// the fact. Built from ChromeCheckColor the same way BarStyle is built
// from ChromeLiveColor - see its own doc comment.
var CheckBarStyle = buildChromeStyle("white", ChromeCheckColor)

// DebugRunningBarStyle/DebugPausedBarStyle (design-docs/DebugMode.md's
// "Stage 1 decisions") are a debug session's own two extra chrome states,
// built the same way BarStyle/CheckBarStyle are - applied dynamically,
// not just once at construction the way every other chrome style here is
// (livesession_debug.go's own debugRefreshChrome): a debug session's
// phase (idle/running/paused/finished) changes repeatedly over its own
// lifetime, unlike --check/revisit, which are each fixed for a whole
// session.
var DebugRunningBarStyle = buildChromeStyle("white", ChromeDebugRunningColor)
var DebugPausedBarStyle = buildChromeStyle("white", ChromeDebugPausedColor)

// SearchBarStyle is the drill-down/host-detail/template view's own bottom
// bar style while an in-tab search (design-docs/Search.md) is being
// composed or is currently active - black on yellow by default, the same
// "a distinct color signals a mode change" precedent BarStyle/
// ReplayBarStyle already establish for the whole session's chrome,
// applied here to just the one bar instead of every bar at once (a
// search is scoped to a single tab, not global to the session the way
// revisit-mode is). Built from search.go's own
// searchMatchForegroundColor/searchMatchBackgroundColor via
// buildChromeStyle, so the bar and the highlighted text are guaranteed
// to read as the same mode, not just coincidentally matching defaults -
// and so design-docs/ColorConfig.md's own search_match_foreground/
// search_match_background config keys reach this bar too.
var SearchBarStyle = buildChromeStyle(searchMatchForegroundColor, searchMatchBackgroundColor)

const SpinnerInterval = 200 * time.Millisecond

var SpinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// TaskIndent is one fixed-width slot, always this exact width regardless
// of what (if anything) currently occupies it - the active-task spinner,
// a warningColor ⚠ once that task has finished and at least one host
// recorded a warning for it (see taskLabel's own prefix construction), or
// plain spaces - so nothing else in this file ever needs to reserve
// separate width for any of them. Spinner and warning glyph deliberately
// share this one slot rather than getting one each: live use showed two
// independent slots pushed every task title two columns further right
// than before this feature existed, and put the warning glyph in its own
// separate column from the spinner instead of the same one - both
// reverted, since a task's own warning only becomes known for certain
// once it's done anyway (the spinner is gone by then), so the two never
// have anything genuine to show at the same instant.
const TaskIndent = "  "

// SplitBottomBarText replaces the tree's own normal shortcut hint
// (session's treeHelp table, design-docs/Help.md) for the duration of a
// two-pane drill-down session
// (design-docs/TwoPanedLayout.md): none of the tree's own shortcuts act on
// it while a drill-down is open (see SetInputCapture's viewingOutput
// branch), even though the tree pane itself stays visible and its bottomBar
// keeps drawing - showing the normal hint text there would advertise keys
// that currently do nothing. Left blank rather than naming Esc specifically
// (dropped per live feedback - not worth the line).
const SplitBottomBarText = ""

const (
	// MinTaskTitleName is the floor task.Name's own text is shortened to
	// before hostnames start getting shrunk instead (see taskLabel) - 30
	// runes, deliberately generous so a title only gives up ground to the
	// host list once the host list has already been squeezed hard.
	MinTaskTitleName = 30

	// TitleHostGapFloor is the minimum breathing room between the title
	// column and the first host name, per TUI.md's third iteration - used
	// both to decide whether the shared title column (see
	// computeHostColumnLayout) needs shrinking, and as the exact gap
	// rendered after the widest title (shorter titles get more, padded out
	// to the same shared column - see taskLabel).
	TitleHostGapFloor = 3

	// SplitMinTreeWidth/splitMaxTreeWidth/splitDividerWidth/
	// splitMinOutputWidth/splitMinTotalWidth implement
	// design-docs/TwoPanedLayout.md's numbers for the two-pane drill-down:
	// below splitMinTotalWidth columns, a drill-down still opens full-screen
	// (see showOutput); at or above it, the tree pane gets at least
	// SplitMinTreeWidth columns, growing up to splitMaxTreeWidth before any
	// further width goes to the drill-down pane instead (see
	// splitTreeWidth). splitMinTotalWidth is derived from the other three
	// rather than a second hardcoded number, so the one-column divider
	// between the panes (splitDivider) is accounted for in the gate too -
	// otherwise a terminal exactly at the tree/output floor would render
	// the drill-down pane one column under its own stated minimum.
	SplitMinTreeWidth   = 30
	SplitMaxTreeWidth   = 80
	SplitDividerWidth   = 1
	SplitMinOutputWidth = 79
	SplitMinTotalWidth  = SplitMinTreeWidth + SplitDividerWidth + SplitMinOutputWidth

	// HalfBlock is U+258C LEFT HALF BLOCK - its filled ("ink") half renders
	// in the cell's current foreground color, its unfilled half shows the
	// current background color. Used as a two-tone transition cell between
	// adjacent hostnames (see taskLabel/hostTransition): foreground = the
	// previous hostname's own color, background = the next hostname's own
	// color, so the separator blends from one into the other instead of an
	// abrupt full-cell change.
	HalfBlock = "▌"
)

// NothingReportedColor/SelectedRowTextColor/ProgressFillColor/
// WarningColor/IgnoredColor used to live here - moved to colors.go, now
// var not const, so ApplyColorConfig can overwrite them alongside every
// other color var (see that file's own top-of-file note for why).

// HostIndent is a host row's own fixed leading indent width - wider than
// taskIndent's, deliberately: a host row sits one level deeper than its
// task in the tree, and needs its own, wider fixed indent to read as
// such. Its one slot never holds a spinner - only a warningColor ⚠ in
// column 1 (see hostLabel) when this host's own result for the task
// carries a warning, plain spaces otherwise - so the hostname (and
// everything after it) always starts at the identical column regardless
// of whether a warning is currently showing.
const HostIndent = "    "
