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

// Package variables is `tangsible variables hostname [playbook.yml]`
// (design-docs/Variables.md): where each variable of one host is defined, as a
// tree of variables that expand to all their definitions in order of
// precedence. The data is internal/varscrawl's; this is the standalone screen
// (its own tview.Application, like the host verb), the pure half of which is
// rows.go and render.go.
package variables

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/host"
	"code.aw.net/claude/tangsible/internal/inventory"
	"code.aw.net/claude/tangsible/internal/template"
	"code.aw.net/claude/tangsible/internal/uikit"
	"code.aw.net/claude/tangsible/internal/varscrawl"
)

// RunVariablesVerb is "tangsible variables [options] hostname [playbook]"'s
// own entry point; it returns the exit status.
func RunVariablesVerb(args []string) int {
	hostname, playbookArg, rest, ok := host.ParseHostArgs(args)
	if !ok {
		fmt.Fprintln(os.Stderr, config.UsageLine(config.VerbVariables))
		return 2
	}
	playbook := playbookArg
	if playbook == "" {
		playbook, _ = config.ResolvePlaybook()
		if _, err := os.Stat(playbook); err != nil {
			playbook = "" // no playbook: only the inventory side is listed
		}
	}
	hosts, err := inventory.ListInventoryHosts(rest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't list the inventory hosts: %v\n", err)
		return 1
	}
	start := indexOf(hosts, hostname)
	if start < 0 {
		fmt.Fprintf(os.Stderr, "tangsible: host %q is not in the inventory\n", hostname)
		return 1
	}
	base, _ := os.Getwd()
	s := &screen{
		hosts: hosts, idx: start, playbook: playbook, rest: rest, base: base,
		cache: map[string]*hostState{}, expanded: map[string]bool{},
	}
	s.run()
	return 0
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// hostState is what is known about one host: loading, failed or read.
type hostState struct {
	loading bool
	err     error
	res     varscrawl.Result
}

// screen is the whole program. Everything but the crawl itself runs on
// tview's event loop (QueueUpdateDraw), so nothing here locks.
type screen struct {
	hosts    []string
	idx      int
	playbook string
	rest     []string
	base     string

	cache    map[string]*hostState
	expanded map[string]bool // by variable name, kept across hosts

	app    *tview.Application
	pages  *tview.Pages
	list   *uikit.TreeList
	header *tview.TextView
	footer *tview.TextView
	help   *uikit.HelpOverlay

	rows       []rowRef
	rebuilding bool
	lastWidth  int
	screenH    int
	picking    bool
	quitting   atomic.Bool
	noticeGen  int

	// The file view (filepane.go): detail is the open one, viewing says it is
	// on screen, shown which definition it is for (so it can be found again
	// when the files are read again).
	main        tview.Primitive
	divider     *tview.Box
	dividerDrag uikit.DividerDrag
	detail      *fileView
	viewing     bool
	shown       definitionKey
}

// definitionKey names a definition by what the user saw, not by position:
// the variable and its place in that variable's list.
type definitionKey struct {
	name string
	def  int
}

// fileView is the file pane: one tab with the file, searchable and copyable.
type fileView struct {
	flex   tview.Primitive
	tabs   *uikit.TabbedPane
	header *tview.TextView
	search *uikit.TabSearchBar
	path   string
}

func (s *screen) hostName() string { return s.hosts[s.idx] }

func (s *screen) state() *hostState { return s.cache[s.hostName()] }

// load starts reading the current host's variables unless they are known.
func (s *screen) load(force bool) {
	name := s.hostName()
	if st := s.cache[name]; st != nil && !force && !st.loading {
		return
	}
	st := &hostState{loading: true}
	s.cache[name] = st
	go func() {
		res, err := varscrawl.Run(name, s.playbook, s.rest)
		if s.quitting.Load() {
			return
		}
		s.app.QueueUpdateDraw(func() {
			if s.cache[name] != st {
				return // reloaded meanwhile
			}
			st.loading, st.err, st.res = false, err, res
			if name == s.hostName() {
				s.rebuild()
				if s.viewing {
					s.refreshFile()
				}
			}
		})
	}()
}

// cursorKey identifies the row under the cursor by what it shows, so the
// cursor survives a rebuild (rows are rebuilt on every move: the cursor's
// styling is part of the row text).
type cursorKey struct {
	name string
	def  int // -1 for the variable's own row
	info bool
}

func (s *screen) keyAt(i int) (cursorKey, bool) {
	st := s.state()
	if st == nil || st.loading || st.err != nil || i < 0 || i >= len(s.rows) {
		return cursorKey{}, false
	}
	r := s.rows[i]
	if r.variable >= len(st.res.Variables) || (r.kind == rowInfo && r.text == "") {
		return cursorKey{}, false
	}
	name := st.res.Variables[r.variable].Name
	switch r.kind {
	case rowVariable:
		return cursorKey{name, -1, false}, true
	case rowDefinition:
		return cursorKey{name, r.def, false}, true
	}
	return cursorKey{name, -1, true}, true
}

func (s *screen) rebuild() {
	s.rebuilding = true
	defer func() { s.rebuilding = false }()

	var want cursorKey
	haveWant := false
	if cur := s.list.GetCurrentItem(); cur >= 0 {
		want, haveWant = s.keyAt(cur)
	}
	offset := s.list.GetOffset()
	selectedIdx := s.list.GetCurrentItem()

	st := s.state()
	s.list.Clear()
	s.rows = nil
	s.updateHeader()
	width := max(s.listWidth(), 40)
	add := func(text string, selected func()) {
		s.list.AddItem(text, selected)
	}
	switch {
	case st == nil || st.loading:
		s.rows = []rowRef{{kind: rowInfo, text: "reading the variables of " + s.hostName() + " ..."}}
		add(infoRow(s.rows[0].text, false), nil)
		return
	case st.err != nil:
		for _, line := range strings.Split(strings.TrimSpace(st.err.Error()), "\n") {
			s.rows = append(s.rows, rowRef{kind: rowInfo, text: line})
			add(styled(false, seg{"  " + line, uikit.OutcomeFailedColor}), nil)
		}
		return
	}

	vars := st.res.Variables
	cols := measure(vars, s.base, width)
	s.rows = flatten(vars, s.expanded)
	for _, n := range notes(st.res, s.playbook) {
		s.rows = append(s.rows, rowRef{kind: rowInfo, variable: len(vars), text: n})
	}
	selected := -1
	for i, r := range s.rows {
		if haveWant {
			if k, ok := s.keyAtRow(r, vars); ok && k == want && selected < 0 {
				selected = i
			}
		}
	}
	if selected < 0 {
		selected = min(max(selectedIdx, 0), len(s.rows)-1)
	}
	for i, r := range s.rows {
		i, r := i, r
		isSel := i == selected
		switch r.kind {
		case rowVariable:
			v := vars[r.variable]
			add(variableRow(v, s.expanded[v.Name], isSel, cols, s.base), func() { s.toggle(v.Name) })
		case rowDefinition:
			v := vars[r.variable]
			add(definitionRow(v.Defs[r.def], r.def == 0, isSel, cols, s.base), func() { s.openFile(definitionKey{v.Name, r.def}) })
		default:
			add(infoRow(r.text, isSel), nil)
		}
	}
	s.list.SetOffset(offset)
	s.list.SetCurrentItem(selected)
}

// keyAtRow is keyAt for a row not yet added to s.rows.
func (s *screen) keyAtRow(r rowRef, vars []varscrawl.Variable) (cursorKey, bool) {
	if r.variable >= len(vars) {
		return cursorKey{}, false
	}
	name := vars[r.variable].Name
	switch r.kind {
	case rowVariable:
		return cursorKey{name, -1, false}, true
	case rowDefinition:
		return cursorKey{name, r.def, false}, true
	}
	return cursorKey{name, -1, true}, r.text != ""
}

// notes are the lines under the list about what the crawl could not look at.
func notes(res varscrawl.Result, playbook string) []string {
	var out []string
	if playbook == "" {
		out = append(out, "no playbook: role variables are not listed")
	}
	out = append(out, res.Notes...)
	return out
}

func (s *screen) toggle(name string) {
	s.expanded[name] = !s.expanded[name]
	s.rebuild()
}

func (s *screen) setAll(open bool) {
	if st := s.state(); st != nil && !st.loading && st.err == nil {
		for _, v := range st.res.Variables {
			s.expanded[v.Name] = open
		}
		s.rebuild()
	}
}

func (s *screen) updateHeader() {
	text := fmt.Sprintf(" Variables: %s  (%d of %d)", s.hostName(), s.idx+1, len(s.hosts))
	if st := s.state(); st != nil && !st.loading && st.err == nil {
		text += fmt.Sprintf("  %d variables", len(st.res.Variables))
		if len(st.res.Plays) > 0 {
			text += "  plays: " + strings.Join(st.res.Plays, ", ")
		}
	}
	if s.playbook != "" {
		text += "  " + s.playbook
	}
	s.header.SetText(tview.Escape(text) + " ")
}

func (s *screen) setHost(i int) {
	if i < 0 || i >= len(s.hosts) || i == s.idx {
		return
	}
	s.idx = i
	s.list.SetOffset(0)
	s.load(false)
	s.rebuild()
	s.list.SetCurrentItem(0)
}

// notify shows msg in the bottom bar for a few seconds.
func (s *screen) notify(msg string) {
	s.noticeGen++
	gen := s.noticeGen
	s.footer.SetText(" " + tview.Escape(msg) + " ")
	time.AfterFunc(4*time.Second, func() {
		if s.quitting.Load() {
			return
		}
		s.app.QueueUpdateDraw(func() {
			if s.noticeGen == gen {
				s.footer.SetText(treeBar)
			}
		})
	})
}

// reload forgets everything read and reads the current host again (any host's
// variables may depend on a file just edited).
func (s *screen) reload() {
	s.cache = map[string]*hostState{}
	s.load(true)
	s.rebuild()
}

// edit opens the file the cursor row's definition is in; when the editor
// returns the files are read again.
func (s *screen) edit() {
	st := s.state()
	if st == nil || st.loading || st.err != nil {
		return
	}
	var d varscrawl.Definition
	if s.viewing {
		def, ok := s.definitionOf(s.shown)
		if !ok {
			return
		}
		d = def
	} else {
		cur := s.list.GetCurrentItem()
		if cur < 0 || cur >= len(s.rows) {
			return
		}
		r := s.rows[cur]
		if r.kind == rowInfo || r.variable >= len(st.res.Variables) {
			return
		}
		v := st.res.Variables[r.variable]
		d = v.Defs[0]
		if r.kind == rowDefinition {
			d = v.Defs[r.def]
		}
	}
	if d.Path == "" {
		s.notify("this definition is not in a file (" + locationLabel(d, s.base) + ")")
		return
	}
	s.app.Suspend(func() {
		cmd := exec.Command(template.PreferredEditor(), d.Path)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		_ = cmd.Run()
	})
	s.reload()
}

// listWidth is how many columns the list has: the whole terminal, or - with
// the file pane beside it - the tree's share, which the rows are laid out for.
func (s *screen) listWidth() int {
	if s.viewing && s.lastWidth >= uikit.SplitMinTotalWidth {
		return s.dividerDrag.TreeWidth(s.lastWidth)
	}
	return s.lastWidth
}

// definitionOf finds the definition k names in the current host's variables.
func (s *screen) definitionOf(k definitionKey) (varscrawl.Definition, bool) {
	st := s.state()
	if st == nil || st.loading || st.err != nil {
		return varscrawl.Definition{}, false
	}
	for _, v := range st.res.Variables {
		if v.Name == k.name && k.def < len(v.Defs) {
			return v.Defs[k.def], true
		}
	}
	return varscrawl.Definition{}, false
}

// openFile shows the file the definition k names is in, scrolled to it (Enter
// on a definition row).
func (s *screen) openFile(k definitionKey) {
	d, ok := s.definitionOf(k)
	if !ok {
		return
	}
	if d.Path == "" {
		s.notify("this definition is not in a file (" + locationLabel(d, s.base) + ")")
		return
	}
	s.shown = k
	s.showFile(d)
	s.viewing = true
	s.layout(true)
}

// showFile (re)builds the file pane for d.
func (s *screen) showFile(d varscrawl.Definition) {
	label := locationLabel(d, s.base)
	file := label
	if i := strings.LastIndex(file, ":"); i > 0 {
		file = file[:i]
	}
	text := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	text.SetTextColor(tcell.GetColor(uikit.StandardTextColor))
	text.SetText(renderFile(d.Path, d.Line))
	text.ScrollTo(scrollOffset(d.Line-1, s.screenH-3), 0)

	tabs := uikit.NewTabbedPane()
	tabs.SetTabs([]string{file}, []tview.Primitive{text})
	header := tview.NewTextView().SetDynamicColors(true)
	header.SetTextStyle(uikit.BarStyle)
	header.SetText(" " + tview.Escape(fmt.Sprintf("%s  -  %s, %s", s.shown.name, levelLabel(d), label)) + " ")
	search := uikit.NewTabSearchBar(s.app, tabs, fileBar, tabs.Primitive())
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(tabs.Primitive(), 0, 1, true).
		AddItem(search.Primitive(), 1, 0, false)
	if s.detail != nil {
		s.detail.tabs.Clear() // a stale focus flag would eat the next dialog's keys
	}
	s.detail = &fileView{flex: flex, tabs: tabs, header: header, search: search, path: d.Path}
}

// refreshFile shows the same definition again after the files were read
// again (its line may have moved); the pane closes if it is gone.
func (s *screen) refreshFile() {
	d, ok := s.definitionOf(s.shown)
	if !ok || d.Path == "" {
		s.closeFile()
		return
	}
	s.showFile(d)
	s.layout(false)
}

func (s *screen) closeFile() {
	if !s.viewing {
		return
	}
	s.detail.search.CloseComposing()
	s.detail.tabs.Clear()
	s.viewing = false
	s.layout(true)
}

// layout puts the file pane beside the list when the terminal is wide enough
// (the two-pane layout of the other views), over it when not, or takes it away.
// focus says whether the pane (or the list, once it is gone) takes the keyboard.
func (s *screen) layout(focus bool) {
	s.arrange(focus)
	s.rebuild() // the list may have a different width now
}

func (s *screen) arrange(focus bool) {
	_, _, w, _ := s.pages.GetInnerRect()
	if !s.viewing {
		s.dividerDrag.Cancel()
		s.pages.RemovePage("split")
		s.pages.RemovePage("detail")
		s.pages.SwitchToPage("main")
		if focus {
			s.app.SetFocus(s.list)
		}
		return
	}
	if w >= uikit.SplitMinTotalWidth {
		split := tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(s.main, s.dividerDrag.TreeWidth(w), 0, false).
			AddItem(s.divider, uikit.SplitDividerWidth, 0, false).
			AddItem(s.detail.flex, 0, 1, true)
		s.pages.RemovePage("detail")
		s.pages.RemovePage("split")
		s.pages.AddPage("split", split, true, true)
		s.pages.SwitchToPage("split")
	} else {
		s.pages.RemovePage("split")
		s.pages.RemovePage("detail")
		s.pages.AddPage("detail", s.detail.flex, true, true)
		s.pages.SwitchToPage("detail")
	}
	if focus {
		s.app.SetFocus(s.detail.tabs.Primitive())
	}
}

// openPicker shows the host list as a modal; Enter picks, Esc cancels.
func (s *screen) openPicker() {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetMainTextColor(tcell.GetColor(uikit.StandardTextColor))
	list.SetSelectedTextColor(tcell.GetColor(uikit.SelectedRowTextColor))
	list.SetSelectedBackgroundColor(tcell.GetColor(uikit.SelectedRowBackgroundColor))
	list.SetBorder(true).SetTitle(" host ").SetTitleColor(tcell.GetColor(uikit.StandardTextColor))
	for i, h := range s.hosts {
		i := i
		list.AddItem(h, "", 0, func() {
			s.closePicker()
			s.setHost(i)
		})
	}
	list.SetCurrentItem(s.idx)
	height := min(len(s.hosts)+2, 20)
	s.pages.RemovePage("picker")
	s.pages.AddPage("picker", uikit.CenteredModal(list, 40, height), true, true)
	s.picking = true
	s.app.SetFocus(list)
}

func (s *screen) closePicker() {
	s.picking = false
	s.pages.RemovePage("picker")
	s.app.SetFocus(s.list)
}

func (s *screen) run() {
	s.app = tview.NewApplication()
	s.app.EnableMouse(true)
	s.list = uikit.NewTreeList()
	s.header = tview.NewTextView().SetDynamicColors(true)
	s.header.SetTextStyle(uikit.BarStyle)
	s.footer = tview.NewTextView().SetDynamicColors(true).SetText(treeBar)
	s.footer.SetTextStyle(uikit.BarStyle)
	main := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.header, 1, 0, false).
		AddItem(s.list, 0, 1, true).
		AddItem(s.footer, 1, 0, false)
	s.main = main
	s.divider = tview.NewBox().SetBackgroundColor(tcell.ColorNavy)
	s.pages = tview.NewPages().AddPage("main", main, true, true)
	s.help = uikit.NewHelpOverlay(s.app, s.pages)

	s.list.SetChangedFunc(func(int) {
		if !s.rebuilding {
			s.rebuild()
		}
	})
	s.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		w, h := screen.Size()
		s.screenH = h
		if w != s.lastWidth {
			s.lastWidth = w
			s.rebuild()
			if s.viewing {
				s.layout(false) // the split may have become (or stopped being) wide enough
			}
		}
		return false
	})
	s.app.SetMouseCapture(s.handleMouse)
	s.app.SetInputCapture(s.handleKey)
	s.load(false)
	s.rebuild()

	s.app.SetRoot(s.pages, true).SetFocus(s.list)
	if err := s.app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
	}
	s.quitting.Store(true)
}

