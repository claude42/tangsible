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

package debugverb

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"time"

	"code.aw.net/claude/tangsible/internal/debugrun"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// breakpointPrefixWidth is the fixed 2-column slot rowText reserves in
// front of uikit.TaskLabel's own output for the breakpoint marker - a
// concept the main live tree has no equivalent of, so it can't live
// inside TaskLabel's own (shared, verb-agnostic) prefix slot. Subtracted
// from avail before computing the host-column layout/calling TaskLabel,
// so the two prefixes together still fit the real terminal width.
const breakpointPrefixWidth = 2

// debugRow is one row of the pre-run tree - either a play header or a
// task, built once from the --list-tasks --list-hosts skeleton
// (runner.ProgressEntry) and never reshaped afterward; only the backing
// *playbook.TaskNode's own Hosts map (and pausedHost) mutate as the run
// progresses. Backed by the real playbook.TaskNode/PlayNode structs - not
// a locally-invented shape - specifically so rowText can call straight
// into uikit.TaskLabel/PlayRowText/ComputeHostColumnLayout, the exact
// functions the main live tree calls, instead of re-deriving a smaller,
// divergent version of the same rendering (confirmed live to have
// drifted: wrong task-line color, no busy indicator, hostnames shown as
// bare dots, selected-row style bleeding into the prefix column - see
// design-docs/DebugMode.md's follow-up entry). A task row's own
// breakpoint state is deliberately NOT stored here - see taskLabel/
// debugApp.bps below - so there is exactly one source of truth for it
// (BreakpointSet), never a second copy that could go stale.
type debugRow struct {
	isPlayHeader bool
	play         *playbook.PlayNode // header row only
	task         *playbook.TaskNode // task row only
}

// taskLabel is a task row's own breakpoint-set key, matching the
// debugger's own prompt text exactly (internal/debugrun's promptRe) -
// design-docs/DebugMode.md's accepted limitation: two task rows sharing
// an exact name alias to the same breakpoint.
func taskLabel(taskName string) string {
	return "TASK: " + taskName
}

// debugApp holds every piece of mutable state the TUI's closures share -
// deliberately one small struct, not internal/session's liveSession (see
// debugverb.go's own package doc for why this is a separate, smaller
// construct rather than an extension of that one).
type debugApp struct {
	app      *tview.Application
	list     *uikit.TreeList
	help     *uikit.HelpOverlay
	bar      *tview.TextView
	pages    *tview.Pages
	varsView *tview.TextView

	copyDir, relPlaybook string
	rest                 []string

	rows            []*debugRow
	navRows         []uikit.Row             // parallel to rows; Selected nil on a header row, backs the Up/Down/j/k header-skip (uikit.NextInteractiveRow) - see handleRowSkip
	pbState         *playbook.PlaybookState // Plays built once in buildRows (never reshaped), purely so uikit.ComputeHostColumnLayout can measure the shared host column the same way the main tree does
	allHosts        []string                // run-wide, sorted, grows as hosts report - mirrors playbook.PlaybookState.AllHosts' own contract (recordHost)
	rowForSkeleton  []int                   // skeleton index -> rows index
	bps             *debugrun.BreakpointSet
	tracker         *runner.ProgressTracker
	currentPlayName string
	currentRowIdx   int // -1 until the first task-start event arrives
	pausedHost      string

	startedAt time.Time   // drives rowText's own busy-spinner frame (uikit.SpinnerAt), same as liveSession's own startedAt
	quitting  atomic.Bool // set just before app.Stop() - startHeartbeat's own guard against queueing into the 100-slot update queue after nothing drains it (CLAUDE.md)

	sess     *debugrun.Session
	driving  bool // a background Start/Continue/Query call is in flight
	finished bool
	varsOpen bool
}

