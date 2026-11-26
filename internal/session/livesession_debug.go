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
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/debugrun"
	"code.aw.net/claude/tangsible/internal/debugverb"
	"code.aw.net/claude/tangsible/internal/origin"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/source"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Debug mode (design-docs/DebugMode.md's "Stage 2"): a liveSession with
// debugMode true gets new keys (F5/F9/F10/F11/F12/v - design-docs/
// DebugMode.md's "Stage 1" [keys] config section isn't wired yet) and one
// background orchestration goroutine, parallel to runner.RunOneGeneration
// rather than a modification of it - everything else (the tree, two-pane,
// the drill-down, filter/search, recap) is the exact same liveSession
// machinery every other Verb already uses. Ctrl-C is too (handleCtrlC,
// livesession_input.go, completely unmodified for debug mode); q isn't
// quite, while F5/F10/F11's own drive loop is actually in flight - see
// debugConsumeStopRequest/handleGlobalQuit's own doc comments and design-
// docs/DebugMode.md's "q's own soft stop while actively driving".

// debugArmed reports whether t has an armed breakpoint - FlattenRows' own
// armed parameter, and the "selected row re-render"'s own marker
// computation (livesession_rebuild.go). Safe to call for every Verb, not
// just "debug": false whenever debugMode is false, so a plain run's own
// rendering is completely unaffected.
func (s *liveSession) debugArmed(t *playbook.TaskNode) bool {
	return s.debugMode && s.debugBPS.Has(debugverb.TaskLabel(t.Name, t.IsHandler))
}

// debugIsPaused reports whether (t, host) is the exact pause the
// debugger is currently sitting at - FlattenRows' own isPaused parameter,
// and the "selected row re-render"'s own marker computation
// (livesession_rebuild.go). Safe to call for every Verb, same reasoning
// as debugArmed.
func (s *liveSession) debugIsPaused(t *playbook.TaskNode, host string) bool {
	return s.debugMode && s.debugPausedHost == host && s.debugCursor != nil && s.debugCursor.CurrentTask == t
}

// debugChromeStyle picks the bottom bar's own style for the session's
// current phase (design-docs/DebugMode.md's "Stage 1 decisions") -
// mirrors chromeColorName's own switch (livesession_rebuild.go), but as
// a tcell.Style for SetTextStyle: the bottom bar's own text
// (currentMainBottomBarText) is plain, untagged text, unlike the top
// bar's own progress-fill, which paints its own color tags directly and
// so is already dynamic for free (chromeColorName's own doc comment).
func (s *liveSession) debugChromeStyle() tcell.Style {
	switch {
	case s.debugDriving:
		return uikit.DebugRunningBarStyle
	case s.debugPausedHost != "":
		return uikit.DebugPausedBarStyle
	default:
		return s.liveChromeStyle
	}
}

// debugChromeBg is debugChromeStyle's own tcell.Color equivalent, for
// s.splitDivider.SetBackgroundColor (a plain Box, no tag-based fill to
// fall back on the way the top/split-header bars have). Unlike
// s.liveChromeBg/s.chromeBg (tui.go) - resolved once at construction,
// since chrome_live/revisit/check never change for a session's own
// lifetime - debugDriving/debugPausedHost flip repeatedly within one
// session, so this reads uikit.ChromeDebugRunningColor/
// ChromeDebugPausedColor fresh via tcell.GetColor on every call: a
// config override (or a live edit of the vars themselves, as happened
// here) must reach the divider exactly as it reaches debugChromeStyle's
// own uikit.DebugRunningBarStyle/DebugPausedBarStyle.
func (s *liveSession) debugChromeBg() tcell.Color {
	switch {
	case s.debugDriving:
		return tcell.GetColor(uikit.ChromeDebugRunningColor)
	case s.debugPausedHost != "":
		return tcell.GetColor(uikit.ChromeDebugPausedColor)
	default:
		return s.liveChromeBg
	}
}

// debugRefreshChrome updates every chrome-styled widget for the
// session's current phase, together - called from every debug state
// transition rather than left to a per-rebuild refresh, because there
// isn't one for most of these (unlike the top/split-header bars' own
// progress-fill, already dynamic for free via chromeColorName's own
// tags - see its doc comment): s.bottomBar's own text/style, and
// s.outputBottomBar/s.outputTabs/s.splitDivider, the drill-down's own
// three remaining pieces of chrome that were previously only ever set
// once, at construction, using s.chromeStyle's own static snapshot -
// confirmed live as a real, reported bug, the drill-down staying plain
// navy throughout a debug session regardless of whether it was actually
// running or paused.
func (s *liveSession) debugRefreshChrome() {
	s.bottomBar.SetText(s.currentMainBottomBarText())
	style := s.debugChromeStyle()
	s.bottomBar.SetTextStyle(style)
	s.outputBottomBar.SetTextStyle(style)
	s.outputTabs.SetHeaderStyle(style, s.chromeColorName())
	s.splitDivider.SetBackgroundColor(s.debugChromeBg())
}

// debugBottomBarHint is appended to the tree's own bottom bar
// (currentMainBottomBarText) whenever debugMode is true - the F5/F9/F10/
// F11/F12 keys have no existing tree binding to replace, so they're purely
// additive, and the wording changes with the session's own current phase
// (mirroring a previous, separate debug TUI's own debugBar* constants).
func (s *liveSession) debugBottomBarHint() string {
	switch {
	case s.debugRestarting:
		return " restarting... "
	case s.processDone.Load() && s.everStarted:
		return " finished - r: restart  F9: breakpoint "
	case s.debugPausedHost != "":
		return " F5: continue  F9: breakpoint  F10: next task  F11: step  F12: redo  r: restart "
	case s.debugLoopDriving && s.debugStopPending.Load():
		return " stopping after this task... (ctrl-c to interrupt now) "
	case s.debugLoopDriving:
		return " running...  F6: stop after this task  ctrl-c: interrupt "
	case s.debugDriving:
		return " running... (ctrl-c to interrupt) "
	default:
		return " F5: run  F9: breakpoint  F10: next task  F11: step "
	}
}

