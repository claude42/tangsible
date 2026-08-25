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

	"github.com/gdamore/tcell/v2"
)

// BarStyle is used for every non-list chrome bar (top bar, bottom bar, and
// the output drill-down page's own top/bottom bars) - white on blue, bold.
var BarStyle = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorNavy).Bold(true)

// ReplayBarStyle is design-docs/Revisit.md's own chrome for a revisited
// (historical) run - the same bars barStyle normally covers, plus the
// two-pane divider's own background (see NewLiveTUI's chromeStyle/chromeBg
// locals), switch to this instead for as long as the session is showing
// replayed data rather than a live/finished one. tcell.ColorPurple, not a
// hex value - a fixed base-16 ANSI palette slot, same reasoning as maroon
// (not brown/darkred) elsewhere in this file: reliable across terminal
// themes rather than RGB-approximated. See design-docs/Colors.md.
var ReplayBarStyle = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorPurple).Bold(true)

// SearchBarStyle is the drill-down/host-detail/template view's own bottom
// bar style while an in-tab search (design-docs/Search.md) is being
// composed or is currently active - black on yellow, the same "a distinct
// color signals a mode change" precedent BarStyle/ReplayBarStyle already
// establish for the whole session's chrome, applied here to just the one
// bar instead of every bar at once (a search is scoped to a single tab,
// not global to the session the way revisit-mode is). Matches the color
// TextSearch's own match highlighting uses (search.go's
// searchMatchFg/searchMatchBg), so the bar and the highlighted text read
// as the same mode.
var SearchBarStyle = tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorYellow).Bold(true)

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

// MainBottomBarText is the tree's own normal shortcut hint - bottomBar's
// initial text, and what closeOutput restores it to. splitBottomBarText
// replaces it for the duration of a two-pane drill-down session
// (design-docs/TwoPanedLayout.md): none of the tree's own shortcuts act on
// it while a drill-down is open (see SetInputCapture's viewingOutput
// branch), even though the tree pane itself stays visible and its bottomBar
// keeps drawing - showing the normal hint text there would advertise keys
// that currently do nothing.
const MainBottomBarText = " n/N: next/prev task  E/C: exp/coll all  F: follow  A/f: filter  r: re-run  d: diff  q: quit  ←/→: expand/collapse  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom "

const SplitBottomBarText = " Esc: close drill-down to use the tree "

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

	GrayTag = "gray" // placeholder color for a host AllHosts knows about
	// run-wide but that hasn't reported for *this* task yet.

	// PureBlack is a fixed hex value, not tcell's named "black" - some
	// terminal themes remap the base-16 ANSI "black" slot to a dark gray
	// rather than true black (the same base-16-vs-fixed-value trap already
	// documented for red/maroon, see colorTag). Used for every selected-row
	// text color, which specifically needs to read as unambiguously black
	// against the light backgrounds those rows use.
	PureBlack = "#1a1a1a"

	// ProgressFillColor is topBarText's own "headline as a progress bar"
	// fill - a fixed hex value, not tcell's named "green", for the same
	// reason pureBlack isn't named "black": tcell's nearest-color search
	// prefers an EXACT match to a base-16 slot's own nominal RGB even when
	// given as a hex value (confirmed against tcell's colorfit.go, same
	// finding pureBlack's own doc comment already made), and some terminal
	// themes remap that slot to something that reads poorly under white
	// text. Deliberately not the nominal xterm green (#008000) for exactly
	// that reason - nudged dark enough to keep white bold text readable
	// while still landing on a fixed, non-remappable extended-256 slot.
	ProgressFillColor = "#146414"

	// WarningColor marks a ⚠ indicator wherever one appears (taskLabel's
	// own collapsed-row aggregate marker, hostLabel's own per-host
	// marker, recap.go's own "warnings" category) - a pinkish shade,
	// matching real ansible-playbook's own default [WARNING] color.
	// Unlike pureBlack/progressFillColor above, a plain named tcell color
	// is fine here rather than a hand-picked hex: "hotpink" doesn't equal
	// any base-16 ANSI slot's own nominal RGB, so it's not subject to the
	// same terminal-theme remapping risk those two had to work around.
	WarningColor = "hotpink"
)

// HostIndent is a host row's own fixed leading indent width - wider than
// taskIndent's, deliberately: a host row sits one level deeper than its
// task in the tree, and needs its own, wider fixed indent to read as
// such. Its one slot never holds a spinner - only a warningColor ⚠ in
// column 1 (see hostLabel) when this host's own result for the task
// carries a warning, plain spaces otherwise - so the hostname (and
// everything after it) always starts at the identical column regardless
// of whether a warning is currently showing.
const HostIndent = "    "