// handleKey is the whole key dispatch, in the order that matters: help,
// Ctrl-C, the host picker, the aliases, then the screen's own keys.
func (s *screen) handleKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyCtrlC {
		s.quitting.Store(true)
		s.app.Stop()
		return nil
	}
	if s.help.HandleKey(event) {
		return nil
	}
	if s.viewing && s.detail.search.IsComposing() {
		s.detail.search.HandleComposingKey(event)
		return nil
	}
	if s.picking {
		if event.Key() == tcell.KeyEscape || event.Rune() == 'q' {
			s.closePicker()
			return nil
		}
		return event
	}
	if translated, ok := uikit.TranslateNavAlias(event); ok {
		return translated
	}
	if s.viewing {
		return s.handleFileKey(event)
	}
	switch {
	case uikit.IsHelpKey(event):
		s.help.Open(treeHelp, nil)
		return nil
	case event.Rune() == 'q':
		s.quitting.Store(true)
		s.app.Stop()
		return nil
	case event.Key() == tcell.KeyRight:
		s.setHost(s.idx + 1)
		return nil
	case event.Key() == tcell.KeyLeft:
		s.setHost(s.idx - 1)
		return nil
	case event.Rune() == 'h':
		s.openPicker()
		return nil
	case event.Rune() == 'e':
		s.edit()
		return nil
	case event.Rune() == 'R':
		s.reload()
		s.notify("reading the files again ...")
		return nil
	case event.Rune() == 'E':
		s.setAll(true)
		return nil
	case event.Rune() == 'C':
		s.setAll(false)
		return nil
	}
	return event
}

