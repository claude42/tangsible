package main

import (
	"encoding/json"
	"fmt"
	"os"
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
// by the top bar's own heartbeat and taskLabel's active-task suffix so both
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
// duration). Once frozen, the spinner is dropped entirely (simplicity over
// cuteness) rather than stuck on an arbitrary frame or swapped for a
// checkmark. No tview.Escape() needed here - unlike the list, this
// TextView never enables dynamic color tags.
func topBarText(playbookName string, elapsed time.Duration, frozen bool) string {
	mm, ss := minutesSeconds(elapsed)
	if frozen {
		return fmt.Sprintf(" %s  %02d:%02d ", playbookName, mm, ss)
	}
	return fmt.Sprintf(" %s  %c %02d:%02d ", playbookName, spinnerAt(elapsed), mm, ss)
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
// task row once the run has finished, or "" for any code this deliberately
// doesn't speak for (genuine failures, or a non-ExitError -1) - no extra
// row at all for those, preserving today's behavior. hadUnreachable mirrors
// main.go's benignHostUnreachable check: exit 4 (ansible-core's own
// overloaded HOST_UNREACHABLE/PARSER_ERROR value) reads as success here
// too, once Tangsible has independently observed a real unreachable host
// this run.
func statusRowText(code int, hadUnreachable bool) string {
	benignHostUnreachable := code == 4 && hadUnreachable
	switch {
	case code == 0 || benignHostUnreachable:
		return "[green]Playbook completed successfully[-]"
	case code == ansibleUserInterruptedExitCode:
		return "[red]Playbook stopped, press q again to quit tangsible.[-]"
	default:
		return ""
	}
}

// flattenRows walks state's play/task/host tree into an ordered row list,
// respecting which tasks are currently expanded. Rebuilt fresh on every
// event - cheap at this project's target scale (~10 hosts, Purpose.md), and
// avoids needing to incrementally patch a tree structure by hand.
//
// width is the list's current available width (see rebuild), used to
// right-align each TASK row's counts segment (see taskLabel). activeTask
// (nil once the run has finished) gets a spinner suffix on its row instead
// of an elapsed-time readout - frame is the shared spinner frame for this
// rebuild pass (see spinnerAt), computed once and passed in rather than
// each row picking its own, so every active indicator in the UI ticks in
// lockstep. showOutput is called when a host row is selected (Enter), to
// display that host's full result for that task.
func flattenRows(state *playbookState, expanded map[*taskNode]bool, width int, activeTask *taskNode, frame rune, showOutput func(task *taskNode, host string)) []row {
	var rows []row
	for _, play := range state.Plays {
		rows = append(rows, row{text: playRowText(play, false), id: play})
		for _, task := range play.Tasks {
			t := task
			rows = append(rows, row{
				text:     taskLabel(t, state.AllHosts, width, t == activeTask, frame, false),
				id:       t,
				selected: func() { expanded[t] = !expanded[t] },
			})
			if expanded[t] {
				for _, host := range t.HostOrder {
					h := host
					rows = append(rows, row{
						text:     "    " + hostLabel(t, h, false),
						id:       hostRowID{t, h},
						selected: func() { showOutput(t, h) },
					})
				}
			}
		}
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

// NewLiveTUI builds an initially-empty list UI and wires it to state's
// hooks so it grows as events arrive. It does not block — the caller must
// call app.Run() and feed events through applyLive.
//
// proc is ansible-playbook's process, used so Ctrl-C/q can forward SIGINT
// to it while it's still running (tcell's raw mode disables the OS's own
// Ctrl-C-to-SIGINT delivery, so without this the child would stop
// receiving the interrupt it used to get for free — see Purpose.md's
// Ctrl-C decision). processDone/quitting are shared with the caller:
// this function only reads processDone and only writes quitting.
func NewLiveTUI(state *playbookState, playbookName string, proc *os.Process, processDone, quitting *atomic.Bool, exitCode *atomic.Int32) (app *tview.Application, applyLive func(rawEvent)) {
	startedAt := time.Now() // wall-clock the TUI itself came up - see
	// topBarText's doc comment for why this is deliberately not sourced
	// from any event.

	list := tview.NewList().ShowSecondaryText(false)
	list.SetWrapAround(false)

	expanded := map[*taskNode]bool{}
	var currentRows []row
	var currentID any
	var rebuilding bool
	following := true     // auto-follow the newest row until the user navigates away
	var jumpingToEnd bool  // true only while our own End/G handler drives SetCurrentItem
	var viewingOutput bool // true while the host-output page is frontmost; see
	// SetInputCapture below - suppresses the list's own End/'G' handling so
	// it doesn't swallow the output TextView's native End/'G' scrolling. A
	// plain locally-owned bool, not a pages.GetFrontPage() query, since this
	// function owns both places that ever switch pages.

	// Moved up here (was previously declared after rebuild/hooks) - rebuild()
	// now updates it on every call, so it must exist first.
	topBar := tview.NewTextView().SetText(topBarText(playbookName, 0, false))
	topBar.SetTextStyle(barStyle)

	// The cursor row's actual look (black-on-light-gray title, black bold
	// text on a per-outcome colored background for each hostname - see
	// playRowText/taskLabel/hostLabel's selected parameter) can't be
	// expressed as a single style applied uniformly to a row's whole text -
	// different runs of the same row need different foreground/background
	// combinations. So List's own automatic per-row highlighting is turned
	// into a no-op (matching mainTextStyle's own colors exactly) and
	// rebuild() instead re-renders whichever one row is currently selected
	// with its own selected=true variant before ever calling AddItem.
	list.SetSelectedStyle(tcell.StyleDefault.Foreground(tview.Styles.PrimaryTextColor).Background(tview.Styles.PrimitiveBackgroundColor))

	// Output drill-down page: a single, reused TextView (never recreated
	// per drill-down), updated via SetText each time a host row is
	// selected. styleTags stays off (tview's default) so raw command
	// output can go straight into SetText with no tview.Escape() and no
	// risk of it being misparsed as color tags.
	outputView := tview.NewTextView()
	outputView.SetDynamicColors(false) // explicit, though already the
	// default - self-documents that "no tag parsing" is deliberate here,
	// not an oversight, given raw command output goes straight into it.

	outputTopBar := tview.NewTextView()
	outputTopBar.SetTextStyle(barStyle)

	outputBottomBar := tview.NewTextView().SetText(" ↑/↓ or j/k/h/l scroll  g/home top  G/end bottom  esc/enter: back ")
	outputBottomBar.SetTextStyle(barStyle)

	outputFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(outputTopBar, 1, 0, false).
		AddItem(outputView, 0, 1, true).
		AddItem(outputBottomBar, 1, 0, false)

	pages := tview.NewPages()

	showOutput := func(task *taskNode, host string) {
		outputTopBar.SetText(fmt.Sprintf(" %s — %s ", host, task.Name))
		outputView.SetText(formatHostOutput(task, host))
		// SetText does not reset scroll position (lineOffset/trackEnd) -
		// without this, reopening a different host's output right after
		// scrolling through a previous one would open already scrolled to
		// the old position, potentially hiding the new content entirely.
		outputView.ScrollToBeginning()
		viewingOutput = true
		pages.SwitchToPage("output")
	}

	outputView.SetDoneFunc(func(tcell.Key) {
		// Fires on Escape, Enter, Tab, or Backtab (TextView's fixed set of
		// "done" keys). Tab/Backtab also backing out is a harmless side
		// effect, not a real concern.
		viewingOutput = false
		pages.SwitchToPage("main")
	})

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
		topBar.SetText(topBarText(playbookName, elapsed, frozen))

		_, _, width, _ := list.GetInnerRect()
		// Belt-and-suspenders only: tview.Box's own zero-value width is 15
		// (never 0), and QueueUpdateDraw can't run this closure before
		// Run()'s first real-size draw pass anyway - taskLabel is also
		// panic-safe for any width - but clamp defensively in case that
		// ordering assumption ever changes.
		if width < 20 {
			width = 20
		}

		var activeTask *taskNode
		if !frozen {
			activeTask = state.CurrentTask()
		}

		currentRows = flattenRows(state, expanded, width, activeTask, spinnerAt(elapsed), showOutput)
		if frozen {
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
		// row; otherwise restore by currentID's identity (row order shifts
		// as things are appended, so a raw index can't be trusted across
		// rebuilds), defaulting to 0 if that id no longer exists (shouldn't
		// happen - nothing is ever removed - but not indexing out of range
		// if it somehow did).
		selectedIndex := 0
		if following {
			selectedIndex = len(currentRows) - 1
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
				}
			}
			list.AddItem(r.text, "", 0, selected)
		}
		list.SetCurrentItem(selectedIndex)
	}

	list.SetChangedFunc(func(index int, _ string, _ string, _ rune) {
		if rebuilding {
			// List.Clear()+AddItem() fires spurious "changed" events while
			// rebuild() is repopulating (e.g. as soon as the first item
			// lands back in the now-empty list) - ignore those, rebuild()
			// restores the real selection itself once done. This guard is
			// also what makes the rebuild() call below safe against
			// reentering itself: SetCurrentItem (tview's list.go) fires
			// "changed" BEFORE updating its own currentItem, so if this
			// handler's own rebuild() (via its closing SetCurrentItem call)
			// cascaded back into this same handler with rebuilding still
			// false, it would recurse without ever terminating. Since
			// rebuild() sets rebuilding true for its entire body - Clear(),
			// every AddItem(), and its own final SetCurrentItem() - any
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

	bottomBar := tview.NewTextView().SetText(" ↑/↓ navigate  home/end/G: top/bottom  enter: expand/view output  q: quit ")
	bottomBar.SetTextStyle(barStyle)

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(topBar, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(bottomBar, 1, 0, false)

	pages.AddPage("main", flex, true, true)
	pages.AddPage("output", outputFlex, true, false)

	app = tview.NewApplication().SetRoot(pages, true)

	// Top-bar heartbeat ticker - the first self-driven (not event- or
	// input-triggered) source of QueueUpdateDraw calls in this codebase.
	// Placed after `app` is assigned: the go statement's happens-before
	// edge (Go memory model) guarantees this goroutine sees that
	// assignment - if this were moved earlier in the function, reading
	// `app` here would be a genuine data race, not just a latency
	// curiosity, even though the first tick is spinnerInterval away.
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
				// rather than redrawing a static screen forever.
			}
		}
	}()

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		isQuit := event.Key() == tcell.KeyCtrlC ||
			(event.Key() == tcell.KeyRune && event.Rune() == 'q')
		if isQuit {
			if processDone.Load() {
				quitting.Store(true) // before Stop() - see main.go's race note
				app.Stop()
			} else {
				_ = proc.Signal(os.Interrupt) // best-effort; child may race-exit
			}
			return nil
		}

		isJumpToEnd := event.Key() == tcell.KeyEnd ||
			(event.Key() == tcell.KeyRune && event.Rune() == 'G')
		if isJumpToEnd && !viewingOutput {
			following = true
			jumpingToEnd = true
			if list.GetItemCount() > 0 {
				list.SetCurrentItem(list.GetItemCount() - 1)
			}
			jumpingToEnd = false
			return nil
		}
		// While viewingOutput, End/'G' fall through unmodified so the output
		// TextView's own native End/'G' scroll-to-bottom handling gets them
		// instead - rebuild() keeps updating the hidden list regardless of
		// which page is frontmost, so nothing about the list's state is lost.

		return event
	})

	applyLive = func(ev rawEvent) {
		app.QueueUpdateDraw(func() {
			state.Apply(ev)
		})
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
// frame (this rebuild's shared spinner frame - see spinnerAt) renders as a
// fixed-cost " <frame>" suffix right after the title, reserved from
// availContent up front, before any of the shrink math below runs - it is
// never itself a shrink target, unlike the title or hostnames. When active
// is false, frame is ignored and two blank spaces render in its place
// instead - the same fixed width is reserved either way specifically so
// every row's hostnames shrink identically regardless of which task, if
// any, happens to be active; letting only the active row reserve it made
// that one row's hostnames truncate slightly more than the others'.
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

	// Fixed 2-cell width regardless of active: previously only the active
	// task's row reserved this, so its hostnames shrunk slightly more
	// aggressively than every other row's - reserving the same width
	// unconditionally (rendering blank spaces where the spinner would go,
	// on every other row) keeps every row's hostname layout identical.
	suffixText := "  "
	if active {
		suffixText = " " + string(frame)
	}
	availContent -= len([]rune(suffixText))
	if availContent < 0 {
		availContent = 0
	}

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
	// tview.Escape's own guaranteed-correct handling of "[...]"-shaped text
	// applies here too (list rows parse tags, unlike the top bar) - harmless
	// no-op on suffixText's plain spaces/spinner rune, neither of which is
	// ever "["-shaped.
	suffix := tview.Escape(suffixText)

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
			return taskIndent + "[" + pureBlack + ":lightgray:b]" + title + suffix + "[-:-:-]"
		}
		return taskIndent + "[silver::-]" + title + suffix + "[-::-]"
	}

	// The actual rendered gap is whatever's really left over once title and
	// (possibly-shrunk) hosts are sized - right-aligning the host list to
	// the row's far edge, same "variable padding, floored low" shape as the
	// old counts-based taskLabel's own padding math. suffixText's width is
	// deliberately not subtracted again here: availContent already had it
	// carved out at the top of this function, so it's already accounted
	// for (title + suffix + padding + hosts == availContent + suffixWidth,
	// same identity the original elapsed-suffix code relied on).
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
		b.WriteString(taskIndent)
		b.WriteString("[" + pureBlack + ":lightgray:b]")
		b.WriteString(title)
		b.WriteString(suffix)
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
	styledTitle := "[silver::-]" + title + suffix + "[-::-]"
	hostSegments := make([]string, len(allHosts))
	for i, h := range allHosts {
		o, done := task.Hosts[h]
		tag := grayTag
		if done {
			tag = colorTag(o)
		}
		hostSegments[i] = fmt.Sprintf("[%s]%s[-]", tag, tview.Escape(string(hostRunes[i])))
	}

	return taskIndent + styledTitle + strings.Repeat(" ", padding) + strings.Join(hostSegments, " ")
}