// runDebugTUI builds and runs the debug verb's own TUI - blocks until the
// user quits. skeleton is the pre-run task list (runner.
// BuildProgressSkeleton, already probed against the spliced copy).
func runDebugTUI(copyDir, relPlaybook string, rest []string, skeleton []runner.ProgressEntry) {
	d := &debugApp{
		copyDir:       copyDir,
		relPlaybook:   relPlaybook,
		rest:          rest,
		bps:           debugrun.NewBreakpointSet(),
		tracker:       runner.NewProgressTracker(skeleton),
		currentRowIdx: -1,
	}
	d.buildRows(skeleton)

	d.app = tview.NewApplication()
	d.app.EnableMouse(true)

	header := tview.NewTextView().SetDynamicColors(true)
	header.SetTextStyle(uikit.BarStyle)
	header.SetText(" Debug: " + tview.Escape(relPlaybook) + " ")

	d.list = uikit.NewTreeList()
	// Wired before the first rebuildList() call below - without it,
	// confirmed live, moving the cursor updates TreeList's own
	// currentItem but nothing ever re-renders that row's text with the
	// selected style (there is no built-in highlight - see rowText's own
	// doc comment), so navigation looked like it did nothing at all.
	d.list.SetChangedFunc(func(index int) { d.rebuildList() })
	d.rebuildList()

	d.bar = tview.NewTextView().SetDynamicColors(true).SetText(debugBarIdle)
	d.bar.SetTextStyle(uikit.BarStyle)

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(d.list, 0, 1, true).
		AddItem(d.bar, 1, 0, false)

	d.varsView = tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	d.varsView.SetTextColor(tcell.GetColor(uikit.StandardTextColor))
	varsBar := tview.NewTextView().SetDynamicColors(true).SetText(debugBarVars)
	varsBar.SetTextStyle(uikit.BarStyle)
	varsFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(d.varsView, 0, 1, true).
		AddItem(varsBar, 1, 0, false)

	d.pages = tview.NewPages().
		AddPage("main", flex, true, true).
		AddPage("vars", varsFlex, true, false)

	d.help = uikit.NewHelpOverlay(d.app, d.pages)

	d.app.SetRoot(d.pages, true)
	d.app.SetInputCapture(d.handleKey)

	d.startedAt = time.Now()
	d.startHeartbeat()

	if err := d.app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
	}
	if d.sess != nil {
		_ = d.sess.Close()
	}
}

// buildRows turns the flat skeleton into rows, inserting a play header
// whenever the play changes - the skeleton itself has no separate notion
// of a play boundary beyond "this entry's own Play field differs from the
// previous one" (runner.ProgressEntry's own doc comment: --list-tasks
// already merges pre_tasks/roles/tasks/post_tasks into one ordered list
// per play). Also builds d.pbState (a real playbook.PlaybookState, Plays
// populated directly rather than via Apply - the skeleton's own shape is
// static, only a TaskNode's own Hosts map mutates later) and d.navRows,
// both purely so rowText/handleRowSkip can call the exact same
// uikit helpers the main live tree does (ComputeHostColumnLayout,
// NextInteractiveRow) rather than reimplementing their own versions.
func (d *debugApp) buildRows(skeleton []runner.ProgressEntry) {
	d.rowForSkeleton = make([]int, len(skeleton))
	d.pbState = &playbook.PlaybookState{}
	var currentPlay *playbook.PlayNode
	lastPlayName := ""
	first := true
	for i, e := range skeleton {
		if first || e.Play != lastPlayName {
			currentPlay = &playbook.PlayNode{Name: e.Play}
			d.pbState.Plays = append(d.pbState.Plays, currentPlay)
			d.rows = append(d.rows, &debugRow{isPlayHeader: true, play: currentPlay})
			d.navRows = append(d.navRows, uikit.Row{}) // Selected stays nil: a header row is never a cursor target (NextInteractiveRow)
			lastPlayName = e.Play
			first = false
		}
		task := &playbook.TaskNode{Name: e.Task, Hosts: map[string]playbook.Outcome{}}
		currentPlay.Tasks = append(currentPlay.Tasks, task)
		d.rows = append(d.rows, &debugRow{task: task})
		d.navRows = append(d.navRows, uikit.Row{Selected: func() {}}) // non-nil marks this row interactive; the func itself is never called - TreeList's own AddItem(text, nil) activation path is used instead, see rebuildList
		d.rowForSkeleton[i] = len(d.rows) - 1
	}
}

// recordHost inserts host into d.allHosts (kept sorted, deduplicated) the
// first time it reports anything for any task - mirrors
// playbook.PlaybookState.AllHosts' own "alphabetically-sorted, only ever
// grows" contract (aggregate.go), which uikit.TaskLabel/
// ComputeHostColumnLayout rely on for the shared host column every row
// aligns to.
func (d *debugApp) recordHost(host string) {
	i := sort.SearchStrings(d.allHosts, host)
	if i < len(d.allHosts) && d.allHosts[i] == host {
		return
	}
	d.allHosts = append(d.allHosts, "")
	copy(d.allHosts[i+1:], d.allHosts[i:])
	d.allHosts[i] = host
}

