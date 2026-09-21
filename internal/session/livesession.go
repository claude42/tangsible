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

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/playbook"
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
	app       *tview.Application
	applyLive func(playbook.RawEvent)

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
	lastTotalWidth           int
	viewingOutput            bool
	viewingOutputFromRecap   bool
	useColor                 bool
	splitMode                bool
	currentPageName          string

	// --- chrome/notification bookkeeping ---
	startedAt                  time.Time
	checkMode                  bool
	notifyCfg                  config.SettingsConfig
	notifyPlaybookFinishedKind config.NotificationKind
	notifyTaskFailedKind       config.NotificationKind
	notifyTaskFailedMax        int
	taskFailedNotifyCount      int
	suppressedTaskFailures     int
	finishedNotifySent         bool
	failureCursorPlaced        bool
	frozenElapsed              time.Duration
	haveFrozenElapsed          bool
	liveChromeStyle            tcell.Style
	liveChromeBg               tcell.Color
	liveChromeColorName        string
	chromeStyle                tcell.Style
	chromeBg                   tcell.Color

	// --- widgets touched by more than one closure ---
	bottomBar         *tview.TextView
	flex              *tview.Flex
	splitFlex         *tview.Flex
	splitBody         *tview.Flex
	treeBody          *tview.Flex
	splitDivider      *tview.Box
	splitHeader       *tview.TextView
	topBar            *tview.TextView
	outputTabs        *uikit.TabbedPane
	outputTopBar      *tview.TextView
	outputBottomBar   *tview.TextView
	tabSearchInput    *tview.InputField
	outputFooterPages *tview.Pages
	pages             *tview.Pages
	filterDialog      *tview.TextView
	searchInput       *tview.InputField
	searchDialogFlex  *tview.Flex
	filterFlex        *tview.Flex
	rerunForm         *tview.Form

	// --- tree-level filter/search dialogs ---
	currentFilter    uikit.FilterQuery
	filterDialogOpen bool
	searchDialogOpen bool
	rerunDialogOpen  bool

	// --- rerun dialog field-sync (design-docs/Rerun.md) ---
	playField                *tview.InputField
	tagsField                *tview.InputField
	skipTagsField            *tview.InputField
	hostsField               *tview.InputField
	currentFailedHosts       []string
	currentUnreachableHosts  []string
	currentResumePlay        string
	syncingPlayField         bool
	syncingHostsField        bool
	suppressPlayClear        bool
	suppressHostsClear       bool
	resumeCheckbox           *tview.Checkbox
	onlyFailedCheckbox       *tview.Checkbox
	onlyUnreachableCheckbox  *tview.Checkbox
	acDismissed              bool
	appliedInitialRerunFlags bool
	playPreFilled            bool
	tagsPreFilled            bool
	skipTagsPreFilled        bool
	hostsPreFilled           bool

	// --- output drill-down ---
	outputTask            *playbook.TaskNode
	outputHost            string
	outputTopBarPlainText string
	resolveCache          map[resolveKey]uikit.ResolvedRender
	docsCache             map[string]uikit.ResolvedRender
	fileCache             map[resolveKey]uikit.ResolvedRender
	tabSearch             *uikit.TextSearch
	tabSearchComposing    bool
}