// handleDebugKey is handleKey's own debug-specific guard (livesession_
// input.go), checked right after handleFilterDialogKey and before the
// s.viewingStdout branch - early enough that none of the three dialogs
// can have their own keys stolen, but before the tree/drill-down fork, so
// these keys work identically whichever one is frontmost. A no-op
// (returns handled=false) for every Verb but "debug" - tcell.KeyF5/F9/
// F10/F11/F12 are unclaimed anywhere else in this codebase (confirmed by
// grep before adopting them), so this never shadows an existing binding.
func (s *liveSession) handleDebugKey(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.debugMode || s.viewingStats {
		return nil, false
	}
	switch event.Key() {
	case tcell.KeyF5:
		s.debugStartOrContinue(debugrun.RunTillBreakpoint)
		return nil, true
	case tcell.KeyF10:
		// Reported live: StepOne's own "every pause, armed or not"
		// stepped through every remaining host of the *current* task one
		// at a time, which was tedious when only the next task actually
		// mattered - F10 moved here to mean "skip the rest of this
		// task's hosts, stop at the next task's first one" instead
		// (debugrun.StepNextTask), bumping the old F10 behavior to F11.
		s.debugStartOrContinue(debugrun.StepNextTask)
		return nil, true
	case tcell.KeyF11:
		s.debugStartOrContinue(debugrun.StepOne)
		return nil, true
	case tcell.KeyF9:
		s.debugToggleBreakpoint()
		return nil, true
	case tcell.KeyF12:
		s.debugRedo()
		return nil, true
	case tcell.KeyF6:
		s.debugSoftStop()
		return nil, true
	}
	return nil, false
}

// debugSoftStop is F6, and what q does while the run is being driven: no
// signal reaches the real child; the current task finishes and the next pause
// is shown instead of being continued past (one-shot, consumed by
// debugConsumeStopRequest), so the session is in the debugger right there.
// A no-op unless an F5/F10/F11 drive loop is in flight - Ctrl-C stays the
// way to interrupt a stuck task, since a soft stop only takes effect at the
// next task boundary. Scoped to debugLoopDriving, not debugDriving: see that
// field's doc comment (a slow F12 redo or watch-set must stay interruptible).
func (s *liveSession) debugSoftStop() {
	if !s.debugLoopDriving {
		return
	}
	s.debugStopPending.Store(true)
	s.debugRefreshChrome()
}

// debugToggleBreakpoint arms/disarms a breakpoint on s.currentID - a
// no-op unless the cursor is currently on a task row (currentID holds
// many other ID shapes: a *playbook.PlayNode, a uikit.HostRowID, a recap
// row, ...). Mirrors a previous, separate debug TUI's own
// toggleBreakpoint, using currentID (identity, this package's own
// convention - CLAUDE.md's "Selection is tracked by row identity, not
// index") instead of a row index. F9's own target - the mouse-click
// equivalent (handleTreeMouse/handleOutputViewMouse) resolves its own
// task directly instead, since a click can land on any row, not just
// the cursor's.
func (s *liveSession) debugToggleBreakpoint() {
	s.debugToggleBreakpointFor(s.debugCursorTask())
}

// debugToggleBreakpointFor is debugToggleBreakpoint's own shared body,
// parameterized on which task to toggle - a no-op for nil.
func (s *liveSession) debugToggleBreakpointFor(task *playbook.TaskNode) {
	if task == nil {
		return
	}
	label := debugverb.TaskLabel(task.Name, task.IsHandler)
	if s.debugBPS.Has(label) {
		s.debugBPS.Clear(label)
	} else {
		s.debugBPS.Set(label)
	}
	s.rebuild()
}

// debugTaskAtFirstColumn reports the task row at screen position (x, y) -
// but only when x is exactly s.list's own leftmost column, where a task
// row's prefix slot always sits (uikit.TaskIndent/BreakpointMarkerText,
// tui_layout.go - both exactly one slot wide, whichever of the two is
// currently showing). nil/false for every other column, a non-task row
// at that position (a play header, a host row), or a non-debug session.
// The mouse-click equivalent of F9: requested live, so a breakpoint can
// be armed/disarmed with a click, without moving the cursor there first.
// index is the row's own index into s.currentRows (and so into s.list),
// handed back so a caller can move the cursor there afterward without a
// second lookup.
func (s *liveSession) debugTaskAtFirstColumn(x, y int) (task *playbook.TaskNode, index int, ok bool) {
	if !s.debugMode {
		return nil, 0, false
	}
	rectX, rectY, _, height := s.list.GetInnerRect()
	if x != rectX || y < rectY || y >= rectY+height {
		return nil, 0, false
	}
	i := s.list.GetOffset() + (y - rectY)
	if i < 0 || i >= len(s.currentRows) {
		return nil, 0, false
	}
	t, isTask := s.currentRows[i].ID.(*playbook.TaskNode)
	if !isTask {
		return nil, 0, false
	}
	return t, i, true
}