// startHeartbeat drives rowText's own busy-indicator spinner animation
// (uikit.SpinnerAt, fed through TaskLabel's frame parameter) and keeps the
// host-column layout reacting live to terminal resizes - mirrors
// internal/session's own startHeartbeat (livesession_heartbeat.go), same
// uikit.SpinnerInterval cadence, simplified: this view has no elapsed
// clock to freeze and no separate resize-only watcher, so one ticker just
// keeps running for the app's whole lifetime (redrawing an unchanged
// frozen screen every tick once finished is harmless) rather than
// stopping once finished the way the main tree's own ticker does.
func (d *debugApp) startHeartbeat() {
	go func() {
		ticker := time.NewTicker(uikit.SpinnerInterval)
		defer ticker.Stop()
		for range ticker.C {
			if d.quitting.Load() {
				return // mirrors liveSession.startHeartbeat's own guard -
				// tview's update queue is a fixed 100-slot buffer nothing
				// drains once app.Run() has returned.
			}
			d.app.QueueUpdateDraw(d.rebuildList)
		}
	}()
}

// rebuildList re-renders every row into d.list, preserving the current
// selection - the same "Clear then re-AddItem every row" shape tui.go's
// own rebuild() uses for the identical reason (TreeList has no per-row
// update, only whole-row AddItem). Also the sole place the host-column
// layout and spinner frame are computed - both read fresh on every call
// (not cached), the same "called from every heartbeat tick, not just once"
// reasoning liveSession's own chromeColorName/showElapsed etc. document.
func (d *debugApp) rebuildList() {
	selIdx := d.list.GetCurrentItem()
	if selIdx < 0 {
		// -1 only before the very first populate (TreeList's own
		// zero-value/post-Clear state) - AddItem below will default
		// currentItem to 0 regardless, so render a row as selected to
		// match rather than rendering no row as selected at all
		// (confirmed live: left as -1, the very first screen showed no
		// cursor highlight anywhere). Lands on the first interactive row,
		// skipping a leading play header, same as handleRowSkip does for
		// every later move - confirmed live: defaulting to literal row 0
		// put the very first screen's cursor on the play header, the
		// exact bug handleRowSkip exists to prevent for ordinary
		// navigation.
		if first := uikit.NextInteractiveRow(d.navRows, -1, 1); first != -1 {
			selIdx = first
		} else {
			selIdx = 0
		}
	}
	_, _, avail, _ := d.list.GetInnerRect()
	taskAvail := avail - breakpointPrefixWidth
	if taskAvail < 0 {
		taskAvail = 0
	}
	layout := uikit.ComputeHostColumnLayout(d.pbState, d.allHosts, taskAvail, false)
	frame := uikit.SpinnerAt(time.Since(d.startedAt))

	d.list.Clear()
	for i := range d.rows {
		ii := i
		d.list.AddItem(d.rowText(ii, ii == selIdx, taskAvail, layout, frame), nil)
	}
	if selIdx >= 0 && selIdx < len(d.rows) {
		d.list.RestoreCurrentItem(selIdx)
	}
}

