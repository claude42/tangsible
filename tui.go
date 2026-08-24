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

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// NewLiveTUI builds an initially-empty list UI and wires it to state's
// hooks so it grows as events arrive. It does not block — the caller must
// call app.Run() and feed events through applyLive.
//
// procH holds ansible-playbook's current process, used so Ctrl-C/q can
// forward SIGINT to it while it's still running (tcell's raw mode disables
// the OS's own Ctrl-C-to-SIGINT delivery, so without this the child would
// stop receiving the interrupt it used to get for free — see Purpose.md's
// Ctrl-C decision). A mutable holder rather than a plain *os.Process -
// unlike everything else this function reads at construction time and
// never again - because a rerun (Rerun.md, see requestRerun below) points
// it at a freshly spawned child mid-session; SetInputCapture always reads
// whichever process is current via procH.Load(). processDone/quitting are
// shared with the caller: this function only reads processDone and only
// writes quitting.
//
// initialTags/initialSkipTags/initialHosts pre-fill the re-run dialog's own
// Tags/Skip tags/Hosts fields the first time it's opened (and every time
// after, until the user edits them - the dialog's own fields, once opened,
// keep whatever the user last left in them across repeated 'r' presses) -
// the --tags/--skip-tags/--limit values this process was itself invoked
// with, parsed out by main.go via parsePassthroughArgs (Rerun.md's "if
// tags were already specified in the previous run... pre-filled").
//
// requestRerun is called once the re-run dialog is confirmed (Enter), to
// start a new generation with the dialog's own fields: startAtTask (empty
// for a whole-playbook re-run, otherwise the task name to pass as
// --start-at-task - see openRerunDialog below for how it's pre-filled) and
// the edited tags/skipTags/hosts. main.go's implementation resets
// processDone/exitCode/state, records the new invocation into .tangsible's
// history, and spawns a fresh ansible-playbook invocation; this function's
// own job is only to reset its own view state (expanded/currentID/
// following/the freeze latches) and restart the heartbeat ticker to match
// - see submitRerun below.
//
// sourceIndex (source.go) backs the output drill-down view's TASK:
// section (see formatHostOutput) - a lookup miss (any task whose path
// wasn't found while building the index) just means no TASK: section for
// that entry, never an error.
//
// startExpanded governs the very first task row's own initial
// expand/collapse state (`.tangsible`'s general.default_tree_state - see
// defaultTreeExpanded, resolve.go), read once by main.go before
// construction. Every task after the first inherits whatever the
// previous task's current expand state is at the moment it's added - see
// state.OnTaskAdded below - so this value only ever actually governs one
// row per generation (the very first task added since the last Reset()).
//
// startWithRerunDialog is true only for the "rerun" verb's own startup
// (Rerun.md): no ansible-playbook invocation exists yet at all - not even
// a first one in flight, unlike every other case this function handles -
// so the re-run dialog opens immediately instead of waiting for 'r', and
// processDone is expected to already be true when this is called (main.go
// sets it before constructing the TUI): accurate ("no generation is
// currently in flight"), and what safely unlocks the dialog-opening/
// quit-outright behavior the rest of this function already has for a
// frozen run - see everStarted below for the one place that distinction
// actually matters once frozen means "genuinely nothing has run yet"
// rather than "a run finished."
// passthroughArgs is this session's own current-generation passthrough
// args (main.go's originalArgs.Rest - importantly -i/-e, never
// -l/--limit, which parsePassthroughArgs already extracts separately) -
// threaded through only so showOutput's own resolveTaskValues calls (see
// design-docs/Drilldown, Resolved Values.md) see the same
// inventory/extra-vars context the real run did. Not used for anything
// else in this function.
//
// colorEnabled is general.color's own resolved value (colorEnabledByUser,
// resolve.go), read once by main.go before construction - one of three
// independent inputs (alongside the terminal's own detected color
// capability and the NO_COLOR environment variable) combined below into
// useColor, design-docs/Morehosts.md's own gate on whether the collapsed
// task row's per-host summary may render in color at all.
// revisitReturn, if non-nil, marks this session as design-docs/Revisit.md's
// "revisit" verb showing a replayed (historical) run rather than a live
// run/rerun/role session: state/processDone/exitCode are already fully
// populated by the time this constructor is called (see revisit.go), chrome
// switches to replayBarStyle for as long as revisitActive stays true, and
// pressing Esc at the bare tree level (not in a dialog, not viewing output -
// nothing else has ever claimed that key there) calls revisitReturn, which
// is expected to stop app.Run() and let the caller show the run list again.
// nil for every other verb - Esc keeps doing nothing at that level, exactly
// as before this existed.
//
// targetPlaybook/targetRole (exactly one non-empty, mirroring
// appendInvocation's own playbook/role parameters) is this session's own
// target identity, exactly as recorded in state.toml - distinct from
// playbookName, which is display-only (main.go passes it
// filepath.Base(playbook), not the full path state.toml itself keys on).
// Needed for design-docs/Diff.md's own 'd' key, to look up this session's
// own history entry and filter comparison candidates against it
// (runDiffFlow, diff.go).
func NewLiveTUI(state *playbookState, playbookName string, isRole bool, procH *procHandle, processDone, quitting *atomic.Bool, exitCode *atomic.Int32, sourceIndex taskSourceIndex, startExpanded, twoPaneLayout, colorEnabled bool, initialTags, initialSkipTags, initialHosts string, startWithRerunDialog bool, requestRerun func(startAtTask, tags, skipTags, hosts string), passthroughArgs []string, progH *atomic.Pointer[progressTracker], revisitReturn func(), targetPlaybook, targetRole string) (app *tview.Application, applyLive func(rawEvent)) {
	startedAt := time.Now() // wall-clock the TUI itself came up - see
	// topBarText's doc comment for why this is deliberately not sourced
	// from any event.

	// progressPosition reads whatever progressTracker the current
	// generation has (progH.Load() is nil-safe to call Position() on -
	// see progress.go - both before this session's very first skeleton
	// has ever been built, and for "rerun"'s own startup dialog, where
	// nothing has run yet at all).
	progressPosition := func() (position, total int) { return progH.Load().Position() }

	list := newTreeList() // see treelist.go - a purpose-built replacement
	// for tview.List, needed so mouse-wheel panning can move the viewport
	// independently of the cursor (tview.List's own Draw() forces the two
	// to stay in lockstep, with no way to disable it). No wraparound and
	// no secondary-text/shortcut support built in - this app never used
	// those.

	expanded := map[*taskNode]bool{}
	// recapHostExpanded/recapCategoryExpanded back the recap section's own
	// two-level expand/collapse (design-docs/Recap.md) - kept separate
	// from expanded since neither key type (a hostname, a
	// recapCategoryRowID) is a *taskNode. Both start empty/collapsed
	// unconditionally, regardless of startExpanded - the recap's own
	// "initially only the top level is visible" is a fixed behavior, not
	// tied to the tree's own default_tree_state config knob.
	recapHostExpanded := map[string]bool{}
	recapCategoryExpanded := map[recapCategoryRowID]bool{}
	var currentRows []row
	var currentID any
	var rebuilding bool
	lastAppliedSelectedIndex := -1 // the index last genuinely applied to
	// list via SetCurrentItem, tracked separately from list's own
	// currentItem because list.Clear()/AddItem() (see treelist.go) reset
	// that to 0 on every single rebuild - without this, rebuild()'s
	// trailing selection-apply call below has no way to tell a genuine
	// selection change apart from itself simply reasserting the same
	// logical row again, and would re-clamp (ensureVisible) the viewport
	// back to the cursor on every call - including the heartbeat ticker's,
	// every 200ms, while a run is still live, fighting any mouse-wheel
	// panning the user just did. See restoreCurrentItem's own doc comment.
	following := true                    // auto-follow the newest row until the user navigates away
	var jumpingToEnd bool                // true only while our own 'F' handler drives SetCurrentItem
	everStarted := !startWithRerunDialog // false only for the "rerun"
	// verb's startup dialog, until submitRerun's first-ever call flips it
	// true - see rebuild()'s own use of it below: processDone starts true
	// in that one case (see startWithRerunDialog's own doc comment above)
	// even though nothing has actually run, which would otherwise make
	// rebuild() render a "Playbook completed successfully" status row
	// before anything ever happened.
	revisitActive := revisitReturn != nil // true for the whole lifetime of
	// a revisit session until a real rerun is confirmed (submitRerun) -
	// once that happens there's a live/finished generation of its own on
	// screen, no longer "old data," so chrome reverts to normal and Esc
	// stops meaning "back to the list" (see submitRerun/SetInputCapture
	// below).
	var failureCursorPlaced bool // latches true the first time rebuild()
	// observes the run frozen - guards the one-time "jump to the failed
	// host" placement below so it fires exactly once on the
	// running-to-frozen transition, never re-forcing the cursor back
	// there if the user has since navigated elsewhere.
	var frozenElapsed time.Duration
	var haveFrozenElapsed bool // latches true the first time rebuild()
	// observes the run frozen, capturing that instant's elapsed time for
	// every later rebuild to reuse. Without this, a rebuild triggered long
	// after the run finished - by cursor navigation (SetChangedFunc below)
	// or anything else that isn't the heartbeat ticker, which does stop
	// once frozen - would recompute now.Sub(startedAt) fresh and make the
	// top bar's elapsed time keep climbing after the run is actually done.
	var lastTotalWidth int // pages' own width (the terminal's, regardless of
	// which of "main"/"output"/"split" is frontmost - see rebuild()'s own
	// totalWidth local) last time rebuild() ran. Compared against pages'
	// *current* width by the resize-watcher goroutine (see NewLiveTUI's
	// call to startHeartbeat) to notice a terminal resize that happened
	// with no other event to piggyback a rebuild on - pages itself, being
	// the app's root primitive, always reports the true current terminal
	// size no matter which page is showing, unlike list's own width (used
	// for a different purpose below - the tree's own column layout - which
	// stops tracking the terminal 1:1 once a two-pane drill-down is open,
	// design-docs/TwoPanedLayout.md).
	var viewingOutput bool // true while the host-output page is frontmost; see
	// SetInputCapture below - selects between the main tree's and the output
	// view's own page-specific key bindings (Left/Right and n/p mean
	// different things on each page). A plain locally-owned bool, not a
	// pages.GetFrontPage() query, since this function owns both places that
	// ever switch pages.
	var rebuild func() // declared (not yet assigned - see its real definition
	// further down) before showOutput, which now calls it directly (see
	// design-docs/TwoPanedLayout.md's live-sync) - a closure only needs
	// rebuild's identifier in scope by the time it actually runs, not by the
	// time it's defined, so this forward declaration is enough to let
	// showOutput's own closure reference it here.
	//
	// bottomBar/flex/splitFlex are forward-declared the same way, for the
	// same reason: showOutput (and, for bottomBar, closeOutput too) needs
	// these identifiers in scope before their real construction further
	// down assigns them.
	//
	// useColor (design-docs/Morehosts.md) is forward-declared here for the
	// identical reason: rebuild's own body (further down still) reads it,
	// but its real value isn't known until the terminal color-capability
	// probe runs, right before Application.EnableMouse below.
	var useColor bool
	var bottomBar *tview.TextView
	var flex, splitFlex, splitBody, treeBody *tview.Flex
	var splitDivider *tview.Box
	var splitHeader *tview.TextView // forward-declared for the same
	// reason - rebuild's own split-mode header (a single widget spanning
	// the full terminal width, replacing topBar/outputTopBar for the
	// duration of a split session - see splitFlex's own construction for
	// why) needs setting live, before its real construction (further
	// down) assigns it.
	var splitMode bool // true while the currently-open drill-down is
	// rendered as the two-pane "split" page (design-docs/TwoPanedLayout.md)
	// rather than full-screen "output" - decided once, in showOutput, the
	// moment a drill-down freshly opens (viewingOutput was false), and left
	// alone for the rest of that session even if the terminal is resized
	// while it stays open (per the design doc's own explicit call: only the
	// panes' own internal layout reflows mid-session, the split-vs-full-
	// screen choice itself doesn't re-decide until the next open).
	currentFilter := filterQuery{mode: filterAll} // see Filters.md; the
	// two dialogs below are the only writers.
	//
	// The filter (a/c/f) and search (/) dialogs are two separate modals,
	// not one combined one (reworked from an earlier single-dialog design
	// after live use showed the combined version made it too easy to hit
	// the wrong key). Each gets its own "is this one open" bool rather
	// than a single shared enum, since the two are modal in genuinely
	// different ways: filterDialogOpen (menu mode - a/c/f/Esc/q are the
	// only keys that do anything, everything else is swallowed) vs
	// searchDialogOpen (text-entry mode - every key except Ctrl-C passes
	// straight through to the search box's own editing, including 'q' and
	// 'a'/'c'/'f', since a real search term might contain any of those
	// letters). See SetInputCapture below for exactly how each is modal.
	var filterDialogOpen bool
	var searchDialogOpen bool
	var rerunDialogOpen bool // see openRerunDialog/submitRerun below (Rerun.md) -
	// modal the same way searchDialogOpen is (every key but Ctrl-C/Enter/Esc
	// passes straight through to whichever form item has focus), not the
	// filter dialog's swallow-everything-but-a-few-keys menu style: this
	// dialog is text-entry-first, and any of its fields might legitimately
	// contain the letter 'q' or any other shortcut letter.

	// activeTaskNow returns the run's current in-progress task, or nil once
	// the run has finished - the same "frozen means no active task" rule
	// rebuild() applies to its own activeTask local, pulled out so
	// navigateMainTask/navigateOutputTask/applyFilter (all outside rebuild)
	// can compute the identical thing when deciding what a filter should
	// keep visible (see taskVisible's isActive parameter).
	activeTaskNow := func() *taskNode {
		if processDone.Load() {
			return nil
		}
		return state.CurrentTask()
	}

	// revealExpandedTask, called right after a task row's Enter/Space/click
	// toggle (or the Right-arrow handler, see handleRight below) just
	// expanded it, scrolls the list down - if needed, and only as far as
	// it can - so the newly revealed host rows are actually visible,
	// rather than landing below the bottom of the screen with no visible
	// change. The cursor stays on the task row itself throughout, so
	// treeList's own ensureVisible (see treelist.go - it only runs when
	// SetCurrentItem's index actually changes) never fires here on its
	// own; this is the sole mechanism that scrolls to reveal a task's
	// newly-expanded children. Only ever scrolls further down from
	// wherever the view already was, never up. If the whole block (the
	// task row plus all its hosts) doesn't fit in the viewport at all,
	// this simply reveals as much of the tail as fits.
	revealExpandedTask := func(t *taskNode) {
		_, _, _, height := list.GetInnerRect()
		if height <= 0 {
			return
		}
		taskIndex := -1
		for i, r := range currentRows {
			if r.id == t {
				taskIndex = i
				break
			}
		}
		if taskIndex == -1 {
			return
		}
		blockEnd := taskIndex + len(t.HostOrder) // last newly-revealed row's index
		desired := blockEnd - height + 1
		if desired > list.GetOffset() {
			list.SetOffset(desired)
		}
	}

	// chromeStyle/chromeBg pick the initial look for every chrome bar/the
	// two-pane divider below - replayBarStyle/purple for a revisit session,
	// barStyle/navy for everything else. Mutable local, not a const choice:
	// submitRerun resets both bars and splitDivider back to barStyle/navy
	// directly once revisitActive goes false, so these two only ever matter
	// for how things start out, not as an ongoing source of truth.
	chromeStyle := barStyle
	chromeBg := tcell.ColorNavy
	if revisitActive {
		chromeStyle = replayBarStyle
		chromeBg = tcell.ColorPurple
	}

	// currentMainBottomBarText appends the revisit-only "Esc: back to
	// list" hint onto mainBottomBarText for as long as revisitActive stays
	// true - reads it fresh on every call rather than being decided once,
	// same reasoning chromeStyle/chromeBg above don't need (those are only
	// ever applied at construction, with submitRerun resetting the actual
	// widgets directly afterward) - bottomBar's text, unlike its style, is
	// legitimately re-set many times over a session's life (closeOutput,
	// rebuild's own split-mode toggle), and each of those call sites should
	// see revisitActive's current value, not a snapshot from construction.
	currentMainBottomBarText := func() string {
		text := mainBottomBarText
		if requestRerun == nil {
			// Matches SetInputCapture's own 'r' guard below: nothing to
			// advertise a key that's a guaranteed no-op right now (a
			// Phase 2 revisit session, per design-docs/Revisit.md, before
			// rerun-from-revisit exists).
			text = strings.Replace(text, "r: re-run  ", "", 1)
		}
		if revisitActive {
			text += " Esc: back to list "
		}
		return text
	}

	// chromeColorName is chromeBg's own tag-name equivalent - "navy"/
	// "purple" - for the progress-fill lines (topBarText/
	// composeSplitHeaderLine/outputTopBar's own plain fill, all below),
	// which bake their unfilled-portion background into inline
	// [white:<name>:b] tags rather than reading it from the TextView's own
	// SetTextStyle the way every other chrome bar does (see chromeStyle
	// above) - a single tcell.Style can't vary per-column the way a
	// sweeping fill needs to. Read fresh on every call, same reasoning as
	// currentMainBottomBarText just above: these are called from within
	// rebuild() on every redraw, not just once at construction, so this
	// needs to see revisitActive's current value each time, not a
	// snapshot - discovered the hard way, live: chromeStyle/chromeBg alone
	// left the top/split/output bars still showing plain navy under their
	// own progress-fill text, since SetTextStyle never actually painted
	// those characters at all.
	chromeColorName := func() string {
		if revisitActive {
			return "purple"
		}
		return "navy"
	}

	// showElapsed suppresses the top/split bars' own spinner/mm:ss clock
	// for as long as revisitActive stays true - a revisit session's
	// elapsed is always ~0 (design-docs/Revisit.md: only a run's start
	// time was ever saved, never its duration), and showing that would
	// read as "this just finished in no time" rather than as the honest
	// "we don't know" it actually is. Read fresh on every call, same
	// reasoning as chromeColorName/currentMainBottomBarText just above.
	showElapsed := func() bool { return !revisitActive }

	// Moved up here (was previously declared after rebuild/hooks) - rebuild()
	// now updates it on every call, so it must exist first.
	topBar := tview.NewTextView().SetDynamicColors(true).
		SetText(topBarText(playbookName, isRole, state.AllHosts, 0, false, currentFilter, 0, 0, 20, chromeColorName(), showElapsed()))
	topBar.SetTextStyle(chromeStyle)

	// The cursor row's actual look (black-on-light-gray title, black bold
	// text on a per-outcome colored background for each hostname - see
	// playRowText/taskLabel/hostLabel's selected parameter) can't be
	// expressed as a single style applied uniformly to a row's whole text -
	// different runs of the same row need different foreground/background
	// combinations. treeList (treelist.go) has no built-in per-row
	// highlighting to neutralize in the first place (unlike tview.List, it
	// just prints whatever text each row was given) - rebuild() re-renders
	// whichever one row is currently selected with its own selected=true
	// variant before ever calling AddItem, and that's the entire
	// highlighting mechanism.

	// Output drill-down page (design-docs/Tabbed UI.md): outputTabs is a
	// tabbedPane (tabs.go) - a fresh set of tab content TextViews is built
	// by buildOutputTabs and handed to it via SetTabs every time a host
	// row is selected (see showOutput below), rather than one single,
	// reused TextView the way this used to work. Dynamic colors are on
	// for each tab's own TextView so buildOutputTabs' own tab builders
	// can color their section labels/status line - every piece of dynamic
	// content any of them write (task source, stdout/stderr/msg, the full
	// JSON result) is individually tview.Escape()'d before going in, so a
	// literal "[" in real command output or YAML (e.g. "tags: [a, b]")
	// can never be misread as a color tag.
	outputTabs := newTabbedPane()

	// Dynamic colors on, same reason and same escaping discipline as
	// topBar (see progressFillLine) - its own fill makes host/task.Name
	// (both external content) need escaping, handled once by
	// progressFillLine itself rather than here.
	outputTopBar := tview.NewTextView().SetDynamicColors(true)
	outputTopBar.SetTextStyle(chromeStyle)

	outputBottomBar := tview.NewTextView().SetText(" tab/shift-tab: switch tab  n/p: prev/next task  ←/→: prev/next host  esc/enter: back  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom ")
	outputBottomBar.SetTextStyle(chromeStyle)

	outputFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(outputTopBar, 1, 0, false).
		AddItem(outputTabs.Primitive(), 0, 1, true).
		AddItem(outputBottomBar, 1, 0, false)

	// splitHeader replaces topBar/outputTopBar entirely for the duration
	// of a split session (splitFlex's own construction, further down): a
	// single widget spanning the terminal's true full width, rather than
	// two independently-positioned widgets (the tree pane's own topBar,
	// the drill-down pane's own outputTopBar) either side of splitDivider
	// that are each supposed to land on the identical fill boundary but
	// - reported live, twice - didn't quite: a couple of columns right at
	// the seam stayed the wrong color regardless of how carefully the two
	// widgets' own widths were kept in agreement. One widget's own width
	// trivially agrees with itself, which is what actually fixes that
	// class of bug rather than chasing its exact cause further.
	splitHeader = tview.NewTextView().SetDynamicColors(true)
	splitHeader.SetTextStyle(chromeStyle)

	pages := tview.NewPages()

	// currentPageName/switchPage track which of "main"/"output"/"split" is
	// currently frontmost, so rebuild()'s own live resize-reactivity (see
	// design-docs/TwoPanedLayout.md) can tell whether a page switch is
	// actually needed before calling pages.SwitchToPage - which, per
	// tview's own source, re-focuses the new front page every time it's
	// called, even redundantly. Calling it unconditionally on every
	// rebuild (every heartbeat tick while a drill-down is open) would be
	// harmless in practice but is needless churn; gating on a real change
	// avoids it for free.
	currentPageName := "main"
	switchPage := func(name string) {
		if name == currentPageName {
			return
		}
		currentPageName = name
		pages.SwitchToPage(name)
	}

	// Filter and search dialogs: two small, modal overlays on top of the
	// main page (see Filters.md's Dialog section - split into two separate
	// dialogs after live use of a single combined one showed it was too
	// easy to hit the wrong key) rather than a full page swap like
	// "output" below - tview's Pages supports this natively via
	// ShowPage/HidePage instead of SwitchToPage, which leave other pages'
	// visibility alone (confirmed against pages.go) rather than hiding
	// them, so "main" keeps being drawn underneath. centeredModal wraps
	// each in nested Flexes to get a fixed-size, screen-centered box
	// instead of filling the whole available area - the standard tview
	// pattern for a partial-screen overlay page. Neither is added to pages
	// yet - see the pages.AddPage calls further down, which must add them
	// *last* so they draw on top of "main"/"output" (Pages draws visible
	// pages back to front, in the order they were added - confirmed
	// against pages.go).
	//
	// filterDialog is a plain TextView - a static a/c/f menu, no text
	// entry at all. No border/title of its own - those live on filterFlex
	// instead (constructed further down, once closeDialogs exists for its
	// own Cancel button to call), which wraps this TextView together with
	// a real Cancel button below it.
	filterDialog := tview.NewTextView().SetDynamicColors(true)

	// searchDialog is a headline TextView plus a real tview.InputField for
	// the search box - a TextView can display text but can't accept
	// edits, and the search filter needs genuine text entry. Unlike the
	// old combined dialog's search box, this one gets focus the moment the
	// dialog opens (openSearchDialog below) rather than needing a separate
	// activation keypress first - there's nothing else in this dialog to
	// browse first, so there's no "menu mode" to be in before typing.
	searchHeadline := tview.NewTextView().SetDynamicColors(true).
		SetText(" [::b]Search[::-]\n\n [gray]Enter to apply, Esc to cancel[-]")
	searchInput := tview.NewInputField().SetLabel("Search: ")
	// Top/bottom margin Box items, same as filterFlex's own - a real
	// tview.NewBox() rather than a bare nil, so its Draw() still fills its
	// rect with the dialog's own background instead of letting whatever's
	// behind the "search" page show through (the same background
	// see-through bug fixed for the other dialogs).
	searchDialogFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox(), 1, 0, false). // top margin
		AddItem(searchHeadline, 0, 1, false).
		AddItem(searchInput, 1, 0, true)
	searchDialogFlex.SetBorder(true).SetTitle(" Search ")

	// rerunDialog (Rerun.md) - a real tview.Form, unlike the two dialogs
	// above: it's the first multi-field input this app needs, and Form
	// gives Tab/Backtab focus-cycling between them for free rather than
	// hand-rolling it the way treeList replaced tview.List for the main
	// tree (that replacement was needed because List's own behavior fell
	// short of what the tree needed; here Form's default behavior already
	// matches). Items are built directly (not via Form's AddInputField
	// convenience method) so openRerunDialog/submitRerun below can keep
	// direct references to them.
	//
	// No checkbox gating taskField, despite that being the original design
	// (a "Start at task" checkbox enabling/disabling it via SetDisabled,
	// pre-filled from the cursor's current task either way): live testing
	// surfaced a genuine tview quirk that made it actively worse than
	// simpler alternatives - InputField.SetDisabled unconditionally calls
	// its own finished(-1), which - once any real Tab/Enter has happened
	// anywhere in the form's lifetime - replays that same navigation key
	// (confirmed against inputfield.go/form.go: Form's shared handler's
	// default case does exactly this for a negative key). So toggling the
	// checkbox silently advanced focus by one, *in addition to* whatever
	// Tab the user pressed right after - text reproducibly landed one
	// field over from where it was typed. Rather than fight that, this
	// falls back to the plainer design agreed as the explicit fallback:
	// one freeform Task field, never pre-filled from the cursor, empty
	// means "whole playbook" - exactly like Tags/Hosts, no special-casing.
	taskField := tview.NewInputField().SetLabel("Start with task: ")
	tagsField := tview.NewInputField().SetLabel("Limit tags to: ")
	skipTagsField := tview.NewInputField().SetLabel("Skip tags: ")
	hostsField := tview.NewInputField().SetLabel("Limit hosts to: ")

	rerunForm := tview.NewForm().
		AddFormItem(taskField).
		AddFormItem(tagsField).
		AddFormItem(skipTagsField).
		AddFormItem(hostsField)
	rerunForm.SetBorder(true).SetTitle(" Re-run (enter: run, esc: cancel) ")

	// outputTask/outputHost track which (task, host) pair the output page
	// is currently showing, so navigateOutputTask (below) knows where
	// "current" is without threading it through as extra state on every
	// keypress.
	var outputTask *taskNode
	var outputHost string
	// outputTopBarPlainText is outputTopBar's own "host — task" content,
	// unwrapped and unpadded - set once per navigation (showOutput) but
	// re-wrapped with a fresh progressFillLine on every single rebuild()
	// call, the same live-updating treatment topBar's own text already
	// gets, so the drill-down's own headline keeps sweeping green as the
	// run progresses even while the user isn't actively navigating within
	// it.
	var outputTopBarPlainText string

	// resolveKey identifies one (task, host) pair's own "Resolved"
	// section cache entry (design-docs/Drilldown, Resolved Values.md).
	type resolveKey struct {
		task *taskNode
		host string
	}
	// resolveCache holds every (task, host) pair's own resolvedRender for
	// the lifetime of the current generation - cleared inside
	// submitRerun's own view-state reset (the same place expanded/
	// currentID/following etc. already get reset), so a stale render
	// computed against a previous generation's own vars can never linger.
	// Read/written only from this function's own event-loop goroutine
	// (showOutput directly; the background goroutine below only ever
	// touches it from inside app.QueueUpdateDraw), so - like everything
	// else in this file - it needs no locking of its own.
	resolveCache := map[resolveKey]resolvedRender{}

	// docsCache holds every module's own resolvedRender for the "Docs" tab
	// (ansibledoc.go's fetchAnsibleDoc), keyed by the task's own "action"
	// result field (taskAction) rather than by (task, host) the way
	// resolveCache is - a module's own documentation depends on nothing
	// about the current run (no vars, no facts, not even which host), so
	// every task using the same module shares one entry, and - unlike
	// resolveCache - this is deliberately *not* cleared on a rerun
	// (submitRerun): the installed collections a rerun's ansible-doc would
	// see are the same as before it, so there's nothing stale to flush.
	docsCache := map[string]resolvedRender{}

	// renderOutputTabs rebuilds every tab from buildOutputTabs' own
	// output and hands the result to outputTabs.SetTabs - which itself
	// preserves whichever tab is currently active, by name, so repeatedly
	// calling this while browsing (Left/Right/n/p) doesn't keep resetting
	// the user back to the Task tab.
	renderOutputTabs := func(task *taskNode, host string, resolved resolvedRender, docs resolvedRender) {
		names, contents := buildOutputTabs(task, host, sourceIndex, resolved, docs)
		prims := make([]tview.Primitive, len(names))
		for i, content := range contents {
			tv := tview.NewTextView().SetDynamicColors(true)
			tv.SetText(content)
			prims[i] = tv
		}
		outputTabs.SetTabs(names, prims)
	}

	showOutput := func(task *taskNode, host string) {
		// Pane-mode (split vs. full-screen) is no longer decided here at
		// all - rebuild() (below) re-evaluates it, live, from the
		// terminal's actual current width on every call, including the one
		// this function makes at the very end (see design-docs/
		// TwoPanedLayout.md) - so a fresh open and a later resize while
		// already open are handled by the exact same code path, not two.

		outputTask, outputHost = task, host
		outputTopBarPlainText = fmt.Sprintf(" %s — %s ", host, task.Name)

		// Kicked off the moment a drill-down opens (or navigates to a
		// different host/task), not gated behind a keypress - per
		// design-docs/Drilldown, Resolved Values.md. Runs on its own
		// goroutine so opening the view is never blocked on a real
		// ansible-playbook invocation. The Resolved tab itself starts
		// out entirely absent, not a visible "Resolving..." placeholder
		// (resolvedTabHidden treats Pending as hidden) - an earlier
		// version showed the placeholder immediately, but that made the
		// tab a moving target: a user who tabbed onto it while it read
		// "Resolving..." would watch it vanish out from under them the
		// instant resolving finished identical to source. Silently
		// absent until there's something worth showing reads as "this
		// task doesn't have one" rather than "something just
		// disappeared," even though both are the same outcome. The
		// outputTask/outputHost/viewingOutput check right before
		// updating the view guards against a stale result landing after
		// the user has already navigated elsewhere - the cache itself is
		// still updated regardless, so a later revisit is free.
		key := resolveKey{task, host}
		resolved, cached := resolveCache[key]
		if !cached {
			resolved = resolvedRender{Pending: true}
			resolveCache[key] = resolved
			go func() {
				text, err := resolveTaskValues(task.Path, sourceIndex[task.Path], host, passthroughArgs)
				result := resolvedRender{}
				if err != nil {
					result.Err = err.Error()
				} else {
					result.Text = text
				}
				app.QueueUpdateDraw(func() {
					resolveCache[key] = result
					if outputTask != task || outputHost != host || !viewingOutput {
						return
					}
					if !resolvedTabHidden(result, sourceIndex[task.Path]) {
						// The tab wasn't shown at all until now (see
						// above) - only a full renderOutputTabs can make
						// a brand new tab appear at all, unlike the
						// old design's in-place SetText on an
						// already-existing TextView. Accepted here even
						// though it loses whatever tab/scroll position
						// was current: this fires at most once, very
						// shortly after the view was first opened (see
						// design-docs/Drilldown, Resolved Values.md's
						// own "kicked off the moment a drill-down opens"
						// timing), so there's realistically nothing
						// meaningful to lose yet.
						renderOutputTabs(task, host, result, docsCache[taskAction(task, host)])
					}
					// Otherwise the tab stays exactly as absent as it
					// already was - nothing on screen needs to change.
				})
			}()
		}

		// Same "kick off immediately, stay absent until ready" treatment
		// as Resolved above, for the Docs tab (ansibledoc.go's
		// fetchAnsibleDoc) - cached by module name in docsCache, not by
		// (task, host), since a module's own documentation is the same
		// for every task/host that uses it (see docsCache's own comment).
		// action == "" (no result recorded yet, or this result simply has
		// no action field) means there's nothing to look up at all - docs
		// stays the zero resolvedRender{}, which buildOutputTabs'
		// docsTabHidden already treats as "omit the tab."
		action := taskAction(task, host)
		var docs resolvedRender
		docsCached := action == "" // no action to look up - stays the
		// zero value, and there's nothing to kick off below either.
		if action != "" {
			docs, docsCached = docsCache[action]
		}
		if !docsCached {
			docs = resolvedRender{Pending: true}
			docsCache[action] = docs
			go func() {
				text, err := fetchAnsibleDoc(action)
				result := resolvedRender{}
				if err != nil {
					result.Err = err.Error()
				} else {
					result.Text = text
				}
				app.QueueUpdateDraw(func() {
					docsCache[action] = result
					if outputTask != task || outputHost != host || !viewingOutput {
						return
					}
					if !docsTabHidden(result) {
						renderOutputTabs(task, host, resolveCache[key], result)
					}
				})
			}()
		}

		renderOutputTabs(task, host, resolved, docs)
		// Every fresh tab's own TextView starts scrolled to the top
		// already (a brand new widget), so there's nothing to reset here
		// the way the old single-TextView version needed SetText not to
		// reset scroll position - see tabbedPane's own "reset to top on
		// every switch" behavior in tabs.go for the *within-view*
		// equivalent of that same concern.
		viewingOutput = true

		// Live-sync (design-docs/TwoPanedLayout.md): keep the tree's own
		// cursor pointed at whatever (task, host) the drill-down is
		// currently showing, expanding that task so the row is actually
		// there to point at - on every call, not just the first, so
		// navigateOutputHost/navigateOutputTask (Left/Right, n/p while
		// already viewing output) keep it current too. This used to be
		// closeOutput's own one-time job on the way out; doing it here
		// instead, unconditionally, makes closeOutput's own copy
		// unnecessary (see below) and is what actually makes the tree
		// "follow" while a two-pane session is open. No separate scrolling
		// code is needed to keep the new row visible even when it's off
		// screen: rebuild()'s own SetCurrentItem call fires whenever
		// selectedIndex genuinely changes, and treeList.ensureVisible()
		// (treelist.go) already runs on exactly that.
		expanded[task] = true
		currentID = hostRowID{task, host}
		following = false
		rebuild() // also decides/applies pane mode now that viewingOutput is
		// true - see rebuild()'s own resync block.
	}

	// navigateOutputTask moves the output page to the previous/next task
	// (delta -1/+1) that recorded a result for outputHost, in run order,
	// among currently-visible tasks only (see visibleTasksForHost) - per
	// Filters.md, tasks the active filter is hiding are skipped here too. A
	// no-op at either end (no wraparound, matching the main tree's own
	// no-wraparound convention elsewhere) and before any output has been
	// shown yet (outputTask still nil).
	navigateOutputTask := func(delta int) {
		if outputTask == nil {
			return
		}
		tasks := visibleTasksForHost(state, outputHost, currentFilter, sourceIndex, activeTaskNow())
		idx := -1
		for i, t := range tasks {
			if t == outputTask {
				idx = i
				break
			}
		}
		if idx == -1 {
			return
		}
		newIdx := idx + delta
		if newIdx < 0 || newIdx >= len(tasks) {
			return
		}
		showOutput(tasks[newIdx], outputHost)
	}

	rebuild = func() {
		rebuilding = true
		defer func() { rebuilding = false }()

		now := time.Now() // captured once per rebuild - shared by the top
		// bar's elapsed/spinner and every active row's spinner below, so a
		// single pass renders a self-consistent instant rather than
		// drifting per-row/per-call time.Now() reads.
		frozen := processDone.Load()
		elapsed := now.Sub(startedAt)
		if frozen {
			if !haveFrozenElapsed {
				frozenElapsed = elapsed
				haveFrozenElapsed = true
			}
			elapsed = frozenElapsed
		}
		// Read once per rebuild and shared by both topBar and (while a
		// drill-down is open) outputTopBar below - progressFillLine's own
		// fill needs the identical (progressPos, progressTotal, frozen)
		// triple for both bars to stay in visual agreement with each
		// other, and there's no reason to re-read the tracker twice for
		// one rebuild pass anyway.
		progressPos, progressTotal := progressPosition()

		// Two-pane layout (design-docs/TwoPanedLayout.md) is live, not
		// decided once at open time: every rebuild - including ones driven
		// purely by a terminal resize, with no other event to piggyback on
		// (see the resize-watcher goroutine and the heartbeat ticker below)
		// - re-evaluates whether the terminal is currently wide enough for
		// a split view, and keeps the tree pane's own width current within
		// that range. pages itself, not list, is the width source: as the
		// app's own root primitive it always reports the true current
		// terminal size no matter which page is frontmost, unlike list's
		// own width, which stops tracking the terminal 1:1 the moment a
		// two-pane session fixes it to the tree pane's own share (see
		// width/lastTotalWidth below - two genuinely different quantities
		// now, not one).
		_, _, totalWidth, _ := pages.GetInnerRect()
		lastTotalWidth = totalWidth // compared against pages' *current*
		// width by the resize-watcher goroutine below, to notice a resize
		// that happened with no other event to piggyback a rebuild on.
		if viewingOutput {
			splitMode = twoPaneLayout && totalWidth >= splitMinTotalWidth
			if splitMode {
				splitBody.ResizeItem(treeBody, splitTreeWidth(totalWidth), 0)
				bottomBar.SetText(splitBottomBarText)
				switchPage("split")
			} else {
				bottomBar.SetText(currentMainBottomBarText())
				switchPage("output")
			}

			if splitMode {
				// splitHeader is one single widget spanning the terminal's
				// true full width (totalWidth) - unlike an earlier version
				// of this, which kept topBar/outputTopBar as two
				// independently-positioned widgets either side of
				// splitDivider and tried to keep their own fills in
				// agreement: that was reported live, twice, to leave a
				// couple of columns right at the seam the wrong color
				// regardless of how carefully the two widths were derived
				// to match each other. One widget's own width trivially
				// agrees with itself, which is what actually closes that
				// class of bug. splitDivider itself (the body rows below
				// this one) is deliberately not part of this string at
				// all - this header has no separate divider glyph of its
				// own, so the single column that visually sits above it
				// just participates in the fill like any other character.
				//
				// composeSplitHeaderLine (not composeTopBarLine +
				// outputTopBarPlainText concatenated after it - a second
				// live report caught two real bugs in that approach at
				// once, see its own doc comment) builds hostAndTask from
				// outputHost/outputTask directly - the one host/task this
				// drill-down is actually showing, not state.AllHosts (the
				// tree-only bar's own "every host seen so far" list).
				hostAndTask := outputHost
				if outputTask != nil {
					hostAndTask = outputHost + "   " + outputTask.Name
				}
				splitHeader.SetText(progressFillLine(
					composeSplitHeaderLine(playbookName, isRole, hostAndTask, elapsed, frozen, currentFilter, progressPos, progressTotal, totalWidth, showElapsed()),
					progressPos, progressTotal, frozen, chromeColorName()))
			} else {
				// Padded to the full terminal width before the fill is
				// applied (same reason composeTopBarLine pads its own
				// line) - outputTopBar's own "host — task" text is
				// usually much shorter than the row, and a fill tag only
				// colors runes actually present in the string.
				full := outputTopBarPlainText
				if gap := totalWidth - len([]rune(full)); gap > 0 {
					full += strings.Repeat(" ", gap)
				}
				outputTopBar.SetText(progressFillLine(full, progressPos, progressTotal, frozen, chromeColorName()))
			}
		}

		// width is derived from totalWidth/splitMode (both already decided
		// just above, from pages' own rect - always accurate regardless of
		// which page is frontmost), not list.GetInnerRect() directly - a
		// real, reported bug: tview only updates a primitive's own rect
		// during its next Draw() pass, which hasn't happened yet at this
		// point in rebuild() whenever THIS very call is what just changed
		// which page is frontmost (e.g. closeOutput's own
		// switchPage("main") followed immediately by rebuild()) - so
		// list.GetInnerRect() would still report whatever narrower width
		// it had as part of splitBody a moment ago. Reported live: closing
		// a two-pane drill-down left the host-column-shrink algorithm
		// (computeHostColumnLayout/flattenRows below) rendering far too
		// narrow, only correcting itself once some *other* event (a real
		// terminal resize) forced a genuine Draw() pass first. list fills
		// its own outer Flex row's entire width whenever "main" is
		// frontmost (same "topBar shares list's own width" reasoning just
		// below), so totalWidth itself already *is* list's own eventual
		// width in that case - deriving it directly sidesteps the stale-
		// rect problem entirely rather than working around it.
		width := totalWidth
		if splitMode {
			width = splitTreeWidth(totalWidth)
		}
		// Belt-and-suspenders only: taskLabel is panic-safe for any width,
		// but clamp defensively in case totalWidth is ever unexpectedly
		// tiny (e.g. before Run()'s first real-size draw pass).
		if width < 20 {
			width = 20
		}
		if !splitMode {
			// topBar shares list's own width here (both are full-width
			// children of the same outer Flex row when "main" is
			// frontmost) - reused below for topBarText's own right-
			// alignment/truncation too rather than re-deriving a second
			// width from topBar.GetInnerRect(). Skipped entirely in split
			// mode, where splitHeader (above) shows this same information
			// instead - topBar itself sits unused, off-page, for the
			// duration of a split session.
			topBar.SetText(topBarText(playbookName, isRole, state.AllHosts, elapsed, frozen, currentFilter, progressPos, progressTotal, width, chromeColorName(), showElapsed()))
		}

		// One-time, right on the running-to-frozen transition: for a
		// genuine failure (see genuineFailure - shared with statusRowText
		// below so the two can't disagree on what counts as one), jump
		// straight to the host that actually failed, expanding its task,
		// so a single Enter shows the drill-down with no navigation
		// needed. Must happen before flattenRows runs below, since it
		// reads expanded to decide which host rows to include - setting
		// it after would miss the newly-expanded row in this same pass.
		//
		// Gated on the failed task still matching the currently active
		// filter (Filters.md's own open question about this, resolved
		// once the search filter existed to make it a real case: "filter
		// wins, skip the auto-jump" - simpler than forcing a non-matching
		// row into view, and doesn't quietly break the filter's own
		// promise that only matching tasks are ever shown). isActive is
		// unconditionally false here rather than activeTaskNow() - a frozen
		// run has no in-progress task by definition, so there's no need to
		// even call it. A/C/F can't actually trigger this: a failed task
		// always matches "Changed" and "Failed" by definition, so only a
		// search term that happens not to match the failure can skip the
		// jump.
		if frozen && !failureCursorPlaced {
			failureCursorPlaced = true
			if genuineFailure(int(exitCode.Load()), state.HadUnreachable) {
				if t, h := lastFailedTaskAndHost(state); t != nil && taskVisible(t, currentFilter, sourceIndex, false) {
					expanded[t] = true
					currentID = hostRowID{t, h}
					following = false
				}
			}
		}

		activeTask := activeTaskNow()

		// treeAllHosts is state.AllHosts normally, or nil while a two-pane
		// drill-down session is open (design-docs/TwoPanedLayout.md): hosts
		// aren't shown on collapsed tree rows in that mode (the drill-down
		// pane already shows exactly which host is selected - see
		// showOutput's live-sync). computeHostColumnLayout/taskLabel both
		// already have a documented allHosts == nil fallback - no shared
		// column, title rendered alone against avail - normally only
		// reachable transiently before the run's first host reports
		// anything; reused here deliberately rather than adding a second
		// code path. state.AllHosts itself is untouched - only these two
		// call sites (and the selected-row re-render below) see the
		// override, so the top bar/filters/etc. keep seeing the real list.
		treeAllHosts := state.AllHosts
		if splitMode {
			treeAllHosts = nil
		}

		// Computed once per rebuild and reused for every row - both
		// flattenRows' own per-row taskLabel calls and the standalone
		// selected-row re-render just below - so the cursor row always
		// aligns to the identical column every other row uses (see
		// computeHostColumnLayout).
		layout := computeHostColumnLayout(state, treeAllHosts, width, !useColor)

		currentRows = flattenRows(state, expanded, width, layout, treeAllHosts, activeTask, spinnerAt(elapsed), currentFilter, sourceIndex, showOutput, useColor)
		hasStatusRow := false
		if frozen && everStarted {
			if text := statusRowText(int(exitCode.Load()), state.HadUnreachable); text != "" {
				currentRows = append(currentRows,
					row{text: "", id: statusDividerRowID{}},
					row{text: text, id: statusRowID{}},
				)
				hasStatusRow = true
			}
			// Recap (design-docs/Recap.md) - appended below the status rows
			// regardless of whether one was actually shown, so this doesn't
			// silently disappear if statusRowText's own "always non-empty"
			// guarantee ever changes. Rendered as more rows in the exact
			// same flat list the live tree already uses, not a separate
			// page - Home/End/PageUp/PageDown/arrow navigation all already
			// work on it for free this way. A blank spacer, the "Summary"
			// heading, its underline, and another blank spacer come first,
			// setting the section off visually from the status line above.
			currentRows = append(currentRows,
				row{text: "", id: recapDividerBeforeHeading},
				row{text: recapHeadingRowText(), id: recapHeadingRow},
				row{text: recapHeadingUnderlineRowText(), id: recapHeadingUnderlineRow},
				row{text: "", id: recapDividerAfterHeading},
				row{text: recapNarrativeRowText(state, elapsed), id: recapNarrativeRow},
				row{text: "", id: recapDividerAfterNarrative},
			)
			currentRows = append(currentRows, flattenRecapRows(state, recapHostExpanded, recapCategoryExpanded, showOutput)...)
		}

		if len(currentRows) == 0 {
			list.Clear()
			lastAppliedSelectedIndex = -1 // whatever appears once real rows
			// exist again must be treated as a genuine first selection, not
			// coincidentally matched against whatever index was applied
			// before everything was cleared.
			return
		}

		// Determine which row the cursor belongs on *before* AddItem, not
		// after - see the patch step right below, which needs to know this
		// to re-render that one row's text. following pins to the newest
		// *real* row; otherwise restore by currentID's identity (row order
		// shifts as things are appended, so a raw index can't be trusted
		// across rebuilds), defaulting to 0 if that id no longer exists
		// (shouldn't happen - nothing is ever removed - but not indexing
		// out of range if it somehow did).
		selectedIndex := 0
		if following {
			// Skip back past the trailing status/divider rows (see
			// statusRowText) - they have no selected-row rendering
			// variant (see the switch below), so following would
			// otherwise land the cursor on a row that looks identical
			// whether selected or not: from the user's perspective, the
			// cursor simply vanishes once a run finishes. Landing on the
			// last real row instead keeps the existing, visible
			// highlight - this now always applies, since statusRowText
			// stopped ever returning "" for a finished run.
			selectedIndex = len(currentRows) - 1
			for selectedIndex > 0 {
				_, isDivider := currentRows[selectedIndex].id.(statusDividerRowID)
				_, isStatus := currentRows[selectedIndex].id.(statusRowID)
				if !isDivider && !isStatus {
					break
				}
				selectedIndex--
			}
		} else {
			for i, r := range currentRows {
				if r.id == currentID {
					selectedIndex = i
					break
				}
			}
		}

		// Re-render just the row under the cursor with its selected
		// styling (see playRowText/taskLabel/hostLabel's own selected
		// parameter, and NewLiveTUI's SetSelectedStyle comment for why
		// this is done here rather than via a single List-wide style).
		// statusRowID/statusDividerRowID rows have no selected variant and
		// fall through untouched - they have no selected callback either
		// (see flattenRows), so the cursor never deliberately lands there
		// via Enter, only by navigating past them.
		switch id := currentRows[selectedIndex].id.(type) {
		case *playNode:
			currentRows[selectedIndex].text = playRowText(id, true)
		case *taskNode:
			currentRows[selectedIndex].text = taskLabel(id, treeAllHosts, layout, width, id == activeTask, spinnerAt(elapsed), true, useColor)
		case hostRowID:
			currentRows[selectedIndex].text = hostLabel(id.task, id.host, true)
		case recapHostRowID:
			currentRows[selectedIndex].text = recapHostRowText(string(id), recapForHost(state, string(id)), recapComputeColumnWidths(state), true)
		case recapCategoryRowID:
			for _, cat := range recapForHost(state, id.host).Categories {
				if cat.Label == id.label {
					currentRows[selectedIndex].text = recapCategoryRowText(cat, true)
					break
				}
			}
		case recapTaskRowID:
			detail := recapTaskDetail(id.task, id.host, id.label)
			currentRows[selectedIndex].text = recapTaskRowText(id.task, detail, recapCategoryColor(id.label), true)
		}

		list.Clear()
		for _, r := range currentRows {
			r := r
			var selected func()
			if r.selected != nil {
				selected = func() {
					r.selected()
					rebuild()
					if t, ok := r.id.(*taskNode); ok && expanded[t] {
						revealExpandedTask(t)
					}
				}
			}
			list.AddItem(r.text, selected)
		}
		if selectedIndex == lastAppliedSelectedIndex {
			// Same logical selection as last time - just reassert it after
			// Clear()/AddItem() reset list's own currentItem, without
			// re-clamping the viewport (see restoreCurrentItem's doc
			// comment and lastAppliedSelectedIndex's above).
			list.restoreCurrentItem(selectedIndex)
		} else {
			list.SetCurrentItem(selectedIndex)
			lastAppliedSelectedIndex = selectedIndex
		}

		// Reveal the trailing status row(s) on the running-to-frozen
		// transition, same bug class revealExpandedTask already exists
		// for: following's own selectedIndex deliberately stays on the
		// last *real* row (see above, past the status divider/text rows,
		// which have no selected-row rendering), and that row was already
		// this list's currentItem throughout the run (following kept it
		// pinned to the newest row as it streamed in) - so
		// SetCurrentItem's index doesn't actually change here, and
		// treeList's own ensureVisible (only runs on a genuine index
		// change) never fires. Reported live: once a run's output filled
		// more than one screen, the final "Playbook completed..." line
		// stayed just below the bottom edge until manually scrolled to.
		// Gated on following - once the user has navigated away (or the
		// failure-cursor auto-jump above has already turned it off),
		// their own cursor placement wins and this must not fight it by
		// yanking the view back down to the status row.
		if following && hasStatusRow {
			if _, _, _, height := list.GetInnerRect(); height > 0 {
				desired := len(currentRows) - 1 - height + 1
				if desired > list.GetOffset() {
					list.SetOffset(desired)
				}
			}
		}
	}

	// navigateOutputHost moves the output page to the previous/next host
	// (delta -1/+1) within outputTask's own HostOrder - the same order the
	// expanded tree rows for that task already use. A no-op at either end
	// (no wraparound) and before any output has been shown yet.
	navigateOutputHost := func(delta int) {
		if outputTask == nil {
			return
		}
		hosts := outputTask.HostOrder
		idx := -1
		for i, h := range hosts {
			if h == outputHost {
				idx = i
				break
			}
		}
		if idx == -1 {
			return
		}
		newIdx := idx + delta
		if newIdx < 0 || newIdx >= len(hosts) {
			return
		}
		showOutput(outputTask, hosts[newIdx])
	}

	// expandAll/collapseAll back the main tree's E/C shortcuts.
	// collapseAll's cursor-fallback: if the cursor was on a host row, that
	// row is about to disappear - snap currentID to its enclosing task
	// (still visible, now collapsed) rather than letting rebuild() fall
	// back to index 0.
	expandAll := func() {
		for _, t := range allTasks(state) {
			expanded[t] = true
		}
		// Extends to the recap section too (design-docs/Recap.md), for
		// consistency with E's own "expand everything" meaning everywhere
		// else in this app.
		for _, host := range state.AllHosts {
			recapHostExpanded[host] = true
			for _, cat := range recapForHost(state, host).Categories {
				recapCategoryExpanded[recapCategoryRowID{host: host, label: cat.Label}] = true
			}
		}
		rebuild()
	}
	collapseAll := func() {
		switch id := currentID.(type) {
		case hostRowID:
			currentID = id.task
		case recapTaskRowID:
			currentID = recapHostRowID(id.host)
		case recapCategoryRowID:
			currentID = recapHostRowID(id.host)
		}
		expanded = map[*taskNode]bool{}
		recapHostExpanded = map[string]bool{}
		recapCategoryExpanded = map[recapCategoryRowID]bool{}
		rebuild()
	}

	// handleRight/handleLeft back the main tree's cursor-Right/cursor-Left
	// expand/collapse shortcuts - they act on whichever row is currently
	// under the cursor (currentRows[list.GetCurrentItem()]), not on
	// currentID, since the cursor's actual on-screen position is what the
	// user means by "this element".
	handleRight := func() {
		idx := list.GetCurrentItem()
		if idx < 0 || idx >= len(currentRows) {
			return
		}
		switch id := currentRows[idx].id.(type) {
		case *taskNode:
			if !expanded[id] {
				expanded[id] = true
				rebuild()
				revealExpandedTask(id)
			}
		case recapHostRowID:
			if !recapHostExpanded[string(id)] {
				recapHostExpanded[string(id)] = true
				rebuild()
			}
		case recapCategoryRowID:
			if !recapCategoryExpanded[id] {
				recapCategoryExpanded[id] = true
				rebuild()
			}
		}
		// Already-expanded task/host/category, a recap task line, a host
		// row, or a play row: no-op - see Keyboard-shortcuts.md's "Right
		// on an already-expanded element" decision.
	}
	handleLeft := func() {
		idx := list.GetCurrentItem()
		if idx < 0 || idx >= len(currentRows) {
			return
		}
		switch id := currentRows[idx].id.(type) {
		case *taskNode:
			if expanded[id] {
				expanded[id] = false
				rebuild()
			}
		case hostRowID:
			// Collapsing the parent task removes this row - move the
			// cursor up to the task row that's left behind, per
			// Keyboard-shortcuts.md.
			expanded[id.task] = false
			currentID = id.task
			following = false
			rebuild()
		case recapHostRowID:
			if recapHostExpanded[string(id)] {
				recapHostExpanded[string(id)] = false
				rebuild()
			}
		case recapCategoryRowID:
			if recapCategoryExpanded[id] {
				recapCategoryExpanded[id] = false
				rebuild()
			}
		case recapTaskRowID:
			// Collapsing the parent category removes this row - move the
			// cursor up to the category row left behind, same reasoning
			// as hostRowID above.
			categoryID := recapCategoryRowID{host: id.host, label: id.label}
			recapCategoryExpanded[categoryID] = false
			currentID = categoryID
			following = false
			rebuild()
		}
		// Play row: no-op, plays aren't collapsible.
	}

	// navigateMainTask moves the cursor to the previous/next task (delta
	// -1/+1) in run order, among currently-visible tasks only (see
	// visibleTasks - never targets a task the active filter is hiding,
	// since flattenRows wouldn't have given it a row to land on), expanding
	// it if necessary. If the cursor was on a specific host of the current
	// task, the same host is preserved on the destination task when that
	// task has already recorded a result for it; otherwise the cursor lands
	// on the destination task's own row. From a play row, "next" is that
	// play's own first visible task; "prev" is whichever visible task comes
	// immediately before that in the visible sequence - which, since
	// visibleTasks skips hidden tasks (and, transitively, plays with none
	// visible) entirely, naturally lands on the previous *visible* play's
	// last visible task without needing to search play-by-play - per
	// Keyboard-shortcuts.md.
	navigateMainTask := func(delta int) {
		idx := list.GetCurrentItem()
		if idx < 0 || idx >= len(currentRows) {
			return
		}

		vis := visibleTasks(state, currentFilter, sourceIndex, activeTaskNow())
		var target *taskNode
		var host string
		haveHost := false

		switch id := currentRows[idx].id.(type) {
		case *playNode:
			first := firstVisibleTask(id, taskSet(vis))
			if first == nil {
				return
			}
			pos := -1
			for i, t := range vis {
				if t == first {
					pos = i
					break
				}
			}
			if delta > 0 {
				target = first
			} else if pos > 0 {
				target = vis[pos-1]
			}
		case *taskNode:
			for i, t := range vis {
				if t == id {
					if newIdx := i + delta; newIdx >= 0 && newIdx < len(vis) {
						target = vis[newIdx]
					}
					break
				}
			}
		case hostRowID:
			for i, t := range vis {
				if t == id.task {
					if newIdx := i + delta; newIdx >= 0 && newIdx < len(vis) {
						target = vis[newIdx]
						host = id.host
						haveHost = true
					}
					break
				}
			}
		}

		if target == nil {
			return
		}
		expanded[target] = true
		if _, ok := target.Hosts[host]; haveHost && ok {
			currentID = hostRowID{target, host}
		} else {
			currentID = target
		}
		following = false
		rebuild()
	}

	// openFilterDialog/openSearchDialog/closeDialogs/applyFilter back the
	// 'f'/'/' shortcuts and the two dialogs themselves (Filters.md). Both
	// dialogs are fully modal (see SetInputCapture/SetMouseCapture below)
	// so these are the only places filterDialogOpen/searchDialogOpen/
	// currentFilter ever change.
	openFilterDialog := func() {
		filterDialogOpen = true
		filterDialog.SetText(filterDialogText(currentFilter))
		pages.ShowPage("filter")
	}
	// openSearchDialog pre-fills the box with the previous term whenever
	// one is already active (Filters.md's explicit "reopening the dialog
	// while the search filter is already active should show it right
	// away") and, unlike the old combined dialog, moves keyboard focus
	// into it immediately - there's no menu to browse first in a
	// search-only dialog, so there's nothing to wait for before typing.
	openSearchDialog := func() {
		searchDialogOpen = true
		if currentFilter.mode == filterSearch {
			searchInput.SetText(currentFilter.search)
		} else {
			searchInput.SetText("")
		}
		pages.ShowPage("search")
		app.SetFocus(searchInput)
	}
	// closeDialogs closes whichever of the three dialogs is currently open
	// (harmless no-op on the other two) with no filter/search/rerun change
	// - shared by all three dialogs' own Esc/q/Ctrl-C handling below, by
	// applyFilter, and by submitRerun, so there's exactly one place that
	// resets this state and refocuses the main tree.
	closeDialogs := func() {
		filterDialogOpen = false
		searchDialogOpen = false
		rerunDialogOpen = false
		pages.HidePage("filter")
		pages.HidePage("search")
		pages.HidePage("rerun")
		app.SetFocus(list) // undo openSearchDialog's/openRerunDialog's
		// SetFocus above, if either ran - harmless no-op if neither did
		// (list already has focus in that case).
	}
	// tagsPreFilled/skipTagsPreFilled/hostsPreFilled latch true the first
	// time openRerunDialog ever pre-fills each field, independent of what
	// the field then contains - deliberately not re-derived from
	// GetText() == "" on every open (that was the original design, and a
	// real bug caught live: clearing a field to "" is itself a
	// meaningful, intentional edit - Reassemble treats an empty Hosts as
	// "no --limit, all hosts" - but GetText() == "" can't tell that apart
	// from "never touched," so the *next* open silently re-pre-filled
	// over the user's own deliberate choice to clear it). A one-time
	// latch has no such ambiguity: once a field has been pre-filled once,
	// it is never touched by this function again, regardless of what the
	// user does with it afterward, empty included.
	var tagsPreFilled, skipTagsPreFilled, hostsPreFilled bool

	// openRerunDialog (Rerun.md's 'r' key - see SetInputCapture below,
	// gated there on processDone since re-running only makes sense once a
	// run has finished). Task is deliberately never pre-filled (see
	// rerunForm's own doc comment for why the original cursor-based
	// pre-fill design was dropped) - like Tags/Hosts, it just keeps
	// whatever was last typed into it, empty on the very first open of the
	// session. Tags/Hosts specifically are pre-filled from initialTags/
	// initialHosts (this process's own invocation) only the very first
	// time each is opened at all - see tagsPreFilled/skipTagsPreFilled/
	// hostsPreFilled above - every open after that leaves the field alone,
	// whatever it now contains.
	openRerunDialog := func() {
		rerunDialogOpen = true

		if !tagsPreFilled {
			tagsField.SetText(initialTags)
			tagsPreFilled = true
		}
		if !skipTagsPreFilled {
			skipTagsField.SetText(initialSkipTags)
			skipTagsPreFilled = true
		}
		if !hostsPreFilled {
			hostsField.SetText(initialHosts)
			hostsPreFilled = true
		}

		rerunForm.SetFocus(0) // always start on the Task field, not
		// wherever focus happened to be left inside the form the last time
		// it closed.
		pages.ShowPage("rerun")
		app.SetFocus(rerunForm)
	}
	// applyFilter switches to newFilter (a no-op switch still closes
	// whichever dialog is open, matching "when the user presses a/c/f the
	// respective filter shall be activated and the window shall be closed
	// again" - the search dialog's own Enter-to-apply, wired up on
	// searchInput's SetDoneFunc below, funnels through here too).
	//
	// If the cursor is currently pinned to a specific row (following ==
	// false - if it's true, rebuild() already re-resolves the selection to
	// the newest *visible* row every time, so there's nothing to fix up),
	// and that row's task won't survive the new filter, this moves
	// currentID to the nearest still-visible task first (see
	// nearestVisibleTask) - Filters.md's "cursor moves to the nearest
	// still-visible ancestor" requirement. A task is always the right
	// granularity to land on here: per Filters.md, a filter can only ever
	// hide a whole task (and, transitively, a whole play with none left) at
	// once, never an individual host row on its own - unlike collapsing a
	// task, which removes host rows one task at a time while the task's own
	// row stays put, a filter switch never leaves a "row still there, just
	// fall back to it" case to fall back to.
	applyFilter := func(newFilter filterQuery) {
		if newFilter != currentFilter && !following {
			activeTask := activeTaskNow()
			var anchor *taskNode
			switch id := currentID.(type) {
			case *taskNode:
				anchor = id
			case hostRowID:
				anchor = id.task
			case *playNode:
				stillVisible := false
				for _, t := range id.Tasks {
					if taskVisible(t, newFilter, sourceIndex, t == activeTask) {
						stillVisible = true
						break
					}
				}
				if !stillVisible && len(id.Tasks) > 0 {
					anchor = id.Tasks[0]
				}
			}
			if anchor != nil && !taskVisible(anchor, newFilter, sourceIndex, anchor == activeTask) {
				if nt := nearestVisibleTask(allTasks(state), anchor, visibleTasks(state, newFilter, sourceIndex, activeTask)); nt != nil {
					currentID = nt
				}
			}
		}
		currentFilter = newFilter
		closeDialogs()
		rebuild()
	}

	// searchInput.SetDoneFunc fires on Enter/Esc/Tab/Backtab - InputField's
	// own fixed set of "done" keys (confirmed against inputfield.go),
	// reached because searchDialogOpen tells SetInputCapture below to let
	// these through untouched rather than intercepting them like the
	// filter dialog's own keys. Only Enter actually applies the typed
	// term; Esc/Tab/Backtab all just cancel back out - there's nothing
	// else in this dialog to Tab to, and Filters.md only ever specifies
	// Esc for "close with no change" anyway. 'q' deliberately does *not*
	// appear here - unlike the filter dialog's plain menu, this box must
	// accept 'q' as ordinary typed text (a search term can contain the
	// letter q), so it's never treated as a shortcut while typing.
	searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			applyFilter(filterQuery{mode: filterSearch, search: searchInput.GetText()})
		} else {
			closeDialogs()
		}
	})

	// Mouse-only Search/Cancel buttons, added below the input field -
	// design-docs/Tabbed UI.md's sibling mouse-support pass, closing the
	// gap where this dialog's own InputField had no click-based way to
	// apply/cancel (Enter/Esc already fully cover the keyboard path, so
	// these exist purely for a mouse user). Deliberately plain
	// tview.Buttons, not folded into a tview.Form the way rerunForm below
	// is: searchInput already repurposes Enter/Esc/Tab/Backtab away from
	// their native meaning via SetDoneFunc above, and Form.Focus()
	// silently overwrites a FormItem's own SetFinishedFunc on every focus
	// change to drive its own Tab-cycling - mixing the two would mean both
	// callbacks firing off the same keypress (confirmed against
	// inputfield.go's own "finish" closure, which calls done then finished
	// unconditionally), risking Form's own internal re-focus undoing
	// closeDialogs' app.SetFocus(list) right after it runs. Not worth the
	// risk for two small buttons whose keyboard path already works fully -
	// these are click-only, not Tab-reachable.
	searchApplyButton := tview.NewButton("Search").SetSelectedFunc(func() {
		applyFilter(filterQuery{mode: filterSearch, search: searchInput.GetText()})
	})
	searchCancelButton := tview.NewButton("Cancel").SetSelectedFunc(closeDialogs)
	// A real tview.NewBox(), not a bare nil, for every spacer item below -
	// discovered live (reported as content from behind the dialog "showing
	// through" wherever a blank line/gap sits): a nil Flex item reserves
	// space but draws nothing into it, and unlike a real child primitive
	// (which fills its own cells via its own Draw()), nothing ever
	// repaints that gap's cells on top of whatever this dialog's own
	// containing Pages "search" page previously drew underneath. A plain
	// Box has no visible content of its own, but its Draw() still runs and
	// fills its own rect with the dialog's background - solid, not
	// see-through.
	searchDialogFlex.
		AddItem(tview.NewBox(), 1, 0, false).
		// Right-aligned, Cancel-then-Search - same convention as rerunForm's
		// own AddButton("Cancel", ...).AddButton("Re-run", ...) pair below
		// (matches the template page's host dialog): the rightmost button
		// is always the primary/default action.
		AddItem(tview.NewFlex().
							AddItem(tview.NewBox(), 0, 1, false).
							AddItem(searchCancelButton, 10, 0, false).
							AddItem(tview.NewBox(), 2, 0, false).
							AddItem(searchApplyButton, 10, 0, false).
							AddItem(tview.NewBox(), 1, 0, false), 1, 0, false).
		AddItem(tview.NewBox(), 1, 0, false) // bottom margin

	list.SetChangedFunc(func(index int) {
		if rebuilding {
			// rebuild()'s own trailing selection-apply call (see
			// lastAppliedSelectedIndex) only ever reaches list.SetCurrentItem
			// - and so only ever fires this callback - when the selection
			// has genuinely changed since the last rebuild; a no-op
			// reselection goes through restoreCurrentItem instead, which
			// never calls this at all. So on a genuine change, this guard's
			// only remaining job is to stop that same SetCurrentItem call
			// from recursing into rebuild() again: rebuild() sets rebuilding
			// true for its entire body - Clear(), every AddItem(), and its
			// own final selection-apply call - so any "changed" event that
			// cascades from within it lands here while rebuilding is still
			// true and is correctly ignored instead.
			return
		}
		if index >= 0 && index < len(currentRows) {
			currentID = currentRows[index].id
		}
		if !jumpingToEnd {
			following = false
		}
		// A genuine navigation (this is the row's *text*, not just List's
		// own current-item pointer) now carries the selected-row styling -
		// see rebuild's selected-row patch - so it must re-render on every
		// real cursor move, not just when new data arrives or the next
		// heartbeat tick happens to fire (which stops entirely once the
		// run is frozen - without this, the highlight would never move at
		// all after a run finishes).
		rebuild()
	})

	state.OnPlayAdded = func(*playNode) { rebuild() }
	// Fires for every real play, including one whose hosts: pattern
	// matches nothing in this run - see aggregate.go's OnPlayStarted and
	// progressTracker.AdvanceToPlay for why this resync exists at all
	// (an entirely-skipped play's tasks never fire a single event of
	// their own for Advance, below, to ever match against).
	state.OnPlayStarted = func(name string) { progH.Load().AdvanceToPlay(name) }
	// See inheritedExpandState's own doc comment for the decision itself -
	// this is what makes 'E' (expandAll) "sticky" for a still-running
	// playbook: every task added afterward inherits true from whichever
	// task was added most recently, not just the ones already on screen
	// when 'E' was pressed.
	state.OnTaskAdded = func(play *playNode, task *taskNode) {
		expanded[task] = inheritedExpandState(allTasks(state), expanded, startExpanded)
		// A miss here (a handler - see progress.go's own doc comment -
		// or any task the skeleton couldn't predict) is a silent no-op by
		// design: progressTracker.Advance leaves its own state untouched
		// rather than treating "not found" as a regression.
		progH.Load().Advance(play.Name, task.Name)
		rebuild()
	}
	state.OnHostRecorded = func(*taskNode, string) { rebuild() }

	bottomBar = tview.NewTextView().SetText(currentMainBottomBarText())
	bottomBar.SetTextStyle(chromeStyle)

	flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(topBar, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(bottomBar, 1, 0, false)

	// filterFlex wraps filterDialog's own A/C/F menu together with a real,
	// right-aligned Cancel button below it - built here rather than back
	// where filterDialog itself was constructed, since it needs
	// closeDialogs (defined above) for the button's own click handler.
	// Esc/q still close the dialog too (SetInputCapture's own
	// filterDialogOpen branch, unchanged) - the button is an added mouse
	// affordance, not a replacement for those.
	filterCancelButton := tview.NewButton("Cancel").SetSelectedFunc(closeDialogs)
	// A real tview.NewBox() for every spacer item, not a bare nil - see
	// searchDialogFlex's own button row above for why (a nil Flex item
	// draws nothing, so nothing ever repaints its cells over whatever the
	// main tree drew underneath there - a real Box's Draw() fills its own
	// rect with the dialog's background even though it shows no content).
	filterFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox(), 1, 0, false). // top margin
		AddItem(filterDialog, 0, 1, false).
		AddItem(tview.NewFlex().
							AddItem(tview.NewBox(), 0, 1, false).
							AddItem(filterCancelButton, 10, 0, false).
							AddItem(tview.NewBox(), 1, 0, false), 1, 0, false).
		AddItem(tview.NewBox(), 1, 0, false) // bottom margin
	filterFlex.SetBorder(true).SetTitle(" Filter ")

	// splitDivider is a one-column-wide vertical rule between the two panes
	// - a bare Box with no content, whose Draw() (like every tview
	// Primitive's) unconditionally fills its own rect with its background
	// color, so a solid column is all it takes; no text/rune content needed
	// for a plain vertical line. Colored to match barStyle's own background
	// (tcell.ColorNavy, the same blue every top/bottom bar already uses),
	// not the brighter named "blue", so the divider reads as part of the
	// same chrome rather than a clashing second shade. Deliberately never
	// repainted by rebuild's own split-mode progress-fill coordination
	// (above) - a live report: it's a fixed structural separator between
	// the two panes, not part of the data being visualized, and turning it
	// green as the fill swept past it read as wrong, not as "one seamless
	// bar." Only spans the body rows now (see treeBody/splitBody below) -
	// splitHeader's own row has no separate divider glyph of its own at
	// all, per a second live report: the single column directly above
	// this divider, in the header row, should participate in the header's
	// own fill exactly like every other character there, not read as part
	// of the (fixed, unfilled) separator below it.
	splitDivider = tview.NewBox().SetBackgroundColor(chromeBg)

	// treeBody/outputBody are the tree pane's/drill-down pane's own
	// bodies with their individual header rows carved out - list/
	// bottomBar and outputTabs/outputBottomBar respectively, the exact
	// same primitives flex/outputFlex already use, reused here the same
	// way flex/outputFlex themselves are already reused between their own
	// standalone pages and splitFlex (see splitFlex's own doc comment
	// below) - safe for the identical reason: only one of "main"/
	// "output"/"split" is ever frontmost, so list/bottomBar are never
	// actually drawn via two different parents at once.
	treeBody = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(bottomBar, 1, 0, false)
	outputBody := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(outputTabs.Primitive(), 0, 1, true).
		AddItem(outputBottomBar, 1, 0, false)

	// splitBody is the two-pane row itself - treeBody alongside
	// outputBody, with splitDivider between them - everything splitFlex
	// used to be before splitHeader existed. treeBody's own width here is
	// just a placeholder; showOutput sets it for real via ResizeItem on
	// every fresh drill-down open, once the terminal's actual current
	// width is known (splitTreeWidth).
	splitBody = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(treeBody, splitMinTreeWidth, 0, false).
		AddItem(splitDivider, splitDividerWidth, 0, false).
		AddItem(outputBody, 0, 1, true)

	// splitFlex is design-docs/TwoPanedLayout.md's two-pane drill-down:
	// splitHeader (a single bar spanning the terminal's true full width -
	// see its own doc comment for why it replaces topBar/outputTopBar
	// entirely here, rather than each pane keeping its own) above
	// splitBody's own two-column row.
	splitFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(splitHeader, 1, 0, false).
		AddItem(splitBody, 0, 1, false)

	pages.AddPage("main", flex, true, true)
	pages.AddPage("output", outputFlex, true, false)
	pages.AddPage("split", splitFlex, true, false)
	pages.AddPage("filter", centeredModal(filterFlex, 46, 10), true, false)
	pages.AddPage("search", centeredModal(searchDialogFlex, 46, 11), true, false)
	pages.AddPage("rerun", centeredModal(rerunForm, 56, 13), true, false)

	app = tview.NewApplication().SetRoot(pages, true)

	// Terminal color-capability probe, design-docs/Morehosts.md:
	// Application.Screen() isn't available until after Run() starts, but
	// useColor (below) is needed well before that, on every rebuild - so
	// the tcell.Screen is created here instead and handed to Application
	// via SetScreen() before Run() is ever called. Confirmed against
	// tview's own source (application.go): SetScreen calls screen.Init()
	// itself when Run() hasn't started yet ("Run() has not been called
	// yet" branch), and Run() itself skips creating/initializing its own
	// screen whenever one is already set ("Make a screen if there is none
	// yet"). tcell.NewScreen() failing at all would be unexpected (this
	// app already requires a real TTY - see CLAUDE.md's Commands section)
	// but isn't treated as fatal here: on failure, terminalSupportsColor
	// just defaults to true (today's assumption, unchanged) and Run()
	// falls back to creating its own screen exactly as it always has.
	//
	// Ordering below matters and is the one genuinely risky part of this
	// change: SetScreen must happen *before* EnableMouse(true), since
	// Application.EnableMouse only actually calls screen.EnableMouse() when
	// a.screen != nil at the moment it's called (confirmed against the
	// same source) - calling EnableMouse(true) first, the way this used to
	// read as one chained expression, would silently leave the mouse never
	// enabled on a screen supplied via SetScreen afterward, since Run()'s
	// own "make a screen" branch (the only other place EnableMouse gets
	// applied to a screen) never runs when a.screen is already non-nil.
	terminalSupportsColor := true
	if screen, err := tcell.NewScreen(); err == nil {
		app.SetScreen(screen)
		terminalSupportsColor = screen.Colors() > 1
	}
	app.EnableMouse(true)
	// Everything else falls out of tview's own defaults once mouse events
	// are actually turned on (previously never enabled): List's/TextView's
	// built-in mouse wheel handling already just pans the viewport without
	// touching selection, and List's built-in click handling already fires
	// a row's Selected() callback on the first click - identical to Enter -
	// so a host row's output already opens on a single click, and no
	// custom double-click wiring is needed on top of that.

	// useColor, design-docs/Morehosts.md: whether the collapsed task row's
	// per-host summary (see computeHostColumnLayout/taskLabel) may ever
	// render in color - all three of terminal capability, the NO_COLOR
	// convention (https://no-color.org - presence disables color
	// regardless of value, even ""; hence LookupEnv's ok result, not the
	// value itself), and the user's own general.color setting must permit
	// it. Computed once - none of the three can change mid-session -
	// and captured by rebuild()'s closure below, the same way twoPaneLayout
	// already is.
	_, noColorSet := os.LookupEnv("NO_COLOR")
	useColor = terminalSupportsColor && !noColorSet && colorEnabled

	// Top-bar heartbeat ticker - the first self-driven (not event- or
	// input-triggered) source of QueueUpdateDraw calls in this codebase.
	// Pulled out into a named closure, rather than a bare inline goroutine,
	// specifically so the 'r' key handler below can call it again to
	// resume ticking for a rerun (Rerun.md) - the ticker that started
	// alongside the first invocation permanently returns once it observes
	// processDone true (see its own comment below), so a later generation
	// needs a fresh one.
	startHeartbeat := func() {
		go func() {
			ticker := time.NewTicker(spinnerInterval)
			defer ticker.Stop()
			for range ticker.C {
				if quitting.Load() {
					return // mirrors main.go's streamEvents guard: tview's
					// update queue is a fixed 100-slot buffer nothing drains
					// once the app has stopped, so a goroutine blocked inside
					// QueueUpdateDraw past that point hangs forever. Unlike
					// streamEvents, nothing in main.go waits on this
					// goroutine, so such a hang wouldn't itself block process
					// exit - but there's no reason to rely on that.
				}
				done := processDone.Load()
				app.QueueUpdateDraw(rebuild)
				if done {
					return // one frozen frame pushed above; stop ticking
					// rather than redrawing a static screen forever - until
					// startHeartbeat is called again for a later rerun.
				}
			}
		}()
	}
	// Deliberately no early, pre-Run() rebuild() call for a revisit session,
	// even though its state/processDone/exitCode are already fully
	// populated by this point (see revisit.go) and there'd be real content
	// to show immediately, sparing the ~200ms blank flash before the
	// heartbeat ticker's own first tick below. Tried exactly that and
	// reverted it - a real, reported bug: list has no genuine rect yet at
	// this point (app.Run() hasn't started laying anything out), so
	// ensureVisible/the "reveal trailing status rows" scroll-to-bottom
	// logic inside rebuild() (both below) compute against a bogus size,
	// landing itemOffset somewhere wrong - and since the very next
	// rebuild() (the heartbeat's one tick, once Run() has given list a
	// real rect) sees an unchanged selectedIndex, it takes the
	// restoreCurrentItem path, which deliberately never touches itemOffset
	// - so nothing ever corrects the bogus position on its own, unlike a
	// live run/rerun/role session (which only ever calls rebuild() after
	// Run() has already given every widget a real size). A brief blank
	// flash before the heartbeat's first tick - the same startup
	// experience every other verb already has - is the trade worth making
	// here, not a real regression.
	// Placed after `app` is assigned: the go statement inside
	// startHeartbeat's closure body has a happens-before edge (Go memory
	// model) with this very call, which itself runs after `app` was
	// assigned - if startHeartbeat were defined or first called any
	// earlier, reading `app` from the ticker goroutine would be a genuine
	// data race, not just a latency curiosity, even though the first tick
	// is spinnerInterval away.
	startHeartbeat()

	// resizeWatcher: a second, permanent ticker, deliberately independent of
	// startHeartbeat's own per-generation running/frozen lifecycle (unlike
	// startHeartbeat, this is started exactly once and never restarted by
	// submitRerun). Its only job is noticing a bare terminal resize once the
	// run is frozen - startHeartbeat's own ticker already permanently stops
	// once processDone is observed true, so nothing else is left driving a
	// rebuild() on a terminal resize with no other incoming event. While a
	// run is still live, startHeartbeat's own ticker already re-syncs
	// everything within spinnerInterval regardless of resize - so this
	// goroutine skips its own work entirely until processDone. "Everything"
	// now includes the two-pane drill-down's own split-vs-full-screen mode
	// and tree-pane width, not just the tree's row text/column layout - see
	// rebuild()'s own resync block (design-docs/TwoPanedLayout.md) - so a
	// frozen run's drill-down reacts to a resize exactly as live one does,
	// via the same rebuild() call, just noticed by this ticker instead of
	// startHeartbeat's.
	go func() {
		ticker := time.NewTicker(spinnerInterval) // reused only as a
		// convenient existing interval - not tied to spinner-animation cadence.
		defer ticker.Stop()
		for range ticker.C {
			if quitting.Load() {
				return // same accepted best-effort guard startHeartbeat's own
				// ticker already uses - nothing waits on this goroutine, so a
				// hang here wouldn't itself block process exit.
			}
			if !processDone.Load() {
				continue // startHeartbeat's own ticker already handles this
				// case every spinnerInterval regardless of resize.
			}
			app.QueueUpdate(func() { // NOT QueueUpdateDraw - avoid forcing a
				// real screen redraw on every tick when nothing changed.
				_, _, totalWidth, _ := pages.GetInnerRect() // pages, not
				// list - the terminal's true current width regardless of
				// which page is frontmost (see rebuild()'s own
				// lastTotalWidth comment); using list here would miss a
				// resize entirely while a two-pane session has fixed list's
				// own width to the tree pane's share, or while viewing a
				// full-screen drill-down at all (list isn't part of that
				// page's own draw tree, so its rect goes stale).
				if totalWidth != lastTotalWidth {
					rebuild()
					// app.Draw() would deadlock here: it's QueueUpdate under
					// another name, and this closure is already running via
					// QueueUpdate - i.e. already on the event-loop goroutine -
					// so a nested QueueUpdate call would enqueue itself and
					// then block forever waiting for the event loop to loop
					// back and process it, which it structurally cannot do
					// while stuck inside this very call. ForceDraw() calls
					// a.draw() directly, no channel round-trip - and its own
					// doc comment says exactly this is safe: "safe to call
					// this function during queued updates and direct event
					// handling."
					app.ForceDraw()
				}
			})
		}
	}()

	// submitRerun (Enter while rerunDialogOpen - see SetInputCapture below)
	// reads the form's own current values, closes the dialog, and starts a
	// new generation the same way Phase B's direct requestRerun() call
	// used to - resetting this function's own view state and restarting
	// the heartbeat ticker - except now driven by the dialog's fields
	// instead of always repeating the original invocation verbatim.
	// Defined here rather than up with openRerunDialog/closeDialogs: it
	// closes over startHeartbeat, which - like this closure itself -
	// can't exist before `app` is assigned above.
	submitRerun := func() {
		startAtTask := strings.TrimSpace(taskField.GetText()) // empty means
		// "whole playbook" - see rerunForm's own doc comment.
		tags := strings.TrimSpace(tagsField.GetText())
		skipTags := strings.TrimSpace(skipTagsField.GetText())
		hosts := strings.TrimSpace(hostsField.GetText())
		closeDialogs()

		requestRerun(startAtTask, tags, skipTags, hosts) // resets
		// processDone/exitCode/state synchronously (see main.go) - by the
		// time this returns, rebuild() below already sees a running, empty
		// generation.
		expanded = map[*taskNode]bool{}
		recapHostExpanded = map[string]bool{}
		recapCategoryExpanded = map[recapCategoryRowID]bool{}
		currentID = nil
		following = true
		failureCursorPlaced = false
		haveFrozenElapsed = false
		frozenElapsed = 0
		lastAppliedSelectedIndex = -1 // a fresh generation's row 0 must not
		// be mistaken for "no change" just because it happens to match
		// whatever index the previous generation last applied.
		resolveCache = map[resolveKey]resolvedRender{} // a new generation
		// means new vars/facts - any cached "Resolved" render is for a
		// previous generation's own values and must not linger.
		everStarted = true // only a real transition the very first time
		// this fires for the "rerun" verb's startup dialog (see
		// startWithRerunDialog) - a harmless no-op reassignment every time
		// after that, since it's already true for every other case.
		if revisitActive {
			// A real generation is starting - this session is no longer
			// showing "old data," so the revisit chrome and the Esc-back-
			// to-the-list binding both go away, for good, for the rest of
			// this session (design-docs/Revisit.md). Reset directly on the
			// already-constructed widgets rather than via chromeStyle/
			// chromeBg (those only ever governed how things started out).
			revisitActive = false
			topBar.SetTextStyle(barStyle)
			outputTopBar.SetTextStyle(barStyle)
			outputBottomBar.SetTextStyle(barStyle)
			splitHeader.SetTextStyle(barStyle)
			bottomBar.SetTextStyle(barStyle)
			splitDivider.SetBackgroundColor(tcell.ColorNavy)
			// Style alone isn't enough for bottomBar: unlike topBar/
			// splitHeader (whose visible text is rebuilt from scratch
			// on every rebuild() call, always reading chromeColorName/
			// showElapsed/revisitActive fresh), bottomBar's own text is
			// a plain string baked in once at whichever point last set
			// it (construction, closeOutput, or rebuild's own split-
			// mode toggle) and never otherwise refreshed - a real bug
			// caught live: without this, "Esc: back to list" kept
			// showing (with the right style/color!) even after a
			// revisit session was promoted to a real rerun and Esc
			// had already stopped doing that.
			bottomBar.SetText(currentMainBottomBarText())
		}
		startedAt = time.Now()
		rebuild() // clear the previous run's rows immediately, rather than
		// leaving them on screen until the new generation's first event
		// arrives.
		startHeartbeat()
	}

	// Re-run/Cancel buttons - unlike searchDialogFlex's own buttons above,
	// rerunForm is already a real tview.Form (see its own doc comment), so
	// AddButton gets native keyboard Tab-reachability and mouse click
	// handling for free: Form.Focus() already cycles through f.items then
	// f.buttons on Tab/Backtab (unchanged by this addition, just extended
	// to include these two), and SetMouseCapture's existing rerunDialogOpen
	// pass-through (any click inside rerunForm's own rect reaches Pages'
	// native dispatch unchanged) already covers whatever rect Form ends up
	// drawing the buttons in - no separate mouse wiring needed here. The
	// one adjustment this requires is in SetInputCapture's own rerunDialogOpen
	// branch below: Enter must defer to Form's native "trigger the focused
	// button" behavior when a button has focus, rather than always calling
	// submitRerun regardless of focus the way it does for the text fields.
	// Cancel-left/Re-run-right, right-aligned against the form's own inner
	// edge (SetButtonsAlign) - matches the template page's host dialog
	// buttons and the general "affirmative action on the right" convention;
	// AddButton's own call order determines both visual left-to-right order
	// and Tab-cycling order (Form.Focus() walks f.buttons in the order
	// they were added), so this one call controls both at once.
	rerunForm.SetButtonsAlign(tview.AlignRight)
	rerunForm.AddButton("Cancel", closeDialogs).AddButton("Re-run", submitRerun)

	// closeOutput backs out of the output drill-down view. Used to be
	// tview.TextView's own native SetDoneFunc (firing on Escape/Enter/Tab/
	// Backtab, its fixed "done key" set) - now called explicitly for
	// Escape/Enter/q from SetInputCapture's own viewingOutput branch below,
	// since Tab/Backtab mean "switch tab" here now (design-docs/Tabbed
	// UI.md) rather than "close," and outputTabs' own per-tab TextViews are
	// recreated fresh on every renderOutputTabs call anyway, so there's no
	// single, persistent TextView left to hang a native SetDoneFunc off of.
	//
	// Restoring the main tree's own cursor to whatever (task, host) the
	// drill-down was last showing needs no work here anymore: showOutput's
	// own live-sync (design-docs/TwoPanedLayout.md) already keeps
	// expanded/currentID/following current on every call, including
	// whichever navigateOutputTask/navigateOutputHost call was the most
	// recent one before this fires - there's nothing left to reconcile on
	// the way out.
	closeOutput := func() {
		viewingOutput = false
		splitMode = false
		bottomBar.SetText(currentMainBottomBarText())
		switchPage("main")
		rebuild() // list's own row text was last baked while viewingOutput
		// was still true - possibly at the tree pane's own (narrower,
		// hosts-omitted) width rather than the full terminal's, especially
		// now that a resize can happen live while split is open
		// (design-docs/TwoPanedLayout.md) - a plain page switch alone only
		// fixes list's box size via tview's own native redraw, not its
		// already-baked text content.
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Ctrl-C's meaning never changes based on what's open - per
		// Purpose.md's "behaves like running ansible-playbook directly"
		// guarantee, it always aborts/quits, unconditionally. If either
		// dialog happens to be open, it also closes that dialog first
		// (with no filter/search change) - the one exception to both
		// dialogs' own "q closes without aborting" rule below, since
		// Ctrl-C is unambiguous about wanting to abort too. Checked first,
		// before anything else in this function, so it can never be
		// swallowed or reinterpreted by dialog- or page-specific logic
		// below (most importantly, by searchDialogOpen's own "let
		// everything through" pass-through just below this).
		if event.Key() == tcell.KeyCtrlC {
			closeDialogs() // harmless no-op if neither dialog is open
			if processDone.Load() {
				quitting.Store(true) // before Stop() - see main.go's race note
				app.Stop()
			} else {
				_ = procH.Load().Signal(os.Interrupt) // best-effort; child may race-exit
			}
			return nil
		}

		// While the search dialog's box has focus, every other key must
		// reach InputField's own editing logic completely untouched -
		// including 'q' itself (a real search term can contain the letter
		// q, e.g. "request") and letters that double as shortcuts
		// elsewhere ('a'/'c'/'f'). There's deliberately no "q closes this
		// one too" here, unlike the filter dialog below: this dialog is
		// nothing but a text box, so unlike a menu where q is never a
		// valid choice, typing q here is always meaningful input, never a
		// mistake to rescue the user from.
		if searchDialogOpen {
			return event
		}

		// The re-run dialog (Rerun.md) is a real form, not a plain text
		// box like the search dialog above - but the same reasoning
		// applies to letting most keys through untouched (any field might
		// legitimately contain 'q' or any other shortcut letter, and
		// Tab/Backtab need to reach Form's own native focus-cycling). The
		// two exceptions are handled centrally here rather than via each
		// item's own SetDoneFunc: Escape, because Form's own default
		// Escape behavior (reset focus to the first item, unless a cancel
		// func is set) isn't what's wanted - Esc should close the dialog
		// outright, per Rerun.md. Enter, because tview.Form treats Enter
		// identically to Tab on a FormItem - just advances focus,
		// confirmed against form.go's own Focus() - never submits on its
		// own, so submission has to be driven from here regardless of
		// which field currently has focus, exactly matching "Re-run shall
		// be initiated by pressing return" for the whole dialog, not just
		// one field. The one exception to that "regardless of focus" rule:
		// once Tab has moved focus onto the Re-run/Cancel buttons
		// themselves (added alongside rerunForm's own construction above),
		// Enter should trigger whichever button is actually focused rather
		// than always forcing a submit - checked via GetFocusedItemIndex
		// (form.go's own accessor for "does a button currently have
		// focus"), letting the event through untouched so Button's native
		// InputHandler (button.go: KeyEnter calls its own selected func)
		// fires the correct one of the two.
		if rerunDialogOpen {
			switch event.Key() {
			case tcell.KeyEnter:
				if _, button := rerunForm.GetFocusedItemIndex(); button >= 0 {
					return event
				}
				submitRerun()
			case tcell.KeyEscape:
				closeDialogs()
			default:
				return event
			}
			return nil
		}

		// The filter dialog is a plain modal menu (Filters.md), no text
		// entry at all: every key except Esc/q and the three filter
		// shortcuts is swallowed outright - checked before the isQuit
		// check below (so q closes the dialog here instead of quitting -
		// per explicit request, pressing q was too often just a
		// reflex to close something, not a real intent to quit) and
		// before the vim-alias translation block and the viewingOutput
		// branch further down, so it takes priority over all of them
		// regardless of which page is otherwise frontmost.
		if filterDialogOpen {
			switch {
			case event.Key() == tcell.KeyEscape, event.Key() == tcell.KeyRune && event.Rune() == 'q':
				closeDialogs()
			case event.Key() == tcell.KeyRune && event.Rune() == 'a':
				applyFilter(filterQuery{mode: filterAll})
			case event.Key() == tcell.KeyRune && event.Rune() == 'c':
				applyFilter(filterQuery{mode: filterChanged})
			case event.Key() == tcell.KeyRune && event.Rune() == 'f':
				applyFilter(filterQuery{mode: filterFailed})
			}
			return nil
		}

		if viewingOutput && event.Key() == tcell.KeyRune && event.Rune() == 'q' {
			// Same convention as the filter dialog's own q (Filters.md):
			// closes/backs out rather than quitting. Calls closeOutput
			// directly rather than synthesizing an Escape the way this
			// used to (relying on outputView's own native SetDoneFunc to
			// catch it) - there's no single persistent TextView left to
			// forward a synthesized event to now that each tab's content
			// is its own TextView, recreated fresh on every
			// renderOutputTabs call.
			closeOutput()
			return nil
		}

		isQuit := event.Key() == tcell.KeyRune && event.Rune() == 'q'
		if isQuit {
			if processDone.Load() {
				quitting.Store(true) // before Stop() - see main.go's race note
				app.Stop()
			} else {
				_ = procH.Load().Signal(os.Interrupt) // best-effort; child may race-exit
			}
			return nil
		}

		// Esc at the bare tree level (no dialog open - both already
		// returned above - and not viewing a drill-down, which has its own
		// Esc meaning further down) has never meant anything here before
		// revisitReturn existed. design-docs/Revisit.md: back out to the
		// run list. quitting is deliberately NOT set here, unlike isQuit
		// above - this doesn't stop app.Run() itself, it stops THIS
		// session's Application (see revisit.go), so the process as a
		// whole keeps going.
		if !viewingOutput && revisitActive && event.Key() == tcell.KeyEscape {
			revisitReturn()
			return nil
		}

		// Main tree only (not the output view, a real tview.TextView with
		// its own unrelated meaning for these same keys): Up/Down/j/k skip
		// straight over the trailing status/recap section's own purely
		// decorative rows (nextInteractiveRow) instead of falling through
		// to treeList's native one-row-at-a-time handling, which has no
		// idea any of these rows are meant to be invisible to the cursor.
		// Checked here, before the vim-alias translation just below, so
		// 'j'/'k' get exactly the same treatment as the real arrow keys
		// rather than being translated first and forwarded straight to
		// treeList, bypassing this entirely.
		if !viewingOutput {
			var delta int
			switch {
			case event.Key() == tcell.KeyUp, event.Key() == tcell.KeyRune && event.Rune() == 'k':
				delta = -1
			case event.Key() == tcell.KeyDown, event.Key() == tcell.KeyRune && event.Rune() == 'j':
				delta = 1
			}
			if delta != 0 {
				// A genuine SetCurrentItem call (not one made while
				// rebuilding) already makes list's own SetChangedFunc
				// update currentID and clear following itself - no need to
				// do either of those here too.
				if next := nextInteractiveRow(currentRows, list.GetCurrentItem(), delta); next != -1 {
					list.SetCurrentItem(next)
				}
				return nil
			}
		}

		// vim/emacs navigation aliases, translated to the native key tview
		// itself already understands and handled identically by both List
		// (main tree) and TextView (output view) - confirmed against
		// tview's own source rather than reimplementing this logic here.
		// j/k specifically only ever reach this translation for the output
		// view now - the main tree's own j/k are already handled, skip-aware,
		// by the block just above. Returning a *different* event than the
		// one passed in makes
		// is currently focused, as if the user had typed that key - see
		// tview's application.go. This is also what makes plain 'G'/Ctrl-E/'>'
		// ride the exact same path plain End already does: ordinary
		// navigation, deliberately with no special "resume autoscroll"
		// side effect (that's F's job alone, below). '<'/'>' are plain
		// mnemonic aliases for Home/End (think "jump to the start/end",
		// same shape as many pagers/viewers) - not otherwise meaningful
		// input in either the main tree or the output view, so safe to
		// claim unconditionally the same way 'G' already is. Space/'b' are
		// the same idea for Page down/up (a common pager convention,
		// e.g. `less`/`man`) - claiming Space here does mean it's no
		// longer also an alias for Enter in the main tree (treeList's own
		// native InputHandler used to activate the current row on Space,
		// same as Enter); Enter alone still does that, so nothing is
		// actually lost, just no longer duplicated onto this key.
		switch {
		case event.Key() == tcell.KeyRune && event.Rune() == 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
		case event.Key() == tcell.KeyRune && event.Rune() == 'k':
			return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlF:
			return tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlB:
			return tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone)
		case event.Key() == tcell.KeyRune && event.Rune() == ' ':
			return tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)
		case event.Key() == tcell.KeyRune && event.Rune() == 'b':
			return tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlA:
			return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlE:
			return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
		case event.Key() == tcell.KeyRune && event.Rune() == 'G':
			return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
		case event.Key() == tcell.KeyRune && event.Rune() == '<':
			return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
		case event.Key() == tcell.KeyRune && event.Rune() == '>':
			return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
		}

		if viewingOutput {
			switch {
			case event.Key() == tcell.KeyEscape, event.Key() == tcell.KeyEnter:
				// Used to be tview.TextView's own native "done key"
				// handling (SetDoneFunc, which also fired on Tab/Backtab -
				// harmless there since those had no other meaning yet).
				// Now explicit, since Tab/Backtab below mean "switch tab"
				// instead (design-docs/Tabbed UI.md), and there's no
				// single persistent TextView left to hang a native
				// SetDoneFunc off of - each tab's content is recreated
				// fresh on every renderOutputTabs call.
				closeOutput()
				return nil
			case event.Key() == tcell.KeyTab:
				outputTabs.Next()
				return nil
			case event.Key() == tcell.KeyBacktab:
				outputTabs.Prev()
				return nil
			case event.Key() == tcell.KeyLeft:
				navigateOutputHost(-1)
				return nil
			case event.Key() == tcell.KeyRight:
				navigateOutputHost(1)
				return nil
			// Shift-N is a plain alias for 'p' here (not a distinct
			// binding of its own) - added on request, echoing vim's own
			// n/N ("next/previous match") convention even though n/p
			// here aren't search-related.
			case event.Key() == tcell.KeyRune && event.Rune() == 'p', event.Key() == tcell.KeyRune && event.Rune() == 'N':
				navigateOutputTask(-1)
				return nil
			case event.Key() == tcell.KeyRune && event.Rune() == 'n':
				navigateOutputTask(1)
				return nil
			case event.Key() == tcell.KeyRune && event.Rune() == 'e':
				// Opens the file the currently displayed task's own source
				// came from, per source.go's taskSourceIndex/task.Path -
				// same app.Suspend + $VISUAL/$EDITOR/vi mechanism the
				// template verb's own 'e' binding already uses
				// (template.go's preferredEditor). Deliberately does NOT
				// refresh anything afterward, unlike the template verb -
				// there's no live render to redo here, and this view's own
				// content (the task's already-recorded result) can't
				// change by editing the source after the fact.
				if outputTask != nil {
					if file := taskSourceFile(outputTask.Path); file != "" {
						app.Suspend(func() {
							cmd := exec.Command(preferredEditor(), file)
							cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
							_ = cmd.Run()
						})
					}
				}
				return nil
			}
			return event
		}

		switch {
		case event.Key() == tcell.KeyRune && event.Rune() == 'F':
			// The only way to resume autoscroll (see the translation block
			// above: End/Ctrl-E/G are deliberately plain navigation now).
			// jumpingToEnd guards against SetCurrentItem's own resulting
			// "changed" event immediately flipping following back off -
			// same two-step dance End/G used to need for this exact reason.
			following = true
			jumpingToEnd = true
			if list.GetItemCount() > 0 {
				list.SetCurrentItem(list.GetItemCount() - 1)
			}
			jumpingToEnd = false
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'E':
			expandAll()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'C':
			collapseAll()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'r':
			// Rerun.md: only once a run has actually finished - a no-op
			// while ansible-playbook is still going, same "processDone
			// gates it" convention as the failure auto-jump above.
			// requestRerun == nil is a second, independent reason to no-op
			// here (design-docs/Revisit.md's Phase 2: a replay session
			// with rerun-from-revisit not yet wired up passes nil rather
			// than a real closure - submitRerun would otherwise nil-panic
			// calling it) - currentMainBottomBarText already drops the
			// "r: re-run" hint whenever this is the case, so there's
			// nothing advertised for this to silently fail to do.
			if !processDone.Load() || requestRerun == nil {
				return nil
			}
			openRerunDialog()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'd':
			// design-docs/Diff.md: only once a run has actually finished,
			// same processDone gate 'r' already has - and only from the
			// bare tree (this whole switch is already un-reachable while
			// viewingOutput, a dialog is open, or a filter is active, so
			// nothing further is needed for "d can only be pressed from
			// the tree view"). No 'd' binding inside diff mode itself -
			// runDiffFlow's own Application has no such case, by design.
			//
			// app.Suspend hands the real terminal to runDiffFlow's own
			// nested Applications (the candidate-run list, then the diff
			// tree) for as long as the user keeps navigating them - the
			// same primitive already used for the output view's own 'e'
			// (open $EDITOR) - and automatically resumes THIS Application,
			// exactly where it left off, the moment runDiffFlow returns.
			// No custom state save/restore needed for that "Esc/q
			// eventually returns to the standard tree view" requirement -
			// it falls out of Suspend's own contract for free.
			if !processDone.Load() {
				return nil
			}
			app.Suspend(func() {
				runDiffFlow(state, targetPlaybook, targetRole, initialTags, initialHosts, sourceIndex)
			})
			return nil
		case event.Key() == tcell.KeyRight:
			handleRight()
			return nil
		case event.Key() == tcell.KeyLeft:
			handleLeft()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'n':
			navigateMainTask(1)
			return nil
		// Shift-N alias for 'p' - see the same binding's own comment in
		// the viewingOutput branch above.
		case event.Key() == tcell.KeyRune && event.Rune() == 'p', event.Key() == tcell.KeyRune && event.Rune() == 'N':
			navigateMainTask(-1)
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'f':
			// Main-tree-only, deliberately, same as '/' below: opening
			// either dialog while the output drill-down view is frontmost
			// isn't supported (the viewingOutput branch above already
			// returned by this point).
			openFilterDialog()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == '/':
			openSearchDialog()
			return nil
		}

		return event
	})

	// Mouse wheel/trackpad plainly pans the view - it does NOT move the
	// cursor (see Keyboard-shortcuts.md). An earlier version drove
	// list.SetCurrentItem() from the wheel instead, to get more scroll
	// range out of tview.List.Draw()'s unconditional "keep the current
	// item visible" clamp (checked directly against tview's list.go -
	// there was no flag to disable it). That traded away more than
	// intended: (1) it moved the cursor on every tick, which is not what a
	// wheel/trackpad should do; and (2) tview.List.SetCurrentItem(index)
	// wraps a negative index around to len(items)+index unconditionally -
	// independent of SetWrapAround(false), which only ever governed the
	// arrow-key InputHandler - so scrolling up from the very first row
	// silently wrapped the cursor to the last row instead of stopping.
	// Reverted per explicit request, and fixed properly rather than just
	// reverted: list is now treeList (treelist.go), a purpose-built
	// widget with no such clamp at all, so plain itemOffset panning (its
	// own default wheel handling, left to run below) has no range limit -
	// unlike tview.List, it's not bounded by the cursor's own position.
	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if event == nil {
			// tview's own fireMouseActions (application.go) fires several
			// actions per physical mouse event (move, then down/up/click)
			// against this same callback, threading the event/action pair
			// from one call's return value into the next call's arguments
			// within that batch - so once any earlier call in the same
			// batch returns a nil event (e.g. a MouseMove that happened to
			// land on a swallowed bar below), every later call in that
			// same batch is invoked with event == nil too. Hit live as a
			// real crash (event.Position() on a nil event) before this
			// guard existed - every branch below assumes a non-nil event,
			// so bail out immediately rather than touch it.
			return nil, action
		}
		if filterDialogOpen {
			// filterDialog (the plain TextView rendering the A/C/F menu)
			// has no click handling of its own for that text - there's no
			// real widget underneath to unlock there, unlike the two
			// dialogs below, so those three rows still need their own
			// hit-test. filterFlex (see NewLiveTUI) wraps filterDialog
			// together with a real Cancel button below it, though - a
			// click on that button needs no hit-test of its own: letting
			// it through reaches Pages' native dispatch and Button's own
			// MouseHandler, exactly like searchDialogOpen/rerunDialogOpen's
			// own buttons/fields below.
			//
			// Only a click landing outside filterFlex's own box (not just
			// filterDialog's - that would incorrectly swallow clicks on
			// the Cancel button sitting below it) is unconditionally
			// swallowed here (same reasoning as searchDialogOpen/
			// rerunDialogOpen below - Pages tries every visible page,
			// topmost first, so an unswallowed click outside the dialog
			// would otherwise fall through to the page underneath).
			// Everything else - Down/Up/Move inside the box, and a click on
			// the Cancel button - is deliberately let through unchanged
			// rather than swallowed unconditionally the way an earlier
			// version of this code did: tview's own fireMouseActions
			// (application.go) synthesizes MouseLeftClick right after
			// MouseLeftUp within the same physical click, threading the
			// *same* event value through both calls - unconditionally
			// returning a nil event from the MouseLeftUp call (as this
			// used to) meant the click action was invoked with an
			// already-nil event and could never fire at all, silently
			// eating every click. Confirmed live: with the old
			// unconditional swallow, clicking a menu row did nothing
			// whatsoever, not even the wrong row.
			x, y := event.Position()
			if !inRect(x, y, filterFlex) {
				return nil, action
			}
			if action == tview.MouseLeftClick && inRect(x, y, filterDialog) {
				// filterDialogText's own fixed layout: row 0 headline, row
				// 1 blank, rows 2/3/4 = All/Changed/Failed. filterDialog
				// itself has no border of its own (that lives on
				// filterFlex instead), so GetRect()'s own y is already the
				// first content row - unlike the dialogs below, which are
				// bordered themselves.
				_, ry, _, _ := filterDialog.GetRect()
				switch y - ry {
				case 2:
					applyFilter(filterQuery{mode: filterAll})
				case 3:
					applyFilter(filterQuery{mode: filterChanged})
				case 4:
					applyFilter(filterQuery{mode: filterFailed})
				}
				return nil, action
			}
			return event, action
		}
		if searchDialogOpen {
			// searchDialogFlex holds a real tview.InputField (searchInput)
			// with its own native click-to-position-cursor handling - a
			// click inside the dialog's own box is let through unchanged
			// so Pages' own dispatch (confirmed against pages.go: it tries
			// every visible page, topmost first) reaches it naturally;
			// anything outside the box is swallowed so it can't leak
			// through to the main page underneath.
			if x, y := event.Position(); inRect(x, y, searchDialogFlex) {
				return event, action
			}
			return nil, action
		}
		if rerunDialogOpen {
			// Same reasoning as searchDialogOpen above, for rerunForm's
			// own native per-field click-to-focus (tview.Form).
			if x, y := event.Position(); inRect(x, y, rerunForm) {
				return event, action
			}
			return nil, action
		}
		if viewingOutput {
			// While a two-pane drill-down (design-docs/TwoPanedLayout.md) is
			// open, the tree pane stays visible but must stay fully inert -
			// a click landing on it would otherwise reach list's own
			// MouseHandler (toggling expand/collapse, opening a different
			// host's output) with no keyboard-side equivalent guarding it,
			// unlike the full-screen case where the tree isn't drawn at all
			// so no click can ever land there. Checked first, before any of
			// the output-specific hit-tests below.
			if splitMode {
				if x, y := event.Position(); inRect(x, y, treeBody) {
					return nil, action
				}
				// splitHeader is a plain, non-interactive TextView, same
				// focus-steal reasoning as outputTopBar/outputBottomBar
				// just below - it replaces topBar/outputTopBar entirely
				// for the duration of a split session (splitFlex's own
				// construction), so it needs the identical guard they'd
				// otherwise each carry on their own.
				if x, y := event.Position(); inRect(x, y, splitHeader) {
					return nil, action
				}
			}
			// outputTopBar/outputBottomBar are plain, non-interactive
			// TextViews - swallow a click there before it can reach
			// TextView's own default MouseLeftDown handling, which would
			// otherwise silently move keyboard focus onto a one-line
			// status bar (confirmed live: Escape/Enter/arrow-key
			// navigation then stop reaching the output view at all, since
			// TextView's own InputHandler intercepts Escape/Enter for
			// itself and there's nothing else to visibly scroll).
			if x, y := event.Position(); inRect(x, y, outputTopBar) || inRect(x, y, outputBottomBar) {
				return nil, action
			}
			// A left click on the tab bar itself switches tabs
			// (design-docs/Tabbed UI.md) - checked here, at the
			// Application level, rather than via outputTabs' own
			// MouseHandler, matching this app's existing convention of
			// doing mouse/key overrides centrally rather than inside a
			// widget (see this function's own doc comment). Anything else
			// (a click elsewhere, wheel scrolling) passes through
			// unchanged - TextView's own wheel handling has no "keep the
			// selected line visible" clamp to fight the way the main
			// tree's list once did, so the active tab's own content
			// already pans freely without any help.
			if action == tview.MouseLeftClick {
				if x, y := event.Position(); outputTabs.HandleClick(x, y) {
					return nil, action
				}
			}
			return event, action
		}
		// topBar/bottomBar - same focus-steal guard as outputTopBar/
		// outputBottomBar above, for the main page.
		if x, y := event.Position(); inRect(x, y, topBar) || inRect(x, y, bottomBar) {
			return nil, action
		}
		switch action {
		case tview.MouseScrollUp, tview.MouseScrollDown:
			// treeList's default handling (left to run below) never fires
			// SetChangedFunc, since it never touches currentItem - so
			// disengaging autoscroll on a genuine pan has to happen here
			// explicitly instead of falling out of that callback the way
			// keyboard navigation gets it for free.
			following = false
		}
		return event, action
	})

	applyLive = func(ev rawEvent) {
		app.QueueUpdateDraw(func() {
			state.Apply(ev)
		})
	}

	if startWithRerunDialog {
		openRerunDialog() // no 'r' keypress to wait for - this is the
		// "rerun" verb's own startup (Rerun.md): nothing has run yet, so
		// the dialog IS the first thing the user sees.
	}

	return app, applyLive
}
