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

// Implements the "template" verb (design-docs/Tangsible template.md): a
// standalone, single-view program for interactively debugging a Jinja2
// template, entirely separate from the run/rerun/role verbs' own live
// tree UI - there's no tree to browse here, and no live jsonl-streaming
// pipeline is needed, since each render is exactly one synchronous
// ansible-playbook invocation against exactly one host.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// parseTemplateArgs splits args (everything after the "template" verb)
// into the required template path, an optional hostname, and everything
// else as passthrough args - per design-docs/Tangsible template.md's own
// syntax, "tangsible template <path to template> [<hostname>] [-e...]":
// hostname is only recognized when it's the *second* leading positional,
// immediately after the path and before any flag-shaped token - anything
// after the first flag-shaped token (or after hostname, if present) is
// rest. ok is false if no template path was given at all (a missing or
// flag-shaped first argument).
func parseTemplateArgs(args []string) (templatePath, hostname string, rest []string, ok bool) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", "", nil, false
	}
	templatePath = args[0]
	remaining := args[1:]
	if len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") {
		hostname = remaining[0]
		remaining = remaining[1:]
	}
	return templatePath, hostname, remaining, true
}

// ansibleInventoryGroup is the shape of one non-"_meta" entry in
// `ansible-inventory --list`'s own JSON output - a group's own direct
// hosts and child group names. Confirmed empirically: a host with no vars
// of its own is entirely absent from "_meta.hostvars", so that map alone
// can't be trusted as the full host list - the group tree (starting from
// "all") is the only reliably complete source.
type ansibleInventoryGroup struct {
	Hosts    []string `json:"hosts"`
	Children []string `json:"children"`
}

// flattenInventoryHosts walks raw's group tree from "all" down through
// Children, collecting every group's own Hosts into a deduplicated,
// alphabetically sorted list. Sorted deliberately, not left in whatever
// order the source happened to produce - `ansible-inventory --list` and
// `ansible ... --list-hosts` were both confirmed empirically to return
// hosts in an inventory/pattern-matching order that isn't even consistent
// between the two tools, and Go's own map iteration order is
// unspecified - "the first host" needs one well-defined answer, not
// whatever an upstream tool's internal traversal happens to produce.
func flattenInventoryHosts(raw map[string]json.RawMessage) []string {
	seen := map[string]bool{}
	visited := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if visited[name] {
			return // guards against a (malformed or cyclic) children loop
		}
		visited[name] = true
		data, ok := raw[name]
		if !ok {
			return
		}
		var g ansibleInventoryGroup
		if err := json.Unmarshal(data, &g); err != nil {
			return
		}
		for _, h := range g.Hosts {
			seen[h] = true
		}
		for _, c := range g.Children {
			walk(c)
		}
	}
	walk("all")

	hosts := make([]string, 0, len(seen))
	for h := range seen {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}

// resolveInventoryHost runs `ansible-inventory --list`, forwarding
// passthroughArgs verbatim (the same -i/-e/... args this invocation was
// given - ansible-inventory accepts a real subset of ansible-playbook's
// own flags, confirmed via `ansible-inventory --help`; passing one it
// doesn't recognize surfaces ansible's own clear error rather than
// tangsible trying to filter the list itself), and returns the first host
// per flattenInventoryHosts' own deterministic ordering. listInventoryHosts
// (host.go) does the actual invocation and JSON parsing - shared with the
// "hosts" verb's own full host listing (design-docs/HostVerb.md).
func resolveInventoryHost(passthroughArgs []string) (string, error) {
	hosts, err := listInventoryHosts(passthroughArgs)
	if err != nil {
		return "", err
	}
	if len(hosts) == 0 {
		return "", fmt.Errorf("no hosts found in the inventory")
	}
	return hosts[0], nil
}