// rowText renders one row. TreeList (unlike tview.List) has no built-in
// per-row highlight at all (confirmed live - without this, there was no
// visual cursor whatsoever) - tui.go's own rebuild() re-renders whichever
// row is currently selected with a uniform selected-style variant before
// ever calling AddItem, "the entire highlighting mechanism"; uikit.
// TaskLabel/PlayRowText already implement exactly that (and everything
// else a row needs: hostname-shrinking, the active-task spinner, the
// right task-line color), so this calls straight into them rather than
// re-deriving a smaller version - confirmed live to have drifted the
// first time around (design-docs/DebugMode.md's follow-up entry): wrong
// task-line color (StandardTextColor instead of TaskTitleColor), no busy
// indicator at all, hostnames shown as bare dots instead of names, and
// the selected-row background wrongly swallowing the prefix column along
// with the title.
//
// The breakpoint marker (and, while paused, the "(paused: host)"
// annotation) are debug mode's own concepts with no equivalent in
// TaskLabel - composed here as their own segments, each in its own color
// tag, outside whatever TaskLabel itself returns, so neither one ever
// inherits the selected-row's gray background the way the old
// reimplementation's prefix did (TaskLabel's own prefix slot - the
// spinner/indent - already keeps itself out of that background; see its
// own doc comment).
func (d *debugApp) rowText(idx int, selected bool, avail int, layout uikit.HostColumnLayout, frame rune) string {
	r := d.rows[idx]
	if r.isPlayHeader {
		return uikit.PlayRowText(r.play, selected)
	}

	armed := d.bps.Has(taskLabel(r.task.Name))
	active := idx == d.currentRowIdx && d.pausedHost == "" && !d.finished
	paused := idx == d.currentRowIdx && d.pausedHost != ""

	var bp string
	if armed {
		bp = "[" + uikit.ColorTag(playbook.OutcomeFailed) + "]●[-] "
	} else {
		bp = "  "
	}

	label := uikit.TaskLabel(r.task, d.allHosts, layout, avail, active, frame, selected, true)

	var suffix string
	if paused {
		suffix = " [" + uikit.AnnotationColor + "](paused: " + tview.Escape(d.pausedHost) + ")[-]"
	}

	return bp + label + suffix
}

func (d *debugApp) setBar(text string) {
	d.bar.SetText(text)
}

// statusBarForState picks the bottom bar text matching the session's
// current phase - idle (never started), running (a driving call in
// flight), paused (surfaced, waiting for a command) or finished.
func (d *debugApp) statusBarForState() string {
	switch {
	case d.finished:
		return debugBarFinished
	case d.driving:
		return debugBarRunning
	case d.sess != nil && d.pausedHost != "":
		return debugBarPaused
	default:
		return debugBarIdle
	}
}

// toggleBreakpoint arms/disarms the currently selected task row. A no-op
// on a play header row - there is nothing to arm there.
func (d *debugApp) toggleBreakpoint() {
	idx := d.list.GetCurrentItem()
	if idx < 0 || idx >= len(d.rows) || d.rows[idx].isPlayHeader {
		return
	}
	label := taskLabel(d.rows[idx].task.Name)
	if d.bps.Has(label) {
		d.bps.Clear(label)
	} else {
		d.bps.Set(label)
	}
	d.rebuildList()
}

// startOrContinue drives one Start/Continue call in the background
// (debugrun.Session's own calls block on real process I/O) and reports
// back through d.app.QueueUpdateDraw - the "kick off eagerly, update via
// QueueUpdateDraw once done" pattern internal/host's own fetch() already
// uses, which is what keeps every mutation of d's own fields confined to
// tview's single event-loop goroutine (CLAUDE.md's "one event-loop
// goroutine" invariant) even though the actual I/O happens elsewhere.
func (d *debugApp) startOrContinue(mode debugrun.StepMode) {
	if d.driving || d.finished {
		return
	}
	d.driving = true
	d.setBar(d.statusBarForState())

	if d.sess == nil {
		go func() {
			sess, err := debugrun.Spawn(d.copyDir, d.relPlaybook, d.rest)
			if err != nil {
				d.app.QueueUpdateDraw(func() {
					d.driving = false
					d.finished = true
					d.setBar(" [" + uikit.AsyncErrorColor + "]failed to start: " + tview.Escape(err.Error()) + "[-]  q: quit ")
				})
				return
			}
			go d.drainEvents(sess)
			pause, eof, perr := debugrun.RunWithBreakpoints(sess, d.bps, mode)
			d.app.QueueUpdateDraw(func() {
				d.sess = sess
				d.applyPauseResult(pause, eof, perr)
			})
		}()
		return
	}

	sess := d.sess
	go func() {
		pause, eof, perr := debugrun.ContinueWithBreakpoints(sess, d.bps, mode)
		d.app.QueueUpdateDraw(func() {
			d.applyPauseResult(pause, eof, perr)
		})
	}()
}