// debugCursorTask resolves "the task row the cursor is logically on
// right now" - F9's own target. s.currentID alone isn't enough while
// s.following is still true: it's only ever set by a genuine SetCurrentItem
// call made from *outside* rebuild() (TreeList's own SetChangedFunc bails
// out whenever s.rebuilding is true, guarding against a recursive
// rebuild - CLAUDE.md's "one event-loop goroutine"), but debug mode's own
// following-pinned selection (rebuild's own debug-aware selectedIndex
// branch) is applied *from inside* rebuild() itself - so s.currentID
// never actually gets synced to it, and F9 pressed before ever manually
// navigating silently armed nothing at all. Confirmed by checking the
// live tree for the marker afterward, not assumed: pressing F9
// immediately on open, then F5, ran the whole playbook with no pause at
// all. Mirrors rebuild()'s own resolution instead of trusting currentID
// whenever still following: debugCursor.CurrentTask once something has
// started, the first task row before that - the exact same two cases
// rebuild() itself already picks between.
func (s *liveSession) debugCursorTask() *playbook.TaskNode {
	if s.following {
		if s.debugCursor != nil && s.debugCursor.CurrentTask != nil {
			return s.debugCursor.CurrentTask
		}
		for _, r := range s.currentRows {
			if t, ok := r.ID.(*playbook.TaskNode); ok {
				return t
			}
		}
		return nil
	}
	t, _ := s.currentID.(*playbook.TaskNode)
	return t
}

// debugStartOrContinue drives one Start/Continue/Step call in the
// background (debugrun.Session's own calls block on real process I/O)
// and reports back through s.app.QueueUpdateDraw - mirrors a previous,
// separate debug TUI's own startOrContinue exactly, down to the one real
// trick it already had to learn the hard way: s.debugSess is published
// via QueueUpdateDraw *immediately* once debugrun.Spawn succeeds, before
// the blocking RunWithBreakpoints call that follows it - not deferred
// until that whole call returns. An earlier version of that previous TUI
// deferred it, which left s.debugSess nil (so q/Ctrl-C force-quit the
// whole process outright) for as long as the first Start call's own
// blocking duration - often several real task durations for a run with
// no armed breakpoints - even though the real child was already alive
// and unsignaled. s.procH is stored the same way, right alongside it, so
// q/Ctrl-C's own existing handleCtrlC/handleGlobalQuit code (unchanged,
// no debug-specific branch needed there at all) can reach it exactly as
// it already does for a plain run.
func (s *liveSession) debugStartOrContinue(mode debugrun.StepMode) {
	if s.debugDriving || s.debugRestarting || (s.processDone.Load() && s.everStarted) {
		return
	}
	s.debugDriving = true
	s.debugLoopDriving = true
	// Defensive, not load-bearing: applyPolicy already consumes a pending
	// stop request the instant it's observed (debugConsumeStopRequest's
	// own CompareAndSwap), so this only guards against a stale true
	// somehow surviving into an unrelated new drive.
	s.debugStopPending.Store(false)
	// processDone starts forced true for debug's own idle state (main.go,
	// NewLiveTUI's pendingSpawned doc comment - "safe to quit outright,
	// nothing to interrupt") and is never otherwise touched until
	// debugFinish/the error path below set it true again for real - so
	// without this, q/Ctrl-C's own handleGlobalQuit/handleCtrlC (which
	// read processDone directly, not s.everStarted) would still see it as
	// true the entire time a real child is alive and running, and quit
	// outright on the very first q instead of interrupting it. Confirmed
	// live: without this line, q while genuinely mid-task-run (not yet
	// paused) force-quit tangsible on one press, child still running.
	s.processDone.Store(false)
	if s.debugSess == nil {
		// The heartbeat ticker NewLiveTUI started already returned on its
		// first tick (processDone was true then), so nothing redraws the
		// busy spinner between events until it is started again - same as
		// submitRerun does for a rerun. It then keeps running across
		// pauses (processDone stays false) until debugFinish.
		s.startHeartbeat()
	}
	s.debugRefreshChrome()

	if s.debugSess == nil {
		go func() {
			sess, err := debugrun.Spawn(s.debugCopyPlaybookPath, s.passthroughArgs)
			if err != nil {
				s.app.QueueUpdateDraw(func() {
					s.debugDriving = false
					s.debugLoopDriving = false
					s.everStarted = true
					s.processDone.Store(true)
					s.exitCode.Store(-1)
					errText := []string{"couldn't start the debug session: " + err.Error()}
					s.lastStderr.Store(&errText)
					s.debugRefreshChrome()
					s.rebuild()
				})
				return
			}
			s.procH.Store(sess.Process())
			s.app.QueueUpdateDraw(func() { s.debugSess = sess })
			go s.debugDrainEvents(sess)
			pause, eof, perr := debugrun.RunWithBreakpoints(sess, s.debugBPS, mode, s.debugConsumeStopRequest, s.debugOnPause(sess))
			s.app.QueueUpdateDraw(func() {
				s.debugApplyPauseResult(pause, eof, perr)
			})
		}()
		return
	}

	sess := s.debugSess
	fromTask := s.debugLastPauseTask
	go func() {
		pause, eof, perr := debugrun.ContinueWithBreakpoints(sess, s.debugBPS, mode, fromTask, s.debugConsumeStopRequest, s.debugOnPause(sess))
		s.app.QueueUpdateDraw(func() {
			s.debugApplyPauseResult(pause, eof, perr)
		})
	}()
}

