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

// Package variables is the Variables tab of the host, hosts and inventory
// views (design-docs/Variables.md): where each variable of one host is
// defined, as a tree of variables that expand to all their definitions in
// order of precedence; Enter on a definition shows its file in the tab. The
// data is internal/varscrawl's; Pane is the tab's body, the pure half of
// which is rows.go, render.go and filepane.go.
package variables

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"code.aw.net/claude/tangsible/internal/template"
	"code.aw.net/claude/tangsible/internal/uikit"
	"code.aw.net/claude/tangsible/internal/varscrawl"
)

// Cache is what one hosts/inventory session has read, shared by the panes it
// builds (one per visit to a host): each host's result and which variables
// are open. Only touched on tview's event loop.
type Cache struct {
	results  map[string]varscrawl.Result
	expanded map[string]bool // by variable name, kept across hosts
}

// NewCache returns an empty Cache.
func NewCache() *Cache {
	return &Cache{results: map[string]varscrawl.Result{}, expanded: map[string]bool{}}
}

// definitionKey names a definition by what the user saw, not by position: the
// variable and its place in that variable's list.
type definitionKey struct {
	name string
	def  int
}

// cursorKey identifies the row under the cursor by what it shows, so the
// cursor survives a rebuild (rows are rebuilt on every move: the cursor's
// styling is part of the row text).
type cursorKey struct {
	name string
	def  int // -1 for the variable's own row
	info bool
}

// Pane is the tab's body: the tree of variables, or - after Enter on a
// definition - the file it is in, over the same space.
type Pane struct {
	app      *tview.Application
	hostname string
	playbook string
	rest     []string
	base     string
	cache    *Cache

	loading bool
	err     error
	res     varscrawl.Result
	gen     int // bumped by every read, so a stale one is dropped

	root     *paneRoot
	info     *tview.TextView
	pages    *tview.Pages
	list     *uikit.TreeList
	plain    *tview.TextView // tree mode's text for BodyTextView (copy)
	fileText *tview.TextView

	rows       []rowRef
	rebuilding bool
	lastWidth  int
	lastHeight int
	viewing    bool
	shown      definitionKey
	notice     string // what HandleKey last asked the caller to show
}

// New builds the pane for hostname and starts reading its variables (unless
// cache already has them). playbook may be empty: only the inventory side is
// listed then. cache may be nil.
func New(app *tview.Application, hostname, playbook string, rest []string, cache *Cache) *Pane {
	if cache == nil {
		cache = NewCache()
	}
	if _, err := os.Stat(playbook); err != nil {
		playbook = "" // the default playbook may not exist: only the inventory side is listed
	}
	base, _ := os.Getwd()
	p := &Pane{app: app, hostname: hostname, playbook: playbook, rest: rest, base: base, cache: cache}
	p.list = uikit.NewTreeList()
	p.info = tview.NewTextView().SetDynamicColors(true)
	p.info.SetTextStyle(uikit.BarStyle)
	p.plain = tview.NewTextView()
	p.fileText = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	p.fileText.SetTextColor(tcell.GetColor(uikit.StandardTextColor))
	p.pages = tview.NewPages().
		AddPage("tree", p.list, true, true).
		AddPage("file", p.fileText, true, false)
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.info, 1, 0, false).
		AddItem(p.pages, 0, 1, true)
	p.root = &paneRoot{Flex: flex, p: p}
	p.list.SetChangedFunc(func(int) {
		if !p.rebuilding {
			p.rebuild()
		}
	})
	if res, ok := cache.results[hostname]; ok {
		p.res = res
	} else {
		p.load()
	}
	p.rebuild()
	return p
}

// paneRoot is the pane's primitive. It notices the width it is drawn at (the
// rows are laid out for it) and is what TabbedPane asks for the text copy and
// search work on (BodyTextView).
type paneRoot struct {
	*tview.Flex
	p *Pane
}

func (r *paneRoot) Draw(screen tcell.Screen) {
	_, _, w, h := r.Flex.GetRect()
	r.p.lastHeight = h
	if w != r.p.lastWidth {
		r.p.lastWidth = w
		r.p.rebuild()
	}
	r.Flex.Draw(screen)
}

// BodyTextView is the text the tab's copy (y) and search work on: the whole
// file while one is shown, else a plain listing of the variables.
func (r *paneRoot) BodyTextView() *tview.TextView {
	if r.p.viewing {
		return r.p.fileText
	}
	return r.p.plain
}

// Primitive is what goes into the tab bar's page.
func (p *Pane) Primitive() tview.Primitive { return p.root }

// load reads the host's variables in the background.
func (p *Pane) load() {
	p.gen++
	gen := p.gen
	p.loading, p.err = true, nil
	go func() {
		res, err := varscrawl.Run(p.hostname, p.playbook, p.rest)
		p.app.QueueUpdateDraw(func() {
			if gen != p.gen {
				return // read again meanwhile
			}
			p.loading, p.err, p.res = false, err, res
			if err == nil {
				p.cache.results[p.hostname] = res // errors are never cached
			}
			p.rebuild()
			if p.viewing {
				p.refreshFile()
			}
		})
	}()
}