// applyPauseResult runs on the event-loop goroutine (always called from
// inside a QueueUpdateDraw callback) - see startOrContinue's own doc
// comment.
func (d *debugApp) applyPauseResult(pause debugrun.Pause, eof bool, err error) {
	d.driving = false
	if err != nil {
		d.finished = true
		d.setBar(" [" + uikit.AsyncErrorColor + "]" + tview.Escape(err.Error()) + "[-]  q: quit ")
		return
	}
	if eof {
		d.finished = true
		d.pausedHost = ""
		if d.sess != nil {
			_ = d.sess.Close()
		}
		d.setBar(d.statusBarForState())
		d.rebuildList()
		return
	}
	d.pausedHost = pause.Host
	d.setBar(d.statusBarForState())
	d.rebuildList()
}

// drainEvents feeds the session's own jsonl stream (the same mechanism a
// normal live run uses, runner.ScanEvents - see debugverb.go's own
// package doc comment) into the tree, for the session's whole lifetime.
// Runs on its own goroutine; every mutation it causes goes through
// QueueUpdateDraw, same discipline as startOrContinue.
func (d *debugApp) drainEvents(sess *debugrun.Session) {
	for item := range sess.Events {
		if !item.IsEvent {
			continue
		}
		ev := item.Ev
		d.app.QueueUpdateDraw(func() {
			d.applyEvent(ev)
		})
	}
	// The event pipe only ever closes once the child itself has exited -
	// its write end (fd 3) closes with the process - independently of
	// whether anything is currently blocked reading the REPL side. Needed
	// for a real, confirmed-live gap: Ctrl-C while genuinely paused (no
	// Start/Continue/Query in flight) kills the child, but nothing was
	// blocked on its pipes to notice - without this, the bar kept showing
	// a stale "paused" prompt, and q/Ctrl-C alone (which only re-signals
	// an already-dead process while !finished) could never actually quit
	// until some other key happened to issue a doomed Continue/Query
	// first and discover the eof. Idempotent with applyPauseResult's own
	// eof handling - whichever reaches the event-loop goroutine first
	// wins, Session.Close is documented safe to call more than once.
	d.app.QueueUpdateDraw(func() {
		if !d.finished {
			d.finished = true
			d.pausedHost = ""
			_ = sess.Close()
			d.setBar(d.statusBarForState())
			d.rebuildList()
		}
	})
}

// applyEvent resolves a live jsonl event's (play, task) against the
// static skeleton via d.tracker - the exact forward-matching algorithm
// the live run's own progress indicator already uses, for the same
// reason (a task name is not unique across a real playbook - see
// runner.ProgressTracker's own doc comment). currentPlayName is tracked
// separately, the same way aggregate.go's PlaybookState.Apply does for
// its own currentPlay (confirmed by reading it first): a task-start
// event's own ev.Play is not reliable for this, only a play-start
// event's is.
func (d *debugApp) applyEvent(ev playbook.RawEvent) {
	switch ev.Event {
	case "v2_playbook_on_play_start":
		if ev.Play != nil {
			d.currentPlayName = ev.Play.Name
			d.tracker.AdvanceToPlay(ev.Play.Name)
		}
	case "v2_playbook_on_task_start", "v2_playbook_on_handler_task_start":
		if ev.Task != nil {
			d.tracker.Advance(d.currentPlayName, ev.Task.Name, ev.Task.IsHandler)
			if pos, _ := d.tracker.Position(); pos > 0 {
				newIdx := d.rowForSkeleton[pos-1]
				if newIdx != d.currentRowIdx {
					d.currentRowIdx = newIdx
					d.pausedHost = ""
				}
			}
		}
	case "v2_runner_on_ok", "v2_runner_on_changed", "v2_runner_on_skipped", "v2_runner_on_failed", "v2_runner_on_unreachable":
		if d.currentRowIdx >= 0 {
			task := d.rows[d.currentRowIdx].task
			for host, raw := range ev.Hosts {
				task.Hosts[host] = outcomeFor(ev.Event, raw)
				d.recordHost(host)
			}
		}
	}
	d.rebuildList()
}

// outcomeFor classifies one host's raw result the same way aggregate.go's
// PlaybookState.Apply does (confirmed by reading it first, not guessed):
// "v2_runner_on_ok" splits into OK/Changed via the result's own "changed"
// flag; every other terminal event name maps 1:1 to its own Outcome.
func outcomeFor(eventName string, raw json.RawMessage) playbook.Outcome {
	switch eventName {
	case "v2_runner_on_skipped":
		return playbook.OutcomeSkipped
	case "v2_runner_on_failed":
		return playbook.OutcomeFailed
	case "v2_runner_on_unreachable":
		return playbook.OutcomeUnreachable
	default: // v2_runner_on_ok
		if playbook.DecodeHostResult(raw).Changed {
			return playbook.OutcomeChanged
		}
		return playbook.OutcomeOK
	}
}

