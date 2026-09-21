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
	"fmt"

	"code.aw.net/claude/tangsible/internal/ansibledoc"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/rivo/tview"
)

// renderOutputTabs rebuilds every tab from BuildOutputTabs' own output and
// hands the result to s.outputTabs.SetTabs - which itself preserves
// whichever tab is currently active, by name, so repeatedly calling this
// while browsing (Left/Right/n/N) doesn't keep resetting the user back to
// the Task tab. s.search.clear() runs first - every one of this function's
// own call sites (below) is itself a moment the active tab's content is
// about to change (a new host/task, a resolve/docs fetch landing, a
// rerun), and design-docs/Search.md is explicit that a search does not
// survive that.
func (s *liveSession) renderOutputTabs(task *playbook.TaskNode, host string, resolved, docs, file uikit.ResolvedRender) {
	s.search.clear()
	names, contents := uikit.BuildOutputTabs(task, host, s.sourceIndex, resolved, docs, file)
	prims := make([]tview.Primitive, len(names))
	for i, content := range contents {
		tv := tview.NewTextView().SetDynamicColors(true)
		tv.SetText(content)
		prims[i] = tv
	}
	s.outputTabs.SetTabs(names, prims)
	if s.search.composing {
		// s.outputTabs.SetTabs rebuilds every tab's TextView from scratch
		// (RemovePage/AddPage on its own internal tview.Pages) - confirmed
		// live that this can silently steal application-level focus back
		// onto whichever tab ends up active, even though s.search.input
		// lives in a completely separate sibling subtree of outputFlex,
		// not inside s.outputTabs at all. SetInputCapture's own
		// s.search.composing branch no longer depends on app-level focus
		// to reach s.search.input (it invokes its InputHandler directly
		// via tabSearchPanel.handleComposingKey - see that method's own
		// doc comment for why), so this can't break typing anymore either
		// way; re-asserting focus here is still worth doing so the
		// field's own cursor rendering doesn't visibly jump elsewhere if
		// an async Resolved/Docs fetch happens to land mid-keystroke.
		// renderOutputTabs's other two call sites (host/task navigation)
		// can't be composing at the same time, since navigateOutputHost/
		// Task aren't reachable while s.search.composing gates
		// SetInputCapture earlier.
		s.app.SetFocus(s.search.input)
	}
}