// debugConsumeStopRequest is applyPolicy's own stopRequested parameter
// (internal/debugrun/breakpoints.go) for the F5/F10/F11 drive above - q's
// own soft-stop request (handleGlobalQuit, livesession_input.go). A
// check-and-clear via CompareAndSwap, not a plain Load: this is what
// makes applyPolicy's own "consumed the instant it's observed true"
// contract real, and what makes it safe for debugStopPending to need no
// mutex (CLAUDE.md's "Atomics only, no mutex") despite being written from
// the event-loop goroutine and read here, repeatedly, on
// debugStartOrContinue's own background one.
func (s *liveSession) debugConsumeStopRequest() bool {
	return s.debugStopPending.CompareAndSwap(true, false)
}

// debugRedo drives one redo-and-wait call in the background (F12) -
// re-running the exact task/host the debugger is currently paused at,
// and showing nothing else until that exact result is back. A no-op
// unless genuinely paused (s.debugPausedHost != "") - unlike F9, which
// operates on the cursor's own row regardless of pause state, redo only
// means something relative to the pause itself, so there's nothing to
// redo before F5 has ever been pressed or after the session has already
// finished (debugSess == nil covers both). s.debugDriving (already set
// for the goroutine's whole duration, same as every other debug key)
// already guards against a second F12/F5/F10/F11 press interfering while
// this runs - nothing extra needed for that.
//
// Calls debugrun.RedoUntilTarget, not a plain Redo - design-docs/
// DebugMode.md's "redo" follow-up (rounds 9-11): Session.Redo's own next
// pause is not reliably a re-pause of the redone host at all (confirmed
// live, internal/debugrun's TestSession_Redo_MultiHostInterleaving) - a
// still-pending different host sharing the same task can surface first.
// Two earlier rounds tried surfacing that interleaved pause plainly and
// separately guarding a second F12 press - technically correct, but
// reported live, twice, as still-confusing behavior: the tree visibly
// jumped to a host the user never asked to look at, mid-redo.
// RedoUntilTarget instead drains any such interleaved pause silently in
// the same background call (see its own doc comment, debugrun/
// breakpoints.go) - debugApplyPauseResult, reused completely unchanged,
// only ever sees the one pause actually worth showing: a genuine
// re-pause of the redone task/host, or - rare - a pause for a *different*
// task if the target was somehow never seen again.
func (s *liveSession) debugRedo() {
	if s.debugDriving || s.debugRestarting || s.debugSess == nil || s.debugPausedHost == "" {
		return
	}
	targetTask, targetHost := s.debugLastPauseTask, s.debugPausedHost
	s.debugDriving = true
	s.debugRefreshChrome()

	sess := s.debugSess
	go func() {
		pause, eof, perr := debugrun.RedoUntilTarget(sess, targetTask, targetHost, s.debugOnPause(sess))
		s.app.QueueUpdateDraw(func() {
			s.debugApplyPauseResult(pause, eof, perr)
		})
	}()
}

// debugOnPause returns applyPolicy's own onPause callback (debugrun/
// breakpoints.go) for sess: queries task_vars for *every* pause it's
// given - including one silently continued past under
// RunTillBreakpoint, never otherwise surfaced to this package at all -
// design-docs/DebugMode.md's "Resolved Variables tab, every task." Runs
// on debugStartOrContinue's own background goroutine (the same one
// blocked inside RunWithBreakpoints/ContinueWithBreakpoints calling
// this), so Query's blocking pipe round-trip never touches the
// event-loop goroutine at all; only the cache write below is
// QueueUpdateDraw-wrapped.
//
// Every pause is queried, whether or not it will be surfaced (the
// Resolved tab then works for any task you navigate to): the query is
// cheap now that readUntilPrompt only matches at a possible prompt end
// (~90ms for a gather_facts-sized task_vars; it was ~0.75s, and was why
// this used to be opt-in via --all-vars).
//
// Keyed by (pause.Task, pause.Host) - the debugger's own task label and
// host, not resolveKey{*playbook.TaskNode, string} - deliberately: this
// fires on the REPL-driving goroutine, in lockstep with the REPL's own
// pause sequence, with no reliable way to correlate a given pause
// against debugCursor.CurrentTask, which is updated by an entirely
// separate, independently-racing jsonl event stream (the exact same
// race debugPendingOpenTask/Host already exists to cope with, for one
// pause at a time - doing that reliably for every pause in an unbroken,
// auto-continued run-to-completion sequence would need a comparably
// complex buffering mechanism of its own). This accepts the same label-
// based ambiguity breakpoints themselves already do (debugverb.
// TaskLabel's own doc comment: two tasks sharing an exact name alias to
// the same breakpoint) - a known, pre-existing limitation, not a new one.
func (s *liveSession) debugOnPause(sess *debugrun.Session) func(debugrun.Pause, bool) {
	return func(pause debugrun.Pause, _ bool) {
		raw, meta, ok := s.debugCaptureSnapshot(sess)
		if !ok {
			return
		}
		s.app.QueueUpdateDraw(func() {
			key := debugVarsKey{pause.Task, pause.Host}
			s.debugTaskVarsCache[key] = raw
			s.debugVaultMaskCache[key] = meta.mask
			s.debugOriginCache[key] = meta.origins
		})
	}
}

