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
	"code.aw.net/claude/tangsible/internal/debugrun"
	"code.aw.net/claude/tangsible/internal/debugverb"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
)

// Debug mode (design-docs/DebugMode.md's "Stage 2"): a liveSession with
// debugMode true gets three new keys (F5/F9/F10 for now - design-docs/
// DebugMode.md's "Stage 1" [keys] config section isn't wired yet) and one
// background orchestration goroutine, parallel to runner.RunOneGeneration
// rather than a modification of it - everything else (the tree, two-pane,
// the drill-down, filter/search, recap, q/Ctrl-C) is the exact same
// liveSession machinery every other Verb already uses.

// debugArmed reports whether t has an armed breakpoint - FlattenRows' own
// armed parameter, and the "selected row re-render"'s own marker
// computation (livesession_rebuild.go). Safe to call for every Verb, not
// just "debug": false whenever debugMode is false, so a plain run's own
// rendering is completely unaffected.
func (s *liveSession) debugArmed(t *playbook.TaskNode) bool {
	return s.debugMode && s.debugBPS.Has(debugverb.TaskLabel(t.Name))
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
// fall back on the way the top/split-header bars have). Mirrors the
// hardcoded-constant style the rest of this session's own chrome colors
// already use for this (s.liveChromeBg/s.chromeBg, tui.go) rather than
// tcell.GetColor(uikit.ChromeDebugRunningColor) - consistent with them,
// not a new, second derivation style for just these two.
func (s *liveSession) debugChromeBg() tcell.Color {
	switch {
	case s.debugDriving:
		return tcell.ColorTeal
	case s.debugPausedHost != "":
		return tcell.ColorMaroon
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
// (currentMainBottomBarText) whenever debugMode is true - the F5/F9/F10
// keys have no existing tree binding to replace, so they're purely
// additive, and the wording changes with the session's own current phase
// (mirroring a previous, separate debug TUI's own debugBar* constants).
func (s *liveSession) debugBottomBarHint() string {
	switch {
	case s.processDone.Load() && s.everStarted:
		return " finished - F5/F9/F10 disabled "
	case s.debugPausedHost != "":
		return " F5: continue  F9: breakpoint  F10: step "
	case s.debugDriving:
		return " running... (ctrl-c to interrupt) "
	default:
		return " F5: run  F9: breakpoint  F10: step "
	}
}

// handleDebugKey is handleKey's own debug-specific guard (livesession_
// input.go), checked right after handleFilterDialogKey and before the
// s.viewingStdout branch - early enough that none of the three dialogs
// can have their own keys stolen, but before the tree/drill-down fork, so
// these three keys work identically whichever one is frontmost. A no-op
// (returns handled=false) for every Verb but "debug" - tcell.KeyF5/F9/F10
// are unclaimed anywhere else in this codebase (confirmed by grep before
// adopting them), so this never shadows an existing binding.
func (s *liveSession) handleDebugKey(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.debugMode {
		return nil, false
	}
	switch event.Key() {
	case tcell.KeyF5:
		s.debugStartOrContinue(debugrun.RunTillBreakpoint)
		return nil, true
	case tcell.KeyF10:
		s.debugStartOrContinue(debugrun.StepOne)
		return nil, true
	case tcell.KeyF9:
		s.debugToggleBreakpoint()
		return nil, true
	}
	return nil, false
}

// debugToggleBreakpoint arms/disarms a breakpoint on s.currentID - a
// no-op unless the cursor is currently on a task row (currentID holds
// many other ID shapes: a *playbook.PlayNode, a uikit.HostRowID, a recap
// row, ...). Mirrors a previous, separate debug TUI's own
// toggleBreakpoint, using currentID (identity, this package's own
// convention - CLAUDE.md's "Selection is tracked by row identity, not
// index") instead of a row index.
func (s *liveSession) debugToggleBreakpoint() {
	task := s.debugCursorTask()
	if task == nil {
		return
	}
	label := debugverb.TaskLabel(task.Name)
	if s.debugBPS.Has(label) {
		s.debugBPS.Clear(label)
	} else {
		s.debugBPS.Set(label)
	}
	s.rebuild()
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
	if s.debugDriving || (s.processDone.Load() && s.everStarted) {
		return
	}
	s.debugDriving = true
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
	s.debugRefreshChrome()

	if s.debugSess == nil {
		go func() {
			sess, err := debugrun.Spawn(s.debugCopyPlaybookPath, s.passthroughArgs)
			if err != nil {
				s.app.QueueUpdateDraw(func() {
					s.debugDriving = false
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
			pause, eof, perr := debugrun.RunWithBreakpoints(sess, s.debugBPS, mode)
			s.app.QueueUpdateDraw(func() {
				s.debugApplyPauseResult(pause, eof, perr)
			})
		}()
		return
	}

	sess := s.debugSess
	go func() {
		pause, eof, perr := debugrun.ContinueWithBreakpoints(sess, s.debugBPS, mode)
		s.app.QueueUpdateDraw(func() {
			s.debugApplyPauseResult(pause, eof, perr)
		})
	}()
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
			if s.debugCursor == nil {
				return
			}
			host, taskChanged := s.debugCursor.Apply(ev)
			if host != "" {
				s.state.NoteHost(host)
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
	s.app.QueueUpdateDraw(s.debugFinish)
}

// q/Ctrl-C needs no debug-specific code at all: handleCtrlC/
// handleGlobalQuit (livesession_input.go) already signal s.procH.Load()
// unconditionally whenever !s.processDone.Load(), and debugStartOrContinue
// above stores the real child process into s.procH the moment it's
// spawned - so both already reach the right process, exactly as they
// already do for a plain run.