// showOutputWithOrigin is showOutput/showOutputFromRecap's own shared
// implementation - see their own doc comments, and the fromRecap
// discussion further down, for why the two need to differ at all.
func (s *liveSession) showOutputWithOrigin(task *playbook.TaskNode, host string, fromRecap bool) {
	// Pane-mode (split vs. full-screen) is no longer decided here at all -
	// s.rebuild() (below) re-evaluates it, live, from the terminal's
	// actual current width on every call, including the one this function
	// makes at the very end (see design-docs/TwoPanedLayout.md) - so a
	// fresh open and a later resize while already open are handled by the
	// exact same code path, not two.

	s.outputTask, s.outputHost = task, host
	s.outputTopBarPlainText = fmt.Sprintf(" %s — %s ", host, task.Name)

	// Kicked off the moment a drill-down opens (or navigates to a
	// different host/task), not gated behind a keypress - per
	// design-docs/Drilldown, Resolved Values.md. Runs on its own
	// goroutine so opening the view is never blocked on a real
	// ansible-playbook invocation. The Resolved tab itself starts out
	// entirely absent, not a visible "Resolving..." placeholder
	// (ResolvedTabHidden treats Pending as hidden) - an earlier version
	// showed the placeholder immediately, but that made the tab a moving
	// target: a user who tabbed onto it while it read "Resolving..."
	// would watch it vanish out from under them the instant resolving
	// finished identical to source. Silently absent until there's
	// something worth showing reads as "this task doesn't have one"
	// rather than "something just disappeared," even though both are the
	// same outcome. The s.outputTask/s.outputHost/s.viewingOutput check
	// right before updating the view guards against a stale result
	// landing after the user has already navigated elsewhere - the cache
	// itself is still updated regardless, so a later revisit is free.
	key := resolveKey{task, host}
	resolved, cached := s.resolveCache[key]
	if !cached {
		resolved = uikit.ResolvedRender{Pending: true}
		s.resolveCache[key] = resolved
		go func() {
			text, err := resolveTaskValues(task.Path, s.sourceIndex[task.Path], host, s.passthroughArgs)
			result := uikit.ResolvedRender{}
			if err != nil {
				result.Err = err.Error()
			} else {
				result.Text = text
			}
			s.app.QueueUpdateDraw(func() {
				s.resolveCache[key] = result
				if s.outputTask != task || s.outputHost != host || !s.viewingOutput {
					return
				}
				if !uikit.ResolvedTabHidden(result, s.sourceIndex[task.Path]) {
					// The tab wasn't shown at all until now (see above) -
					// only a full renderOutputTabs can make a brand new
					// tab appear at all, unlike the old design's
					// in-place SetText on an already-existing TextView.
					// Accepted here even though it loses whatever
					// tab/scroll position was current: this fires at
					// most once, very shortly after the view was first
					// opened (see design-docs/Drilldown, Resolved
					// Values.md's own "kicked off the moment a
					// drill-down opens" timing), so there's realistically
					// nothing meaningful to lose yet.
					s.renderOutputTabs(task, host, result, s.docsCache[uikit.TaskAction(task, host)], s.fileCache[key])
				}
				// Otherwise the tab stays exactly as absent as it
				// already was - nothing on screen needs to change.
			})
		}()
	}

	// Same "kick off immediately, stay absent until ready" treatment as
	// Resolved above, for the Docs tab (ansibledoc.go's
	// ansibledoc.FetchAnsibleDoc) - cached by module name in
	// s.docsCache, not by (task, host), since a module's own
	// documentation is the same for every task/host that uses it (see
	// s.docsCache's own comment). action == "" (no result recorded yet,
	// or this result simply has no action field) means there's nothing
	// to look up at all - docs stays the zero ResolvedRender{}, which
	// BuildOutputTabs' DocsTabHidden already treats as "omit the tab."
	action := uikit.TaskAction(task, host)
	var docs uikit.ResolvedRender
	docsCached := action == "" // no action to look up - stays the
	// zero value, and there's nothing to kick off below either.
	if action != "" {
		docs, docsCached = s.docsCache[action]
	}
	if !docsCached {
		docs = uikit.ResolvedRender{Pending: true}
		s.docsCache[action] = docs
		go func() {
			text, err := ansibledoc.FetchAnsibleDoc(action)
			result := uikit.ResolvedRender{}
			if err != nil {
				result.Err = err.Error()
			} else {
				result.Text = text
			}
			s.app.QueueUpdateDraw(func() {
				s.docsCache[action] = result
				if s.outputTask != task || s.outputHost != host || !s.viewingOutput {
					return
				}
				if !uikit.DocsTabHidden(result) {
					s.renderOutputTabs(task, host, s.resolveCache[key], result, s.fileCache[key])
				}
			})
		}()
	}

	// Same "kick off immediately, stay absent until ready" treatment as
	// Resolved/Docs above, for the File tab (design-docs/
	// ShowFileContents.md, fetchfile.go's fetchRemoteFileContents) -
	// cached by (task, host) in s.fileCache, only attempted at all when
	// uikit.RemoteFilePath recognizes this task's own module and can
	// find a path to fetch. local (delegate_to: localhost, see
	// uikit.DelegatedToLocalhost) reads the file straight off local disk
	// instead of spawning a fetch against host - the file never left the
	// control host in the first place.
	file, fileCached := s.fileCache[key]
	if filePath, supported, local := uikit.RemoteFilePath(task, host); supported && !fileCached {
		file = uikit.ResolvedRender{Pending: true}
		s.fileCache[key] = file
		go func() {
			var text string
			var err error
			if local {
				text, err = readLocalFileContents(filePath)
			} else {
				text, err = fetchRemoteFileContents(filePath, host, s.passthroughArgs)
			}
			result := uikit.ResolvedRender{}
			if err != nil {
				result.Err = err.Error()
			} else {
				result.Text = text
			}
			s.app.QueueUpdateDraw(func() {
				s.fileCache[key] = result
				if s.outputTask != task || s.outputHost != host || !s.viewingOutput {
					return
				}
				if !uikit.FileTabHidden(result) {
					s.renderOutputTabs(task, host, s.resolveCache[key], s.docsCache[uikit.TaskAction(task, host)], result)
				}
			})
		}()
	}

	s.renderOutputTabs(task, host, resolved, docs, file)
	// Every fresh tab's own TextView starts scrolled to the top already
	// (a brand new widget), so there's nothing to reset here the way the
	// old single-TextView version needed SetText not to reset scroll
	// position - see TabbedPane's own "reset to top on every switch"
	// behavior in tabs.go for the *within-view* equivalent of that same
	// concern.
	s.viewingOutput = true

	// Live-sync (design-docs/TwoPanedLayout.md): keep the tree's own
	// cursor pointed at whatever (task, host) the drill-down is
	// currently showing, expanding that task so the row is actually
	// there to point at - on every call, not just the first, so
	// navigateOutputHost/navigateOutputTask (Left/Right, n/N while
	// already viewing output) keep it current too. This used to be
	// closeOutput's own one-time job on the way out; doing it here
	// instead, unconditionally, makes closeOutput's own copy
	// unnecessary (see below) and is what actually makes the tree
	// "follow" while a two-pane session is open. No separate scrolling
	// code is needed to keep the new row visible even when it's off
	// screen: s.rebuild()'s own SetCurrentItem call fires whenever
	// selectedIndex genuinely changes, and TreeList.ensureVisible()
	// (treelist.go) already runs on exactly that.
	//
	// fromRecap skips the s.currentID line specifically - a real report:
	// opening a drill-down from a recap task row already has s.currentID
	// correctly pointing at that recap row (SetChangedFunc already set
	// it there, same as any other cursor move), and forcibly
	// overwriting it with the tree's own HostRowID here yanked the
	// cursor into the main tree the instant the view opened - so
	// closing it (Esc) landed back in the tree too, not the recap row
	// the user actually came from, defeating "go through all failed
	// tasks from the recap" as a workflow. s.expanded[task] still runs
	// unconditionally either way - harmless, and leaves the
	// corresponding tree row expanded for later if the user does scroll
	// up into the tree.
	s.expanded[task] = true
	s.viewingOutputFromRecap = fromRecap
	if !fromRecap {
		s.currentID = uikit.HostRowID{Task: task, Host: host}
	}
	s.following = false
	s.rebuild() // also decides/applies pane mode now that s.viewingOutput is
	// true - see s.rebuild()'s own resync block.
}