// debugCaptureSnapshot runs the task_vars capture sequence (Query of
// the callback plugin's tangsible_snapshot + mergeRegisteredResult +
// debugUnfakeShadowPaths) - factored out of debugOnPause so
// debugSetWatch's own post-assignment refresh (design-docs/DebugMode.md's
// "Stage 5") can reuse it exactly, instead of duplicating it. The raw
// snapshot keeps the real values (the watch panel shows them); the mask
// says what to hide everywhere else (design-docs/Data Tagging.md). If the
// helper can't be queried, it falls back to a bare task_vars with an empty
// mask. ok=false on any query failure, the same silent-fallback contract
// debugOnPause's own inline version already had.
func (s *liveSession) debugCaptureSnapshot(sess *debugrun.Session) (json.RawMessage, snapshotMeta, bool) {
	var meta snapshotMeta
	raw, err := sess.Query(debugSnapshotExpr)
	if err == nil {
		vars, mask, origins, parsed := parseSnapshotQuery(raw)
		if parsed {
			raw = vars
			meta.mask = mask
			base, _ := os.Getwd()
			meta.origins = origin.Labels(origins, s.debugCopyDir, s.debugPlaybookDir, base)
		} else {
			err = errNoSnapshotHelper
		}
	}
	if err != nil {
		meta = snapshotMeta{}
		raw, err = sess.Query("task_vars")
		if err != nil {
			return nil, snapshotMeta{}, false
		}
	}
	raw, regName := mergeRegisteredResultNamed(raw, sess)
	if regName != "" {
		if meta.origins == nil {
			meta.origins = map[string]string{}
		}
		meta.origins[regName] = "registered"
	}
	raw = s.debugUnfakeShadowPaths(raw)
	return raw, meta, true
}

// snapshotMeta is what debugCaptureSnapshot learns about a snapshot besides
// its values: the vault mask and the variables' origin labels.
type snapshotMeta struct {
	mask    vaultMask
	origins map[string]string
}

var errNoSnapshotHelper = errors.New("tangsible_snapshot reply unusable")

// debugSetWatch performs design-docs/DebugMode.md's "Stage 5" set action
// for the watch panel's "expr = value" form: target/value have already
// been validated and resolved against the live pause by watchAttemptSet
// (livesession_watch.go) - this only drives the actual REPL round-trip,
// mirroring debugRedo's own shape exactly (background goroutine,
// s.debugDriving as the shared reentrancy guard every other debug key
// already uses, QueueUpdateDraw to report back). Returns false - without
// touching the REPL at all - when it isn't actually free right now, so
// the caller knows not to clear the input field.
//
// On success, re-runs debugCaptureSnapshot to refresh the cache entry for
// this exact pause: Session.Set's own mutation lands in the live
// debugger's task_vars dict, not in s.debugTaskVarsCache, which is only
// ever a snapshot copy taken at Query time - without this refresh the
// panel would keep showing the stale, pre-set value until the next real
// pause happened to recapture it.
func (s *liveSession) debugSetWatch(expr, target, value string) bool {
	if s.debugDriving || s.debugSess == nil {
		s.watchShowStatus("Not paused here - can't set a variable")
		return false
	}
	sess := s.debugSess
	label, host := s.debugLastPauseTask, s.debugPausedHost
	s.debugDriving = true
	s.debugRefreshChrome()

	go func() {
		setErr := sess.Set(target, value)
		var raw json.RawMessage
		var meta snapshotMeta
		var ok bool
		if setErr == nil {
			raw, meta, ok = s.debugCaptureSnapshot(sess)
		}
		s.app.QueueUpdateDraw(func() {
			s.debugDriving = false
			s.debugRefreshChrome()
			if setErr != nil {
				s.watchShowStatus("Assignment failed: " + tview.Escape(setErr.Error()))
				s.rebuild()
				return
			}
			if ok {
				s.debugTaskVarsCache[debugVarsKey{label, host}] = raw
				s.debugVaultMaskCache[debugVarsKey{label, host}] = meta.mask
				s.debugOriginCache[debugVarsKey{label, host}] = meta.origins
				if ast, parsed := parseWatchExpr(expr); parsed {
					names := s.debugSetNames[debugVarsKey{label, host}]
					if names == nil {
						names = map[string]bool{}
						s.debugSetNames[debugVarsKey{label, host}] = names
					}
					names[ast.root] = true
				}
			}
			s.watch.markJustSet(expr)
			s.rebuild()
			s.watchSyncBottomBar()
		})
	}()
	return true
}

// mergeRegisteredResult adds the paused task's own register: output into
// raw's top-level object, if it has one - closing design-docs/
// DebugMode.md's "Scope, deliberately not chased further here" gap for
// failed_when:/changed_when:/until: referencing a result.rc-shaped
// register target. task_vars itself is confirmed (DebugMode.md's
// 2026-09-27 pivot spike) to be a pre-task snapshot that never reflects
// this same task's own register: output - but since debugger: always
// fires *after* the task completes (debug_closure wraps
// StrategyBase._process_pending_results), the debugger's own scope at
// that exact same pause separately exposes the completed task and its
// result (Session.Query's own doc comment) - task.register gives the
// variable name ansible would store the result under, and
// result._return_data gives that value (registeredValuesExpr: the same
// through result.utr on ansible-core 2.22+), confirmed live both queryable
// at the same pause task_vars comes from. Leaves raw completely
// unchanged on any error (no register: on this task, a query failure,
// unparseable JSON) - the same silent-fallback contract the rest of this
// cache's consumers already rely on.
func mergeRegisteredResult(raw json.RawMessage, sess *debugrun.Session) json.RawMessage {
	merged, _ := mergeRegisteredResultNamed(raw, sess)
	return merged
}

// registeredValuesExpr is the debugger-scope expression for what the paused
// task registered, as a name -> value mapping, written for both result
// shapes ansible-core has had (design-docs/AnsibleInternals.md, "Tested
// releases"): before 2.22 `task.register` is the variable's name and
// `result._return_data` its value; from 2.22 `result` is a HostTaskResult
// whose `utr.registered_values` is that mapping itself (and `task.register`
// a mapping too, so it can't be used the old way). Empty for a task without
// register:.
const registeredValuesExpr = `(dict(result.utr.registered_values or {}) if hasattr(result, 'utr') else ({task.register: result._return_data} if task.register else {}))`

