package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// barStyle is used for every non-list chrome bar (top bar, bottom bar, and
// the output drill-down page's own top/bottom bars) - white on blue, bold.
var barStyle = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorNavy).Bold(true)

const spinnerInterval = 200 * time.Millisecond

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// spinnerAt returns the spinner frame for a given elapsed duration - shared
// by the top bar's own heartbeat and taskLabel's active-task prefix so both
// tick the same frame at the same instant when driven from the same elapsed
// value (see rebuild).
func spinnerAt(elapsed time.Duration) rune {
	return spinnerFrames[int(elapsed/spinnerInterval)%len(spinnerFrames)]
}

// minutesSeconds splits d into whole minutes and the remaining seconds
// (0-59), both floored - shared by the top bar's own elapsed display and
// taskLabel's active-task elapsed suffix, which are two independent
// measures (see NewLiveTUI's startedAt vs taskNode.StartedAt) that should
// at least agree on formatting.
func minutesSeconds(d time.Duration) (mm, ss int) {
	return int(d / time.Minute), int(d/time.Second) % 60
}

// topBarText renders the top bar: playbook name, plus a heartbeat - a
// spinner frame and total elapsed time since the TUI itself started (our
// own time.Now(), NOT any event's _timestamp - "has our program been
// alive/responsive," a different question from any one task's own
// duration) - plus the currently active filter (see Filters.md's "title
// bar shows the currently selected filter" requirement; shown
// unconditionally, including "All", rather than only when a filter is
// actually narrowing anything). Once frozen, the spinner is dropped
// entirely (simplicity over cuteness) rather than stuck on an arbitrary
// frame or swapped for a checkmark. No tview.Escape() needed here - unlike
// the list, this TextView never enables dynamic color tags.
func topBarText(playbookName string, elapsed time.Duration, frozen bool, filter filterQuery) string {
	mm, ss := minutesSeconds(elapsed)
	if frozen {
		return fmt.Sprintf(" %s  %02d:%02d  Filter: %s ", playbookName, mm, ss, filter.label())
	}
	return fmt.Sprintf(" %s  %c %02d:%02d  Filter: %s ", playbookName, spinnerAt(elapsed), mm, ss, filter.label())
}

// row is one flattened, currently-visible line in the list: a play, a task,
// or (if its task is expanded) a host. selected is nil for play/host rows;
// for task rows it toggles that task's expand state. id identifies the row
// across rebuilds (a *playNode, *taskNode, or hostRowID), used to restore
// the selection to the same logical row after the list is repopulated.
type row struct {
	text     string
	selected func()
	id       any
}

type hostRowID struct {
	task *taskNode
	host string
}

// statusRowID/statusDividerRowID identify the trailing status rows rebuild()
// appends once the run has finished (see statusRowText) - given explicit
// non-nil ids rather than leaving the divider row's id as the implicit zero
// value, so nothing relies on "no other row's id is ever nil" holding by
// coincidence.
type statusRowID struct{}
type statusDividerRowID struct{}

// statusRowText returns the inline status line to append below the last
// task row once the run has finished - every case gets one, including a
// genuine failure (red "Playbook failed"). This used to fall through to
// "" for anything other than success/benign-unreachable/user-interrupted,
// on the reasoning that a genuine failure is already visible from the red
// host rows themselves - but a real run (one host, one task, that task
// failed, nothing else executed) showed no closing line at all, which
// reads as "did this even finish?" rather than a clear verdict; every
// other terminal case already gets an explicit line, so failure should
// too, red like the row(s) that caused it. hadUnreachable mirrors
// main.go's benignHostUnreachable check: exit 4 (ansible-core's own
// overloaded HOST_UNREACHABLE/PARSER_ERROR value) doesn't make main.go treat
// this as a hard tool error once Tangsible has independently observed a real
// unreachable host this run - but that's a distinct decision from what this
// function shows. A run where the only reachable-or-not evidence is "some
// host(s) never responded" is not the same as one where everything actually
// ran clean, and rendering both as identical green "completed successfully"
// text was actively misleading (confirmed live: a single-host playbook whose
// one host is unreachable on its very first task exits 4 with
// hadUnreachable=true and nothing else ever having run) - so this case gets
// its own yellow, distinctly-not-"successfully" message instead.
func statusRowText(code int, hadUnreachable bool) string {
	benignHostUnreachable := code == 4 && hadUnreachable
	switch {
	case code == 0:
		return "[green]Playbook completed successfully[-]"
	case benignHostUnreachable:
		return "[yellow]Playbook completed - one or more hosts were unreachable[-]"
	case code == ansibleUserInterruptedExitCode:
		return "[red]Playbook stopped, press q again to quit tangsible.[-]"
	default:
		return fmt.Sprintf("[red]Playbook failed (exit code %d)[-]", code)
	}
}

// genuineFailure reports whether code represents an actual failure - not
// success, not the benign "some host(s) were unreachable" case (see
// main.go's benignHostUnreachable), and not a user-requested interrupt.
// Structurally the same condition as statusRowText's own default case
// above, pulled out separately so rebuild's one-time post-freeze cursor
// placement (see below) can't silently drift out of agreement with it
// about what counts as "actually failed."
func genuineFailure(code int, hadUnreachable bool) bool {
	benignHostUnreachable := code == 4 && hadUnreachable
	return code != 0 && !benignHostUnreachable && code != ansibleUserInterruptedExitCode
}

// lastFailedTaskAndHost finds the most recent task (in tree order) that
// recorded a Failed or Unreachable host, and that host's own name - the
// first such host recorded on that task, per HostOrder - or (nil, "") if
// none is found (a genuine failure for some other reason, e.g. an
// unparsable exit code, with no Failed/Unreachable host ever recorded).
// Used once, right as a run freezes into a genuine failure (see rebuild),
// to put the cursor exactly where a user drilling into "what failed"
// would want it, without needing to navigate there themselves.
func lastFailedTaskAndHost(state *playbookState) (*taskNode, string) {
	for pi := len(state.Plays) - 1; pi >= 0; pi-- {
		tasks := state.Plays[pi].Tasks
		for ti := len(tasks) - 1; ti >= 0; ti-- {
			t := tasks[ti]
			for _, h := range t.HostOrder {
				if o := t.Hosts[h]; o == outcomeFailed || o == outcomeUnreachable {
					return t, h
				}
			}
		}
	}
	return nil, ""
}

// filterMode is one of the main tree's row filter kinds (Filters.md). The
// zero value, filterAll, is the default - no filtering at all.
type filterMode int

const (
	filterAll filterMode = iota
	filterChanged
	filterFailed
	filterSearch // Filters.md's "Contents"/M filter - see filterQuery.search
)

// filterQuery is the main tree's complete currently active filter: the
// kind, plus the search term filterSearch matches against (meaningless for
// the other three kinds). Kept as one comparable value (both fields are,
// so switching filters can be detected with a plain !=) rather than
// threading mode and search as two separate parameters through every
// function that needs "the current filter" - flattenRows, visibleTasks,
// visibleTasksForHost, taskVisible, applyFilter - since the two always
// travel together. sourceIndex (needed by taskVisible's filterSearch case
// to look up a task's source text) is deliberately NOT part of this type:
// it's a map, which isn't comparable, so it's threaded as its own
// parameter everywhere instead - the same parameter NewLiveTUI itself
// already receives and passes to formatHostOutput.
type filterQuery struct {
	mode   filterMode
	search string
}