// handleFileKey is the keys while the file pane is open: back, search, copy,
// edit; everything else (the arrows, paging) falls through to whichever of the
// pane and the list has the keyboard.
func (s *screen) handleFileKey(event *tcell.EventKey) *tcell.EventKey {
	search := s.detail.search
	switch {
	case uikit.IsHelpKey(event):
		s.help.Open(fileHelp, nil)
		return nil
	case event.Key() == tcell.KeyEscape && search.HasActive():
		search.Clear()
		return nil
	case event.Key() == tcell.KeyEscape, event.Rune() == 'q':
		s.closeFile()
		return nil
	case event.Rune() == 'e':
		s.edit()
		return nil
	case event.Rune() == 'y':
		search.ShowMessage(uikit.CopyActiveTabStatus(s.detail.tabs))
		return nil
	case event.Rune() == '/':
		search.Open()
		return nil
	case event.Rune() == 'n':
		search.Next()
		return nil
	case event.Rune() == 'N':
		search.Prev()
		return nil
	}
	return event
}

// handleMouse: the bars are not clickable, the divider can be dragged, and a
// click on the file pane's tab bar is the tab's.
func (s *screen) handleMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if event == nil {
		return nil, action
	}
	if ev, act, handled := s.help.HandleMouse(event, action); handled {
		return ev, act
	}
	x, y := event.Position()
	if !s.viewing {
		if uikit.InRect(x, y, s.header) || uikit.InRect(x, y, s.footer) {
			return nil, action
		}
		return event, action
	}
	_, _, w, _ := s.pages.GetInnerRect()
	if w >= uikit.SplitMinTotalWidth {
		if ev, handled := s.dividerDrag.HandleMouse(event, action, s.divider, 0, w, func() { s.layout(false) }); handled {
			return ev, action
		}
	}
	if uikit.InRect(x, y, s.detail.header) || uikit.InRect(x, y, s.detail.search.Primitive()) {
		return nil, action
	}
	if action == tview.MouseLeftClick && s.detail.tabs.HandleClick(x, y) {
		return nil, action
	}
	if w >= uikit.SplitMinTotalWidth && (uikit.InRect(x, y, s.header) || uikit.InRect(x, y, s.footer)) {
		return nil, action
	}
	return event, action
}