// mergeRegisteredResultNamed is mergeRegisteredResult that also reports the
// (first, by name) register name it merged under ("" when it left raw
// unchanged).
func mergeRegisteredResultNamed(raw json.RawMessage, sess *debugrun.Session) (json.RawMessage, string) {
	regRaw, err := sess.Query(registeredValuesExpr)
	if err != nil {
		return raw, ""
	}
	var registered map[string]json.RawMessage
	if err := json.Unmarshal(regRaw, &registered); err != nil || len(registered) == 0 {
		return raw, ""
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, ""
	}
	names := make([]string, 0, len(registered))
	for name, value := range registered {
		obj[name] = value
		names = append(names, name)
	}
	sort.Strings(names)
	merged, err := json.Marshal(obj)
	if err != nil {
		return raw, ""
	}
	return merged, names[0]
}

// debugUnfakeShadowPaths rewrites every occurrence of the shadow copy's
// own root (s.debugCopyDir) in raw back to the user's real project tree
// (s.debugPlaybookDir) - design-docs/DebugMode.md's "faked paths" follow-
// up: debug mode actually runs ansible-playbook against a disposable
// hardlink shadow copy (internal/debugcopy.BuildShadowCopy), so magic
// variables like role_path/playbook_dir genuinely, correctly reflect
// that copy's own location - which reads as a confusing, unexpected path
// to a user who never asked to see it. A plain, unconditional substring
// replace is enough and nothing more is needed: BuildShadowCopy's own
// hardlink walk gives every file in the copy the exact same relative
// path it has in the real tree, so copyDir is always just playbookDir's
// own string plus a fixed extra prefix - no per-variable-name logic, no
// line-shift math the way debugcopy.BuildPathMapping's task-location
// mapping needs (that one exists only because splicing shifts line
// numbers; a bare directory path carries none). Applied once, here, to
// the raw JSON bytes before they ever enter s.debugTaskVarsCache, so
// every consumer (the watch explorer, and - via the Resolved tab's own
// --extra-vars-fed stub render, resolveTaskValuesFromSnapshot - any
// task referencing role_path/inventory_dir/inventory_file in its own
// source) sees the rewritten value for free, with nothing further to
// change at any of those call sites. Deliberately not scoped to any
// particular variable name: a derived value (e.g. a register:ed
// command's own captured stdout, merged in by mergeRegisteredResult just
// above) that happens to contain the shadow path gets fixed the same
// way. playbook_dir itself is the one magic variable this can't reach
// inside the Resolved tab specifically - ansible-core injects it
// unconditionally for every play, never overridable via --extra-vars
// (confirmed live), so a task referencing it there needs
// resolveTaskValues' own separate, output-side fix instead (resolved.go).
// A no-op whenever either string is empty (every non-debug Verb, and
// bytes.ReplaceAll's own special "insert between every byte" behavior
// for an empty old value makes that guard required, not just tidy).
func (s *liveSession) debugUnfakeShadowPaths(raw json.RawMessage) json.RawMessage {
	if s.debugCopyDir == "" || s.debugPlaybookDir == "" {
		return raw
	}
	return bytes.ReplaceAll(raw, []byte(s.debugCopyDir), []byte(s.debugPlaybookDir))
}

// debugApplyPauseResult runs on the event-loop goroutine (always called
// from inside a QueueUpdateDraw callback - see debugStartOrContinue's own
// doc comment). Auto-opens the drill-down for the surfaced pause's own
// (task, host) - design-docs/DebugMode.md's "UI ideas": "When the
// debugger hits a break point or if there's an error or if the user has
// just performed a single step, the drill down opens for the current
// task+host" - a step always surfaces unconditionally (StepOne's own
// contract, debugrun/breakpoints.go).
//
// Only opens immediately if that host's own result is already recorded
// on debugCursor.CurrentTask - the debugger's own REPL pause notification
// (this function) and the jsonl event reporting that exact host's result
// (debugDrainEvents, a completely separate pipe and goroutine) have no
// ordering guarantee between them, confirmed live: opening before the
// event arrives showed an Output tab reading "no output recorded for
// <host>" with every other tab missing too (nothing to show yet on any
// of them), until navigating away and back re-opened it correctly - by
// which point the race had already resolved. When the event hasn't
// arrived yet, this just remembers the target (debugPendingOpenTask/
// Host) and rebuilds; debugDrainEvents' own event-apply path finishes
// the job once it does.
func (s *liveSession) debugApplyPauseResult(pause debugrun.Pause, eof bool, err error) {
	s.debugDriving = false
	s.debugLoopDriving = false
	s.watch.clearJustSet() // Stage 5: the live pause is about to change either way
	if err != nil {
		s.everStarted = true
		s.processDone.Store(true)
		s.exitCode.Store(-1)
		errText := []string{err.Error()}
		s.lastStderr.Store(&errText)
		s.debugRefreshChrome()
		s.rebuild()
		return
	}
	if eof {
		s.debugFinish()
		return
	}
	s.debugPausedHost = pause.Host
	s.debugLastPauseTask = pause.Task
	s.debugRefreshChrome()
	if s.debugCursor == nil || s.debugCursor.CurrentTask == nil {
		s.rebuild()
		return
	}
	task := s.debugCursor.CurrentTask
	if _, ok := task.Hosts[pause.Host]; ok {
		s.debugPendingOpenTask, s.debugPendingOpenHost = nil, ""
		s.showOutputWithOrigin(task, pause.Host, false)
		return
	}
	s.debugPendingOpenTask, s.debugPendingOpenHost = task, pause.Host
	s.rebuild()
}