// hostLabel builds one host row's text, colored uniformly by its single
// outcome. No width-based truncation/alignment applies here - that rule is
// TASK-row-specific per TUI.md. selected mirrors taskLabel's own parameter -
// black bold text on the outcome color as a background, instead of the
// outcome color as a foreground.
func hostLabel(task *taskNode, host string, selected bool) string {
	o := task.Hosts[host]
	if selected {
		return fmt.Sprintf("[%s:%s:b]%s: %s[-:-:-]", pureBlack, colorTag(o), tview.Escape(host), o)
	}
	return fmt.Sprintf("[%s]%s: %s[-]", colorTag(o), tview.Escape(host), o)
}

// formatHostOutput renders task.Raw[host] for the output drill-down view.
// It decodes into a generic map (not a fixed struct) since different
// Ansible modules return wildly different result shapes; msg/stdout/stderr
// are pulled out as labeled, human-readable sections (real newlines, no
// escaping needed - the output TextView keeps style tags off) since those
// are by far the most commonly wanted fields for the common
// command/shell/script case, followed unconditionally by the complete
// result as pretty-printed JSON, which is what makes this work for any
// module type without having to special-case each one.
func formatHostOutput(task *taskNode, host string) string {
	raw := task.Raw[host]
	if len(raw) == 0 {
		// Shouldn't happen in normal operation - every host recorded via
		// recordHost always has some raw payload - but a live jsonl stream
		// from an external process isn't something to trust blindly, so
		// degrade gracefully rather than showing a blank screen.
		return fmt.Sprintf("(no output recorded for %s)", host)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// Not a JSON object - shouldn't happen for any real module
		// result, but show the raw bytes rather than nothing.
		return string(raw)
	}

	var b strings.Builder
	writeSection := func(label, key string) {
		s, ok := decoded[key].(string)
		if !ok || s == "" {
			return
		}
		fmt.Fprintf(&b, "%s:\n%s\n\n", label, s)
	}
	writeSection("MSG", "msg")
	writeSection("STDOUT", "stdout")
	writeSection("STDERR", "stderr")

	pretty, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		fmt.Fprintf(&b, "FULL RESULT: (failed to format: %v)\n%s", err, string(raw))
	} else {
		fmt.Fprintf(&b, "FULL RESULT:\n%s", pretty)
	}

	return b.String()
}