// label is q's own display name, used by the top bar and the filter
// dialog.
func (q filterQuery) label() string {
	switch q.mode {
	case filterChanged:
		return "Changed"
	case filterFailed:
		return "Failed"
	case filterSearch:
		return fmt.Sprintf("Search: %q", q.search)
	default:
		return "All"
	}
}

// taskHasAnyOutcome reports whether any of t's hosts currently have one of
// the given outcomes.
func taskHasAnyOutcome(t *taskNode, outcomes ...outcome) bool {
	for _, o := range t.Hosts {
		for _, want := range outcomes {
			if o == want {
				return true
			}
		}
	}
	return false
}

// taskOutputText returns primaryOutputField's text for host's result on
// task, or "" if there's none (no result recorded yet, undecodable, or the
// module simply didn't report stdout/msg) - shared support for the search
// filter's "Output" criterion below, reusing exactly the same decode-then-
// primaryOutputField path outputSummary/formatHostOutput already use, so
// "the output" means the same thing everywhere in this app.
func taskOutputText(t *taskNode, host string) string {
	raw, ok := t.Raw[host]
	if !ok {
		return ""
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	_, text := primaryOutputField(decoded)
	return text
}

// taskMatchesSearch reports whether t matches the "Contents" search filter
// (Filters.md's "Contents"/M filter): term found in the task's own title,
// its source ("ansible command" - the same TASK: section source text
// formatHostOutput shows, from sourceIndex/source.go), or any host's
// Output (taskOutputText above - the same field the drill-down view and
// the collapsed OK/Changed line both already call "the output"). An empty
// term matches everything (same as filterAll) rather than hiding
// everything - a safer default than making an accidental blank search
// (pressing Enter on an empty text box) look like "nothing matches, is
// this broken?". Case-insensitive substring match, nothing fancier (no
// regex/fuzzy matching) - matches this project's existing "documented
// heuristic, not chased further" style elsewhere (taskLabel's truncation,
// primaryOutputField's own stdout-vs-msg choice).
func taskMatchesSearch(t *taskNode, term string, sourceIndex taskSourceIndex) bool {
	if term == "" {
		return true
	}
	term = strings.ToLower(term)
	if strings.Contains(strings.ToLower(t.Name), term) {
		return true
	}
	if source, ok := sourceIndex[t.Path]; ok && strings.Contains(strings.ToLower(source), term) {
		return true
	}
	for host := range t.Hosts {
		if strings.Contains(strings.ToLower(taskOutputText(t, host)), term) {
			return true
		}
	}
	return false
}

// taskVisible reports whether t should get a row under q - see Filters.md's
// Acceptance criteria. A task's status for filtering purposes is
// host-level: it matches "Changed"/"Failed" if *at least one* of its hosts
// has that outcome (a task can have hosts in different states), and when
// it matches, flattenRows below shows all of its hosts - not just the
// matching ones. Unreachable hosts count as a failure for this purpose too
// - same bucket lastFailedTaskAndHost already treats it as for the
// auto-jump-on-failure feature. isActive means t is the run's current
// in-progress task (see playbookState.CurrentTask) - always shown
// regardless of filter, since it may simply not have recorded any host
// outcome yet (and, for filterSearch specifically, wouldn't have any
// output to search yet either).
func taskVisible(t *taskNode, q filterQuery, sourceIndex taskSourceIndex, isActive bool) bool {
	if isActive || q.mode == filterAll {
		return true
	}
	failed := taskHasAnyOutcome(t, outcomeFailed, outcomeUnreachable)
	switch q.mode {
	case filterFailed:
		return failed
	case filterSearch:
		return taskMatchesSearch(t, q.search, sourceIndex)
	default: // filterChanged
		return failed || taskHasAnyOutcome(t, outcomeChanged)
	}
}

// flattenRows walks state's play/task/host tree into an ordered row list,
// respecting which tasks are currently expanded and (per filter) currently
// visible at all. Rebuilt fresh on every event - cheap at this project's
// target scale (~10 hosts, Purpose.md), and avoids needing to incrementally
// patch a tree structure by hand.
//
// width is the list's current available width (see rebuild), used to
// right-align each TASK row's counts segment (see taskLabel). activeTask
// (nil once the run has finished) gets a spinner prefix on its row instead
// of an elapsed-time readout - frame is the shared spinner frame for this
// rebuild pass (see spinnerAt), computed once and passed in rather than
// each row picking its own, so every active indicator in the UI ticks in
// lockstep. showOutput is called when a host row is selected (Enter), to
// display that host's full result for that task. sourceIndex is only read
// by taskVisible's filterSearch case, to search a task's own source text.
func flattenRows(state *playbookState, expanded map[*taskNode]bool, width int, activeTask *taskNode, frame rune, filter filterQuery, sourceIndex taskSourceIndex, showOutput func(task *taskNode, host string)) []row {
	var rows []row
	for _, play := range state.Plays {
		var playRows []row
		for _, task := range play.Tasks {
			t := task
			if !taskVisible(t, filter, sourceIndex, t == activeTask) {
				continue
			}
			playRows = append(playRows, row{
				text:     taskLabel(t, state.AllHosts, width, t == activeTask, frame, false),
				id:       t,
				selected: func() { expanded[t] = !expanded[t] },
			})
			if expanded[t] {
				for _, host := range t.HostOrder {
					h := host
					playRows = append(playRows, row{
						text:     "    " + hostLabel(t, h, false),
						id:       hostRowID{t, h},
						selected: func() { showOutput(t, h) },
					})
				}
			}
		}
		if len(playRows) == 0 {
			// Same rule as a play with no executed tasks at all (see
			// playbookState's own doc comment) - a play with no *visible*
			// tasks after filtering doesn't get a row either.
			continue
		}
		rows = append(rows, row{text: playRowText(play, false), id: play})
		rows = append(rows, playRows...)
	}
	return rows
}

// playRowText builds one PLAY row's text - just the play's name, white and
// bold normally. selected switches to the cursor-row styling (see
// taskLabel/hostLabel's own selected parameter and NewLiveTUI's
// SetSelectedStyle comment for why this can't just be a single uniform
// List-wide style): black bold text on a light gray background across the
// whole name.
func playRowText(play *playNode, selected bool) string {
	name := tview.Escape(play.Name)
	if selected {
		return fmt.Sprintf("[%s:lightgray:b]%s[-:-:-]", pureBlack, name)
	}
	return fmt.Sprintf("[white::b]%s[-::-]", name)
}

// centeredModal wraps p in nested Flexes so it renders as a fixed-size box
// centered within whatever space its container gives it, instead of
// filling all of it - the standard tview pattern for a page that overlays
// only part of the screen (see tview's own Pages wiki example). Used for
// the filter dialog (see NewLiveTUI).
func centeredModal(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}

// filterDialogText renders the filter dialog's body - a headline and the
// three filters this dialog itself offers (All/Changed/Failed - the search
// filter is a separate dialog, see NewLiveTUI's searchDialog), each with a
// small marker next to whichever one is currently active. No marker shown
// at all if a search filter is currently active instead - none of these
// three apply then. No tview.Escape() needed - every piece of text here is
// a fixed literal, never external content (same reasoning as
// formatHostOutput's own fixed labels).
func filterDialogText(active filterQuery) string {
	mark := func(mode filterMode) string {
		if mode == active.mode {
			return "[aqua]*[-]"
		}
		return " "
	}
	return fmt.Sprintf(
		" [::b]Select filter[::-]\n\n"+
			" %s A - Show all\n"+
			" %s C - Show changed (includes failed)\n"+
			" %s F - Show only failed tasks\n\n"+
			" [gray]Esc/q to cancel[-]",
		mark(filterAll), mark(filterChanged), mark(filterFailed),
	)
}

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
// initialTags/initialHosts pre-fill the re-run dialog's own Tags/Hosts
// fields the first time it's opened (and every time after, until the user
// edits them - the dialog's own fields, once opened, keep whatever the
// user last left in them across repeated 'r' presses) - the --tags/--limit
// values this process was itself invoked with, parsed out by main.go via
// parsePassthroughArgs (Rerun.md's "if tags were already specified in the
// previous run... pre-filled").
//
// requestRerun is called once the re-run dialog is confirmed (Enter), to
// start a new generation with the dialog's own fields: startAtTask (empty
// for a whole-playbook re-run, otherwise the task name to pass as
// --start-at-task - see openRerunDialog below for how it's pre-filled) and
// the edited tags/hosts. main.go's implementation resets
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
func NewLiveTUI(state *playbookState, playbookName string, procH *procHandle, processDone, quitting *atomic.Bool, exitCode *atomic.Int32, sourceIndex taskSourceIndex, initialTags, initialHosts string, startWithRerunDialog bool, requestRerun func(startAtTask, tags, hosts string)) (app *tview.Application, applyLive func(rawEvent)) {
	startedAt := time.Now() // wall-clock the TUI itself came up - see
	// topBarText's doc comment for why this is deliberately not sourced
	// from any event.

	list := newTreeList() // see treelist.go - a purpose-built replacement
	// for tview.List, needed so mouse-wheel panning can move the viewport
	// independently of the cursor (tview.List's own Draw() forces the two
	// to stay in lockstep, with no way to disable it). No wraparound and
	// no secondary-text/shortcut support built in - this app never used
	// those.

	expanded := map[*taskNode]bool{}
	var currentRows []row
	var currentID any
	var rebuilding bool
	following := true // auto-follow the newest row until the user navigates away
	var jumpingToEnd bool // true only while our own 'F' handler drives SetCurrentItem
	everStarted := !startWithRerunDialog // false only for the "rerun"
	// verb's startup dialog, until submitRerun's first-ever call flips it
	// true - see rebuild()'s own use of it below: processDone starts true
	// in that one case (see startWithRerunDialog's own doc comment above)
	// even though nothing has actually run, which would otherwise make
	// rebuild() render a "Playbook completed successfully" status row
	// before anything ever happened.
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
	var viewingOutput bool // true while the host-output page is frontmost; see
	// SetInputCapture below - selects between the main tree's and the output
	// view's own page-specific key bindings (Left/Right and n/p mean
	// different things on each page). A plain locally-owned bool, not a
	// pages.GetFrontPage() query, since this function owns both places that
	// ever switch pages.
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

	// Moved up here (was previously declared after rebuild/hooks) - rebuild()
	// now updates it on every call, so it must exist first.
	topBar := tview.NewTextView().SetText(topBarText(playbookName, 0, false, currentFilter))
	topBar.SetTextStyle(barStyle)

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

	// Output drill-down page: a single, reused TextView (never recreated
	// per drill-down), updated via SetText each time a host row is
	// selected. Dynamic colors are on so formatHostOutput can color its
	// section labels/status line/TASK: highlighting - every piece of
	// dynamic content it writes (task source, stdout/stderr/msg, the full
	// JSON result) is individually tview.Escape()'d before going in, so a
	// literal "[" in real command output or YAML (e.g. "tags: [a, b]")
	// can never be misread as a color tag.
	outputView := tview.NewTextView()
	outputView.SetDynamicColors(true)

	outputTopBar := tview.NewTextView()
	outputTopBar.SetTextStyle(barStyle)

	outputBottomBar := tview.NewTextView().SetText(" ↑/↓ or j/k/h/l scroll  g/home top  G/end bottom  ←/→: prev/next host  n/p: prev/next task  esc/enter: back ")
	outputBottomBar.SetTextStyle(barStyle)

	outputFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(outputTopBar, 1, 0, false).
		AddItem(outputView, 0, 1, true).
		AddItem(outputBottomBar, 1, 0, false)

	pages := tview.NewPages()

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
	// entry at all.
	filterDialog := tview.NewTextView().SetDynamicColors(true)
	filterDialog.SetBorder(true).SetTitle(" Filter ")

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
	searchDialogFlex := tview.NewFlex().SetDirection(tview.FlexRow).
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
	taskField := tview.NewInputField().SetLabel("Task: ")
	tagsField := tview.NewInputField().SetLabel("Tags: ")
	hostsField := tview.NewInputField().SetLabel("Hosts: ")

	rerunForm := tview.NewForm().
		AddFormItem(taskField).
		AddFormItem(tagsField).
		AddFormItem(hostsField)
	rerunForm.SetBorder(true).SetTitle(" Re-run (enter: run, esc: cancel) ")

	// outputTask/outputHost track which (task, host) pair the output page
	// is currently showing, so navigateOutputTask (below) knows where
	// "current" is without threading it through as extra state on every
	// keypress.
	var outputTask *taskNode
	var outputHost string

	showOutput := func(task *taskNode, host string) {
		outputTask, outputHost = task, host
		outputTopBar.SetText(fmt.Sprintf(" %s — %s ", host, task.Name))
		outputView.SetText(formatHostOutput(task, host, sourceIndex))
		// SetText does not reset scroll position (lineOffset/trackEnd) -
		// without this, reopening a different host's output right after
		// scrolling through a previous one would open already scrolled to
		// the old position, potentially hiding the new content entirely.
		outputView.ScrollToBeginning()
		viewingOutput = true
		pages.SwitchToPage("output")
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

	var rebuild func()
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
		topBar.SetText(topBarText(playbookName, elapsed, frozen, currentFilter))

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

		_, _, width, _ := list.GetInnerRect()
		// Belt-and-suspenders only: tview.Box's own zero-value width is 15
		// (never 0), and QueueUpdateDraw can't run this closure before
		// Run()'s first real-size draw pass anyway - taskLabel is also
		// panic-safe for any width - but clamp defensively in case that
		// ordering assumption ever changes.
		if width < 20 {
			width = 20
		}

		activeTask := activeTaskNow()

		currentRows = flattenRows(state, expanded, width, activeTask, spinnerAt(elapsed), currentFilter, sourceIndex, showOutput)
		if frozen && everStarted {
			if text := statusRowText(int(exitCode.Load()), state.HadUnreachable); text != "" {
				currentRows = append(currentRows,
					row{text: "", id: statusDividerRowID{}},
					row{text: text, id: statusRowID{}},
				)
			}
		}

		if len(currentRows) == 0 {
			list.Clear()
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
			currentRows[selectedIndex].text = taskLabel(id, state.AllHosts, width, id == activeTask, spinnerAt(elapsed), true)
		case hostRowID:
			currentRows[selectedIndex].text = "    " + hostLabel(id.task, id.host, true)
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
		list.SetCurrentItem(selectedIndex)
	}

	outputView.SetDoneFunc(func(tcell.Key) {
		// Fires on Escape, Enter, Tab, or Backtab (TextView's fixed set of
		// "done" keys). Tab/Backtab also backing out is a harmless side
		// effect, not a real concern.
		viewingOutput = false
		if outputTask != nil {
			// Leave the tree's cursor on whatever (task, host) the output
			// page was last showing - which may have moved via
			// navigateOutputTask/navigateOutputHost since the page was
			// opened - expanding its task if it isn't already, so the row
			// is actually visible rather than just logically "selected".
			expanded[outputTask] = true
			currentID = hostRowID{outputTask, outputHost}
			following = false
			rebuild()
		}
		pages.SwitchToPage("main")
	})

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
		rebuild()
	}
	collapseAll := func() {
		if hid, ok := currentID.(hostRowID); ok {
			currentID = hid.task
		}
		expanded = map[*taskNode]bool{}
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
		if t, ok := currentRows[idx].id.(*taskNode); ok && !expanded[t] {
			expanded[t] = true
			rebuild()
			revealExpandedTask(t)
		}
		// Already-expanded task, a host row, or a play row: no-op - see
		// Keyboard-shortcuts.md's "Right on an already-expanded element"
		// decision.
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
	// openRerunDialog (Rerun.md's 'r' key - see SetInputCapture below,
	// gated there on processDone since re-running only makes sense once a
	// run has finished). Task is deliberately never pre-filled (see
	// rerunForm's own doc comment for why the original cursor-based
	// pre-fill design was dropped) - like Tags/Hosts, it just keeps
	// whatever was last typed into it, empty on the very first open of the
	// session. Tags/Hosts specifically are pre-filled from initialTags/
	// initialHosts (this process's own invocation) only the first time
	// each is opened while still empty - once the user has typed anything
	// into either, later opens leave it alone.
	openRerunDialog := func() {
		rerunDialogOpen = true

		if tagsField.GetText() == "" {
			tagsField.SetText(initialTags)
		}
		if hostsField.GetText() == "" {
			hostsField.SetText(initialHosts)
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

	list.SetChangedFunc(func(index int) {
		if rebuilding {
			// treeList.Clear() resets currentItem to -1, and the first
			// AddItem() afterward sets it straight to 0 (treelist.go) - so
			// rebuild()'s own trailing SetCurrentItem(selectedIndex) call
			// almost always looks like a genuine change from treeList's own
			// point of view (0 -> whatever the real selection is) and fires
			// this callback, even though nothing the user did caused it.
			// rebuild() restores the real selection itself once done, so
			// these need to be ignored - this guard is also what stops that
			// same SetCurrentItem call from recursing into rebuild() again:
			// rebuild() sets rebuilding true for its entire body - Clear(),
			// every AddItem(), and its own final SetCurrentItem() - so any
			// "changed" event that cascades from within it lands here while
			// rebuilding is still true and is correctly ignored instead.
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
	state.OnTaskAdded = func(*playNode, *taskNode) { rebuild() }
	state.OnHostRecorded = func(*taskNode, string) { rebuild() }

	bottomBar := tview.NewTextView().SetText(" ↑/↓/j/k navigate  ←/→: expand/collapse  n/p: prev/next task  home/end/G: top/bottom  F: resume follow  E/C: expand/collapse all  enter/space: toggle  /: filter  r: re-run  q: quit ")
	bottomBar.SetTextStyle(barStyle)

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(topBar, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(bottomBar, 1, 0, false)

	pages.AddPage("main", flex, true, true)
	pages.AddPage("output", outputFlex, true, false)
	pages.AddPage("filter", centeredModal(filterDialog, 46, 9), true, false)
	pages.AddPage("search", centeredModal(searchDialogFlex, 46, 7), true, false)
	pages.AddPage("rerun", centeredModal(rerunForm, 56, 11), true, false)

	app = tview.NewApplication().SetRoot(pages, true).EnableMouse(true)
	// Everything else falls out of tview's own defaults once mouse events
	// are actually turned on (previously never enabled): List's/TextView's
	// built-in mouse wheel handling already just pans the viewport without
	// touching selection, and List's built-in click handling already fires
	// a row's Selected() callback on the first click - identical to Enter -
	// so a host row's output already opens on a single click, and no
	// custom double-click wiring is needed on top of that.

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
	// Placed after `app` is assigned: the go statement inside
	// startHeartbeat's closure body has a happens-before edge (Go memory
	// model) with this very call, which itself runs after `app` was
	// assigned - if startHeartbeat were defined or first called any
	// earlier, reading `app` from the ticker goroutine would be a genuine
	// data race, not just a latency curiosity, even though the first tick
	// is spinnerInterval away.
	startHeartbeat()

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
		hosts := strings.TrimSpace(hostsField.GetText())
		closeDialogs()

		requestRerun(startAtTask, tags, hosts) // resets
		// processDone/exitCode/state synchronously (see main.go) - by the
		// time this returns, rebuild() below already sees a running, empty
		// generation.
		expanded = map[*taskNode]bool{}
		currentID = nil
		following = true
		failureCursorPlaced = false
		haveFrozenElapsed = false
		frozenElapsed = 0
		everStarted = true // only a real transition the very first time
		// this fires for the "rerun" verb's startup dialog (see
		// startWithRerunDialog) - a harmless no-op reassignment every time
		// after that, since it's already true for every other case.
		startedAt = time.Now()
		rebuild() // clear the previous run's rows immediately, rather than
		// leaving them on screen until the new generation's first event
		// arrives.
		startHeartbeat()
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
		// one field.
		if rerunDialogOpen {
			switch event.Key() {
			case tcell.KeyEnter:
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

		// vim/emacs navigation aliases, translated to the native key tview
		// itself already understands and handled identically by both List
		// (main tree) and TextView (output view) - confirmed against
		// tview's own source rather than reimplementing this logic here.
		// Returning a *different* event than the one passed in makes
		// Application forward the synthesized event to whichever primitive
		// is currently focused, as if the user had typed that key - see
		// tview's application.go. This is also what makes plain 'G'/Ctrl-E
		// ride the exact same path plain End already does: ordinary
		// navigation, deliberately with no special "resume autoscroll"
		// side effect (that's F's job alone, below).
		switch {
		case event.Key() == tcell.KeyRune && event.Rune() == 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
		case event.Key() == tcell.KeyRune && event.Rune() == 'k':
			return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlF:
			return tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlB:
			return tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlA:
			return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlE:
			return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
		case event.Key() == tcell.KeyRune && event.Rune() == 'G':
			return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
		}

		if viewingOutput {
			switch {
			case event.Key() == tcell.KeyLeft:
				navigateOutputHost(-1)
				return nil
			case event.Key() == tcell.KeyRight:
				navigateOutputHost(1)
				return nil
			case event.Key() == tcell.KeyRune && event.Rune() == 'p':
				navigateOutputTask(-1)
				return nil
			case event.Key() == tcell.KeyRune && event.Rune() == 'n':
				navigateOutputTask(1)
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
			if !processDone.Load() {
				return nil
			}
			openRerunDialog()
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
		case event.Key() == tcell.KeyRune && event.Rune() == 'p':
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
		if filterDialogOpen || searchDialogOpen || rerunDialogOpen {
			// Fully modal (Filters.md; the re-run dialog follows the same
			// rule even though tview.Form has its own native mouse
			// handling - kept keyboard-only, like the other two, rather
			// than reasoning through whether Pages forwards mouse events
			// to more than just the frontmost visible page): none of the
			// three dialogs need mouse interaction, so the simplest way to
			// stop a click reaching the treeview underneath - including
			// one outside the dialog's own small centered box, which its
			// own MouseHandler would just ignore rather than block - is to
			// swallow all mouse input outright while either is open,
			// rather than teach the dialog pages themselves to consume
			// clicks they don't actually care about.
			return nil, action
		}
		if viewingOutput {
			return event, action // TextView's own wheel handling has no
			// such clamp - there's no "selected line" to keep visible -
			// so the output view already pans freely without any help.
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

const taskIndent = "  "

// colorTag returns the tview style-tag foreground color name for o, per
// TUI.md's OK/Changed/Skipped/Failed = green/yellow/cyan/red convention
// (using tcell's/W3C's "teal" as the closest named match for cyan).
func colorTag(o outcome) string {
	switch o {
	case outcomeOK:
		return "green"
	case outcomeChanged:
		return "yellow"
	case outcomeSkipped:
		return "teal"
	case outcomeFailed:
		return "red"
	case outcomeUnreachable:
		return "maroon" // deliberately muted vs "red" - see TUI.md; both are
		// base ANSI-16 names (index 9 vs 1), not RGB-approximated extended
		// W3C names, so they stay reliably distinct across terminal themes.
	default:
		return "-"
	}
}

const (
	// minTaskTitleName is the floor task.Name's own text is shortened to
	// before hostnames start getting shrunk instead (see taskLabel) - 30
	// runes, deliberately generous so a title only gives up ground to the
	// host list once the host list has already been squeezed hard.
	minTaskTitleName = 30

	// titleHostGapFloor is the minimum acceptable breathing room between the
	// title and the first host name, per TUI.md - used only to decide
	// *whether* shrinking is needed and *how far* to shrink the title. The
	// gap actually rendered (see taskLabel) is whatever's left over once
	// title and hosts are sized, which is normally >= this - true
	// right-alignment, matching TUI.md's own sketch, not a fixed 3 spaces.
	titleHostGapFloor = 3

	// minRenderedGap is the hard floor on the space actually rendered
	// between title and hosts. Only reached once the title (floored at 15
	// chars) and every hostname (floored at 1 char each) still don't leave
	// titleHostGapFloor of room - a pathologically narrow, accepted overflow
	// case (same style as aggregate.go's noteHost comment and this file's
	// width-staleness note below), not engineered away further.
	minRenderedGap = 1

	// halfBlock is U+258C LEFT HALF BLOCK - its filled ("ink") half renders
	// in the cell's current foreground color, its unfilled half shows the
	// current background color. Used as a two-tone transition cell between
	// adjacent hostnames (see taskLabel/hostTransition): foreground = the
	// previous hostname's own color, background = the next hostname's own
	// color, so the separator blends from one into the other instead of an
	// abrupt full-cell change.
	halfBlock = "▌"

	grayTag = "gray" // placeholder color for a host AllHosts knows about
	// run-wide but that hasn't reported for *this* task yet.

	// pureBlack is a fixed hex value, not tcell's named "black" - some
	// terminal themes remap the base-16 ANSI "black" slot to a dark gray
	// rather than true black (the same base-16-vs-fixed-value trap already
	// documented for red/maroon, see colorTag). Used for every selected-row
	// text color, which specifically needs to read as unambiguously black
	// against the light backgrounds those rows use.
	pureBlack = "#1a1a1a"
)

// hostTransition builds the halfBlock separator cell between two adjacent
// hostnames' color tags - left's color bleeds into right's across that one
// cell, rather than an abrupt full-cell jump from one solid color to the
// next.
func hostTransition(leftTag, rightTag string) string {
	return fmt.Sprintf("[%s:%s:-]%s[-:-:-]", leftTag, rightTag, halfBlock)
}

// taskLabel builds one TASK row's full text, including its leading indent.
// Per TUI.md's "New ideas for the task lines", every host in allHosts (the
// run-wide, alphabetically-sorted set of hosts seen so far - see
// playbookState.AllHosts) is shown right-aligned after the task title, each
// colored by its outcome for this specific task, or gray if this task
// hasn't recorded a result for it yet. If allHosts is empty (nothing has
// been discovered run-wide yet - always true right up until the first
// result of the run lands, for whichever task that turns out to be), the
// row is just the title, with no trailing gap or content.
//
// Fitting/shrinking happens entirely in plain, untagged rune space (raw task
// name, raw host names) and only wraps the final, already-correctly-sized
// pieces in color tags and tview.Escape() once, at the end - avoids repeated
// tview.TaggedStringWidth calls inside what can otherwise be a
// multi-iteration shrink loop, and mirrors the truncate-raw-then-escape
// discipline the old counts-based taskLabel already used.
//
// Per TUI.md: if title+gap+hosts doesn't fit, first the title's own name
// text is shortened (down to minTaskTitleName, or its own natural length if
// that's already shorter); if that alone isn't enough, hostnames are
// gradually shortened next - always the currently-longest one, one
// character at a time, down to a 1-character floor each. Truncation
// collisions between hostnames are an accepted, known tradeoff (TUI.md) -
// not solved here. Hostname truncation has no ellipsis marker (per TUI.md's
// own example); title truncation keeps the old "…" convention, since
// TUI.md says the title's rendering is otherwise unchanged ("as before").
//
// avail reflects the terminal size as of the last rebuild (see
// flattenRows) - a bare resize with no new incoming event won't re-flow
// existing rows until the next event triggers one. Accepted limitation,
// not a bug - unchanged from the old taskLabel.
//
// active marks the currently-executing task (see flattenRows); when true,
// frame (this rebuild's shared spinner frame - see spinnerAt) renders in
// place of the row's leading indent (taskIndent) instead of as a trailing
// suffix after the title - moved there so the space it needs comes out of
// the already-existing indent instead of being carved out of availContent
// on top of it, leaving hostnames that much more room on the right. When
// active is false, taskIndent's own plain spaces render there instead -
// the same fixed width either way, so every row's hostnames still shrink
// identically regardless of which task, if any, happens to be active;
// letting only the active row's prefix differ in width would make that
// one row's hostnames truncate slightly more than the others'.
//
// selected marks this as the row currently under the cursor (see rebuild's
// selected-row patch, and NewLiveTUI's SetSelectedStyle comment for why
// this can't just be a single uniform List-wide style): the title gets
// black bold text on a light gray background, and each hostname gets black
// bold text on its own outcome color as a background instead of a
// foreground - the inverse of the normal rendering below.
func taskLabel(task *taskNode, allHosts []string, avail int, active bool, frame rune, selected bool) string {
	availContent := avail - len(taskIndent)
	if availContent < 0 {
		availContent = 0
	}

	// rawPrefix fills the row's leading taskIndent-width slot: the spinner
	// frame plus one space for the active task, or taskIndent's own plain
	// spaces otherwise - always exactly len(taskIndent) wide either way,
	// so (unlike the old trailing-suffix version) no separate width needs
	// reserving from availContent for this at all.
	rawPrefix := taskIndent
	if active {
		rawPrefix = string(frame) + " "
	}
	// tview.Escape's own guaranteed-correct handling of "[...]"-shaped text
	// applies here too (list rows parse tags, unlike the top bar) - harmless
	// no-op on taskIndent's plain spaces or the spinner rune, neither of
	// which is ever "["-shaped.
	prefix := tview.Escape(rawPrefix)

	nameRunes := []rune(task.Name)
	nameWidth := len(nameRunes)

	// Per-host raw display text, shrunk (if at all) as independent copies -
	// never mutates allHosts or its strings.
	hostRunes := make([][]rune, len(allHosts))
	for i, h := range allHosts {
		hostRunes[i] = []rune(h)
	}
	hostsWidth := func() int {
		w := 0
		for i, hr := range hostRunes {
			w += len(hr)
			if i > 0 {
				w++ // fixed 1-space separator between adjacent host names -
				// not itself a shrink target per TUI.md's algorithm, which
				// only calls out the title and the hostnames themselves.
			}
		}
		return w
	}

	haveHosts := len(allHosts) > 0

	// fits reports whether the current nameWidth, plus a hypothetical
	// titleHostGapFloor-sized gap, plus the current host list, would fit -
	// i.e. "is there at least the minimum acceptable breathing room". Drives
	// the shrink decisions below; the gap actually rendered (see padding,
	// at the end) is computed separately, as whatever's really left over.
	fits := func() bool {
		need := nameWidth
		if haveHosts {
			need += titleHostGapFloor + hostsWidth()
		}
		return need <= availContent
	}

	truncatedName := false

	if !fits() {
		// Step 1 (TUI.md): shorten the title's own name text, down to
		// minTaskTitleName (or its own natural length, if that's already
		// shorter - the floor only ever shrinks, never pads).
		floor := nameWidth
		if floor > minTaskTitleName {
			floor = minTaskTitleName
		}
		var need int
		if haveHosts {
			need += titleHostGapFloor + hostsWidth()
		}
		target := availContent - need
		if target < floor {
			target = floor
		}
		if target < nameWidth {
			nameWidth = target
			truncatedName = true
		}
	}

	// Step 2 (TUI.md): if the title, even floored, still doesn't leave
	// titleHostGapFloor of room before the full host list, gradually shrink
	// hostnames - always the currently-longest, one character at a time,
	// down to a 1-character floor each.
	for !fits() {
		longest := -1
		for i, hr := range hostRunes {
			if len(hr) > 1 && (longest == -1 || len(hr) > len(hostRunes[longest])) {
				longest = i
			}
		}
		if longest == -1 {
			// Every hostname is already at its 1-character floor and it
			// still doesn't fit even alongside a title floored at
			// minTaskTitleName characters. Accept the overflow (see
			// minRenderedGap).
			break
		}
		hostRunes[longest] = hostRunes[longest][:len(hostRunes[longest])-1]
	}

	var rawTitle string
	if truncatedName && nameWidth >= 1 {
		rawTitle = string(nameRunes[:nameWidth-1]) + "…"
	} else {
		rawTitle = string(nameRunes[:nameWidth])
	}
	// Escape only now that the raw text is already correctly sized, so
	// slicing above can never cut into an escape sequence Escape() would
	// otherwise have produced.
	title := tview.Escape(rawTitle)

	// Normally a foreground-only tag (background left untouched, so it
	// shows whatever the row's base background already is), regular
	// weight, per TUI.md's task-line styling. "silver", not "lightgray":
	// deliberately not grayTag's plain "gray" either - that's already the
	// established color for "host hasn't reported for this task yet";
	// reusing it here for the title itself would blur that distinction.
	// When selected, black bold text on an explicit light gray background
	// instead - see this function's own selected doc above. pureBlack (a
	// hex value, not the named "black") is used everywhere selected text
	// needs black: tcell's named "black" is the base-16 ANSI slot, which
	// some terminal themes remap to a dark gray rather than true black -
	// the same base-16-vs-fixed-value trap already documented for
	// red/maroon (colorTag) elsewhere in this file. A hex value is a fixed
	// RGB, immune to that remapping.
	if !haveHosts {
		if selected {
			return prefix + "[" + pureBlack + ":lightgray:b]" + title + "[-:-:-]"
		}
		return prefix + "[silver::-]" + title + "[-::-]"
	}

	// The actual rendered gap is whatever's really left over once title and
	// (possibly-shrunk) hosts are sized - right-aligning the host list to
	// the row's far edge, same "variable padding, floored low" shape as the
	// old counts-based taskLabel's own padding math.
	padding := availContent - tview.TaggedStringWidth(title) - hostsWidth()
	if padding < minRenderedGap {
		padding = minRenderedGap
	}

	if selected {
		// No neutral/uncolored cells anywhere: the gray title background
		// extends right up to one space before the first hostname (that
		// last space, and every hostname's own leading space thereafter,
		// belongs to that hostname's own color block instead) - so hosts
		// are concatenated directly, not joined with a separate plain " ".
		greyPadding := padding - 1
		if greyPadding < 0 {
			greyPadding = 0
		}
		var b strings.Builder
		b.WriteString(prefix)
		b.WriteString("[" + pureBlack + ":lightgray:b]")
		b.WriteString(title)
		b.WriteString(strings.Repeat(" ", greyPadding))
		b.WriteString("[-:-:-]")
		// Host[0]'s own leading space stays solid-colored (transitioning
		// from the title's grey isn't attempted here - the user's ask was
		// specifically about the space between adjacent hostnames). Every
		// later host's leading space is replaced by a halfBlock transition
		// cell blending the previous host's color into this one's, instead
		// of just restating this host's own color again.
		var prevTag string
		for i, h := range allHosts {
			o, done := task.Hosts[h]
			tag := grayTag
			if done {
				tag = colorTag(o)
			}
			name := tview.Escape(string(hostRunes[i]))
			if i == 0 {
				fmt.Fprintf(&b, "[%s:%s:b] %s[-:-:-]", pureBlack, tag, name)
			} else {
				b.WriteString(hostTransition(prevTag, tag))
				fmt.Fprintf(&b, "[%s:%s:b]%s[-:-:-]", pureBlack, tag, name)
			}
			prevTag = tag
		}
		return b.String()
	}

	// Plain foreground-colored text on a plain " " separator - tried the
	// same halfBlock transition used in the selected branch above here too,
	// but confirmed (by looking at it) that it doesn't read well against
	// unselected hostnames' plain foreground-only coloring - reverted.
	styledTitle := "[silver::-]" + title + "[-::-]"
	hostSegments := make([]string, len(allHosts))
	for i, h := range allHosts {
		o, done := task.Hosts[h]
		tag := grayTag
		if done {
			tag = colorTag(o)
		}
		hostSegments[i] = fmt.Sprintf("[%s]%s[-]", tag, tview.Escape(string(hostRunes[i])))
	}

	return prefix + styledTitle + strings.Repeat(" ", padding) + strings.Join(hostSegments, " ")
}

// hostLabel builds one host row's text, colored uniformly by its single
// outcome. No width-based truncation/alignment applies here - that rule is
// TASK-row-specific per TUI.md. selected mirrors taskLabel's own parameter -
// black bold text on the outcome color as a background, instead of the
// outcome color as a foreground.
func hostLabel(task *taskNode, host string, selected bool) string {
	o := task.Hosts[host]
	// detail is the extra parenthesized bit appended after the outcome
	// word - what it is depends on the outcome. Only OK/Changed/Failed and
	// Skipped have one defined so far; Unreachable renders exactly as
	// before.
	var detail string
	switch o {
	case outcomeOK, outcomeChanged, outcomeFailed:
		detail = outputSummary(task.Raw[host])
	case outcomeSkipped:
		detail = skipDetail(task.Raw[host])
	}
	line := fmt.Sprintf("%s: %s%s", tview.Escape(host), o, tview.Escape(detail))
	if selected {
		return fmt.Sprintf("[%s:%s:b]%s[-:-:-]", pureBlack, colorTag(o), line)
	}
	return fmt.Sprintf("[%s]%s[-]", colorTag(o), line)
}

// allTasks returns every task across every play, in run order (play order,
// then task order within each play) - the host-agnostic sibling of
// tasksForHost below, backing the main tree's n/p task-hop and E (expand
// all) shortcuts.
func allTasks(state *playbookState) []*taskNode {
	var tasks []*taskNode
	for _, play := range state.Plays {
		tasks = append(tasks, play.Tasks...)
	}
	return tasks
}

// tasksForHost returns, in run order (play order, then task order within
// each play), every task that has recorded a result for host - used by the
// output drill-down view's prev/next-task navigation (see NewLiveTUI's
// navigateOutputTask) to step through one host's results across tasks,
// skipping tasks that host wasn't part of.
func tasksForHost(state *playbookState, host string) []*taskNode {
	var tasks []*taskNode
	for _, play := range state.Plays {
		for _, t := range play.Tasks {
			if _, ok := t.Hosts[host]; ok {
				tasks = append(tasks, t)
			}
		}
	}
	return tasks
}

// visibleTasks is allTasks' filtered sibling - every task that gets a row
// under filter (see taskVisible), in the same run order. Used by
// navigateMainTask (n/p) and the filter-switch cursor fallback so neither
// ever targets a task flattenRows wouldn't actually have rendered a row
// for.
func visibleTasks(state *playbookState, filter filterQuery, sourceIndex taskSourceIndex, activeTask *taskNode) []*taskNode {
	var tasks []*taskNode
	for _, t := range allTasks(state) {
		if taskVisible(t, filter, sourceIndex, t == activeTask) {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// visibleTasksForHost is tasksForHost's filtered sibling, for the output
// drill-down view's prev/next-task navigation (see navigateOutputTask).
func visibleTasksForHost(state *playbookState, host string, filter filterQuery, sourceIndex taskSourceIndex, activeTask *taskNode) []*taskNode {
	var tasks []*taskNode
	for _, t := range tasksForHost(state, host) {
		if taskVisible(t, filter, sourceIndex, t == activeTask) {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// taskSet turns a task slice into a membership set - shared by
// nearestVisibleTask and navigateMainTask's play-row case below, both of
// which need repeated "is this task currently visible" checks against the
// same visibleTasks() result.
func taskSet(tasks []*taskNode) map[*taskNode]bool {
	set := make(map[*taskNode]bool, len(tasks))
	for _, t := range tasks {
		set[t] = true
	}
	return set
}

// firstVisibleTask returns play's own first task that's currently visible
// (per the visible set, built from visibleTasks), or nil if it has none -
// used by navigateMainTask's play-row case so "next" from a play row never
// targets a task the active filter is hiding. Never actually returns nil in
// practice: flattenRows only ever gives a play a row at all when it has at
// least one visible task, and navigateMainTask only reaches this for a play
// the cursor is currently sitting on.
func firstVisibleTask(play *playNode, visible map[*taskNode]bool) *taskNode {
	for _, t := range play.Tasks {
		if visible[t] {
			return t
		}
	}
	return nil
}

// nearestVisibleTask finds, within visible (a filtered, order-preserving
// subset of all), the task closest to anchor's original position: the next
// visible task at or after it in run order, or failing that, the last
// visible task before it. Returns nil if visible is empty. Used when
// switching filters removes the row the cursor was pinned to (see
// NewLiveTUI's applyFilter) - "removed" always means a whole task (and, per
// Filters.md, all of its still-shown hosts) disappearing at once, never an
// individual host row on its own, so a task is always the right granularity
// to land on.
func nearestVisibleTask(all []*taskNode, anchor *taskNode, visible []*taskNode) *taskNode {
	if len(visible) == 0 {
		return nil
	}
	visibleSet := taskSet(visible)
	anchorIdx := -1
	for i, t := range all {
		if t == anchor {
			anchorIdx = i
			break
		}
	}
	if anchorIdx == -1 {
		return visible[0]
	}
	for i := anchorIdx; i < len(all); i++ {
		if visibleSet[all[i]] {
			return all[i]
		}
	}
	for i := anchorIdx - 1; i >= 0; i-- {
		if visibleSet[all[i]] {
			return all[i]
		}
	}
	return visible[0] // unreachable given visible is non-empty and a subset of all
}

// primaryOutputField picks a single field to represent a host's textual
// output, preferring "stdout" (the actual command output, for
// command/shell-style modules) over the more generic "msg" field - on a
// command/shell task with a non-zero return code, msg is often just a
// fixed status string ("non-zero return code") alongside the real output
// already sitting in stdout. Falls back to msg for modules that only set
// that field (most non-command modules, e.g. debug/fail/assert). Shared by
// formatHostOutput (the full drill-down view) and outputSummary (the
// collapsed treeview OK line) so both agree on what "the output" means for
// a given result.
func primaryOutputField(decoded map[string]interface{}) (label, text string) {
	if stdout, ok := decoded["stdout"].(string); ok && stdout != "" {
		return "STDOUT", stdout
	}
	if msg, ok := decoded["msg"].(string); ok && msg != "" {
		return "MSG", msg
	}
	return "", ""
}

// outputSummary returns the parenthesized detail hostLabel appends after
// "OK"/"Changed"/"Failed" - the single line of output verbatim if
// primaryOutputField's chosen text is exactly one line, or its line count
// otherwise. "" (nothing appended) if there's no output text at all, e.g.
// modules like copy/template that only report changed, with no msg/stdout
// of their own.
func outputSummary(raw json.RawMessage) string {
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	_, text := primaryOutputField(decoded)
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 1 {
		return fmt.Sprintf(" (%s)", lines[0])
	}
	return fmt.Sprintf(" (%d lines of output)", len(lines))
}

// skipDetail returns the parenthesized "(skip_reason: false_condition)"
// detail hostLabel appends after "Skipped", pulled straight from the
// task's own recorded result for that host - "" if skip_reason wasn't
// present (shouldn't happen for a real v2_runner_on_skipped event, but
// this is live external jsonl, not trusted blindly - same caveat as
// formatHostOutput's own decode below).
func skipDetail(raw json.RawMessage) string {
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	reason, _ := decoded["skip_reason"].(string)
	if reason == "" {
		return ""
	}
	if cond, ok := decoded["false_condition"].(string); ok && cond != "" {
		return fmt.Sprintf(" (%s: %s)", reason, cond)
	}
	return fmt.Sprintf(" (%s)", reason)
}

// sectionLabel renders one of formatHostOutput's section headers in its
// own color, bold - a distinct color per section (deliberately outside the
// outcome palette in colorTag, so a reader never confuses "this section is
// STDERR" with "this host's outcome is Failed") makes the view scannable
// at a glance rather than a wall of uniform text. label is a fixed literal
// from formatHostOutput itself, never external content, so it needs no
// escaping.
// yamlKeyLine matches a line's "key:" shape: optional leading indentation
// and a "- " list marker (both preserved verbatim, uncolored - group 1),
// a plain-scalar key (group 2, letters/digits/underscore/dot/hyphen -
// covers ordinary task fields like "name"/"when" as well as FQCN module
// names like "ansible.builtin.debug"), the colon itself (group 3), and
// whatever follows - either more content after a space, or nothing, for a
// key whose value starts on the next line (group 4). Deliberately not a
// real YAML parser - a line that doesn't match this shape (a continuation
// of a multi-line scalar, or a plain "- item" list entry with no key)
// just renders unstyled; good enough for the "key: value" and "- key:
// value" shapes every real task definition checked against this project
// uses.
var yamlKeyLine = regexp.MustCompile(`^(\s*(?:-\s+)?)([A-Za-z0-9_.-]+)(:)(\s.*|)$`)

// colorizeYAML renders raw YAML task source (see source.go) with a light,
// line-based highlight: each line's "key:" portion, if it has one, is
// colored, so structure is scannable at a glance without a real
// tokenizer. Every dynamic piece is escaped separately (not the line as a
// whole before coloring) so a literal "[" in the source itself - e.g.
// "tags: [foo, bar]", which this project's own test fixtures actually
// contain - can never be misread as a color tag.
func colorizeYAML(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		m := yamlKeyLine.FindStringSubmatch(line)
		if m == nil {
			lines[i] = tview.Escape(line)
			continue
		}
		lines[i] = tview.Escape(m[1]) + "[orange::b]" + tview.Escape(m[2]+m[3]) + "[-::-]" + tview.Escape(m[4])
	}
	return strings.Join(lines, "\n")
}

func sectionLabel(color, label string) string {
	return fmt.Sprintf("[%s::b]%s[-::-]\n[%s]%s[-]\n", color, label, color, strings.Repeat("=", len([]rune(label))))
}

// taskSourceLocation formats task.Path ("<absolute file>:<line>", see
// events.go/aggregate.go) as "[<file>, line <n>]" for display right below
// the Task section's own YAML - "" if path doesn't have that shape
// (shouldn't happen for a real event, but not trusted blindly).
func taskSourceLocation(path string) string {
	idx := strings.LastIndex(path, ":")
	if idx == -1 {
		return ""
	}
	file, lineStr := path[:idx], path[idx+1:]
	if file == "" || lineStr == "" {
		return ""
	}
	return fmt.Sprintf("[%s, line %s]", file, lineStr)
}

// formatHostOutput renders task.Raw[host] for the output drill-down view.
// It decodes into a generic map (not a fixed struct) since different
// Ansible modules return wildly different result shapes; msg/stdout/stderr
// are pulled out as labeled, human-readable sections, followed
// unconditionally by the complete result as pretty-printed JSON, which is
// what makes this work for any module type without having to special-case
// each one. The output TextView has dynamic colors on (see NewLiveTUI), so
// every piece of dynamic/external content here - task source, stdout/
// stderr/msg, the full JSON, even the raw bytes on a decode failure - is
// individually tview.Escape()'d before being written, so a literal "[" in
// any of it (e.g. "tags: [a, b]", a JSON array) can never be misread as a
// color tag; only this function's own fixed label/status text is trusted
// unescaped.
//
// Leads with a colored status line (colorTag(o), the same outcome palette
// the tree itself uses) so the host's outcome for this task is visible
// without reading anything else first.
//
// sourceIndex backs the TASK: section - task.Path's own raw YAML source
// (source.go), shown ahead of everything else so the task definition and
// its result are visible together without switching views, and lightly
// highlighted line-by-line (colorizeYAML) rather than as flat text. A
// lookup miss (task.Path empty, or just not found - an unusual file
// layout, or a task genuinely generated at runtime) omits the section
// entirely rather than showing an error; this is best-effort by design.
func formatHostOutput(task *taskNode, host string, sourceIndex taskSourceIndex) string {
	raw := task.Raw[host]
	if len(raw) == 0 {
		// Shouldn't happen in normal operation - every host recorded via
		// recordHost always has some raw payload - but a live jsonl stream
		// from an external process isn't something to trust blindly, so
		// degrade gracefully rather than showing a blank screen.
		return fmt.Sprintf("(no output recorded for %s)", tview.Escape(host))
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// Not a JSON object - shouldn't happen for any real module
		// result, but show the raw bytes rather than nothing.
		return tview.Escape(string(raw))
	}

	var b strings.Builder
	o := task.Hosts[host]
	fmt.Fprintf(&b, "[%s::b]● %s[-::-]\n\n", colorTag(o), tview.Escape(o.String()))

	if source, ok := sourceIndex[task.Path]; ok && source != "" {
		b.WriteString(sectionLabel("orange", "Task"))
		b.WriteString(colorizeYAML(source))
		b.WriteString("\n")
		if loc := taskSourceLocation(task.Path); loc != "" {
			fmt.Fprintf(&b, "[gray]%s[-]\n", tview.Escape(loc))
		}
		b.WriteString("\n")
	}
	writeSection := func(color, label, key string) {
		s, ok := decoded[key].(string)
		if !ok || s == "" {
			return
		}
		b.WriteString(sectionLabel(color, label))
		b.WriteString(tview.Escape(s))
		b.WriteString("\n\n")
	}
	// Only one of MSG/STDOUT is shown, not both - see primaryOutputField;
	// always labeled "Output" here regardless of which field it came
	// from, since that distinction is an internal selection detail, not
	// something worth surfacing in the section header.
	if _, text := primaryOutputField(decoded); text != "" {
		b.WriteString(sectionLabel("aqua", "Output"))
		b.WriteString(tview.Escape(text))
		b.WriteString("\n\n")
	}
	writeSection("red", "Errors", "stderr")

	pretty, err := json.MarshalIndent(decoded, "", "  ")
	b.WriteString(sectionLabel("gray", "Details"))
	if err != nil {
		fmt.Fprintf(&b, "(failed to format: %s)\n%s", tview.Escape(err.Error()), tview.Escape(string(raw)))
	} else {
		b.WriteString(tview.Escape(string(pretty)))
	}

	return b.String()
}