// debugFinish marks the session done once debugrun reports eof (the
// child exited - the normal way a debug run ends, once its very last
// task's own pause is resumed) - mirrors runner.RunOneGeneration's own
// tail exactly: exitCode/lastStderr stored before processDone
// (CLAUDE.md's "Generation outcome ordering" invariant), via sess.Close's
// own cmd.Wait() (debugrun.Session has no separate stderr stream to
// capture - design-docs/DebugMode.md's own "stdout-ownership wrinkle" -
// so lastStderr stays empty here, a known, accepted gap). No invocation-
// history recording - a debug session isn't a replayable invocation
// (debugverb.PrepareSession's own doc comment).
//
// Guarded against running twice (the eof branches of both
// debugApplyPauseResult and debugDrainEvents' own trailing fallback can
// each reach this, racing on which one observes eof first) - confirmed
// live as a real bug, not a hypothetical: os/exec's own *exec.Cmd.Wait
// explicitly refuses a second call ("Wait was already called"), which
// runner.ExitCodeOf then has no way to tell apart from a genuine failure,
// reporting a bogus exit code -1 even though the run actually finished
// cleanly. debugDrainEvents' own call already checked !s.processDone
// first; this same check, repeated here, is what makes
// debugApplyPauseResult's own unconditional call safe too, regardless of
// which of the two actually runs first.
func (s *liveSession) debugFinish() {
	if s.processDone.Load() {
		return
	}
	s.watch.clearJustSet() // reached directly by debugDrainEvents too, not only via debugApplyPauseResult
	s.debugPausedHost = ""
	s.debugPendingOpenTask, s.debugPendingOpenHost = nil, ""
	s.everStarted = true
	if s.debugSess != nil {
		waitErr := s.debugSess.Close()
		code := runner.ExitCodeOf(waitErr)
		s.exitCode.Store(int32(code))
	}
	s.processDone.Store(true)
	s.debugDriving = false
	s.debugLoopDriving = false
	s.debugRefreshChrome()
	s.rebuild()
}

// debugDrainEvents feeds the session's own jsonl stream (runner.
// ScanEvents, via debugrun.Session.Events - the exact same mechanism a
// normal live run uses) into s.debugCursor, for the session's whole
// lifetime. Runs on its own goroutine; every mutation goes through
// QueueUpdateDraw, same discipline as debugStartOrContinue.
func (s *liveSession) debugDrainEvents(sess *debugrun.Session) {
	for item := range sess.Events {
		if !item.IsEvent {
			continue
		}
		ev := item.Ev
		s.app.QueueUpdateDraw(func() {
			if s.debugCursor == nil || s.debugSess != sess {
				return // a restart replaced this session; its events are stale
			}
			host, taskChanged := s.debugCursor.Apply(ev)
			if host != "" {
				s.state.NoteHost(host)
			}
			// The cursor bypasses PlaybookState.Apply, which is what sets
			// HadUnreachable elsewhere; without it exit code 4 from a run
			// whose only problem was an unreachable host reads as a
			// failure (uikit.GenuineFailure).
			if ev.Event == "v2_runner_on_unreachable" {
				s.state.HadUnreachable = true
			}
			if taskChanged {
				s.debugPausedHost = ""
			}
			// Finishes debugApplyPauseResult's own pending-open dance
			// (see debugPendingOpenTask/Host's own doc comment,
			// livesession.go) the moment this exact host's result
			// actually arrives - whichever of the two reaches the
			// event-loop goroutine first.
			if host != "" && host == s.debugPendingOpenHost && s.debugCursor.CurrentTask == s.debugPendingOpenTask {
				task, h := s.debugPendingOpenTask, s.debugPendingOpenHost
				s.debugPendingOpenTask, s.debugPendingOpenHost = nil, ""
				s.showOutputWithOrigin(task, h, false)
				return
			}
			s.rebuild()
		})
	}
	// The event pipe only ever closes once the child itself has exited -
	// independently of whether anything is currently blocked reading the
	// REPL side. Needed for a real, confirmed-live gap (found once
	// already, in a previous, separate debug TUI): Ctrl-C while genuinely
	// paused (no Start/Continue/Step call in flight) kills the child, but
	// nothing was blocked on its pipes to notice - without this, the
	// bottom bar kept showing a stale "paused" hint, and q/Ctrl-C alone
	// (which only re-signals an already-dead process while !processDone)
	// could never actually quit until some other key happened to issue a
	// doomed Continue/Step first and discover the eof. Racing with
	// debugApplyPauseResult's own eof handling - whichever reaches the
	// event-loop goroutine first wins - is exactly why debugFinish itself
	// (not just here) guards against running twice; see its own doc
	// comment.
	s.app.QueueUpdateDraw(func() {
		if s.debugSess == sess {
			s.debugFinish()
		}
	})
}

// Ctrl-C needs no debug-specific code at all: handleCtrlC
// (livesession_input.go) already signals s.procH.Load() unconditionally
// whenever !s.processDone.Load(), and debugStartOrContinue above stores
// the real child process into s.procH the moment it's spawned - so it
// already reaches the right process, exactly as it already does for a
// plain run. q is almost the same story (handleGlobalQuit, same file) -
// except while debugLoopDriving is true, where it requests a soft stop
// instead (debugConsumeStopRequest's own doc comment, design-docs/
// DebugMode.md's "q's own soft stop while actively driving") rather than
// touching the real process at all.