// roleVarsFiles returns the existing defaults/main.yml and vars/main.yml
// paths (in that order, skipping whichever doesn't exist on disk) for the
// role templatePath belongs to, per rolePathPattern (tui.go) - nil if
// templatePath isn't role-owned at all. Deliberately doesn't invoke the
// role itself (roles: [name] in the stub) - confirmed live that doing so
// would run every task in the role's own tasks/main.yml as a side effect,
// which a read-only template preview must never do; vars_files loads the
// role's own default/var definitions without executing anything.
func roleVarsFiles(templatePath string) []string {
	abs, err := filepath.Abs(templatePath)
	if err != nil {
		return nil
	}
	loc := rolePathPattern.FindStringSubmatchIndex(abs)
	if loc == nil {
		return nil
	}
	// loc[0]:loc[1] is the full match ("/roles/<name>/templates/", here -
	// tasks/handlers never apply to a template path in practice); the role
	// name itself is capture group 1, loc[2]:loc[3].
	roleDir := abs[:loc[0]] + "/roles/" + abs[loc[2]:loc[3]]

	var files []string
	for _, rel := range []string{"defaults/main.yml", "vars/main.yml"} {
		p := filepath.Join(roleDir, rel)
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
	}
	return files
}

// writeTemplateStub generates a minimal playbook rendering templatePath to
// outputPath, and writes it to a fresh file in the system temp directory -
// unlike the "role" verb's own stub, this one needs no special placement
// (there's no tree/drill-down source lookup happening here to find), so a
// real temp file is fine. gather_facts is left at ansible's own default
// (unset here), same reasoning as the "role" verb's stub: templates
// commonly reference ansible_facts, and there's no reason to disable that;
// ignore_unreachable: true means a host that can't be reached for fact
// gathering doesn't abort the play - the delegated template task (below)
// still runs afterward using whatever vars the host does have, just
// without live facts. Confirmed live: an unreachable host's own "Gathering
// Facts" step prints "...ignoring" and the play continues normally, exit
// 0, rather than failing the whole render over a host being down - this
// tool exists to debug a template, not to test host reachability.
//
// The template task itself always runs with delegate_to: localhost -
// confirmed live this was a real, not hypothetical, bug: without it, the
// task (and its dest: write) executes on whatever host the play targets,
// over its real connection, so the rendered file only ever lands on the
// *target* host's filesystem, never locally where tangsible can read it
// back - "worked" only when the selected host happened to be the same
// machine tangsible itself runs on, and silently produced nothing for any
// other host. delegate_to only changes *where* the module executes, not
// whose variables are in scope - inventory_hostname, host_vars, group_vars
// and any vars_files above all still resolve to the play's own targeted
// host, confirmed live against a deliberately-unreachable fake host whose
// own vars still came through correctly once delegated. The jsonl event
// for the delegated task is still keyed by the play's own host, not
// "localhost" (also confirmed live), so renderTemplate's own
// ev.Hosts[hostname] lookup needs no special-casing for this.
//
// Always targets hosts: all, never a specific host - the actual target is
// narrowed at render time via --limit (see renderTemplate), not baked
// into the stub itself. This is what makes the stub genuinely reusable,
// unchanged, across every host switch in an interactive session (per
// design-docs/Tangsible template.md's Cleanup section) - an earlier
// version generated hosts: <hostname> once at startup, which meant
// switching hosts via 'h' only changed which host's JSON event
// renderTemplate looked for, while the play itself kept only ever
// targeting the original host - a real bug caught live (switching to a
// second, equally valid inventory host produced "no result reported for
// host ...", since that host's own task genuinely never ran).
func writeTemplateStub(templatePath, outputPath string) (stubPath string, err error) {
	absTemplate, err := filepath.Abs(templatePath)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("- hosts: all\n")
	b.WriteString("  ignore_unreachable: true\n")
	if varsFiles := roleVarsFiles(templatePath); len(varsFiles) > 0 {
		b.WriteString("  vars_files:\n")
		for _, f := range varsFiles {
			fmt.Fprintf(&b, "    - %s\n", f)
		}
	}
	b.WriteString("  tasks:\n")
	b.WriteString("    - name: render template\n")
	b.WriteString("      delegate_to: localhost\n")
	b.WriteString("      ansible.builtin.template:\n")
	fmt.Fprintf(&b, "        src: %s\n", absTemplate)
	fmt.Fprintf(&b, "        dest: %s\n", outputPath)

	f, err := os.CreateTemp("", "tangsible-template-*.yml")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// templateResult is one render's own outcome - either Content (the
// rendered file, read back from disk) or, if the task itself failed,
// ErrMsg (extracted from the task's own result the same way
// primaryOutputField already does for the drill-down view - reused
// directly, not reimplemented).
type templateResult struct {
	Content string
	Failed  bool
	ErrMsg  string
}

// renderTemplate runs stubPath synchronously, narrowed to hostname via
// --limit (the stub itself always targets hosts: all - see
// writeTemplateStub), forwarding rest (the same passthrough args given on
// the command line) alongside it, and reports the outcome. err is
// non-nil only when nothing usable could be determined at all (e.g. a bad
// inventory/host - ansible-playbook's own pre-flight failing before any
// real event fires, mirroring the same class of failure run's own
// pre-flight gate exists for) - a genuine task failure (a Jinja error,
// most commonly) is reported via templateResult.Failed instead, not err,
// since that's the normal, expected outcome this whole tool exists to
// surface.
func renderTemplate(stubPath, outputPath, hostname string, rest []string) (templateResult, error) {
	args := append([]string{stubPath, "--limit", hostname}, rest...)
	cmd := exec.Command("ansible-playbook", args...)
	cmd.Env = append(os.Environ(),
		"ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl",
		"ANSIBLE_JSON_INDENT=0",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()

	var raw json.RawMessage
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev rawEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch ev.Event {
		case "v2_runner_on_ok", "v2_runner_on_failed", "v2_runner_on_unreachable":
			if hostRaw, ok := ev.Hosts[hostname]; ok {
				raw = hostRaw
			}
		}
	}

	if raw == nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		if msg == "" {
			msg = fmt.Sprintf("no result reported for host %q - check that it resolves in the inventory", hostname)
		}
		return templateResult{}, fmt.Errorf("%s", msg)
	}

	decoded := decodeHostResult(raw)
	if decoded.Failed || decoded.Unreachable {
		var full map[string]interface{}
		_ = json.Unmarshal(raw, &full)
		_, msg := primaryOutputField(full)
		if msg == "" {
			msg = decoded.Msg
		}
		return templateResult{Failed: true, ErrMsg: msg}, nil
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		return templateResult{Failed: true, ErrMsg: fmt.Sprintf("the template task reported success but the rendered file couldn't be read: %v", err)}, nil
	}
	return templateResult{Content: string(content)}, nil
}