// navigateOutputTask moves the output page to the previous/next task
// (delta -1/+1) that recorded a result for s.outputHost, in run order,
// among currently-visible tasks only (see VisibleTasksForHost) - per
// Filters.md, tasks the active filter is hiding are skipped here too. A
// no-op at either end (no wraparound, matching the main tree's own
// no-wraparound convention elsewhere) and before any output has been
// shown yet (s.outputTask still nil).
func (s *liveSession) navigateOutputTask(delta int) {
	if s.outputTask == nil {
		return
	}
	tasks := uikit.VisibleTasksForHost(s.state, s.outputHost, s.currentFilter, s.sourceIndex, s.activeTaskNow())
	idx := -1
	for i, t := range tasks {
		if t == s.outputTask {
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
	s.showOutputWithOrigin(tasks[newIdx], s.outputHost, s.viewingOutputFromRecap)
}

// navigateOutputHost moves the output page to the previous/next host
// (delta -1/+1) within s.outputTask's own HostOrder - the same order the
// s.expanded tree rows for that task already use. A no-op at either end
// (no wraparound) and before any output has been shown yet.
func (s *liveSession) navigateOutputHost(delta int) {
	if s.outputTask == nil {
		return
	}
	hosts := s.outputTask.HostOrder
	idx := -1
	for i, h := range hosts {
		if h == s.outputHost {
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
	s.showOutputWithOrigin(s.outputTask, hosts[newIdx], s.viewingOutputFromRecap)
}

// closeOutput backs out of the output drill-down view. Used to be
// tview.TextView's own native SetDoneFunc (firing on Escape/Enter/Tab/
// Backtab, its fixed "done key" set) - now called explicitly for
// Escape/Enter/q from SetInputCapture's own s.viewingOutput branch, since
// Tab/Backtab mean "switch tab" here now (design-docs/Tabbed UI.md)
// rather than "close," and s.outputTabs' own per-tab TextViews are
// recreated fresh on every renderOutputTabs call anyway, so there's no
// single, persistent TextView left to hang a native SetDoneFunc off of.
//
// Restoring the main tree's own cursor to whatever (task, host) the
// drill-down was last showing needs no work here anymore: showOutput's
// own live-sync (design-docs/TwoPanedLayout.md) already keeps
// s.expanded/s.currentID/s.following current on every call, including
// whichever navigateOutputTask/navigateOutputHost call was the most
// recent one before this fires - there's nothing left to reconcile on
// the way out.
func (s *liveSession) closeOutput() {
	s.search.clear() // leaving the drill-down entirely - nothing left to
	// search once its own tabs are gone.
	s.fileCache = map[resolveKey]uikit.ResolvedRender{} // design-docs/
	// ShowFileContents.md's own caching rule: a remote file's fetched
	// content is only trusted for as long as this one drill-down visit
	// stays open - closing it (even to immediately reopen the same
	// task) means the next open fetches fresh, unlike s.resolveCache/
	// s.docsCache, which intentionally survive a close/reopen within the
	// same generation.
	s.viewingOutput = false
	s.viewingOutputFromRecap = false
	s.splitMode = false
	s.bottomBar.SetText(s.currentMainBottomBarText())
	s.switchPage("main")
	s.rebuild() // s.list's own row text was last baked while s.viewingOutput
	// was still true - possibly at the tree pane's own (narrower,
	// hosts-omitted) width rather than the full terminal's, especially
	// now that a resize can happen live while split is open
	// (design-docs/TwoPanedLayout.md) - a plain page switch alone only
	// fixes s.list's box size via tview's own native redraw, not its
	// already-baked text content.
}