// debugResetKey is 'r': open the restart dialog (the re-run dialog's tags/
// skip-tags/hosts fields; its title says when a paused run will be ended).
// Enter then goes back to the idle state a fresh `tangsible debug` starts in
// - nothing run, the tree rebuilt from the files on disk now, breakpoints
// kept - without starting anything (the next F5/F10/F11 does, so stepping
// from the very first task works). Only offered while paused or after the run
// finished; while the child is running between pauses, or before anything
// started, it does nothing. Works in the tree and over the drill-down.
func (s *liveSession) debugResetKey() {
	if s.debugRefresher == nil || s.debugDriving || s.debugRestarting {
		return
	}
	finished := s.processDone.Load() && s.everStarted
	paused := !finished && s.debugSess != nil && s.debugPausedHost != ""
	if !finished && !paused {
		return
	}
	s.openRerunDialog()
}

// debugSubmitRestart is the dialog's Enter in debug: the passthrough
// arguments get the dialog's tags/skip-tags/hosts (the other options stay as
// they were), then the session restarts.
func (s *liveSession) debugSubmitRestart() {
	parsed := config.ParsePassthroughArgs(s.passthroughArgs)
	parsed.Tags = strings.TrimSpace(s.rerunFields.tagsField.GetText())
	parsed.SkipTags = strings.TrimSpace(s.rerunFields.skipTagsField.GetText())
	parsed.Hosts = strings.TrimSpace(s.rerunFields.hostsField.GetText())
	s.closeDialogs()
	s.debugRestart(parsed.Reassemble())
}

// debugRestart ends the current debug session (if any) and rebuilds the
// shadow copy and the predicted tree from the files on disk, with args as
// the ansible-playbook arguments from now on (kept only once the rebuild
// worked). Runs off the event loop; debugApplyRestart installs the result.
func (s *liveSession) debugRestart(args []string) {
	old := s.debugSess
	s.debugRestarting = true
	// Nil first: the old session's drain goroutine (debugDrainEvents)
	// ignores everything once debugSess is no longer its own.
	s.debugSess = nil
	s.debugPausedHost = ""
	s.debugPendingOpenTask, s.debugPendingOpenHost = nil, ""
	s.watch.clearJustSet()
	if s.viewingOutput {
		s.closeOutput()
	}
	s.debugRefreshChrome()
	s.rebuild()
	refresher := s.debugRefresher
	go func() {
		if old != nil {
			endDebugSession(old)
		}
		prep, err := refresher.Reprepare(args)
		if s.quitting.Load() {
			refresher.Cleanup() // the copy just built is not the one the exit cleanup saw
			return
		}
		s.app.QueueUpdateDraw(func() { s.debugApplyRestart(prep, err) })
	}()
}

// endDebugSession closes sess's pipes (EOF makes a paused debugger quit)
// and waits for the child, killing it if it does not leave within a few
// seconds.
func endDebugSession(sess *debugrun.Session) {
	done := make(chan struct{})
	go func() {
		sess.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = sess.Process().Kill()
		<-done
	}
}

// debugApplyRestart runs on the event loop: swaps the freshly seeded tree
// into the session's state (in place - the state pointer is shared) and
// returns every piece of view state to what a new session starts with.
func (s *liveSession) debugApplyRestart(prep debugverb.SessionPrep, err error) {
	s.debugRestarting = false
	if err != nil {
		// The old copy is still there; the run is over, so another r tries
		// again once the playbook is fixed.
		s.everStarted = true
		s.processDone.Store(true)
		s.exitCode.Store(-1)
		errText := append([]string{"couldn't restart the debug session:"}, strings.Split(err.Error(), "\n")...)
		s.lastStderr.Store(&errText)
		s.debugRefreshChrome()
		s.rebuild()
		return
	}

	seeded, nodes, nodePlay := debugverb.SeedState(prep.Skeleton)
	s.state.Reset()
	s.state.Plays = seeded.Plays
	tracker := runner.NewProgressTracker(prep.Skeleton)
	s.progH.Store(tracker)
	s.debugCursor = debugverb.NewCursor(nodes, nodePlay, tracker, prep.PathMapping)
	s.debugCopyPlaybookPath = prep.CopyPlaybookPath
	s.debugCopyDir = prep.CopyDir
	s.passthroughArgs = prep.Rest
	// Tasks may have moved or appeared: refresh the source lookups, as a
	// rerun does. Entries of deleted tasks stay but are never looked up.
	source.MergeSourceIndex(s.sourceIndex, prep.Playbook)

	s.debugLastPauseTask = ""
	s.debugStopPending.Store(false)
	s.debugDriving, s.debugLoopDriving = false, false
	s.expanded = map[*playbook.TaskNode]bool{}
	s.autoExpanded = map[*playbook.TaskNode]bool{}
	s.messageTasks = nil
	s.messagesCollapsed = false
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
	s.lastAppliedSelectedIndex = -1
	s.resolveCache = map[resolveKey]uikit.ResolvedRender{}
	s.fileCache = map[resolveKey]uikit.ResolvedRender{}
	s.debugTaskVarsCache = map[debugVarsKey]json.RawMessage{}
	s.debugVaultMaskCache = map[debugVarsKey]vaultMask{}
	s.debugOriginCache = map[debugVarsKey]map[string]string{}
	s.debugSetNames = map[debugVarsKey]map[string]bool{}
	s.everStarted = false
	s.processDone.Store(true) // the idle state: nothing to interrupt
	s.exitCode.Store(0)
	s.lastStderr.Store(nil)
	s.startedAt = time.Now()
	s.debugRefreshChrome()
	s.rebuild()
}