// openVars queries the currently paused task's own full task_vars (the
// pre-task snapshot - design-docs/DebugMode.md's own confirmed
// limitation) and shows it as indented JSON. A no-op unless genuinely
// paused and no other call is in flight - Session's REPL is one
// synchronous channel; a concurrent Query while Continue is in flight
// would corrupt it, so this reuses d.driving as the same single-flight
// guard startOrContinue already relies on.
func (d *debugApp) openVars() {
	if d.driving || d.finished || d.sess == nil || d.pausedHost == "" {
		return
	}
	d.driving = true
	d.setBar(d.statusBarForState())
	sess := d.sess
	go func() {
		raw, err := sess.Query("task_vars")
		d.app.QueueUpdateDraw(func() {
			d.driving = false
			d.setBar(d.statusBarForState())
			if err != nil {
				d.varsView.SetText("[" + uikit.AsyncErrorColor + "]" + tview.Escape(err.Error()) + "[-]")
			} else {
				var v any
				_ = json.Unmarshal(raw, &v)
				pretty, _ := json.MarshalIndent(v, "", "  ")
				d.varsView.SetText(tview.Escape(string(pretty)))
			}
			d.varsView.ScrollToBeginning()
			d.varsOpen = true
			d.pages.SwitchToPage("vars")
		})
	}()
}

func (d *debugApp) closeVars() {
	d.varsOpen = false
	d.pages.SwitchToPage("main")
	d.app.SetFocus(d.list)
}

// handleRowSkip: Up/Down/j/k skip straight over play-header rows (which
// aren't a cursor target at all, see buildRows' own navRows) instead of
// landing on one and requiring a second keypress to move past it -
// mirrors internal/session's own handleTreeRowSkip (livesession_input.go)
// exactly, down to using the same uikit.NextInteractiveRow and calling
// SetCurrentItem (not RestoreCurrentItem) so the real selection change
// fires d.list's own SetChangedFunc. Checked before uikit.TranslateNavAlias
// so j/k get this treatment instead of being translated to plain Down/Up
// first and falling through to TreeList's native (header-row-unaware)
// one-row-at-a-time handling - same ordering reasoning as
// handleTreeRowSkip's own doc comment.
func (d *debugApp) handleRowSkip(event *tcell.EventKey) bool {
	var delta int
	switch {
	case event.Key() == tcell.KeyUp, event.Key() == tcell.KeyRune && event.Rune() == 'k':
		delta = -1
	case event.Key() == tcell.KeyDown, event.Key() == tcell.KeyRune && event.Rune() == 'j':
		delta = 1
	default:
		return false
	}
	if next := uikit.NextInteractiveRow(d.navRows, d.list.GetCurrentItem(), delta); next != -1 {
		d.list.SetCurrentItem(next)
	}
	return true
}

func (d *debugApp) handleKey(event *tcell.EventKey) *tcell.EventKey {
	if d.help.HandleKey(event) {
		return nil
	}
	if d.varsOpen {
		if event.Key() == tcell.KeyEscape || event.Rune() == 'v' {
			d.closeVars()
			return nil
		}
		return event // let varsView handle its own scrolling
	}
	if d.handleRowSkip(event) {
		return nil
	}
	if translated, ok := uikit.TranslateNavAlias(event); ok {
		return translated
	}
	switch {
	case uikit.IsHelpKey(event):
		d.help.Open(debugHelp, nil)
		return nil
	case event.Key() == tcell.KeyCtrlC, event.Rune() == 'q':
		if d.sess == nil || d.finished {
			d.quitting.Store(true)
			d.app.Stop()
		} else {
			_ = d.sess.Signal(os.Interrupt) // best-effort; child may race-exit
		}
		return nil
	case event.Rune() == 't':
		d.toggleBreakpoint()
		return nil
	case event.Key() == tcell.KeyEnter:
		d.startOrContinue(debugrun.RunTillBreakpoint)
		return nil
	case event.Rune() == 's':
		d.startOrContinue(debugrun.StepOne)
		return nil
	case event.Rune() == 'v':
		d.openVars()
		return nil
	}
	return event
}
