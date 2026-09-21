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
	"time"

	"code.aw.net/claude/tangsible/internal/uikit"
)

// startHeartbeat is the top-bar heartbeat ticker - the first self-driven
// (not event- or input-triggered) source of QueueUpdateDraw calls in this
// codebase. A method rather than a bare goroutine, specifically so
// submitRerun can call it again to resume ticking for a rerun (Rerun.md) -
// the ticker that started alongside the first invocation permanently
// returns once it observes processDone true (see its own comment below),
// so a later generation needs a fresh one.
func (s *liveSession) startHeartbeat() {
	go func() {
		ticker := time.NewTicker(uikit.SpinnerInterval)
		defer ticker.Stop()
		for range ticker.C {
			if s.quitting.Load() {
				return // mirrors main.go's streamEvents guard: tview's
				// update queue is a fixed 100-slot buffer nothing drains
				// once the app has stopped, so a goroutine blocked inside
				// QueueUpdateDraw past that point hangs forever. Unlike
				// streamEvents, nothing in main.go waits on this
				// goroutine, so such a hang wouldn't itself block process
				// exit - but there's no reason to rely on that.
			}
			done := s.processDone.Load()
			s.app.QueueUpdateDraw(s.rebuild)
			if done {
				return // one frozen frame pushed above; stop ticking
				// rather than redrawing a static screen forever - until
				// startHeartbeat is called again for a later rerun.
			}
		}
	}()
}

// startResizeWatcher is a second, permanent ticker, deliberately
// independent of startHeartbeat's own per-generation running/frozen
// lifecycle (unlike startHeartbeat, this is started exactly once and
// never restarted by submitRerun). Its only job is noticing a bare
// terminal resize once the run is frozen - startHeartbeat's own ticker
// already permanently stops once processDone is observed true, so
// nothing else is left driving a rebuild on a terminal resize with no
// other incoming event. While a run is still live, startHeartbeat's own
// ticker already re-syncs everything within SpinnerInterval regardless
// of resize - so this goroutine skips its own work entirely until
// processDone. "Everything" now includes the two-pane drill-down's own
// split-vs-full-screen mode and tree-pane width, not just the tree's row
// text/column layout - see rebuild's own resync block
// (design-docs/TwoPanedLayout.md) - so a frozen run's drill-down reacts
// to a resize exactly as a live one does, via the same rebuild call, just
// noticed by this ticker instead of startHeartbeat's.
func (s *liveSession) startResizeWatcher() {
	go func() {
		ticker := time.NewTicker(uikit.SpinnerInterval) // reused only as a
		// convenient existing interval - not tied to spinner-animation cadence.
		defer ticker.Stop()
		for range ticker.C {
			if s.quitting.Load() {
				return // same accepted best-effort guard startHeartbeat's own
				// ticker already uses - nothing waits on this goroutine, so a
				// hang here wouldn't itself block process exit.
			}
			if !s.processDone.Load() {
				continue // startHeartbeat's own ticker already handles this
				// case every SpinnerInterval regardless of resize.
			}
			s.app.QueueUpdate(func() { // NOT QueueUpdateDraw - avoid forcing a
				// real screen redraw on every tick when nothing changed.
				_, _, totalWidth, _ := s.pages.GetInnerRect() // s.pages, not
				// s.list - the terminal's true current width regardless of
				// which page is frontmost (see rebuild's own
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
}
