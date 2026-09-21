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

package session

import (
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/diff"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/source"
	"code.aw.net/claude/tangsible/internal/template"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// NewLiveTUI builds an initially-empty list UI and wires it to state's
// hooks so it grows as events arrive. It does not block — the caller must
// call s.app.Run() and feed events through s.applyLive.
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
// initialPlay/initialTags/initialSkipTags/initialHosts pre-fill the re-run
// dialog's own Play/Tags/Skip tags/Hosts fields the first time it's opened
// (and every time after, until the user edits them - the dialog's own
// fields, once opened, keep whatever the user last left in them across
// repeated 'r' presses) - the --start-at-play/--tags/--skip-tags/--limit
// values this process was itself invoked with, parsed out by main.go
// (ExtractStartAtPlay/ParsePassthroughArgs respectively) - design-docs/
// StartWithPlay.md's own "small follow-on" for --start-at-play, once
// "run"/"rerun" could both already resolve one, mirroring how --tags/
// --limit already pre-fill Tags/Hosts (Rerun.md's "if tags were already
// specified in the previous run... pre-filled").
//
// requestRerun is called once the re-run dialog is confirmed (Enter), to
// start a new generation with the dialog's own fields: startAtPlay (empty
// unless a top-level play was chosen - design-docs/StartWithPlay.md; spawns
// against a trimmed, temporary copy of the playbook with every play before
// it dropped, rather than editing the real file) and the edited
// tags/skipTags/hosts. main.go's implementation resets processDone/exitCode/
// state, records the new invocation into .tangsible's history, and spawns a
// fresh ansible-playbook invocation; this function's own job is only to
// reset its own view state (s.expanded/s.currentID/s.following/the freeze
// latches) and restart the heartbeat ticker to match - see submitRerun
// below.
//
// sourceIndex (source.go) backs the output drill-down view's TASK:
// section (see formatHostOutput) - a lookup miss (any task whose path
// wasn't found while building the index) just means no TASK: section for
// that entry, never an error.
//
// knownTags (source.go's BuildTaskSourceIndex, its own second return
// value) is every literal tags: value found in the playbook/role tree,
// unioned with source.ReservedTagNames - the re-run dialog's Tags/Skip
// tags fields' own autocomplete candidate list (design-docs/Autocomplete.md).
// knownPlayNames (source.ListTopLevelPlayNames) is the same idea for the
// "Start with play" field (design-docs/StartWithPlay.md) - every named
// top-level play, in file order; v1 deliberately doesn't follow
// import_playbook, so a play defined in an included file simply isn't a
// candidate. Hosts need no equivalent parameter: the Limit hosts field's
// own candidates come straight from state.AllHosts, already in scope.
//
// initialRerunDefaults (runner.InitialRerunDefaults) is design-docs/
// Rerun.md's "Extend rerun dialog" own data for the three checkboxes'
// very first appearance: the "rerun" verb's own --only-failed/--only-
// unreachable/--resume-where-failed flags (main.go, pre-validated against
// data actually available before this is ever called) plus the failed/
// unreachable hosts and earliest failing play replayed from that target's
// last saved run log (runner.ReplayRunLog), since nothing has run yet in
// *this* process for state itself to answer that from. Every other case -
// "run"/"role"'s own first 'r' press, a revisit session, or any dialog
// reopen once a generation has actually produced events in this process -
// ignores this and recomputes the same three things fresh from state
// itself instead (rerunFieldSync.rebuild, livesession_rerundialog.go);
// zero value for every Verb but "rerun".
//
// startExpanded governs the very first task row's own initial
// expand/collapse state (`.tangsible`'s general.default_tree_state - see
// DefaultTreeExpanded, resolve.go), read once by main.go before
// construction. Every task after the first inherits whatever the
// previous task's current expand state is at the moment it's added - see
// state.OnTaskAdded below - so this value only ever actually governs one
// row per generation (the very first task added since the last Reset()).
//
// startWithRerunDialog is true for the "rerun" Verb's own startup
// (Rerun.md), and - since design-docs/RerunDialog.md - also for a "run"/
// "role" session whose dialog was forced on (--dialog or
// run_dialog=always): either way, no ansible-playbook invocation exists
// yet at all - not even a first one in flight, unlike every other case
// this function handles - so the dialog-or-auto-submit startup flow below
// runs instead of waiting for 'r', and processDone is expected to already
// be true when this is called (main.go sets it before constructing the
// TUI): accurate ("no generation is currently in flight"), and what safely
// unlocks the dialog-opening/quit-outright behavior the rest of this
// function already has for a frozen run - see s.everStarted below for the
// one place that distinction actually matters once frozen means
// "genuinely nothing has run yet" rather than "a run finished."
//
// showDialogAtStartup, consulted only when startWithRerunDialog is true,
// is design-docs/RerunDialog.md's own resolved decision for whether that
// dialog should actually render (true) or be auto-submitted immediately
// with its own pre-filled values, never visibly shown (false) - see the
// startWithRerunDialog handling at the very end of this function. Always
// true for "run"/"role" (main.go only ever leaves their own generation
// unspawned - the condition startWithRerunDialog tests - when this was
// already true); the one case where it can differ from
// startWithRerunDialog is "rerun" with --no-dialog/run_dialog=never, which
// still has nothing spawned yet (startWithRerunDialog true) but skips
// rendering the dialog to get there (showDialogAtStartup false).
// passthroughArgs is this session's own current-generation passthrough
// args (main.go's originalArgs.Rest - importantly -i/-e, never
// -l/--limit, which ParsePassthroughArgs already extracts separately) -
// threaded through only so showOutput's own resolveTaskValues calls (see
// design-docs/Drilldown, Resolved Values.md) see the same
// inventory/extra-vars context the real run did. Not used for anything
// else in this function.
//
// colorEnabled is general.color's own resolved value (ColorEnabledByUser,
// resolve.go), read once by main.go before construction - one of three
// independent inputs (alongside the terminal's own detected color
// capability and the NO_COLOR environment variable) combined below into
// s.useColor, design-docs/Morehosts.md's own gate on whether the collapsed
// task row's per-host summary may render in color at all.
// revisitReturn, if non-nil, marks this session as design-docs/Revisit.md's
// "revisit" Verb showing a replayed (historical) run rather than a live
// run/rerun/role session: state/processDone/exitCode are already fully
// populated by the time this constructor is called (see revisit.go), chrome
// switches to ReplayBarStyle for as long as s.revisitActive stays true, and
// pressing Esc at the bare tree level (not in a dialog, not viewing output -
// nothing else has ever claimed that key there) calls revisitReturn, which
// is expected to stop s.app.Run() and let the caller show the run list again.
// nil for every other Verb - Esc keeps doing nothing at that level, exactly
// as before this existed.
//
// targetPlaybook/targetRole (exactly one non-empty, mirroring
// AppendInvocation's own playbook/role parameters) is this session's own
// target identity, exactly as recorded in state.toml - distinct from
// playbookName, which is display-only (main.go passes it
// filepath.Base(playbook), not the full path state.toml itself keys on).
// Needed for design-docs/Diff.md's own 'd' key, to look up this session's
// own history entry and filter comparison candidates against it
// (RunDiffFlow, diff.go).
func NewLiveTUI(state *playbook.PlaybookState, playbookName string, isRole bool, procH *runner.ProcHandle, processDone, quitting *atomic.Bool, exitCode *atomic.Int32, sourceIndex source.TaskSourceIndex, knownTags, knownPlayNames []string, startExpanded, twoPaneLayout, colorEnabled bool, initialPlay, initialTags, initialSkipTags, initialHosts string, initialRerunDefaults runner.InitialRerunDefaults, startWithRerunDialog, showDialogAtStartup bool, requestRerun func(startAtPlay, tags, skipTags, hosts string), passthroughArgs []string, progH *atomic.Pointer[runner.ProgressTracker], revisitReturn func(), targetPlaybook, targetRole string) (*tview.Application, func(playbook.RawEvent)) {
	// s (*liveSession, livesession.go) holds every piece of state this
	// function's closures share - see its own doc comment for the full
	// rationale (design-docs/Restructuring.md's own postponed "Phase 3").
	// Constructed here, first, since every closure below captures it.
	s := &liveSession{}

	// Copied onto s read-only (see liveSession's own doc comment) so
	// s.rebuild() and its own small chrome/style helpers
	// (livesession_rebuild.go) can reach them once they're no longer
	// lexically nested in this function. Every other closure below keeps
	// reading the bare parameter directly - unaffected by this.
	s.state = state
	s.playbookName = playbookName
	s.isRole = isRole
	s.processDone = processDone
	s.exitCode = exitCode
	s.sourceIndex = sourceIndex
	s.twoPaneLayout = twoPaneLayout
	s.requestRerun = requestRerun
	s.progH = progH
	s.passthroughArgs = passthroughArgs
	s.startExpanded = startExpanded

	s.startedAt = time.Now() // wall-clock the TUI itself came up - see
	// TopBarText's doc comment for why this is deliberately not sourced
	// from any event.

	s.list = uikit.NewTreeList() // see treelist.go - a purpose-built replacement
	// for tview.List, needed so mouse-wheel panning can move the viewport
	// independently of the cursor (tview.List's own Draw() forces the two
	// to stay in lockstep, with no way to disable it). No wraparound and
	// no secondary-text/shortcut support built in - this app never used
	// those.

	s.expanded = map[*playbook.TaskNode]bool{}
	// s.recapHostExpanded/s.recapCategoryExpanded back the recap section's own
	// two-level expand/collapse (design-docs/Recap.md) - kept separate
	// from s.expanded since neither key type (a hostname, a
	// recapCategoryRowID) is a *TaskNode. Both start empty/collapsed
	// unconditionally, regardless of startExpanded - the recap's own
	// "initially only the top level is visible" is a fixed behavior, not
	// tied to the tree's own default_tree_state config knob.
	s.recapHostExpanded = map[string]bool{}
	s.recapCategoryExpanded = map[recapCategoryRowID]bool{}
	s.lastAppliedSelectedIndex = -1 // the index last genuinely applied to
	// s.list via SetCurrentItem, tracked separately from s.list's own
	// currentItem because s.list.Clear()/AddItem() (see treelist.go) reset
	// that to 0 on every single rebuild - without this, s.rebuild()'s
	// trailing selection-apply call below has no way to tell a genuine
	// selection change apart from itself simply reasserting the same
	// logical row again, and would re-clamp (ensureVisible) the viewport
	// back to the cursor on every call - including the heartbeat ticker's,
	// every 200ms, while a run is still live, fighting any mouse-wheel
	// panning the user just did. See RestoreCurrentItem's own doc comment.
	s.following = true // auto-follow the newest row until the user navigates away
	// s.everStarted is false only for the "rerun" Verb's startup dialog, until
	// submitRerun's first-ever call flips it true - see s.rebuild()'s own use
	// of it below: processDone starts true in that one case (see
	// startWithRerunDialog's own doc comment above) even though nothing has
	// actually run, which would otherwise make s.rebuild() render a "Playbook
	// completed successfully" status row before anything ever happened. A
	// revisit session opened straight into the re-run dialog ('r' on the
	// s.list) also sets startWithRerunDialog, but there a run genuinely *has*
	// happened (replayed frozen), so its status row/recap must still show
	// behind the dialog - hence the revisitReturn != nil exception.
	s.everStarted = !startWithRerunDialog || revisitReturn != nil
	s.revisitActive = revisitReturn != nil // true for the whole lifetime of
	// a revisit session until a real rerun is confirmed (submitRerun) -
	// once that happens there's a live/finished generation of its own on
	// screen, no longer "old data," so chrome reverts to normal and Esc
	// stops meaning "back to the list" (see submitRerun/SetInputCapture
	// below).

	// s.checkMode is fixed for this whole session, not a var re-derived per
	// generation: passthroughArgs is the original invocation's own Rest
	// (see this function's own doc comment), and a rerun always carries
	// Rest forward unedited (the dialog never exposes --check for
	// toggling), so whatever this session started with is what every
	// later generation runs with too - live, revisit, and rerun-from-
	// revisit alike (revisit.go threads the replayed run's own recorded
	// Rest through identically). See CheckBarStyle's own doc comment.
	s.checkMode = config.HasCheckFlag(passthroughArgs)

	// Notification settings (design-docs/Notifications.md) - read once and
	// fixed for the whole session, same convention s.checkMode just above
	// already follows (none of these can change mid-session; there's no
	// dialog field for them, unlike tags/hosts).
	s.notifyCfg = config.ReadSettingsConfig(config.TangsibleConfigPath)
	s.notifyPlaybookFinishedKind = config.NotifyPlaybookFinishedKind(s.notifyCfg)
	s.notifyTaskFailedKind = config.NotifyTaskFailedKind(s.notifyCfg)
	s.notifyTaskFailedMax = config.NotifyTaskFailedMax(s.notifyCfg)
	// s.taskFailedNotifyCount/s.suppressedTaskFailures both reset per
	// generation (submitRerun below) - notify_task_failed_max's own cap is
	// per run, not per session (design-docs/Notifications.md: "Failure
	// counter will reset at each rerun"). s.suppressedTaskFailures counts
	// failures beyond the cap; the "N further task failures suppressed"
	// notice can only be sent once the generation actually finishes (only
	// then is the final count known), so it's fired alongside
	// notify_playbook_finished below rather than at the moment the cap is
	// first exceeded. s.finishedNotifySent/s.failureCursorPlaced/
	// s.frozenElapsed+s.haveFrozenElapsed are the other one-shot-per-
	// generation latches s.rebuild() sets the first time it observes the run
	// frozen - see their own field comments in livesession.go for exactly
	// what each guards. s.lastTotalWidth/s.viewingOutput/
	// s.viewingOutputFromRecap are likewise documented there now, having
	// moved out of this constructor along with the closures that used to
	// read them here.
	s.currentFilter = uikit.FilterQuery{Mode: uikit.FilterAll} // see Filters.md; the
	// two dialogs below are the only writers.
	//
	// The filter (a/c/f) and search (/) dialogs are two separate modals,
	// not one combined one (reworked from an earlier single-dialog design
	// after live use showed the combined version made it too easy to hit
	// the wrong key). Each gets its own "is this one open" bool rather
	// than a single shared enum, since the two are modal in genuinely
	// different ways: s.filterDialogOpen (menu mode - a/i/c/f/Esc/q are the
	// only keys that do anything, everything else is swallowed) vs
	// s.searchDialogOpen (text-entry mode - every key except Ctrl-C passes
	// straight through to the search box's own editing, including 'q' and
	// 'a'/'i'/'c'/'f', since a real search term might contain any of those
	// letters). See SetInputCapture below for exactly how each is modal.
	// modal the same way s.searchDialogOpen is (every key but Ctrl-C/Enter/Esc
	// passes straight through to whichever form item has focus), not the
	// filter dialog's swallow-everything-but-a-few-keys menu style: this
	// dialog is text-entry-first, and any of its fields might legitimately
	// contain the letter 'q' or any other shortcut letter.

	// s.liveChromeStyle/s.liveChromeBg/s.liveChromeColorName are what this
	// session's chrome resolves to whenever it isn't showing revisit's own
	// purple - BarStyle/navy normally, CheckBarStyle/olive for the whole
	// lifetime of a --check session (s.checkMode is fixed - see its own
	// declaration above). Read once: unlike s.revisitActive, s.checkMode never
	// flips mid-session, so there's no "goes stale after a real rerun"
	// concern the way s.chromeStyle/s.chromeBg (below) have for revisit.
	s.liveChromeStyle = uikit.BarStyle
	s.liveChromeBg = tcell.ColorNavy
	s.liveChromeColorName = "navy"
	if s.checkMode {
		s.liveChromeStyle = uikit.CheckBarStyle
		s.liveChromeBg = tcell.ColorOlive
		s.liveChromeColorName = "olive"
	}

	// s.chromeStyle/s.chromeBg pick the initial look for every chrome bar/the
	// two-pane divider below - ReplayBarStyle/purple for a revisit session,
	// s.liveChromeStyle/s.liveChromeBg for everything else. Mutable local, not
	// a const choice: submitRerun resets both bars and s.splitDivider back
	// to s.liveChromeStyle/s.liveChromeBg directly once s.revisitActive goes
	// false, so these two only ever matter for how things start out, not
	// as an ongoing source of truth.
	s.chromeStyle = s.liveChromeStyle
	s.chromeBg = s.liveChromeBg
	if s.revisitActive {
		s.chromeStyle = uikit.ReplayBarStyle
		s.chromeBg = tcell.ColorPurple
	}

	// Moved up here (was previously declared after rebuild/hooks) - s.rebuild()
	// now updates it on every call, so it must exist first.
	s.topBar = tview.NewTextView().SetDynamicColors(true).
		SetText(uikit.TopBarText(s.playbookName, s.isRole, s.state.AllHosts, 0, false, s.currentFilter, 0, 0, 20, s.chromeColorName(), s.showElapsed()))
	s.topBar.SetTextStyle(s.chromeStyle)

	// The cursor row's actual look (black-on-light-gray title, black bold
	// text on a per-outcome colored background for each hostname - see
	// PlayRowText/TaskLabel/HostLabel's selected parameter) can't be
	// expressed as a single style applied uniformly to a row's whole text -
	// different runs of the same row need different foreground/background
	// combinations. TreeList (treelist.go) has no built-in per-row
	// highlighting to neutralize in the first place (unlike tview.List, it
	// just prints whatever text each row was given) - s.rebuild() re-renders
	// whichever one row is currently selected with its own selected=true
	// variant before ever calling AddItem, and that's the entire
	// highlighting mechanism.

	// Output drill-down page (design-docs/Tabbed UI.md): s.outputTabs is a
	// TabbedPane (tabs.go) - a fresh set of tab content TextViews is built
	// by BuildOutputTabs and handed to it via SetTabs every time a host
	// row is selected (see showOutput below), rather than one single,
	// reused TextView the way this used to work. Dynamic colors are on
	// for each tab's own TextView so BuildOutputTabs' own tab builders
	// can color their section labels/status line - every piece of dynamic
	// content any of them write (task source, stdout/stderr/msg, the full
	// JSON result) is individually tview.Escape()'d before going in, so a
	// literal "[" in real command output or YAML (e.g. "tags: [a, b]")
	// can never be misread as a color tag.
	s.outputTabs = uikit.NewTabbedPane()
	s.outputTabs.SetHeaderStyle(s.chromeStyle, s.chromeColorName()) // match
	// whatever chrome this session started with (navy/purple/olive) -
	// otherwise the tab bar's own "Output/Task/Resolved/..." row stays
	// TabbedPane's hardcoded navy default regardless of s.chromeStyle,
	// most visible in two-pane mode where it sits right next to
	// s.splitDivider (already s.chromeBg-colored) - reported live.

	// Dynamic colors on, same reason and same escaping discipline as
	// s.topBar (see ProgressFillLine) - its own fill makes host/task.Name
	// (both external content) need escaping, handled once by
	// ProgressFillLine itself rather than here.
	s.outputTopBar = tview.NewTextView().SetDynamicColors(true)
	s.outputTopBar.SetTextStyle(s.chromeStyle)

	s.outputBottomBar = tview.NewTextView().SetText(outputHintBarText)
	s.outputBottomBar.SetTextStyle(s.chromeStyle)

	// s.search (design-docs/Search.md, livesession_tabsearch.go) owns the
	// in-tab search prompt swapped into s.outputBottomBar's own Flex slot
	// while composing a query - built once here and reused for the whole
	// session, same as s.searchInput above.
	s.search = newTabSearchPanel(s.app, s.outputTabs, s.outputBottomBar, s.list, s.outputBottomBarNormalStyle)

	outputFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(s.outputTopBar, 1, 0, false).
		AddItem(s.outputTabs.Primitive(), 0, 1, true).
		AddItem(s.search.footer, 1, 0, false)

	// s.splitHeader replaces s.topBar/s.outputTopBar entirely for the duration
	// of a split session (s.splitFlex's own construction, further down): a
	// single widget spanning the terminal's true full width, rather than
	// two independently-positioned widgets (the tree pane's own s.topBar,
	// the drill-down pane's own s.outputTopBar) either side of s.splitDivider
	// that are each supposed to land on the identical fill boundary but
	// - reported live, twice - didn't quite: a couple of columns right at
	// the seam stayed the wrong color regardless of how carefully the two
	// widgets' own widths were kept in agreement. One widget's own width
	// trivially agrees with itself, which is what actually fixes that
	// class of bug rather than chasing its exact cause further.
	s.splitHeader = tview.NewTextView().SetDynamicColors(true)
	s.splitHeader.SetTextStyle(s.chromeStyle)

	s.pages = tview.NewPages()
	s.currentPageName = "main" // see switchPage's own doc comment (livesession_rebuild.go)

	// Filter and search dialogs: two small, modal overlays on top of the
	// main page (see Filters.md's Dialog section - split into two separate
	// dialogs after live use of a single combined one showed it was too
	// easy to hit the wrong key) rather than a full page swap like
	// "output" below - tview's Pages supports this natively via
	// ShowPage/HidePage instead of SwitchToPage, which leave other s.pages'
	// visibility alone (confirmed against s.pages.go) rather than hiding
	// them, so "main" keeps being drawn underneath. CenteredModal wraps
	// each in nested Flexes to get a fixed-size, screen-centered box
	// instead of filling the whole available area - the standard tview
	// pattern for a partial-screen overlay page. Neither is added to s.pages
	// yet - see the s.pages.AddPage calls further down, which must add them
	// *last* so they draw on top of "main"/"output" (Pages draws visible
	// s.pages back to front, in the order they were added - confirmed
	// against s.pages.go).
	//
	// s.filterDialog is a plain TextView - a static a/c/f menu, no text
	// entry at all. No border/title of its own - those live on s.filterFlex
	// instead (constructed further down, once s.closeDialogs exists for its
	// own Cancel button to call), which wraps this TextView together with
	// a real Cancel button below it.
	s.filterDialog = tview.NewTextView().SetDynamicColors(true)

	// searchDialog is a headline TextView plus a real tview.InputField for
	// the search box - a TextView can display text but can't accept
	// edits, and the search filter needs genuine text entry. Unlike the
	// old combined dialog's search box, this one gets focus the moment the
	// dialog opens (s.openSearchDialog below) rather than needing a separate
	// activation keypress first - there's nothing else in this dialog to
	// browse first, so there's no "menu mode" to be in before typing.
	searchHeadline := tview.NewTextView().SetDynamicColors(true).
		SetText(" [::b]Search[::-]\n\n [gray]Enter to apply, Esc to cancel[-]")
	s.searchInput = tview.NewInputField().SetLabel("Search: ")
	// Top/bottom margin Box items, same as s.filterFlex's own - a real
	// tview.NewBox() rather than a bare nil, so its Draw() still fills its
	// rect with the dialog's own background instead of letting whatever's
	// behind the "search" page show through (the same background
	// see-through bug fixed for the other dialogs).
	s.searchDialogFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox(), 1, 0, false). // top margin
		AddItem(searchHeadline, 0, 1, false).
		AddItem(s.searchInput, 1, 0, true)
	s.searchDialogFlex.SetBorder(true).SetTitle(" Search ")

	// rerunDialog (Rerun.md) - a real tview.Form, unlike the two dialogs
	// above: it's the first multi-field input this app needs, and Form
	// gives Tab/Backtab focus-cycling between them for free rather than
	// hand-rolling it the way TreeList replaced tview.List for the main
	// tree (that replacement was needed because List's own behavior fell
	// short of what the tree needed; here Form's default behavior already
	// matches).
	s.rerunForm = tview.NewForm()
	s.rerunForm.SetBorder(true).SetTitle(" Re-run (enter: run, esc: cancel) ")

	// s.rerunFields (livesession_rerundialog.go) owns the four input
	// fields, three checkboxes, and the sync logic between them - see its
	// own type doc comment for exactly what it does and doesn't own (in
	// particular: not s.rerunForm above, which openRerunDialog/submitRerun
	// below and SetMouseCapture/SetInputCapture still reach directly).
	s.rerunFields = newRerunFieldSync(s.rerunForm, state, knownTags, knownPlayNames, initialRerunDefaults)

	// s.outputTask/s.outputHost track which (task, host) pair the output page
	// is currently showing, so navigateOutputTask (below) knows where
	// "current" is without threading it through as extra state on every
	// keypress.
	// s.outputTopBarPlainText is s.outputTopBar's own "host — task" content,
	// unwrapped and unpadded - set once per navigation (showOutput) but
	// re-wrapped with a fresh ProgressFillLine on every single s.rebuild()
	// call, the same live-updating treatment s.topBar's own text already
	// gets, so the drill-down's own headline keeps sweeping green as the
	// run progresses even while the user isn't actively navigating within
	// it.

	// s.resolveCache holds every (task, host) pair's own ResolvedRender for
	// the lifetime of the current generation - cleared inside
	// submitRerun's own view-state reset (the same place s.expanded/
	// s.currentID/s.following etc. already get reset), so a stale render
	// computed against a previous generation's own vars can never linger.
	// Read/written only from this function's own event-loop goroutine
	// (showOutput directly; the background goroutine below only ever
	// touches it from inside s.app.QueueUpdateDraw), so - like everything
	// else in this file - it needs no locking of its own.
	s.resolveCache = map[resolveKey]uikit.ResolvedRender{}

	// s.docsCache holds every module's own ResolvedRender for the "Docs" tab
	// (ansibledoc.go's ansibledoc.FetchAnsibleDoc), keyed by the task's own "action"
	// result field (TaskAction) rather than by (task, host) the way
	// s.resolveCache is - a module's own documentation depends on nothing
	// about the current run (no vars, no facts, not even which host), so
	// every task using the same module shares one entry, and - unlike
	// s.resolveCache - this is deliberately *not* cleared on a rerun
	// (submitRerun): the installed collections a rerun's ansible-doc would
	// see are the same as before it, so there's nothing stale to flush.
	s.docsCache = map[string]uikit.ResolvedRender{}

	// s.fileCache holds every (task, host) pair's own ResolvedRender for the
	// "File" tab (design-docs/ShowFileContents.md, fetchfile.go's
	// fetchRemoteFileContents) - keyed the same way s.resolveCache is, since a
	// fetched file's content is per-host like a resolved value, not
	// per-module like docs. Unlike s.resolveCache, this is also cleared by
	// closeOutput (below) rather than only by submitRerun: a remote file can
	// keep changing throughout a single generation (a later task modifying
	// it again), so "the same generation's own vars/facts" isn't a strong
	// enough staleness bound the way it is for Resolved/Docs - only "still
	// the same open drill-down visit" is.
	s.fileCache = map[resolveKey]uikit.ResolvedRender{}

	// s.showOutput is FlattenRows' own callback shape (uikit.go) - a plain
	// (task, host) selected-row handler with no notion of "origin," used
	// for every main-tree host row. Always tree-origin.
	s.showOutput = func(task *playbook.TaskNode, host string) {
		s.showOutputWithOrigin(task, host, false)
	}

	// s.showOutputFromRecap is flattenRecapRows' own equivalent (recap.go) -
	// see showOutputWithOrigin's own doc comment for what "recap-origin"
	// actually changes.
	s.showOutputFromRecap = func(task *playbook.TaskNode, host string) {
		s.showOutputWithOrigin(task, host, true)
	}

	// s.tagsPreFilled/s.skipTagsPreFilled/s.hostsPreFilled latch true the first
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

	// openRerunDialog (Rerun.md's 'r' key - see SetInputCapture below,
	// gated there on processDone since re-running only makes sense once a
	// run has finished). Play is never pre-filled from the tree cursor -
	// there's no single "current play" to derive one from reliably - but it
	// IS pre-filled from initialPlay (this process's own invocation's own
	// --start-at-play, if any - design-docs/StartWithPlay.md), the same way
	// Tags/Hosts pre-fill from initialTags/initialHosts: only the very
	// first time the dialog is opened at all - see s.playPreFilled/
	// s.tagsPreFilled/s.skipTagsPreFilled/s.hostsPreFilled above - every open
	// after that leaves the field alone, whatever it now contains.
	openRerunDialog := func() {
		s.rerunDialogOpen = true

		if !s.playPreFilled {
			s.rerunFields.playField.SetText(initialPlay)
			s.playPreFilled = true
		}
		if !s.tagsPreFilled {
			s.rerunFields.tagsField.SetText(initialTags)
			s.tagsPreFilled = true
		}
		if !s.skipTagsPreFilled {
			s.rerunFields.skipTagsField.SetText(initialSkipTags)
			s.skipTagsPreFilled = true
		}
		if !s.hostsPreFilled {
			s.rerunFields.hostsField.SetText(initialHosts)
			s.hostsPreFilled = true
		}

		// s.rerunFields.rebuild runs after the one-shot text pre-fills
		// above, deliberately - on the "rerun" verb's very first open, its
		// own CLI-flag-driven checkbox pre-check (initialRerunDefaults)
		// writes into s.rerunFields.playField/hostsField too (via the
		// checkboxes' own SetChangedFunc cascade), and that write needs to
		// be the one that wins. Running this first (tried live, reverted)
		// let the plain initialPlay/initialHosts pre-fill above - "" for
		// both, since a --resume-where-failed/--only-failed invocation has
		// no --start-at-play/-l of its own - clobber right back over what
		// the checkbox cascade had just filled in. Every open after the
		// first re-derives the three checkboxes' own availability/defaults
		// fresh regardless (see its own doc comment for why this, unlike
		// the text fields' one-shot latches above, is never itself
		// one-time-only) - reordering relative to those latches only
		// matters for the one open where both fire.
		s.rerunFields.rebuild()

		s.rerunForm.SetFocus(0) // always start on the Play field (the first,
		// broadest-scope option - design-docs/StartWithPlay.md), not
		// wherever focus happened to be left inside the form the last time
		// it closed.
		s.pages.ShowPage("rerun")
		s.app.SetFocus(s.rerunForm)
	}
	// s.searchInput.SetDoneFunc fires on Enter/Esc/Tab/Backtab - InputField's
	// own fixed set of "done" keys (confirmed against inputfield.go),
	// reached because s.searchDialogOpen tells SetInputCapture below to let
	// these through untouched rather than intercepting them like the
	// filter dialog's own keys. Only Enter actually applies the typed
	// term; Esc/Tab/Backtab all just cancel back out - there's nothing
	// else in this dialog to Tab to, and Filters.md only ever specifies
	// Esc for "close with no change" anyway. 'q' deliberately does *not*
	// appear here - unlike the filter dialog's plain menu, this box must
	// accept 'q' as ordinary typed text (a search term can contain the
	// letter q), so it's never treated as a shortcut while typing.
	s.searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterSearch, Search: s.searchInput.GetText()})
		} else {
			s.closeDialogs()
		}
	})

	// Mouse-only Search/Cancel buttons, added below the input field -
	// design-docs/Tabbed UI.md's sibling mouse-support pass, closing the
	// gap where this dialog's own InputField had no click-based way to
	// apply/cancel (Enter/Esc already fully cover the keyboard path, so
	// these exist purely for a mouse user). Deliberately plain
	// tview.Buttons, not folded into a tview.Form the way s.rerunForm below
	// is: s.searchInput already repurposes Enter/Esc/Tab/Backtab away from
	// their native meaning via SetDoneFunc above, and Form.Focus()
	// silently overwrites a FormItem's own SetFinishedFunc on every focus
	// change to drive its own Tab-cycling - mixing the two would mean both
	// callbacks firing off the same keypress (confirmed against
	// inputfield.go's own "finish" closure, which calls done then finished
	// unconditionally), risking Form's own internal re-focus undoing
	// s.closeDialogs' s.app.SetFocus(s.list) right after it runs. Not worth the
	// risk for two small buttons whose keyboard path already works fully -
	// these are click-only, not Tab-reachable.
	searchApplyButton := tview.NewButton("Search").SetSelectedFunc(func() {
		s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterSearch, Search: s.searchInput.GetText()})
	})
	searchCancelButton := tview.NewButton("Cancel").SetSelectedFunc(s.closeDialogs)
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
	s.searchDialogFlex.
		AddItem(tview.NewBox(), 1, 0, false).
		// Right-aligned, Cancel-then-Search - same convention as s.rerunForm's
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

	s.list.SetChangedFunc(func(index int) {
		if s.rebuilding {
			// s.rebuild()'s own trailing selection-apply call (see
			// s.lastAppliedSelectedIndex) only ever reaches s.list.SetCurrentItem
			// - and so only ever fires this callback - when the selection
			// has genuinely changed since the last rebuild; a no-op
			// reselection goes through RestoreCurrentItem instead, which
			// never calls this at all. So on a genuine change, this guard's
			// only remaining job is to stop that same SetCurrentItem call
			// from recursing into s.rebuild() again: s.rebuild() sets s.rebuilding
			// true for its entire body - Clear(), every AddItem(), and its
			// own final selection-apply call - so any "changed" event that
			// cascades from within it lands here while s.rebuilding is still
			// true and is correctly ignored instead.
			return
		}
		if index >= 0 && index < len(s.currentRows) {
			s.currentID = s.currentRows[index].ID
		}
		if !s.jumpingToEnd {
			s.following = false
		}
		// A genuine navigation (this is the row's *text*, not just List's
		// own current-item pointer) now carries the selected-row styling -
		// see rebuild's selected-row patch - so it must re-render on every
		// real cursor move, not just when new data arrives or the next
		// heartbeat tick happens to fire (which stops entirely once the
		// run is frozen - without this, the highlight would never move at
		// all after a run finishes).
		s.rebuild()
	})

	state.OnPlayAdded = s.onPlayAdded
	state.OnPlayStarted = s.onPlayStarted
	state.OnTaskAdded = s.onTaskAdded
	state.OnHostRecorded = s.onHostRecorded

	s.bottomBar = tview.NewTextView().SetText(s.currentMainBottomBarText())
	s.bottomBar.SetTextStyle(s.chromeStyle)

	s.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(s.topBar, 1, 0, false).
		AddItem(s.list, 0, 1, true).
		AddItem(s.bottomBar, 1, 0, false)

	// s.filterFlex wraps s.filterDialog's own A/C/F menu together with a real,
	// right-aligned Cancel button below it - built here rather than back
	// where s.filterDialog itself was constructed, since it needs
	// s.closeDialogs (defined above) for the button's own click handler.
	// Esc/q still close the dialog too (SetInputCapture's own
	// s.filterDialogOpen branch, unchanged) - the button is an added mouse
	// affordance, not a replacement for those.
	filterCancelButton := tview.NewButton("Cancel").SetSelectedFunc(s.closeDialogs)
	// A real tview.NewBox() for every spacer item, not a bare nil - see
	// s.searchDialogFlex's own button row above for why (a nil Flex item
	// draws nothing, so nothing ever repaints its cells over whatever the
	// main tree drew underneath there - a real Box's Draw() fills its own
	// rect with the dialog's background even though it shows no content).
	s.filterFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox(), 1, 0, false). // top margin
		AddItem(s.filterDialog, 0, 1, false).
		AddItem(tview.NewFlex().
							AddItem(tview.NewBox(), 0, 1, false).
							AddItem(filterCancelButton, 10, 0, false).
							AddItem(tview.NewBox(), 1, 0, false), 1, 0, false).
		AddItem(tview.NewBox(), 1, 0, false) // bottom margin
	s.filterFlex.SetBorder(true).SetTitle(" Filter ")

	// s.splitDivider is a one-column-wide vertical rule between the two panes
	// - a bare Box with no content, whose Draw() (like every tview
	// Primitive's) unconditionally fills its own rect with its background
	// color, so a solid column is all it takes; no text/rune content needed
	// for a plain vertical line. Colored to match BarStyle's own background
	// (tcell.ColorNavy, the same blue every top/bottom bar already uses),
	// not the brighter named "blue", so the divider reads as part of the
	// same chrome rather than a clashing second shade. Deliberately never
	// repainted by rebuild's own split-mode progress-fill coordination
	// (above) - a live report: it's a fixed structural separator between
	// the two panes, not part of the data being visualized, and turning it
	// green as the fill swept past it read as wrong, not as "one seamless
	// bar." Only spans the body rows now (see s.treeBody/s.splitBody below) -
	// s.splitHeader's own row has no separate divider glyph of its own at
	// all, per a second live report: the single column directly above
	// this divider, in the header row, should participate in the header's
	// own fill exactly like every other character there, not read as part
	// of the (fixed, unfilled) separator below it.
	s.splitDivider = tview.NewBox().SetBackgroundColor(s.chromeBg)

	// s.treeBody/outputBody are the tree pane's/drill-down pane's own
	// bodies with their individual header rows carved out - s.list/
	// s.bottomBar and s.outputTabs/s.outputBottomBar respectively, the exact
	// same primitives s.flex/outputFlex already use, reused here the same
	// way s.flex/outputFlex themselves are already reused between their own
	// standalone s.pages and s.splitFlex (see s.splitFlex's own doc comment
	// below) - safe for the identical reason: only one of "main"/
	// "output"/"split" is ever frontmost, so s.list/s.bottomBar are never
	// actually drawn via two different parents at once.
	s.treeBody = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.list, 0, 1, true).
		AddItem(s.bottomBar, 1, 0, false)
	outputBody := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.outputTabs.Primitive(), 0, 1, true).
		AddItem(s.search.footer, 1, 0, false)

	// s.splitBody is the two-pane row itself - s.treeBody alongside
	// outputBody, with s.splitDivider between them - everything s.splitFlex
	// used to be before s.splitHeader existed. s.treeBody's own width here is
	// just a placeholder; showOutput sets it for real via ResizeItem on
	// every fresh drill-down open, once the terminal's actual current
	// width is known (SplitTreeWidth).
	s.splitBody = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(s.treeBody, uikit.SplitMinTreeWidth, 0, false).
		AddItem(s.splitDivider, uikit.SplitDividerWidth, 0, false).
		AddItem(outputBody, 0, 1, true)

	// s.splitFlex is design-docs/TwoPanedLayout.md's two-pane drill-down:
	// s.splitHeader (a single bar spanning the terminal's true full width -
	// see its own doc comment for why it replaces s.topBar/s.outputTopBar
	// entirely here, rather than each pane keeping its own) above
	// s.splitBody's own two-column row.
	s.splitFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.splitHeader, 1, 0, false).
		AddItem(s.splitBody, 0, 1, false)

	s.pages.AddPage("main", s.flex, true, true)
	s.pages.AddPage("output", outputFlex, true, false)
	s.pages.AddPage("split", s.splitFlex, true, false)
	s.pages.AddPage("filter", uikit.CenteredModal(s.filterFlex, 46, 11), true, false)
	s.pages.AddPage("search", uikit.CenteredModal(s.searchDialogFlex, 46, 11), true, false)
	// Sized for the max case (all three checkboxes present: 7 form items,
	// up from the original 5) - a little empty space in the modal when
	// fewer of them are offered (Rerun.md's "Extend rerun dialog") is a
	// non-issue; the exact number was tuned live rather than computed.
	s.pages.AddPage("rerun", uikit.CenteredModal(s.rerunForm, 56, 19), true, false)

	s.app = tview.NewApplication().SetRoot(s.pages, true)
	// s.search was built earlier (its footer/input widgets are needed for
	// outputFlex's own layout, constructed well before s.app exists), with
	// a nil app - real bug, caught live: tabSearchPanel.open() calling
	// SetFocus on that nil app crashed the instant '/' was pressed. Struct
	// fields copy a pointer's value at the moment they're set, unlike a
	// closure capturing s.app by reference and reading it fresh at call
	// time (what the pre-refactor code did here, safely, by accident) -
	// this fixes it up now that a real one exists.
	s.search.app = s.app

	// Terminal color-capability probe, design-docs/Morehosts.md:
	// Application.Screen() isn't available until after Run() starts, but
	// s.useColor (below) is needed well before that, on every rebuild - so
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
		s.app.SetScreen(screen)
		terminalSupportsColor = screen.Colors() > 1
	}
	s.app.EnableMouse(true)
	// Everything else falls out of tview's own defaults once mouse events
	// are actually turned on (previously never enabled): List's/TextView's
	// built-in mouse wheel handling already just pans the viewport without
	// touching selection, and List's built-in click handling already fires
	// a row's Selected() callback on the first click - identical to Enter -
	// so a host row's output already opens on a single click, and no
	// custom double-click wiring is needed on top of that.

	// s.useColor, design-docs/Morehosts.md: whether the collapsed task row's
	// per-host summary (see ComputeHostColumnLayout/TaskLabel) may ever
	// render in color - all three of terminal capability, the NO_COLOR
	// convention (https://no-color.org - presence disables color
	// regardless of value, even ""; hence LookupEnv's ok result, not the
	// value itself), and the user's own general.color setting must permit
	// it. Computed once - none of the three can change mid-session -
	// and captured by s.rebuild()'s closure below, the same way twoPaneLayout
	// already is.
	_, noColorSet := os.LookupEnv("NO_COLOR")
	s.useColor = terminalSupportsColor && !noColorSet && colorEnabled

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
			ticker := time.NewTicker(uikit.SpinnerInterval)
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
				s.app.QueueUpdateDraw(s.rebuild)
				if done {
					return // one frozen frame pushed above; stop ticking
					// rather than redrawing a static screen forever - until
					// startHeartbeat is called again for a later rerun.
				}
			}
		}()
	}
	// Deliberately no early, pre-Run() s.rebuild() call for a revisit session,
	// even though its state/processDone/exitCode are already fully
	// populated by this point (see revisit.go) and there'd be real content
	// to show immediately, sparing the ~200ms blank flash before the
	// heartbeat ticker's own first tick below. Tried exactly that and
	// reverted it - a real, reported bug: s.list has no genuine rect yet at
	// this point (s.app.Run() hasn't started laying anything out), so
	// ensureVisible/the "reveal trailing status rows" scroll-to-bottom
	// logic inside s.rebuild() (both below) compute against a bogus size,
	// landing itemOffset somewhere wrong - and since the very next
	// s.rebuild() (the heartbeat's one tick, once Run() has given s.list a
	// real rect) sees an unchanged selectedIndex, it takes the
	// RestoreCurrentItem path, which deliberately never touches itemOffset
	// - so nothing ever corrects the bogus position on its own, unlike a
	// live run/rerun/role session (which only ever calls s.rebuild() after
	// Run() has already given every widget a real size). A brief blank
	// flash before the heartbeat's first tick - the same startup
	// experience every other Verb already has - is the trade worth making
	// here, not a real regression.
	// Placed after `s.app` is assigned: the go statement inside
	// startHeartbeat's closure body has a happens-before edge (Go memory
	// model) with this very call, which itself runs after `s.app` was
	// assigned - if startHeartbeat were defined or first called any
	// earlier, reading `s.app` from the ticker goroutine would be a genuine
	// data race, not just a latency curiosity, even though the first tick
	// is SpinnerInterval away.
	startHeartbeat()

	// resizeWatcher: a second, permanent ticker, deliberately independent of
	// startHeartbeat's own per-generation running/frozen lifecycle (unlike
	// startHeartbeat, this is started exactly once and never restarted by
	// submitRerun). Its only job is noticing a bare terminal resize once the
	// run is frozen - startHeartbeat's own ticker already permanently stops
	// once processDone is observed true, so nothing else is left driving a
	// s.rebuild() on a terminal resize with no other incoming event. While a
	// run is still live, startHeartbeat's own ticker already re-syncs
	// everything within SpinnerInterval regardless of resize - so this
	// goroutine skips its own work entirely until processDone. "Everything"
	// now includes the two-pane drill-down's own split-vs-full-screen mode
	// and tree-pane width, not just the tree's row text/column layout - see
	// s.rebuild()'s own resync block (design-docs/TwoPanedLayout.md) - so a
	// frozen run's drill-down reacts to a resize exactly as live one does,
	// via the same s.rebuild() call, just noticed by this ticker instead of
	// startHeartbeat's.
	go func() {
		ticker := time.NewTicker(uikit.SpinnerInterval) // reused only as a
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
				// case every SpinnerInterval regardless of resize.
			}
			s.app.QueueUpdate(func() { // NOT QueueUpdateDraw - avoid forcing a
				// real screen redraw on every tick when nothing changed.
				_, _, totalWidth, _ := s.pages.GetInnerRect() // s.pages, not
				// s.list - the terminal's true current width regardless of
				// which page is frontmost (see s.rebuild()'s own
				// s.lastTotalWidth comment); using s.list here would miss a
				// resize entirely while a two-pane session has fixed s.list's
				// own width to the tree pane's share, or while viewing a
				// full-screen drill-down at all (s.list isn't part of that
				// page's own draw tree, so its rect goes stale).
				if totalWidth != s.lastTotalWidth {
					s.rebuild()
					// s.app.Draw() would deadlock here: it's QueueUpdate under
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
					s.app.ForceDraw()
				}
			})
		}
	}()

	// submitRerun (Enter while s.rerunDialogOpen - see SetInputCapture below)
	// reads the form's own current values, closes the dialog, and starts a
	// new generation the same way Phase B's direct requestRerun() call
	// used to - resetting this function's own view state and restarting
	// the heartbeat ticker - except now driven by the dialog's fields
	// instead of always repeating the original invocation verbatim.
	// Defined here rather than up with openRerunDialog/s.closeDialogs: it
	// closes over startHeartbeat, which - like this closure itself -
	// can't exist before `s.app` is assigned above.
	submitRerun := func() {
		startAtPlay := strings.TrimSpace(s.rerunFields.playField.GetText()) // empty
		// means "whole playbook" - see s.playField's own doc comment.
		tags := strings.TrimSpace(s.rerunFields.tagsField.GetText())
		skipTags := strings.TrimSpace(s.rerunFields.skipTagsField.GetText())
		hosts := strings.TrimSpace(s.rerunFields.hostsField.GetText())
		s.closeDialogs()

		requestRerun(startAtPlay, tags, skipTags, hosts) // resets
		// processDone/exitCode/state synchronously (see main.go) - by the
		// time this returns, s.rebuild() below already sees a running, empty
		// generation.
		s.expanded = map[*playbook.TaskNode]bool{}
		s.recapHostExpanded = map[string]bool{}
		s.recapCategoryExpanded = map[recapCategoryRowID]bool{}
		s.currentID = nil
		s.following = true
		s.failureCursorPlaced = false
		s.haveFrozenElapsed = false
		s.frozenElapsed = 0
		s.finishedNotifySent = false
		s.taskFailedNotifyCount = 0
		s.suppressedTaskFailures = 0
		s.lastAppliedSelectedIndex = -1 // a fresh generation's row 0 must not
		// be mistaken for "no change" just because it happens to match
		// whatever index the previous generation last applied.
		s.resolveCache = map[resolveKey]uikit.ResolvedRender{} // a new generation
		// means new vars/facts - any cached "Resolved" render is for a
		// previous generation's own values and must not linger.
		s.fileCache = map[resolveKey]uikit.ResolvedRender{} // same reasoning -
		// a new generation's fetched file content is just as stale as its
		// resolved values (closeOutput, below, is s.fileCache's *other*
		// invalidation point, for within-generation staleness).
		s.everStarted = true // only a real transition the very first time
		// this fires for the "rerun" Verb's startup dialog (see
		// startWithRerunDialog) - a harmless no-op reassignment every time
		// after that, since it's already true for every other case.
		if s.revisitActive {
			// A real generation is starting - this session is no longer
			// showing "old data," so the revisit chrome and the Esc-back-
			// to-the-list binding both go away, for good, for the rest of
			// this session (design-docs/Revisit.md). Reset directly on the
			// already-constructed widgets rather than via s.chromeStyle/
			// s.chromeBg (those only ever governed how things started out).
			s.revisitActive = false
			s.topBar.SetTextStyle(s.liveChromeStyle)
			s.outputTopBar.SetTextStyle(s.liveChromeStyle)
			s.outputBottomBar.SetTextStyle(s.liveChromeStyle)
			s.splitHeader.SetTextStyle(s.liveChromeStyle)
			s.bottomBar.SetTextStyle(s.liveChromeStyle)
			s.splitDivider.SetBackgroundColor(s.liveChromeBg)
			s.outputTabs.SetHeaderStyle(s.liveChromeStyle, s.liveChromeColorName)
			// Style alone isn't enough for s.bottomBar: unlike s.topBar/
			// s.splitHeader (whose visible text is rebuilt from scratch
			// on every s.rebuild() call, always reading chromeColorName/
			// showElapsed/s.revisitActive fresh), s.bottomBar's own text is
			// a plain string baked in once at whichever point last set
			// it (construction, closeOutput, or rebuild's own split-
			// mode toggle) and never otherwise refreshed - a real bug
			// caught live: without this, "Esc: back to the list" kept
			// showing (with the right style/color!) even after a
			// revisit session was promoted to a real rerun and Esc
			// had already stopped doing that.
			s.bottomBar.SetText(s.currentMainBottomBarText())
		}
		s.startedAt = time.Now()
		s.rebuild() // clear the previous run's rows immediately, rather than
		// leaving them on screen until the new generation's first event
		// arrives.
		startHeartbeat()
	}

	// Re-run/Cancel buttons - unlike s.searchDialogFlex's own buttons above,
	// s.rerunForm is already a real tview.Form (see its own doc comment), so
	// AddButton gets native keyboard Tab-reachability and mouse click
	// handling for free: Form.Focus() already cycles through f.items then
	// f.buttons on Tab/Backtab (unchanged by this addition, just extended
	// to include these two), and SetMouseCapture's existing s.rerunDialogOpen
	// pass-through (any click inside s.rerunForm's own rect reaches Pages'
	// native dispatch unchanged) already covers whatever rect Form ends up
	// drawing the buttons in - no separate mouse wiring needed here. The
	// one adjustment this requires is in SetInputCapture's own s.rerunDialogOpen
	// branch below: Enter must defer to Form's native "trigger the focused
	// button" behavior when a button has focus, rather than always calling
	// submitRerun regardless of focus the way it does for the text fields.
	// Cancel-left/Re-run-right, right-aligned against the form's own inner
	// edge (SetButtonsAlign) - matches the template page's host dialog
	// buttons and the general "affirmative action on the right" convention;
	// AddButton's own call order determines both visual left-to-right order
	// and Tab-cycling order (Form.Focus() walks f.buttons in the order
	// they were added), so this one call controls both at once.
	s.rerunForm.SetButtonsAlign(tview.AlignRight)
	s.rerunForm.AddButton("Cancel", s.closeDialogs).AddButton("Re-run", submitRerun)

	s.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Ctrl-C's meaning never changes based on what's open - per
		// Purpose.md's "behaves like running ansible-playbook directly"
		// guarantee, it always aborts/quits, unconditionally. If either
		// dialog happens to be open, it also closes that dialog first
		// (with no filter/search change) - the one exception to both
		// dialogs' own "q closes without aborting" rule below, since
		// Ctrl-C is unambiguous about wanting to abort too. Checked first,
		// before anything else in this function, so it can never be
		// swallowed or reinterpreted by dialog- or page-specific logic
		// below (most importantly, by s.searchDialogOpen's own "let
		// everything through" pass-through just below this).
		if event.Key() == tcell.KeyCtrlC {
			s.closeDialogs()          // harmless no-op if neither dialog is open
			s.search.closeComposing() // ditto if the tab-search prompt isn't open
			if processDone.Load() {
				quitting.Store(true) // before Stop() - see main.go's race note
				s.app.Stop()
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
		if s.searchDialogOpen {
			if event.Key() == tcell.KeyEscape {
				// Centralized, unlike every other key here (see below) -
				// fixes a real bug: a click on s.searchDialogFlex's own bare
				// margin Box (SetMouseCapture's own s.searchDialogOpen branch)
				// natively steals keyboard focus onto that inert Box (Box's
				// own MouseHandler fallback: "a mouse-down anywhere in its
				// rect refocuses it"), which left Escape with nowhere to
				// go - s.searchInput.SetDoneFunc's own Escape-closes-the-
				// dialog handling only ever fires while s.searchInput itself
				// still has focus. Handled centrally here instead, the same
				// way s.rerunDialogOpen/s.filterDialogOpen already handle Escape
				// (and, for rerun, Enter) regardless of focus, so it always
				// closes this dialog no matter what currently has it.
				s.closeDialogs()
				return nil
			}
			return event
		}

		// Same reasoning as s.searchDialogOpen just above, for the in-tab
		// search prompt (design-docs/Search.md) instead of the tree's own
		// row-filter search - a query might legitimately contain any
		// letter, shortcut or not, so every key but Ctrl-C (handled above)
		// must reach s.search.input's own native editing untouched. Unlike
		// s.searchDialogOpen's own plain "return event" pass-through, this
		// routes through tabSearchPanel.handleComposingKey - see its own
		// doc comment (livesession_tabsearch.go) for why a plain
		// pass-through doesn't reliably reach s.search.input here.
		if s.search.composing {
			s.search.handleComposingKey(event)
			return nil
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
		// themselves (added alongside s.rerunForm's own construction above),
		// Enter should trigger whichever button is actually focused rather
		// than always forcing a submit - checked via GetFocusedItemIndex
		// (form.go's own accessor for "does a button currently have
		// focus"), letting the event through untouched so Button's native
		// InputHandler (button.go: KeyEnter calls its own selected func)
		// fires the correct one of the two.
		//
		// A further exception to both cases (design-docs/Autocomplete.md):
		// if the focused field currently has an open autocomplete
		// drop-down (autocompleteOpenNow), Enter/Escape are let through
		// untouched instead, so InputField's own native handling can pick
		// the current suggestion or dismiss just the drop-down - only once
		// that's no longer true does either key fall back to submitting/
		// closing the whole dialog.
		if s.rerunDialogOpen {
			switch event.Key() {
			case tcell.KeyEnter:
				if _, button := s.rerunForm.GetFocusedItemIndex(); button >= 0 {
					return event
				}
				if s.rerunFields.autocompleteOpenNow() {
					return event
				}
				submitRerun()
			case tcell.KeyEscape:
				if s.rerunFields.autocompleteOpenNow() {
					s.rerunFields.acDismissed = true
					return event
				}
				s.closeDialogs()
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
		// before the vim-alias translation block and the s.viewingOutput
		// branch further down, so it takes priority over all of them
		// regardless of which page is otherwise frontmost.
		if s.filterDialogOpen {
			switch {
			case event.Key() == tcell.KeyEscape, event.Key() == tcell.KeyRune && event.Rune() == 'q':
				s.closeDialogs()
			case event.Key() == tcell.KeyRune && event.Rune() == 'a':
				s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterAll})
			case event.Key() == tcell.KeyRune && event.Rune() == 'i':
				s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterInteresting})
			case event.Key() == tcell.KeyRune && event.Rune() == 'c':
				s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterChanged})
			case event.Key() == tcell.KeyRune && event.Rune() == 'f':
				s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterFailed})
			}
			return nil
		}

		if s.viewingOutput && event.Key() == tcell.KeyRune && event.Rune() == 'q' {
			// Same convention as the filter dialog's own q (Filters.md):
			// closes/backs out rather than quitting. Calls closeOutput
			// directly rather than synthesizing an Escape the way this
			// used to (relying on outputView's own native SetDoneFunc to
			// catch it) - there's no single persistent TextView left to
			// forward a synthesized event to now that each tab's content
			// is its own TextView, recreated fresh on every
			// renderOutputTabs call.
			s.closeOutput()
			return nil
		}

		isQuit := event.Key() == tcell.KeyRune && event.Rune() == 'q'
		if isQuit {
			if processDone.Load() {
				quitting.Store(true) // before Stop() - see main.go's race note
				s.app.Stop()
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
		// above - this doesn't stop s.app.Run() itself, it stops THIS
		// session's Application (see revisit.go), so the process as a
		// whole keeps going.
		if !s.viewingOutput && s.revisitActive && event.Key() == tcell.KeyEscape {
			revisitReturn()
			return nil
		}

		// Main tree only (not the output view, a real tview.TextView with
		// its own unrelated meaning for these same keys): Up/Down/j/k skip
		// straight over the trailing status/recap section's own purely
		// decorative rows (NextInteractiveRow) instead of falling through
		// to TreeList's native one-row-at-a-time handling, which has no
		// idea any of these rows are meant to be invisible to the cursor.
		// Checked here, before the vim-alias translation just below, so
		// 'j'/'k' get exactly the same treatment as the real arrow keys
		// rather than being translated first and forwarded straight to
		// TreeList, bypassing this entirely.
		if !s.viewingOutput {
			var delta int
			switch {
			case event.Key() == tcell.KeyUp, event.Key() == tcell.KeyRune && event.Rune() == 'k':
				delta = -1
			case event.Key() == tcell.KeyDown, event.Key() == tcell.KeyRune && event.Rune() == 'j':
				delta = 1
			}
			if delta != 0 {
				// A genuine SetCurrentItem call (not one made while
				// s.rebuilding) already makes s.list's own SetChangedFunc
				// update s.currentID and clear s.following itself - no need to
				// do either of those here too.
				if next := uikit.NextInteractiveRow(s.currentRows, s.list.GetCurrentItem(), delta); next != -1 {
					s.list.SetCurrentItem(next)
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
		// longer also an alias for Enter in the main tree (TreeList's own
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

		if s.viewingOutput {
			switch {
			case event.Key() == tcell.KeyEscape && s.search.active != nil:
				// Layered per design-docs/Search.md: Esc clears an active
				// search first, rather than immediately closing the whole
				// drill-down out from under it - a second Esc (s.search.active
				// is nil by then) falls through to the case below and closes
				// normally.
				s.search.clear()
				return nil
			case event.Key() == tcell.KeyEscape, event.Key() == tcell.KeyEnter:
				// Used to be tview.TextView's own native "done key"
				// handling (SetDoneFunc, which also fired on Tab/Backtab -
				// harmless there since those had no other meaning yet).
				// Now explicit, since Tab/Backtab below mean "switch tab"
				// instead (design-docs/Tabbed UI.md), and there's no
				// single persistent TextView left to hang a native
				// SetDoneFunc off of - each tab's content is recreated
				// fresh on every renderOutputTabs call.
				s.closeOutput()
				return nil
			case event.Key() == tcell.KeyTab:
				s.outputTabs.Next()
				return nil
			case event.Key() == tcell.KeyBacktab:
				s.outputTabs.Prev()
				return nil
			case event.Key() == tcell.KeyLeft:
				s.navigateOutputHost(-1)
				return nil
			case event.Key() == tcell.KeyRight:
				s.navigateOutputHost(1)
				return nil
			// n/N, not n/p: Search.md's own next/prev-match convention
			// (n forward, N backward, no separate 'p') was made the one
			// convention this whole app uses for "step through a
			// sequence," not just search's own - task-hop/host-hop here
			// and in host.go dropped their own plain 'p' case to match,
			// rather than leaving two different next/prev idioms live
			// side by side. Context-sensitive here specifically: while a
			// search found at least one match, n/N step through matches
			// instead of tasks - an active-but-empty search (HasMatches
			// false) falls through to the normal task-hop meaning instead
			// of doing nothing, so a stale "no matches" search can't
			// silently strand these keys.
			case event.Key() == tcell.KeyRune && event.Rune() == 'N':
				if s.search.hasMatches() {
					s.search.prev()
					return nil
				}
				s.navigateOutputTask(-1)
				return nil
			case event.Key() == tcell.KeyRune && event.Rune() == 'n':
				if s.search.hasMatches() {
					s.search.next()
					return nil
				}
				s.navigateOutputTask(1)
				return nil
			case event.Key() == tcell.KeyRune && event.Rune() == '/':
				s.search.open()
				return nil
			case event.Key() == tcell.KeyRune && event.Rune() == 'y':
				s.outputBottomBar.SetTextStyle(s.outputBottomBarNormalStyle())
				s.outputBottomBar.SetText(uikit.CopyActiveTabStatus(s.outputTabs))
				return nil
			case event.Key() == tcell.KeyRune && event.Rune() == 'e':
				// Opens the file the currently displayed task's own source
				// came from, per source.go's TaskSourceIndex/task.Path -
				// same s.app.Suspend + $VISUAL/$EDITOR/vi mechanism the
				// template Verb's own 'e' binding already uses
				// (template.go's PreferredEditor). Deliberately does NOT
				// refresh anything afterward, unlike the template Verb -
				// there's no live render to redo here, and this view's own
				// content (the task's already-recorded result) can't
				// change by editing the source after the fact.
				if s.outputTask != nil {
					if file := uikit.TaskSourceFile(s.outputTask.Path); file != "" {
						s.app.Suspend(func() {
							cmd := exec.Command(template.PreferredEditor(), file)
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
			// s.jumpingToEnd guards against SetCurrentItem's own resulting
			// "changed" event immediately flipping s.following back off -
			// same two-step dance End/G used to need for this exact reason.
			s.following = true
			s.jumpingToEnd = true
			if s.list.GetItemCount() > 0 {
				s.list.SetCurrentItem(s.list.GetItemCount() - 1)
			}
			s.jumpingToEnd = false
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'E':
			s.expandAll()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'C':
			s.collapseAll()
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
			// s.viewingOutput, a dialog is open, or a filter is active, so
			// nothing further is needed for "d can only be pressed from
			// the tree view"). No 'd' binding inside diff mode itself -
			// RunDiffFlow's own Application has no such case, by design.
			//
			// s.app.Suspend hands the real terminal to RunDiffFlow's own
			// nested Applications (the candidate-run list, then the diff
			// tree) for as long as the user keeps navigating them - the
			// same primitive already used for the output view's own 'e'
			// (open $EDITOR) - and automatically resumes THIS Application,
			// exactly where it left off, the moment RunDiffFlow returns.
			// No custom state save/restore needed for that "Esc/q
			// eventually returns to the standard tree view" requirement -
			// it falls out of Suspend's own contract for free.
			if !processDone.Load() {
				return nil
			}
			s.app.Suspend(func() {
				diff.RunDiffFlow(state, targetPlaybook, targetRole, initialTags, initialHosts, sourceIndex)
			})
			return nil
		case event.Key() == tcell.KeyRight:
			s.handleRight()
			return nil
		case event.Key() == tcell.KeyLeft:
			s.handleLeft()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'n':
			s.navigateMainTask(1)
			return nil
		// n/N, not n/p - see the same binding's own comment in the
		// s.viewingOutput branch above.
		case event.Key() == tcell.KeyRune && event.Rune() == 'N':
			s.navigateMainTask(-1)
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'f':
			// Main-tree-only, deliberately, same as '/' below: opening
			// either dialog while the output drill-down view is frontmost
			// isn't supported (the s.viewingOutput branch above already
			// returned by this point).
			s.openFilterDialog()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == '/':
			s.openSearchDialog()
			return nil
		}

		return event
	})

	// Mouse wheel/trackpad plainly pans the view - it does NOT move the
	// cursor (see Keyboard-shortcuts.md). An earlier version drove
	// s.list.SetCurrentItem() from the wheel instead, to get more scroll
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
	// reverted: s.list is now TreeList (treelist.go), a purpose-built
	// widget with no such clamp at all, so plain itemOffset panning (its
	// own default wheel handling, left to run below) has no range limit -
	// unlike tview.List, it's not bounded by the cursor's own position.
	s.app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
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
		if s.search.composing {
			// Same reasoning as s.searchDialogOpen/s.rerunDialogOpen below,
			// and for the same underlying bug those two already guard
			// against: fireMouseActions forwards every mouse event -
			// including a bare MouseMove with no button down, which tmux/
			// terminals under SGR mouse tracking can send continuously -
			// to whatever primitive sits under the cursor, and that
			// primitive's own MouseHandler can call the setFocus callback
			// on nothing more than a hover. Left unguarded, a stray
			// MouseMove landing on s.outputTabs' own content (the tab body,
			// not the footer) silently steals focus back from
			// s.search.input moments after s.search.open() sets it - caught
			// live: typed characters and Enter/Esc stopped reaching the
			// field at all, with no visible error, because keyboard input
			// was still correctly being forwarded, just to the wrong
			// primitive. A click inside s.search.input's own rect is let
			// through (native click-to-position-cursor); everything else
			// swallowed.
			if x, y := event.Position(); uikit.InRect(x, y, s.search.input) {
				return event, action
			}
			return nil, action
		}
		if s.filterDialogOpen {
			// s.filterDialog (the plain TextView rendering the A/C/F menu)
			// has no click handling of its own for that text - there's no
			// real widget underneath to unlock there, unlike the two
			// dialogs below, so those three rows still need their own
			// hit-test. s.filterFlex (see NewLiveTUI) wraps s.filterDialog
			// together with a real Cancel button below it, though - a
			// click on that button needs no hit-test of its own: letting
			// it through reaches Pages' native dispatch and Button's own
			// MouseHandler, exactly like s.searchDialogOpen/s.rerunDialogOpen's
			// own buttons/fields below.
			//
			// Only a click landing outside s.filterFlex's own box (not just
			// s.filterDialog's - that would incorrectly swallow clicks on
			// the Cancel button sitting below it) is unconditionally
			// swallowed here (same reasoning as s.searchDialogOpen/
			// s.rerunDialogOpen below - Pages tries every visible page,
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
			if !uikit.InRect(x, y, s.filterFlex) {
				return nil, action
			}
			if action == tview.MouseLeftClick && uikit.InRect(x, y, s.filterDialog) {
				// FilterDialogText's own fixed layout: row 0 headline, row
				// 1 blank, rows 2/3/4/5 = All/Interesting/Changed/Failed.
				// s.filterDialog itself has no border of its own (that lives
				// on s.filterFlex instead), so GetRect()'s own y is already
				// the first content row - unlike the dialogs below, which
				// are bordered themselves.
				_, ry, _, _ := s.filterDialog.GetRect()
				switch y - ry {
				case 2:
					s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterAll})
				case 3:
					s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterInteresting})
				case 4:
					s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterChanged})
				case 5:
					s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterFailed})
				}
				return nil, action
			}
			// Anything else inside s.filterFlex but outside s.filterDialog's own
			// A/I/C/F rows - the real Cancel button, or one of s.filterFlex's
			// own bare tview.NewBox() margin/padding cells (NewLiveTUI's own
			// s.filterFlex construction). A click-type action is dispatched to
			// s.filterFlex's own MouseHandler directly and unconditionally
			// swallowed, rather than just letting it fall through to Pages'
			// native dispatch as the code above used to (comment above still
			// describes why Up must never be swallowed too) - see
			// s.rerunDialogOpen's own doc comment below for the confirmed-
			// against-tview's-source root cause: Box.MouseHandler only ever
			// consumes MouseLeftDown, never MouseLeftClick, so a click on one
			// of those bare margin Box cells went unconsumed and leaked
			// straight through to the tree page underneath - reproduced live
			// the same way s.rerunDialogOpen's own bug was.
			switch action {
			case tview.MouseLeftClick, tview.MouseLeftDoubleClick,
				tview.MouseMiddleClick, tview.MouseMiddleDoubleClick,
				tview.MouseRightClick, tview.MouseRightDoubleClick:
				s.filterFlex.MouseHandler()(action, event, func(p tview.Primitive) { s.app.SetFocus(p) })
				return nil, action
			default:
				return event, action
			}
		}
		if s.searchDialogOpen {
			// Same fix, same reasoning, as s.filterDialogOpen above and
			// s.rerunDialogOpen below: s.searchDialogFlex has its own bare
			// tview.NewBox() margin/padding cells (around its top margin and
			// its Cancel/Search buttons - NewLiveTUI's own s.searchDialogFlex
			// construction), which Box.MouseHandler never consumes for a
			// click - only for MouseLeftDown. A click landing there used to
			// leak straight through to the tree page underneath (reproduced
			// live) - and, as an added symptom, still silently steals focus
			// off s.searchInput onto the margin Box itself (Box's own
			// MouseHandler fallback consumes MouseLeftDown by refocusing
			// itself - manually dispatching to s.searchDialogFlex below
			// reaches that same fallback, since it's the exact same
			// dispatch tview's own Pages would have done). That's harmless
			// now: SetInputCapture's own s.searchDialogOpen branch handles
			// Escape centrally, so closing no longer depends on s.searchInput
			// itself still having focus.
			x, y := event.Position()
			if !uikit.InRect(x, y, s.searchDialogFlex) {
				return nil, action
			}
			switch action {
			case tview.MouseLeftClick, tview.MouseLeftDoubleClick,
				tview.MouseMiddleClick, tview.MouseMiddleDoubleClick,
				tview.MouseRightClick, tview.MouseRightDoubleClick:
				s.searchDialogFlex.MouseHandler()(action, event, func(p tview.Primitive) { s.app.SetFocus(p) })
				return nil, action
			default:
				return event, action
			}
		}
		if s.rerunDialogOpen {
			// Real, reported bug this whole block exists to fix: clicking
			// one of the blank separator rows between s.rerunForm's fields
			// used to toggle a tree row on the page behind the dialog.
			// Root cause, confirmed against tview's own source (application.go/
			// s.pages.go/form.go): Form.MouseHandler's own catch-all ("a
			// mouse-down anywhere else refocuses the last element") only
			// ever consumes the MouseLeftDown action - it has no
			// equivalent for MouseLeftUp/MouseLeftClick, so a click
			// landing on a blank row (nothing there to consume Up/Click)
			// went unconsumed by the form, and Pages.MouseHandler (tries
			// every visible page, topmost first, falling through to the
			// next on non-consumption) let it leak straight through to the
			// "main" page underneath.
			x, y := event.Position()
			// An open autocomplete drop-down (design-docs/Autocomplete.md)
			// renders at an absolute screen position directly below its
			// own field (InputField.Draw), independent of s.rerunForm's own
			// fixed-height box - it can render partly or entirely below
			// s.rerunForm's own rect. InputField exposes no accessor for the
			// drop-down's own rect, so this is a deliberately generous
			// fixed band below s.rerunForm sized to the maximum drop-down
			// height, not a precise hit-test.
			rx, ry, rw, rh := s.rerunForm.GetRect()
			inBand := x >= rx && x < rx+rw && y >= ry+rh && y < ry+rh+autocompleteMaxEntries+1
			if !uikit.InRect(x, y, s.rerunForm) && !inBand {
				return nil, action // outside the dialog entirely - fully modal
			}
			switch action {
			case tview.MouseLeftClick, tview.MouseLeftDoubleClick,
				tview.MouseMiddleClick, tview.MouseMiddleDoubleClick,
				tview.MouseRightClick, tview.MouseRightDoubleClick:
				// The actual fix: dispatch straight to s.rerunForm's own
				// MouseHandler ourselves (still reaches a real field/
				// button/autocomplete-entry click exactly as normal Pages
				// dispatch would - neither Box.WrapMouseHandler nor
				// InputField.MouseHandler, tview's box.go/inputfield.go,
				// gate on the primitive's own rect before checking its own
				// open autocomplete list, so Form's per-item loop reaches
				// it correctly even inside the band above), then
				// unconditionally swallow (return nil) so a click Form
				// itself doesn't consume - a blank row - can never fall
				// through to Pages' own dispatch and leak to the page
				// underneath.
				s.rerunForm.MouseHandler()(action, event, func(p tview.Primitive) { s.app.SetFocus(p) })
				return nil, action
			default:
				// MouseMove/MouseLeftDown/MouseLeftUp: let through
				// unchanged via normal Pages dispatch. MouseLeftDown is
				// safe to let through as-is - Form's own catch-all above
				// already consumes it anywhere in rect, so it was never
				// the source of the leak. MouseLeftUp must also be let
				// through unchanged, even though nothing here needs its
				// own effect: tview's fireMouseActions (application.go)
				// only synthesizes the MouseLeftClick action afterward if
				// the MouseLeftUp call's own mouseCapture result came back
				// non-nil (it reassigns its own shared `event` variable to
				// whatever this callback returns) - swallowing Up here,
				// as an earlier version of this fix did, silently
				// suppressed every Click on s.rerunForm, buttons included.
				return event, action
			}
		}
		if s.viewingOutput {
			// While a two-pane drill-down (design-docs/TwoPanedLayout.md) is
			// open, the tree pane stays visible but must stay fully inert -
			// a click landing on it would otherwise reach s.list's own
			// MouseHandler (toggling expand/collapse, opening a different
			// host's output) with no keyboard-side equivalent guarding it,
			// unlike the full-screen case where the tree isn't drawn at all
			// so no click can ever land there. Checked first, before any of
			// the output-specific hit-tests below.
			if s.splitMode {
				if x, y := event.Position(); uikit.InRect(x, y, s.treeBody) {
					// A wheel scroll over the tree pane itself (not its
					// s.bottomBar row - matching the full-screen case below,
					// which swallows a scroll over s.bottomBar the same way)
					// is the one deliberate exception to "fully inert while
					// split" (design-docs/TwoPanedLayout.md's own "no
					// focus-switching, Esc to close" call): unlike a click,
					// it doesn't select or change anything, only pans the
					// view, so it's let through to reach s.list's own
					// MouseHandler via tview's normal position-based
					// dispatch - already correctly unbounded (TreeList's own
					// wheel handling), no new panning logic needed here.
					// s.following=false has to be set explicitly on this path,
					// same reasoning as the shared fallthrough below:
					// TreeList's wheel handling never fires SetChangedFunc
					// (it never touches currentItem), so nothing else
					// disengages autoscroll here.
					if (action == tview.MouseScrollUp || action == tview.MouseScrollDown) && uikit.InRect(x, y, s.list) {
						s.following = false
						return event, action
					}
					return nil, action
				}
				// s.splitHeader is a plain, non-interactive TextView, same
				// focus-steal reasoning as s.outputTopBar/s.outputBottomBar
				// just below - it replaces s.topBar/s.outputTopBar entirely
				// for the duration of a split session (s.splitFlex's own
				// construction), so it needs the identical guard they'd
				// otherwise each carry on their own.
				if x, y := event.Position(); uikit.InRect(x, y, s.splitHeader) {
					return nil, action
				}
			}
			// s.outputTopBar/s.outputBottomBar are plain, non-interactive
			// TextViews - swallow a click there before it can reach
			// TextView's own default MouseLeftDown handling, which would
			// otherwise silently move keyboard focus onto a one-line
			// status bar (confirmed live: Escape/Enter/arrow-key
			// navigation then stop reaching the output view at all, since
			// TextView's own InputHandler intercepts Escape/Enter for
			// itself and there's nothing else to visibly scroll).
			if x, y := event.Position(); uikit.InRect(x, y, s.outputTopBar) || uikit.InRect(x, y, s.outputBottomBar) {
				return nil, action
			}
			// A left click on the tab bar itself switches tabs
			// (design-docs/Tabbed UI.md) - checked here, at the
			// Application level, rather than via s.outputTabs' own
			// MouseHandler, matching this app's existing convention of
			// doing mouse/key overrides centrally rather than inside a
			// widget (see this function's own doc comment). Anything else
			// (a click elsewhere, wheel scrolling) passes through
			// unchanged - TextView's own wheel handling has no "keep the
			// selected line visible" clamp to fight the way the main
			// tree's s.list once did, so the active tab's own content
			// already pans freely without any help.
			if action == tview.MouseLeftClick {
				if x, y := event.Position(); s.outputTabs.HandleClick(x, y) {
					return nil, action
				}
			}
			return event, action
		}
		// s.topBar/s.bottomBar - same focus-steal guard as s.outputTopBar/
		// s.outputBottomBar above, for the main page.
		if x, y := event.Position(); uikit.InRect(x, y, s.topBar) || uikit.InRect(x, y, s.bottomBar) {
			return nil, action
		}
		switch action {
		case tview.MouseScrollUp, tview.MouseScrollDown:
			// TreeList's default handling (left to run below) never fires
			// SetChangedFunc, since it never touches currentItem - so
			// disengaging autoscroll on a genuine pan has to happen here
			// explicitly instead of falling out of that callback the way
			// keyboard navigation gets it for free.
			s.following = false
		}
		return event, action
	})

	if startWithRerunDialog {
		openRerunDialog() // no 'r' keypress to wait for - this is the
		// "rerun" Verb's own startup (Rerun.md), or a "run"/"role" session
		// whose dialog was forced on (design-docs/RerunDialog.md): nothing
		// has run yet, so the dialog IS the first thing the user sees -
		// unless showDialogAtStartup says otherwise, right below.
		if !showDialogAtStartup {
			// design-docs/RerunDialog.md's --no-dialog/run_dialog=never:
			// openRerunDialog above already pre-filled every field (and, on
			// "rerun"'s own first open, ran the checkbox cascade too) -
			// submitRerun reads exactly that state and spawns immediately.
			// Both calls happen before s.app.Run() is ever invoked by the
			// caller, so the dialog never actually renders a frame.
			submitRerun()
		}
	}

	return s.app, s.applyLive
}