// preferredEditor implements the standard Unix convention: $VISUAL, then
// $EDITOR, then a bare "vi" as the last-resort default.
func preferredEditor() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	return "vi"
}

// runTemplateVerb is main.go's entry point for the "template" verb.
// Returns the process exit code rather than calling os.Exit itself, so
// its own deferred cleanup (the stub playbook and the rendered-output
// scratch file, per design-docs/Tangsible template.md's Cleanup section)
// reliably runs on every path - os.Exit skips deferred functions, so
// main.go calls os.Exit on the returned code only after this function has
// already returned.
func runTemplateVerb(args []string) int {
	templatePath, hostname, rest, ok := parseTemplateArgs(args)
	if !ok {
		fmt.Fprintf(os.Stderr, "usage: %s template <path to template> [<hostname>] [ansible-playbook args...]\n", os.Args[0])
		return 2
	}
	if _, err := os.Stat(templatePath); err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't read template %q: %v\n", templatePath, err)
		return 1
	}

	if hostname == "" {
		h, err := resolveInventoryHost(rest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: couldn't resolve a host from the inventory: %v\n", err)
			return 1
		}
		hostname = h
	}

	outFile, err := os.CreateTemp("", "tangsible-template-out-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create a scratch output file: %v\n", err)
		return 1
	}
	outputPath := outFile.Name()
	outFile.Close()
	defer os.Remove(outputPath)

	stubPath, err := writeTemplateStub(templatePath, outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook: %v\n", err)
		return 1
	}
	defer os.Remove(stubPath)

	runTemplateTUI(templatePath, stubPath, outputPath, hostname, rest)
	return 0
}

