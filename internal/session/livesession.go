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
	"encoding/json"
	"sync/atomic"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/debugrun"
	"code.aw.net/claude/tangsible/internal/debugverb"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/source"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// resolveKey identifies one (task, host) pair's own "Resolved"/"File" tab
// cache entry (design-docs/Drilldown, Resolved Values.md,
// design-docs/ShowFileContents.md) - package-level (not local to
// NewLiveTUI, as it used to be) since liveSession.resolveCache/fileCache
// need to name it in their own field types.
type resolveKey struct {
	task *playbook.TaskNode
	host string
}

// liveSession holds every piece of state NewLiveTUI's ~40 closures share -
// design-docs/Restructuring.md's own postponed "Phase 3": turning closures
// over untyped shared locals into methods on a real struct. This is
// increment 1/2 of that refactor (see the plan this session worked from) -
// state is hoisted here and rebuild() is a real method, but most other
// closures (rerun-dialog wiring, output drill-down, filtering, navigation,
// the input/mouse dispatchers) still live as closures inside NewLiveTUI
// itself, now capturing s instead of ~35 separate locals. Splitting those
// into their own methods/types is future work, tracked by the same plan.
//
// Field comments here are deliberately brief - the "why" for most of these
// still lives as prose right where each field is read/written in tui.go,
// exactly as it did before this field existed as a local var; this is
// just enough to know what the field is at a glance.
type liveSession struct {
	// --- app-level ---
	app *tview.Application

	// --- NewLiveTUI's own parameters, copied here read-only (never
	// reassigned after construction) so rebuild() and the small chrome/
	// style helpers it calls (livesession_rebuild.go) can reach them once
	// they're no longer lexically nested inside NewLiveTUI itself. Every
	// other closure still nested there keeps reading the bare parameter
	// directly - these fields exist only for the methods that had to
	// leave that scope, not as a wholesale parameter-to-field rename. ---
	state        *playbook.PlaybookState
	playbookName string
	isRole       bool
	procH        *runner.ProcHandle
	processDone  *atomic.Bool
	quitting     *atomic.Bool
	exitCode     *atomic.Int32
	// lastStderr is the current/most recent generation's own collected
	// stderr lines, written by runner.RunOneGeneration right alongside
	// exitCode (see its own doc comment for the ordering that makes this
	// safe to read here with no separate lock) - design-docs/
	// ErrorOutput.md's own data source. nil-checked before every read,
	// the same "not every caller wires every optional thing" convention
	// revisitReturn below already follows - a revisit session has no live
	// generation to report on.
	lastStderr *atomic.Pointer[[]string]
	// outLog is the current generation's raw console output (runner.
	// OutputLog) - swapped for a fresh one on every rerun; nil-valued
	// until anything has spawned, or for a revisited run saved before
	// .stdout files existed. Backs the 'o' view (livesession_stdout.go).
	outLog        *atomic.Pointer[runner.OutputLog]
	sourceIndex   source.TaskSourceIndex
	twoPaneLayout bool
	requestRerun  func(startAtPlay, tags, skipTags, hosts string)
	progH         *atomic.Pointer[runner.ProgressTracker]
	// lastTitlePercent is the percentage (0-100) most recently written to
	// the window title (updateWindowTitle, design-docs/
	// ProgressIndicator.md) - -1 means none yet, so the very first real
	// percentage (including a genuine 0) still triggers a write. Throttles
	// SetWindowTitle to "only when the number actually changed" rather
	// than writing an escape sequence on every 200ms heartbeat tick.
	lastTitlePercent int
	// terminalProgress is whether OSC 9;4 is enabled for this session
	// (terminal_progress, resolved once in NewLiveTUI);
	// lastTerminalProgress is the sequence most recently written, so
	// updateTerminalProgress only writes on change.
	terminalProgress     bool
	lastTerminalProgress string
	// revisitReturn/targetPlaybook/targetRole - only needed by the input
	// dispatcher (livesession_input.go): revisitReturn backs Esc-at-the-
	// bare-tree's "back to the revisit list" case, targetPlaybook/
	// targetRole feed diff.RunDiffFlow's own 'd' key handling.
	revisitReturn              func()
	targetPlaybook, targetRole string
	// passthroughArgs is this session's own current-generation passthrough
	// args - needed by showOutputWithOrigin's Resolved/File async fetches
	// (resolveTaskValues/fetchRemoteFileContents), which have to see the
	// same inventory/connection/vault context the live run itself used.
	passthroughArgs []string
	// startExpanded (.tangsible/config.toml's general.default_tree_state) -
	// needed by state.OnTaskAdded's own InheritedExpandState call, the
	// fallback for a generation's very first task (every later one instead
	// inherits whichever task was added most recently - see
	// InheritedExpandState's own doc comment).
	startExpanded bool
	// initialPlay/initialTags/initialSkipTags/initialHosts are this
	// process's own invocation's own --start-at-play/--tags/--skip-tags/-l,
	// if any - openRerunDialog's one-time pre-fill source for the rerun
	// dialog's fields (see s.playPreFilled/s.tagsPreFilled/
	// s.skipTagsPreFilled/s.hostsPreFilled below for why "one-time").
	initialPlay, initialTags, initialSkipTags, initialHosts string

	// --- tree/render state ---
	list                     *uikit.TreeList
	expanded                 map[*playbook.TaskNode]bool
	recapHostExpanded        map[string]bool
	recapCategoryExpanded    map[recapCategoryRowID]bool
	currentRows              []uikit.Row
	currentID                any
	rebuilding               bool
	lastAppliedSelectedIndex int
	following                bool
	jumpingToEnd             bool
	everStarted              bool
	revisitActive            bool
	// lastTotalWidth is pages' own width (the terminal's, regardless of
	// which of "main"/"output"/"split" is frontmost - see rebuild()'s own
	// totalWidth local) last time rebuild() ran. Compared against pages'
	// *current* width by the resize-watcher goroutine (startHeartbeat) to
	// notice a terminal resize with no other event to piggyback a rebuild
	// on - pages itself, being the app's root primitive, always reports
	// the true current terminal size no matter which page is showing,
	// unlike list's own width (the tree's own column layout, which stops
	// tracking the terminal 1:1 once a two-pane drill-down is open,
	// design-docs/TwoPanedLayout.md).
	lastTotalWidth int
	// viewingOutput is true while the host-output page is frontmost - see
	// SetInputCapture: selects between the main tree's and the output
	// view's own page-specific key bindings (Left/Right and n/N mean
	// different things on each page). Not a pages.GetFrontPage() query,
	// since this function owns both places that ever switch pages.
	viewingOutput bool
	// viewingOutputFromRecap is true for the duration of a drill-down
	// session opened from a recap task row (design-docs/Recap.md) rather
	// than the main tree - see showOutputWithOrigin's own doc comment for
	// what this changes. Persists across navigateOutputTask/
	// navigateOutputHost calls within the same session (both pass the
	// current value straight through, not a hardcoded false), so hopping
	// between tasks/hosts via n/N or Left/Right while viewing doesn't
	// silently switch the session back to tree-origin behavior partway
	// through.
	viewingOutputFromRecap bool
	// useColor (design-docs/Morehosts.md): whether the collapsed task
	// row's per-host summary may render in color at all - computed once
	// (terminal capability, NO_COLOR, general.color must all permit it)
	// and read by rebuild() on every call thereafter.
	useColor bool
	// splitMode is true while the currently-open drill-down is rendered
	// as the two-pane "split" page (design-docs/TwoPanedLayout.md) rather
	// than full-screen "output" - decided once, in showOutput, the moment
	// a drill-down freshly opens (viewingOutput was false), and left
	// alone for the rest of that session even if the terminal is resized
	// while it stays open (per the design doc's own explicit call: only
	// the panes' own internal layout reflows mid-session, the split-vs-
	// full-screen choice itself doesn't re-decide until the next open).
	splitMode bool
	// dividerDrag holds the tree pane width the user dragged the divider
	// to, for the whole session; read it via splitTreeWidth.
	dividerDrag uikit.DividerDrag
	// currentPageName tracks which of "main"/"output"/"split" is
	// currently frontmost, so switchPage can tell whether a page switch
	// is actually needed before calling pages.SwitchToPage - which, per
	// tview's own source, re-focuses the new front page every time it's
	// called, even redundantly.
	currentPageName string

	// --- chrome/notification bookkeeping ---
	startedAt                  time.Time
	checkMode                  bool
	notifyCfg                  config.SettingsConfig
	notifyPlaybookFinishedKind config.NotificationKind
	notifyTaskFailedKind       config.NotificationKind
	notifyTaskFailedMax        int
	taskFailedNotifyCount      int
	suppressedTaskFailures     int
	// finishedNotifySent latches true the first time rebuild() observes
	// the run frozen - same one-shot-per-generation shape as
	// failureCursorPlaced below, guarding notify_playbook_finished so it
	// fires exactly once per generation.
	finishedNotifySent bool
	// failureCursorPlaced latches true the first time rebuild() observes
	// the run frozen - guards the one-time "jump to the failed host"
	// placement so it fires exactly once on the running-to-frozen
	// transition, never re-forcing the cursor back there if the user has
	// since navigated elsewhere.
	failureCursorPlaced bool
	// frozenElapsed/haveFrozenElapsed latch the first time rebuild()
	// observes the run frozen, capturing that instant's elapsed time for
	// every later rebuild to reuse. Without this, a rebuild triggered
	// long after the run finished - by cursor navigation or anything
	// else that isn't the heartbeat ticker, which stops once frozen -
	// would recompute now.Sub(startedAt) fresh and make the top bar's
	// elapsed time keep climbing after the run is actually done.
	frozenElapsed       time.Duration
	haveFrozenElapsed   bool
	liveChromeStyle     tcell.Style
	liveChromeBg        tcell.Color
	liveChromeColorName string
	chromeStyle         tcell.Style
	chromeBg            tcell.Color

	// --- widgets touched by more than one closure ---
	// bottomBar/flex/splitFlex/splitBody/treeBody/splitDivider/splitHeader
	// used to need forward-declaring in NewLiveTUI (var bottomBar
	// *tview.TextView, etc., assigned only later once really
	// constructed) purely so earlier closures like showOutput could
	// reference the identifier before its real construction ran - struct
	// fields need no such ceremony (a nil *tview.TextView field is a
	// perfectly valid zero value to read before assignment), a genuine
	// simplification this refactor buys for free. splitHeader (a single
	// bar spanning the terminal's true full width) replaces
	// topBar/outputTopBar entirely for the duration of a split session -
	// see its own construction site for why (two independently-widthed
	// widgets either side of splitDivider never quite agreed on the
	// fill boundary; one widget's own width trivially agrees with
	// itself).
	bottomBar        *tview.TextView
	flex             *tview.Flex
	outputFlex       *tview.Flex
	outputBody       *tview.Flex
	splitFlex        *tview.Flex
	splitBody        *tview.Flex
	treeBody         *tview.Flex
	splitDivider     *tview.Box
	splitHeader      *tview.TextView
	topBar           *tview.TextView
	outputTabs       *uikit.TabbedPane
	outputTopBar     *tview.TextView
	outputBottomBar  *tview.TextView
	pages            *tview.Pages
	filterDialog     *tview.TextView
	searchInput      *tview.InputField
	searchDialogFlex *tview.Flex
	filterFlex       *tview.Flex
	rerunForm        *tview.Form

	// --- tree-level filter/search dialogs ---
	// The filter (a/c/f) and search (/) dialogs are two separate modals,
	// not one combined one (reworked from an earlier single-dialog design
	// after live use showed the combined version made it too easy to hit
	// the wrong key). Each gets its own "is this one open" bool rather
	// than a single shared enum, since the two are modal in genuinely
	// different ways: filterDialogOpen (menu mode - a/i/c/f/Esc/q are the
	// only keys that do anything, everything else is swallowed) vs
	// searchDialogOpen (text-entry mode - every key except Ctrl-C passes
	// straight through to the search box's own editing, including 'q'
	// and 'a'/'i'/'c'/'f', since a real search term might contain any of
	// those letters). rerunDialogOpen is modal the same way
	// searchDialogOpen is, not the filter dialog's swallow-everything
	// menu style - see SetInputCapture for exactly how each is modal.
	currentFilter    uikit.FilterQuery // see Filters.md; the two dialogs below are the only writers.
	filterDialogOpen bool
	searchDialogOpen bool
	rerunDialogOpen  bool

	// --- rerun dialog field-sync (design-docs/Rerun.md) ---
	// rerunFields (livesession_rerundialog.go), increment 4 of this
	// refactor - genuinely separable from the rest of this struct, same
	// shape as search below. openRerunDialog/submitRerun (tui.go) stay
	// liveSession-level "bridge" closures reaching into it, since they
	// also touch the rest of the shared state pool (s.expanded/s.currentID/
	// s.resolveCache etc on a rerun) that this subsystem doesn't need to
	// know about. playPreFilled/tagsPreFilled/skipTagsPreFilled/
	// hostsPreFilled stay here too, not in rerunFields - they're
	// openRerunDialog's own one-shot latches, touched by no other closure.
	rerunFields       *rerunFieldSync
	playPreFilled     bool
	tagsPreFilled     bool
	skipTagsPreFilled bool
	hostsPreFilled    bool

	// --- output drill-down ---
	// showOutput/showOutputFromRecap are struct fields (not local
	// closures) purely so rebuild() can still pass them as FlattenRows'/
	// flattenRecapRows' own row-selected callback now that rebuild is a
	// real method, no longer nested inside NewLiveTUI - both remain
	// ordinary closures, assigned once at construction, still capturing
	// NewLiveTUI's whole scope normally otherwise.
	showOutput            func(*playbook.TaskNode, string)
	showOutputFromRecap   func(*playbook.TaskNode, string)
	outputTask            *playbook.TaskNode
	outputHost            string
	outputTopBarPlainText string
	resolveCache          map[resolveKey]uikit.ResolvedRender
	docsCache             map[string]uikit.ResolvedRender
	fileCache             map[resolveKey]uikit.ResolvedRender

	// search is design-docs/Search.md's "find text in the active tab"
	// apparatus, increment 3 of this refactor - genuinely separable from
	// the rest of this struct (livesession_tabsearch.go), unlike
	// everything else still listed above.
	search *tabSearchPanel

	// watch is design-docs/DebugMode.md's "Stage 4" variable watch
	// explorer (livesession_watch.go) - genuinely separable the same way
	// search/rerunFields are, debug-mode only but always constructed
	// (same "always exists, inert without debugMode" convention as
	// debugBPS/debugCursor below) so every call site can stay a plain,
	// unconditional method call gated on s.debugMode rather than a nil
	// check.
	watch *watchPanel

	// --- raw-output view ('o', livesession_stdout.go) ---
	stdoutView      *tview.TextView
	stdoutTopBar    *tview.TextView
	stdoutBottomBar *tview.TextView
	stdoutSearch    *tabSearchPanel
	viewingStdout   bool
	stdoutFollowing bool
	// stdoutShownLog/stdoutShown: which log stdoutView currently holds
	// lines from, and how many - syncStdoutView's "what's new" bookmark.
	stdoutShownLog *runner.OutputLog
	stdoutShown    int

	// help is the '?'/F1 help dialog (design-docs/Help.md), opened with
	// whichever screen's table (helptables.go) is frontmost.
	help *uikit.HelpOverlay

	// --- debug mode (design-docs/DebugMode.md's "Stage 2") ---
	// debugMode is true only for the "debug" Verb - gates handleDebugKey
	// and debugArmed so every other Verb's own key handling/rendering is
	// completely unaffected (both are nil-safe either way, but this keeps
	// the common case from ever touching debug-only state at all).
	debugMode bool
	// debugCopyPlaybookPath is the absolute path into the shadow copy
	// (debugverb.PrepareSession's own CopyPlaybookPath) - what F5/F10/F11
	// actually spawn, never playbookName/targetPlaybook (the real
	// playbook - display/sourceIndex/history use that instead).
	debugCopyPlaybookPath string
	// debugPlaybookDir/debugCopyDir are debugverb.DebugConfig's own
	// PlaybookDir/CopyDir - the real project tree and the shadow copy's
	// own root, respectively. Used by debugUnfakeShadowPaths
	// (livesession_debug.go) to rewrite the shadow copy's own path back
	// to the user's real one wherever it leaks into a captured task_vars
	// snapshot (role_path, playbook_dir, etc. - design-docs/DebugMode.md's
	// "faked paths" follow-up). Both empty for every non-debug Verb.
	debugPlaybookDir string
	debugCopyDir     string
	// debugBPS is this session's own armed-breakpoint set (F9 toggles
	// it) - keyed by debugverb.TaskLabel(task.Name), the debugger's own
	// prompt text, the only identity a Pause carries (internal/debugrun).
	debugBPS *debugrun.BreakpointSet
	// debugCursor resolves a live jsonl event against debugPrep's own
	// skeleton-seeded state.Plays, writing straight into the right
	// pre-built *TaskNode - never through PlaybookState.Apply (see
	// debugverb.SeedState's own doc comment for why not). nil until
	// PrepareSession's own skeleton is seeded (main.go), same lifetime
	// as state.Plays itself.
	debugCursor *debugverb.Cursor
	// debugAllVars is --all-vars/-a (design-docs/DebugMode.md's "Stage 3
	// follow-up round 5"/round 9): false (the default) is "vars at stops
	// only" - debugOnPause skips its task_vars/register query for a pause
	// that won't be surfaced to the user, trading "Resolved tab works for
	// any task" for not paying that query's own real cost on every task
	// regardless of whether the session ever actually stops there. Set,
	// it restores the "every task" behavior (slower, but the Resolved tab
	// then works for any task you navigate to, not just ones you stopped
	// at).
	debugAllVars bool
	// debugSess is nil until the user's first F5/F10/F11 - see
	// debugStartOrContinue's own doc comment for why it's published via
	// QueueUpdateDraw immediately once debugrun.Spawn succeeds, before
	// the blocking call that follows it (the exact race a previous,
	// separate debug TUI already fixed once - must not regress here).
	debugSess *debugrun.Session
	// debugDriving is true while a Start/Continue/Step call is in flight
	// on its own goroutine - guards against a second one starting
	// concurrently (F5 held down, a double keypress, ...), the same
	// single-flight role Session's own REPL channel requires.
	debugDriving bool
	// debugLoopDriving is true only while debugStartOrContinue's own
	// RunWithBreakpoints/ContinueWithBreakpoints call (F5/F10/F11's
	// applyPolicy loop, internal/debugrun) is the thing in flight -
	// narrower than debugDriving above, which is also true during F12's
	// own debugRedo and the watch panel's variable-set round-trip
	// (debugSetWatch). q's own soft-stop meaning (handleGlobalQuit,
	// livesession_input.go) only makes sense for the applyPolicy loop:
	// RedoUntilTarget is already bounded/single-target (its own doc
	// comment, debugrun/breakpoints.go) and debugSetWatch is one bounded
	// REPL round-trip - neither a multi-pause loop a soft stop could act
	// on. q during either of those two still sends a real SIGINT exactly
	// as it always has, gated on this field being false. Set/cleared
	// alongside debugDriving in debugStartOrContinue/
	// debugApplyPauseResult/debugFinish; never touched by debugRedo/
	// debugSetWatch.
	debugLoopDriving bool
	// debugStopPending is q's one-shot "soft stop" request
	// (handleGlobalQuit), made while debugLoopDriving is true - design-
	// docs/DebugMode.md's "q's own soft stop while actively driving".
	// Unlike every other field in this section, it's genuinely written
	// from the event-loop goroutine (q's own synchronous input-capture
	// handler) and read from debugStartOrContinue's own background
	// goroutine, repeatedly, inside applyPolicy's loop - CLAUDE.md's
	// "Atomics only, no mutex" applies here exactly as it already does
	// for processDone/quitting/exitCode, even though this field never
	// leaves internal/session. Consumed via CompareAndSwap, not a plain
	// Load - see debugConsumeStopRequest (livesession_debug.go).
	debugStopPending atomic.Bool
	// debugPausedHost is "" unless genuinely paused at a breakpoint right
	// now - distinct from s.processDone (CLAUDE.md's "Generation outcome
	// ordering" invariant covers that one; a pause is the child process
	// very much still running, just blocked on its own REPL pipe, an
	// entirely different condition from the process having exited).
	debugPausedHost string
	// debugLastPauseTask is the most recent real pause's own task label
	// (debugrun.Pause.Task, e.g. "TASK: some task name") - "" before
	// anything has paused at all. F10's own StepNextTask mode
	// (debugrun/breakpoints.go) needs exactly this: "the task to skip
	// past" is whatever task the session is paused at *right now*, read
	// in debugStartOrContinue before the next Continue call even starts.
	// Kept distinct from debugPausedHost/debugCursor.CurrentTask - this
	// is the debugger's own REPL-stream label, not the jsonl-stream-
	// derived *playbook.TaskNode those two track (debugOnPause's own doc
	// comment explains why the two can't be reliably correlated for
	// every pause in an auto-continued sequence).
	debugLastPauseTask string
	// debugPendingOpenTask/debugPendingOpenHost hold a pause's own
	// (task, host) when the drill-down can't be auto-opened for it yet -
	// the debugger's own REPL pause notification and the jsonl event
	// reporting that exact host's result arrive on two entirely
	// independent pipes/goroutines with no ordering guarantee between
	// them, and opening before the event arrives would show an Output
	// tab reading "no output recorded" with every other tab missing too
	// (confirmed live as a real, reported bug - "sometimes the drilldown
	// only has the Output tab"). debugApplyPauseResult sets these instead
	// of opening immediately when the event hasn't arrived yet;
	// debugDrainEvents' own event-apply path finishes the job once it
	// does. "" / nil once there's nothing pending.
	debugPendingOpenTask *playbook.TaskNode
	debugPendingOpenHost string
	// debugTaskVarsCache holds the raw task_vars JSON captured
	// (debugOnPause, livesession_debug.go) for *every* pause reached,
	// surfaced or not - design-docs/DebugMode.md's "Resolved Variables
	// tab, every task". Keyed by debugVarsKey (the debugger's own pause
	// identity: task label + host), not resolveKey{task, host} -
	// debugOnPause's own doc comment explains why a *playbook.TaskNode
	// pointer isn't a reliable key here. showOutputWithOrigin consumes
	// an entry here, when one exists for a task's own TaskLabel + host,
	// to seed resolveTaskValues' own --extra-vars snapshot instead of
	// letting it re-gather fresh. Only ever grows - a debug session has
	// exactly one generation (no rerun - requestRerun is nil for
	// "debug", main.go), so there's nothing to clear it for.
	debugTaskVarsCache map[debugVarsKey]json.RawMessage
}

// debugVarsKey identifies one captured task_vars snapshot - see
// debugTaskVarsCache/debugOnPause (livesession_debug.go) for why this is
// keyed by the debugger's own pause identity rather than by
// *playbook.TaskNode.
type debugVarsKey struct {
	label, host string
}