func (p *Pane) keyAt(i int) (cursorKey, bool) {
	if p.loading || p.err != nil || i < 0 || i >= len(p.rows) {
		return cursorKey{}, false
	}
	return p.keyAtRow(p.rows[i], p.res.Variables)
}

// keyAtRow is keyAt for a row not yet added to p.rows.
func (p *Pane) keyAtRow(r rowRef, vars []varscrawl.Variable) (cursorKey, bool) {
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

func (p *Pane) rebuild() {
	p.rebuilding = true
	defer func() { p.rebuilding = false }()

	var want cursorKey
	haveWant := false
	if cur := p.list.GetCurrentItem(); cur >= 0 {
		want, haveWant = p.keyAt(cur)
	}
	offset := p.list.GetOffset()
	selectedIdx := p.list.GetCurrentItem()

	p.list.Clear()
	p.rows = nil
	p.updateInfo()
	width := max(p.lastWidth, 40)
	switch {
	case p.loading:
		p.rows = []rowRef{{kind: rowInfo, text: "reading the variables of " + p.hostname + " ..."}}
		p.list.AddItem(infoRow(p.rows[0].text, false), nil)
		p.plain.SetText("")
		return
	case p.err != nil:
		for _, line := range strings.Split(strings.TrimSpace(p.err.Error()), "\n") {
			p.rows = append(p.rows, rowRef{kind: rowInfo, text: line})
			p.list.AddItem(styled(false, seg{"  " + line, uikit.OutcomeFailedColor}), nil)
		}
		p.plain.SetText("")
		return
	}

	vars := p.res.Variables
	cols := measure(vars, p.base, width)
	p.rows = flatten(vars, p.cache.expanded)
	for _, n := range notes(p.res, p.playbook) {
		p.rows = append(p.rows, rowRef{kind: rowInfo, variable: len(vars), text: n})
	}
	selected := -1
	if haveWant {
		for i, r := range p.rows {
			if k, ok := p.keyAtRow(r, vars); ok && k == want {
				selected = i
				break
			}
		}
	}
	if selected < 0 {
		selected = min(max(selectedIdx, 0), len(p.rows)-1)
	}
	for i, r := range p.rows {
		r := r
		isSel := i == selected
		switch r.kind {
		case rowVariable:
			v := vars[r.variable]
			p.list.AddItem(variableRow(v, p.cache.expanded[v.Name], isSel, cols, p.base), func() { p.toggle(v.Name) })
		case rowDefinition:
			v := vars[r.variable]
			p.list.AddItem(definitionRow(v.Defs[r.def], r.def == 0, isSel, cols, p.base), func() { p.openFile(definitionKey{v.Name, r.def}) })
		default:
			p.list.AddItem(infoRow(r.text, isSel), nil)
		}
	}
	p.list.SetOffset(offset)
	p.list.SetCurrentItem(selected)
	p.plain.SetText(plainListing(vars, p.base))
}

// notes are the lines under the list about what the crawl could not look at.
func notes(res varscrawl.Result, playbook string) []string {
	var out []string
	if playbook == "" {
		out = append(out, "no playbook: role variables are not listed")
	}
	return append(out, res.Notes...)
}

func (p *Pane) updateInfo() {
	if p.viewing {
		d, ok := p.definitionOf(p.shown)
		if ok {
			p.info.SetText(" " + tview.Escape(fmt.Sprintf("%s  -  %s, %s", p.shown.name, levelLabel(d), locationLabel(d, p.base))) + " ")
		}
		return
	}
	text := " "
	if !p.loading && p.err == nil {
		text += fmt.Sprintf("%d variables", len(p.res.Variables))
		if len(p.res.Plays) > 0 {
			text += "  plays: " + strings.Join(p.res.Plays, ", ")
		}
	}
	if p.playbook != "" {
		text += "  " + p.playbook
	}
	p.info.SetText(tview.Escape(text) + " ")
}

func (p *Pane) toggle(name string) {
	p.cache.expanded[name] = !p.cache.expanded[name]
	p.rebuild()
}

func (p *Pane) setAll(open bool) {
	if !p.loading && p.err == nil {
		for _, v := range p.res.Variables {
			p.cache.expanded[v.Name] = open
		}
		p.rebuild()
	}
}

// definitionOf finds the definition k names in the variables read.
func (p *Pane) definitionOf(k definitionKey) (varscrawl.Definition, bool) {
	if p.loading || p.err != nil {
		return varscrawl.Definition{}, false
	}
	for _, v := range p.res.Variables {
		if v.Name == k.name && k.def < len(v.Defs) {
			return v.Defs[k.def], true
		}
	}
	return varscrawl.Definition{}, false
}

// openFile shows the file the definition k names is in, scrolled to it (Enter
// on a definition row).
func (p *Pane) openFile(k definitionKey) {
	d, ok := p.definitionOf(k)
	if !ok {
		return
	}
	if d.Path == "" {
		p.notice = "this definition is not in a file (" + locationLabel(d, p.base) + ")"
		p.tell()
		return
	}
	p.shown = k
	p.viewing = true
	p.showFile(d)
}

// showFile (re)fills the file page for d and brings it up.
func (p *Pane) showFile(d varscrawl.Definition) {
	p.fileText.SetText(renderFile(d.Path, d.Line))
	height := p.lastHeight - 1
	if height <= 0 {
		height = 24
	}
	p.fileText.ScrollTo(scrollOffset(d.Line-1, height), 0)
	p.updateInfo()
	p.pages.SwitchToPage("file")
	p.app.SetFocus(p.fileText)
}

// refreshFile shows the same definition again after the files were read
// again (its line may have moved); the pane goes back to the list if it is
// gone.
func (p *Pane) refreshFile() {
	d, ok := p.definitionOf(p.shown)
	if !ok || d.Path == "" {
		p.closeFile()
		return
	}
	p.showFile(d)
}

func (p *Pane) closeFile() {
	if !p.viewing {
		return
	}
	p.viewing = false
	p.pages.SwitchToPage("tree")
	p.updateInfo()
	p.app.SetFocus(p.list)
}

// tell has the host bar show p.notice for a moment, if the caller gave a way:
// HandleKey's own return value is the normal one, this is for Enter, which
// reaches the pane through the list rather than through HandleKey.
func (p *Pane) tell() {
	p.info.SetText(" " + tview.Escape(p.notice) + " ")
	time.AfterFunc(4*time.Second, func() {
		p.app.QueueUpdateDraw(func() { p.updateInfo() })
	})
}

// edit opens the file of the cursor row's definition (or of the one shown);
// when the editor returns the files are read again.
func (p *Pane) edit() string {
	if p.loading || p.err != nil {
		return ""
	}
	var d varscrawl.Definition
	if p.viewing {
		def, ok := p.definitionOf(p.shown)
		if !ok {
			return ""
		}
		d = def
	} else {
		cur := p.list.GetCurrentItem()
		if cur < 0 || cur >= len(p.rows) {
			return ""
		}
		r := p.rows[cur]
		if r.kind == rowInfo || r.variable >= len(p.res.Variables) {
			return ""
		}
		v := p.res.Variables[r.variable]
		d = v.Defs[0]
		if r.kind == rowDefinition {
			d = v.Defs[r.def]
		}
	}
	if d.Path == "" {
		return "this definition is not in a file (" + locationLabel(d, p.base) + ")"
	}
	p.app.Suspend(func() {
		cmd := exec.Command(template.PreferredEditor(), d.Path)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		_ = cmd.Run()
	})
	p.reload()
	return ""
}

// reload forgets everything read (any host's variables may depend on a file
// just edited) and reads this host again.
func (p *Pane) reload() {
	for k := range p.cache.results {
		delete(p.cache.results, k)
	}
	p.load()
	p.rebuild()
}

// HandleKey is the tab's own keys, offered to it by the host views before
// their own while the tab is showing. It reports whether it took the key and,
// when it has something to say, a line for the bar. searchActive says a
// search is going on in the tab, whose n/N then keep stepping through matches.
//
// In the list: e edits the file of the cursor row, R reads everything again,
// E/C open/close every variable; there is no search (/ says so). In a file:
// Esc goes back, e edits, and the host-hopping keys do nothing (the file is
// not the host's). Enter, the arrows and paging reach the list or the file
// through the normal focus.
func (p *Pane) HandleKey(event *tcell.EventKey, searchActive bool) (handled bool, msg string) {
	if p.viewing {
		switch {
		case event.Key() == tcell.KeyEscape:
			p.closeFile()
			return true, ""
		case event.Key() == tcell.KeyLeft, event.Key() == tcell.KeyRight:
			return true, ""
		case event.Key() == tcell.KeyRune && (event.Rune() == 'n' || event.Rune() == 'N') && !searchActive:
			return true, ""
		case event.Key() == tcell.KeyRune && event.Rune() == 'e':
			return true, p.edit()
		}
		return false, ""
	}
	if event.Key() != tcell.KeyRune {
		return false, ""
	}
	switch event.Rune() {
	case 'e':
		return true, p.edit()
	case 'R':
		p.reload()
		return true, "reading the files again ..."
	case 'E':
		p.setAll(true)
		return true, ""
	case 'C':
		p.setAll(false)
		return true, ""
	case '/':
		return true, "no search in the list - Enter on a definition shows its file, which can be searched"
	}
	return false, ""
}