// runTemplateTUI builds and runs the standalone single-view program
// design-docs/Tangsible template.md describes: a thin header (template
// path + current hostname, the same "outputTopBar" pattern the existing
// drill-down view already uses), a two-tab body (design-docs/Tabbed
// UI.md) - "Source" (the template file's own raw content) and "Rendered"
// (the render's own result, or its error message in place of content on
// failure, exactly like this view's original single-body behavior, just
// relocated into a tab) - and a bottom keybinding-hint bar. No
// Pages("main")/tree underneath any of this, since there's nothing here
// to navigate back to.
func runTemplateTUI(templatePath, stubPath, outputPath, initialHost string, rest []string) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	header := tview.NewTextView().SetDynamicColors(true)
	header.SetTextStyle(barStyle)

	sourceView := tview.NewTextView().SetDynamicColors(true)
	renderedView := tview.NewTextView().SetDynamicColors(true)

	tabs := newTabbedPane()
	tabs.SetTabs([]string{"Rendered", "Source"}, []tview.Primitive{renderedView, sourceView})

	footer := tview.NewTextView().SetDynamicColors(true).
		SetText(" tab/shift-tab: switch tab  e: edit template  h: change host  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom ")
	footer.SetTextStyle(barStyle)

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(tabs.Primitive(), 0, 1, true).
		AddItem(footer, 1, 0, false)

	pages := tview.NewPages().AddPage("main", flex, true, true)

	currentHost := initialHost

	setHeader := func() {
		header.SetText(fmt.Sprintf(" Template: %s   Host: %s ", tview.Escape(templatePath), tview.Escape(currentHost)))
	}
	setHeader()
	renderedView.SetText("Rendering...")

	// refreshSource re-reads the template file itself into the Source
	// tab - called both up front and every time 'e' returns from the
	// editor, since that's the one thing that can actually change the
	// file's own content (switching hosts or reprocessing never does).
	refreshSource := func() {
		data, err := os.ReadFile(templatePath)
		if err != nil {
			sourceView.SetText("[red]" + tview.Escape(err.Error()) + "[-]")
			return
		}
		sourceView.SetText(tview.Escape(string(data)))
		sourceView.ScrollToBeginning()
	}
	refreshSource()

	// render runs one synchronous ansible-playbook invocation on its own
	// goroutine (so a slow render - a large template, slow fact-gathering,
	// ...  - never freezes the event loop or its own "Rendering..."
	// placeholder from ever being drawn) and updates the view via
	// QueueUpdateDraw once it's done, same pattern requestRerun's own
	// generations already use elsewhere.
	render := func() {
		result, err := renderTemplate(stubPath, outputPath, currentHost, rest)
		app.QueueUpdateDraw(func() {
			setHeader()
			switch {
			case err != nil:
				renderedView.SetText("[red::b]Error[-::-]\n\n" + tview.Escape(err.Error()))
			case result.Failed:
				renderedView.SetText("[red::b]Error[-::-]\n\n" + tview.Escape(result.ErrMsg))
			default:
				renderedView.SetText(tview.Escape(result.Content))
			}
			renderedView.ScrollToBeginning()
		})
	}
	go render()

	// Host-switch dialog: a single-field modal, same centeredModal/Pages
	// overlay pattern NewLiveTUI's own filter/search dialogs use - first
	// iteration is plain text entry, per design-docs/Tangsible
	// template.md's own "Changing hosts" section (a picker list gleaned
	// from the inventory is a natural later improvement, not required
	// here).
	hostInput := tview.NewInputField().SetLabel("Host: ")
	// A real tview.NewBox() for every spacer item below, not a bare nil -
	// see tui.go's NewLiveTUI (searchDialogFlex/filterFlex) for why: a nil
	// Flex item draws nothing, so nothing ever repaints its cells over
	// whatever this page's own content drew underneath there - a real
	// Box's Draw() fills its own rect with the dialog's background even
	// though it shows no content of its own.
	hostFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox(), 1, 0, false). // top margin
		AddItem(hostInput, 1, 0, true)
	hostFlex.SetBorder(true).SetTitle(" Change host (enter: apply, esc: cancel) ")
	pages.AddPage("host", centeredModal(hostFlex, 50, 7), true, false)

	hostDialogOpen := false
	openHostDialog := func() {
		hostDialogOpen = true
		hostInput.SetText(currentHost)
		pages.ShowPage("host")
		app.SetFocus(hostInput)
	}
	closeHostDialog := func() {
		hostDialogOpen = false
		pages.HidePage("host")
		app.SetFocus(tabs.Primitive())
	}
	// applyHostChange is the Enter/Apply-button shared body - switches to
	// whatever's currently typed into hostInput (a no-op if it's empty or
	// unchanged) and re-renders, but does not itself close the dialog:
	// both callers below do that as their own last step.
	applyHostChange := func() {
		if v := strings.TrimSpace(hostInput.GetText()); v != "" && v != currentHost {
			currentHost = v
			setHeader()
			renderedView.SetText("Rendering...")
			go render()
		}
	}
	hostInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			applyHostChange()
		}
		closeHostDialog()
	})

	// Mouse-only Apply/Cancel buttons, same reasoning as the main app's own
	// searchDialogFlex buttons (tui.go's NewLiveTUI): hostInput already
	// repurposes Enter/Esc away from their native meaning via SetDoneFunc
	// above, so this stays a plain Flex + tview.Button rather than a
	// tview.Form, to avoid Form.Focus() silently overwriting the
	// InputField's own SetFinishedFunc and double-firing on the same
	// keypress. Click-only, not Tab-reachable - Enter/Esc already fully
	// cover the keyboard path.
	hostApplyButton := tview.NewButton("Apply").SetSelectedFunc(func() {
		applyHostChange()
		closeHostDialog()
	})
	hostCancelButton := tview.NewButton("Cancel").SetSelectedFunc(closeHostDialog)
	// Cancel-left/Apply-right, right-aligned (a single flexible spacer on
	// the left pushes both buttons flush against a small fixed right
	// margin) - matches the main app's own rerun dialog buttons
	// (tui.go's NewLiveTUI) and the general "affirmative action on the
	// right" convention.
	hostFlex.
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(tview.NewFlex().
							AddItem(tview.NewBox(), 0, 1, false).
							AddItem(hostCancelButton, 10, 0, false).
							AddItem(tview.NewBox(), 2, 0, false).
							AddItem(hostApplyButton, 9, 0, false).
							AddItem(tview.NewBox(), 1, 0, false), 1, 0, false).
		AddItem(tview.NewBox(), 1, 0, false) // bottom margin

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if hostDialogOpen {
			// Modal like the main app's own search dialog: every key but
			// Esc passes straight through to the InputField's native
			// editing (a hostname could in principle contain a letter
			// that's also a shortcut elsewhere, e.g. "e" or "h" - though
			// unusual for a hostname, the same reasoning still applies).
			if event.Key() == tcell.KeyEscape {
				closeHostDialog()
				return nil
			}
			return event
		}
		switch {
		// Deliberately no KeyEscape case here (unlike Ctrl-C) - Esc used to
		// quit identically to q/Ctrl-C, but that's an easy accidental hit
		// while just browsing the rendered/source tabs; only q and Ctrl-C
		// quit now. Falls through to `return event` at the bottom, which
		// tview.TextView (sourceView/renderedView) with no SetDoneFunc of
		// its own just no-ops on - not a dead binding, just genuinely
		// inert.
		case event.Key() == tcell.KeyCtrlC:
			app.Stop()
			return nil
		case event.Rune() == 'q':
			app.Stop()
			return nil
		case event.Key() == tcell.KeyCtrlA:
			// Not natively handled by tview.TextView (unlike Home/End,
			// which it does handle directly) - translated the same way
			// tui.go's own universal key aliases do, so the footer's
			// "CTRL-A/E: top/bottom" claim is actually true here too.
			return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlE:
			return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
		case event.Key() == tcell.KeyTab:
			// design-docs/Tabbed UI.md: Tab/Backtab are tview.TextView's
			// own default "done key" set (they'd otherwise back out of
			// this view entirely, the same incidental behavior the
			// drill-down view's own SetDoneFunc already documents as
			// harmless there - it isn't harmless here, since these two
			// keys are now this view's own deliberate tab-switching
			// gesture instead), so they're intercepted here before ever
			// reaching either TextView.
			tabs.Next()
			return nil
		case event.Key() == tcell.KeyBacktab:
			tabs.Prev()
			return nil
		case event.Rune() == 'e':
			// Suspend hands the real terminal to the editor as a normal
			// foreground process (tview.Application.Suspend already does
			// the tcell Screen.Suspend/Resume dance) - the first real use
			// case in this codebase for that primitive, previously only
			// noted as "the right one if a mid-run prompt is ever made to
			// work" (CLAUDE.md). Always reprocesses once the editor exits,
			// whether or not anything was actually saved - simplest, and
			// matches how e.g. git commit/crontab -e behave.
			app.Suspend(func() {
				cmd := exec.Command(preferredEditor(), templatePath)
				cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
				_ = cmd.Run()
			})
			refreshSource()
			renderedView.SetText("Rendering...")
			go render()
			return nil
		case event.Rune() == 'h':
			openHostDialog()
			return nil
		}
		return event
	})

	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if event == nil {
			// See tui.go's own SetMouseCapture for why this guard exists -
			// tview's fireMouseActions can invoke this callback again
			// within the same physical mouse event with a nil event, once
			// an earlier call in the same batch already returned nil.
			return nil, action
		}
		if hostDialogOpen {
			// hostFlex holds a real tview.InputField (hostInput) with its
			// own native click-to-position-cursor handling - a click
			// inside the dialog's own box is let through unchanged so
			// Pages' own dispatch (confirmed against tview's pages.go: it
			// tries every visible page, topmost first) reaches it
			// naturally; anything outside the box is swallowed so it
			// can't leak through to the tab bar underneath (same
			// reasoning as tui.go's own dialog handling in
			// SetMouseCapture).
			if x, y := event.Position(); inRect(x, y, hostFlex) {
				return event, action
			}
			return nil, action
		}
		// header/footer are plain, non-interactive TextViews - swallow a
		// click there before it can reach TextView's own default
		// MouseLeftDown handling, which would otherwise silently move
		// keyboard focus onto a one-line status bar (same focus-steal
		// issue as tui.go's topBar/bottomBar/outputTopBar/
		// outputBottomBar - confirmed live there that Escape/Enter/arrow
		// navigation then silently stop reaching the real content).
		if x, y := event.Position(); inRect(x, y, header) || inRect(x, y, footer) {
			return nil, action
		}
		if action == tview.MouseLeftClick {
			if x, y := event.Position(); tabs.HandleClick(x, y) {
				return nil, action
			}
		}
		return event, action
	})

	app.SetRoot(pages, true).SetFocus(tabs.Primitive())
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
	}
}
