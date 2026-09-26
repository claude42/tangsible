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

// Implements the "template" Verb (design-docs/Tangsible template.md): a
// standalone, single-view program for interactively debugging a Jinja2
// template, entirely separate from the run/rerun/role verbs' own live
// tree UI - there's no tree to browse here, and no live jsonl-streaming
// pipeline is needed, since each render is exactly one synchronous
// ansible-playbook invocation against exactly one host.
package template

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/execerr"
	"code.aw.net/claude/tangsible/internal/inventory"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ParseTemplateArgs splits args (everything after the "template" Verb)
// into the required template path, an optional hostname/group spec, and
// everything else as passthrough args - per design-docs/Tangsible
// template.md's own syntax, "tangsible template <path to template>
// [<hostname>[,<hostname>|<group>|all]...] [-e...]": hostSpec is only
// recognized when it's the *second* leading positional, immediately after
// the path and before any flag-shaped token - anything after the first
// flag-shaped token (or after hostSpec, if present) is rest. ok is false
// if no template path was given at all (a missing or flag-shaped first
// argument). hostSpec is returned as one raw string, still
// comma-unsplit - SplitHostTokens (below) does that, keeping this
// function's own job purely about syntax-level positional/flag splitting.
func ParseTemplateArgs(args []string) (templatePath, hostSpec string, rest []string, ok bool) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", "", nil, false
	}
	templatePath = args[0]
	remaining := args[1:]
	if len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") {
		hostSpec = remaining[0]
		remaining = remaining[1:]
	}
	return templatePath, hostSpec, remaining, true
}

// SplitHostTokens splits spec (ParseTemplateArgs' own positional
// hostname/group argument) on commas into trimmed, non-empty tokens, in
// order - design-docs/Tangsible template.md's comma-separated
// hostname/group list, each token resolved later by ResolveHostTokens. An
// empty/whitespace-only spec returns nil - RunTemplateVerb takes that to
// mean "no hosts given at all," falling back to ResolveInventoryHost's own
// single-host auto-pick.
func SplitHostTokens(spec string) []string {
	if strings.TrimSpace(spec) == "" {
		return nil
	}
	var tokens []string
	for _, p := range strings.Split(spec, ",") {
		if t := strings.TrimSpace(p); t != "" {
			tokens = append(tokens, t)
		}
	}
	return tokens
}

// resolveHostTokens is ResolveHostTokens' pure core: each token resolves
// as a group name first (including the reserved "all" group, which is
// what backs design-docs/Tangsible template.md's "all" keyword - no
// special-casing needed at all, since ansible-inventory --list already
// reports "all" as a real, always-present group - see
// inventory.IsGroup), falling back to a literal hostname; a token that's
// neither is a usage error, reported immediately rather than silently
// producing an empty render for it. Mixing hosts and groups (including
// "all") freely is harmless - the result is just their union, in
// first-appearance order, deduplicated.
func resolveHostTokens(tokens []string, raw map[string]json.RawMessage, allHosts []string) ([]string, error) {
	knownHost := make(map[string]bool, len(allHosts))
	for _, h := range allHosts {
		knownHost[h] = true
	}

	seen := make(map[string]bool, len(allHosts))
	var result []string
	add := func(h string) {
		if !seen[h] {
			seen[h] = true
			result = append(result, h)
		}
	}

	for _, tok := range tokens {
		switch {
		case inventory.IsGroup(raw, tok):
			for _, h := range inventory.GroupHosts(raw, tok) {
				add(h)
			}
		case knownHost[tok]:
			add(tok)
		default:
			return nil, fmt.Errorf("%q is not a known host or group in the inventory", tok)
		}
	}
	return result, nil
}

// ResolveHostTokens is resolveHostTokens' public, single-shellout
// convenience form - fetches the raw inventory itself via
// inventory.ListInventoryRaw. RunTemplateVerb calls resolveHostTokens
// directly instead, against a raw tree it already fetched for its own
// autocomplete candidates, so it never pays for two ansible-inventory
// --list invocations.
func ResolveHostTokens(tokens []string, passthroughArgs []string) ([]string, error) {
	raw, err := inventory.ListInventoryRaw(passthroughArgs)
	if err != nil {
		return nil, err
	}
	return resolveHostTokens(tokens, raw, inventory.FlattenInventoryHosts(raw))
}

// ConfirmManyHosts prompts (via out) for a yes/no confirmation before
// rendering against count hosts - design-docs/Tangsible template.md's
// >template_hosts_max guard (config.TemplateHostsMax), shown in the
// terminal's own normal cooked mode since it runs before the TUI (and its
// raw-mode screen) exists at all. Reads one line from in; only an
// explicit "y"/"yes" (case-insensitive) counts as yes, matching the
// "[y/N]" default-to-no prompt text.
func ConfirmManyHosts(count int, in io.Reader, out io.Writer) bool {
	fmt.Fprintf(out, "This will render the template against %d hosts - continue? [y/N] ", count)
	line, _ := bufio.NewReader(in).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// RoleVarsFiles returns the existing defaults/main.yml and vars/main.yml
// paths (in that order, skipping whichever doesn't exist on disk) for the
// role templatePath belongs to, per RolePathPattern (tui.go) - nil if
// templatePath isn't role-owned at all. Deliberately doesn't invoke the
// role itself (roles: [name] in the stub) - confirmed live that doing so
// would run every task in the role's own tasks/main.yml as a side effect,
// which a read-only template preview must never do; vars_files loads the
// role's own default/var definitions without executing anything.
func RoleVarsFiles(templatePath string) []string {
	abs, err := filepath.Abs(templatePath)
	if err != nil {
		return nil
	}
	loc := uikit.RolePathPattern.FindStringSubmatchIndex(abs)
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

// WriteTemplateStub generates a minimal playbook rendering templatePath to
// outputPath, and writes it to a fresh file in the system temp directory -
// unlike the "role" Verb's own stub, this one needs no special placement
// (there's no tree/drill-down source lookup happening here to find), so a
// real temp file is fine. gather_facts is left at ansible's own default
// (unset here), same reasoning as the "role" Verb's stub: templates
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
func WriteTemplateStub(templatePath, outputPath string) (stubPath string, err error) {
	absTemplate, err := filepath.Abs(templatePath)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("- hosts: all\n")
	b.WriteString("  ignore_unreachable: true\n")
	if varsFiles := RoleVarsFiles(templatePath); len(varsFiles) > 0 {
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

// TemplateResult is one render's own outcome - either Content (the
// rendered file, read back from disk) or, if the task itself failed,
// ErrMsg (extracted from the task's own result the same way
// PrimaryOutputField already does for the drill-down view - reused
// directly, not reimplemented).
type TemplateResult struct {
	Content string
	Failed  bool
	ErrMsg  string
}

// RenderTemplate runs stubPath synchronously, narrowed to hostname via
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
func RenderTemplate(stubPath, outputPath, hostname string, rest []string) (TemplateResult, error) {
	pluginEnv, err := runner.CallbackPluginEnv()
	if err != nil {
		return TemplateResult{}, err
	}
	args := append([]string{stubPath, "--limit", hostname}, rest...)
	cmd := exec.Command("ansible-playbook", args...)
	cmd.Env = append(os.Environ(), pluginEnv...)
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
		var ev playbook.RawEvent
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
			msg = execerr.Fallback("ansible-playbook", runErr)
		}
		if msg == "" {
			msg = fmt.Sprintf("no result reported for host %q - check that it resolves in the inventory", hostname)
		}
		return TemplateResult{}, fmt.Errorf("%s", msg)
	}

	decoded := playbook.DecodeHostResult(raw)
	if decoded.Failed || decoded.Unreachable {
		var full map[string]interface{}
		_ = json.Unmarshal(raw, &full)
		_, msg := uikit.PrimaryOutputField(full)
		if msg == "" {
			msg = decoded.Msg
		}
		return TemplateResult{Failed: true, ErrMsg: msg}, nil
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		return TemplateResult{Failed: true, ErrMsg: fmt.Sprintf("the template task reported success but the rendered file couldn't be read: %v", err)}, nil
	}
	return TemplateResult{Content: string(content)}, nil
}

// PreferredEditor implements the standard Unix convention: $VISUAL, then
// $EDITOR, then a bare "vi" as the last-resort default.
func PreferredEditor() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	return "vi"
}

// RunTemplateVerb is main.go's entry point for the "template" Verb.
// Returns the process exit code rather than calling os.Exit itself, so
// its own deferred cleanup (the stub playbook and the rendered-output
// scratch file, per design-docs/Tangsible template.md's Cleanup section)
// reliably runs on every path - os.Exit skips deferred functions, so
// main.go calls os.Exit on the returned code only after this function has
// already returned.
func RunTemplateVerb(args []string) int {
	templatePath, hostSpec, rest, ok := ParseTemplateArgs(args)
	if !ok {
		fmt.Fprintf(os.Stderr, "usage: %s template <path to template> [<hostname>[,<hostname>|<group>|all]...] [ansible-playbook args...]\n", os.Args[0])
		return 2
	}
	if _, err := os.Stat(templatePath); err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't read template %q: %v\n", templatePath, err)
		return 1
	}

	// Fetched once, up front, regardless of whether hostSpec was given at
	// all - both the "no hosts given, pick the first one" fallback below
	// and the host-swap dialog's own autocomplete candidates need it, and
	// resolveHostTokens (the comma-separated case) needs the raw group
	// tree specifically, not just ListInventoryHosts' already-flattened
	// list.
	raw, err := inventory.ListInventoryRaw(rest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't resolve a host from the inventory: %v\n", err)
		return 1
	}
	allHosts := inventory.FlattenInventoryHosts(raw)

	var hosts []string
	if tokens := SplitHostTokens(hostSpec); len(tokens) == 0 {
		if len(allHosts) == 0 {
			fmt.Fprintln(os.Stderr, "tangsible: no hosts found in the inventory")
			return 1
		}
		hosts = allHosts[:1]
	} else {
		h, err := resolveHostTokens(tokens, raw, allHosts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
			return 1
		}
		hosts = h
	}

	if maxHosts := config.TemplateHostsMax(config.ReadSettingsConfig(config.TangsibleConfigPath)); len(hosts) > maxHosts {
		if !ConfirmManyHosts(len(hosts), os.Stdin, os.Stderr) {
			fmt.Fprintln(os.Stderr, "tangsible: cancelled")
			return 0
		}
	}

	// Autocomplete candidates for the 'h' change-host dialog: literal
	// hostnames only, not group names - confirmed live that suggesting a
	// group there is actively misleading, since (unlike the CLI's own
	// comma-separated hostname/group positional) the dialog only ever
	// swaps in one literal host, per design-docs/Tangsible template.md's
	// own "Changing hosts" section; RenderTemplate's own event lookup is
	// keyed by hostname, so picking a group would just render "no result
	// reported" instead of doing anything useful.
	hostCandidates := append([]string{}, allHosts...)

	outFile, err := os.CreateTemp("", "tangsible-template-out-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create a scratch output file: %v\n", err)
		return 1
	}
	outputPath := outFile.Name()
	outFile.Close()
	defer os.Remove(outputPath)

	stubPath, err := WriteTemplateStub(templatePath, outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook: %v\n", err)
		return 1
	}
	defer os.Remove(stubPath)

	RunTemplateTUI(templatePath, stubPath, outputPath, hosts, hostCandidates, rest)
	return 0
}

// RunTemplateTUI builds and runs the standalone single-view program
// design-docs/Tangsible template.md describes: a thin header (just the
// template's own path - each host's own name lives on its own tab title
// instead, not duplicated here), one tab per host in hosts plus a shared
// "Source" tab (the template file's own raw content, host-independent),
// and a bottom keybinding-hint bar. No Pages("main")/tree underneath any
// of this, since there's nothing here to navigate back to. hostCandidates
// (every known literal hostname, no group names - see its own doc comment
// at the call site) backs the 'h' dialog's own autocomplete.
func RunTemplateTUI(templatePath, stubPath, outputPath string, hosts, hostCandidates, rest []string) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	header := tview.NewTextView().SetDynamicColors(true)
	header.SetTextStyle(uikit.BarStyle)
	header.SetText(fmt.Sprintf(" Template: %s ", tview.Escape(templatePath)))

	// hosts is this session's own mutable list of open host tabs (renamed
	// in place by the 'h' dialog - see applyHostChange); renderedViews is
	// kept in the exact same order/index, one persistent TextView per
	// host tab, reused across reprocesses and renames alike (only its own
	// SetText call ever changes, never the widget itself) - the same
	// "long-lived TextViews, content arrives via SetText" shape host.go's
	// detail view already uses.
	renderedViews := make([]*tview.TextView, len(hosts))
	for i := range renderedViews {
		renderedViews[i] = tview.NewTextView().SetDynamicColors(true)
		renderedViews[i].SetText("Rendering...")
	}
	// renderFailed mirrors hosts by index (true once that host's own last
	// render errored - a task failure or a render-level error alike) -
	// backs updateTabColors below, which is what actually colors an
	// errored host's own tab label the same red
	// uikit.ColorTag(playbook.OutcomeFailed) uses for a genuinely failed
	// task everywhere else in the app.
	renderFailed := make([]bool, len(hosts))
	sourceView := tview.NewTextView().SetDynamicColors(true)

	tabs := uikit.NewTabbedPane()
	// rebuildTabs re-derives the tab list from hosts/renderedViews - called
	// up front and again whenever hosts itself changes (currently just
	// applyHostChange's own rename). SetTabs' own "preserve active tab by
	// name" fallback can't find a just-renamed tab under its old name, so
	// every caller that renames a host follows this with an explicit
	// tabs.SetActiveByName of the new name.
	rebuildTabs := func() {
		names := make([]string, 0, len(hosts)+1)
		content := make([]tview.Primitive, 0, len(hosts)+1)
		for i, h := range hosts {
			names = append(names, h)
			content = append(content, renderedViews[i])
		}
		names = append(names, "Source")
		content = append(content, sourceView)
		tabs.SetTabs(names, content)
	}
	rebuildTabs()

	// searchBar (design-docs/Search.md) replaces the plain footer TextView
	// in flex's own bottom slot - see its own doc comment (uikit) and
	// host.go's identical use of it for the full story; this view shares
	// the same "long-lived TextViews, content arrives via SetText" shape
	// host.go's detail view does, unlike tui.go/diff.go's own drill-downs.
	searchBar := uikit.NewTabSearchBar(app, tabs,
		" tab/shift-tab: switch tab  e: edit template (reprocesses every host)  h: change host  /: search tab  y: copy tab  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom ",
		tabs.Primitive())

	// lastActiveHostIndex is which host tab the 'h' dialog targets -
	// updated reactively below whenever the active tab is a host tab
	// (left unchanged while the host-independent Source tab is active, so
	// 'h' still has a sensible target then too). NewTabSearchBar already
	// wired tabs.SetChangedFunc(searchBar.Clear) internally
	// (design-docs/Search.md's "switching tabs clears an active in-tab
	// search"); TabbedPane only ever holds one such callback, so this
	// composes both rather than silently dropping the search-bar's own.
	lastActiveHostIndex := 0
	tabs.SetChangedFunc(func() {
		searchBar.Clear()
		if name := tabs.ActiveName(); name != "" {
			for i, h := range hosts {
				if h == name {
					lastActiveHostIndex = i
					break
				}
			}
		}
	})

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(tabs.Primitive(), 0, 1, true).
		AddItem(searchBar.Primitive(), 1, 0, false)

	pages := tview.NewPages().AddPage("main", flex, true, true)

	// refreshSource re-reads the template file itself into the Source
	// tab - called both up front and every time 'e' returns from the
	// editor, since that's the one thing that can actually change the
	// file's own content (switching/adding hosts or reprocessing never
	// does).
	refreshSource := func() {
		searchBar.ClearForView(sourceView) // its own content is about to
		// change - design-docs/Search.md's "a search does not survive
		// content changing under it," scoped to just this tab.
		data, err := os.ReadFile(templatePath)
		if err != nil {
			sourceView.SetText("[red]" + tview.Escape(err.Error()) + "[-]")
			return
		}
		sourceView.SetText(tview.Escape(string(data)))
		sourceView.ScrollToBeginning()
	}
	refreshSource()

	// updateTabColors recomputes the whole tab-color-override map from
	// hosts/renderFailed and pushes it to tabs - recomputed wholesale
	// rather than mutated incrementally by name, so a stale key from a
	// since-renamed host (applyHostChange) never lingers: the map is
	// always exactly "every currently-open host whose own renderFailed
	// entry is true," nothing more.
	updateTabColors := func() {
		colors := make(map[string]string, len(hosts))
		for i, h := range hosts {
			if i < len(renderFailed) && renderFailed[i] {
				colors[h] = uikit.ColorTag(playbook.OutcomeFailed)
			}
		}
		tabs.SetTabColorOverrides(colors)
	}

	// applyRenderResult writes one host's own finished render (or error)
	// into its tab - shared by renderAll/renderHost below. Guards against
	// a stale result: if hosts[i] no longer equals host (a rename via 'h'
	// landed on the same index while this particular render was still in
	// flight - only possible because a rename is itself deferred behind
	// the rendering/pendingRerenderAll guard below, but defended here too
	// rather than relying on that alone), the result is simply dropped.
	applyRenderResult := func(i int, host string, result TemplateResult, err error) {
		if i < 0 || i >= len(hosts) || i >= len(renderedViews) || hosts[i] != host {
			return
		}
		view := renderedViews[i]
		searchBar.ClearForView(view)
		failed := err != nil || result.Failed
		switch {
		case err != nil:
			view.SetText("[red::b]Error[-::-]\n\n" + tview.Escape(err.Error()))
		case result.Failed:
			view.SetText("[red::b]Error[-::-]\n\n" + tview.Escape(result.ErrMsg))
		default:
			view.SetText(tview.Escape(result.Content))
		}
		view.ScrollToBeginning()
		if i < len(renderFailed) {
			renderFailed[i] = failed
			updateTabColors()
		}
	}

	// rendering/pendingRerenderAll serialize every render this view ever
	// runs against the one shared, reused stub/outputPath pair
	// (design-docs/Tangsible template.md's own "sequential rendering"
	// decision and Cleanup section) - two ansible-playbook invocations
	// racing the same outputPath would otherwise be a real, not
	// hypothetical, bug. A render requested while one is already in
	// flight (e.g. 'e' pressed again before a many-host reprocess
	// finishes) is never silently dropped: it's coalesced into a single
	// trailing renderAll once the current one completes - always safe
	// (redoing a host that didn't strictly need it), never wrong (a
	// completed edit never fails to eventually reprocess), and simple
	// (the pending intent doesn't need to remember which specific request
	// caused it).
	rendering := false
	pendingRerenderAll := false
	var renderAll func()
	finishRendering := func() {
		rendering = false
		if pendingRerenderAll {
			pendingRerenderAll = false
			renderAll()
		}
	}
	renderAll = func() {
		if rendering {
			pendingRerenderAll = true
			return
		}
		rendering = true
		for i, v := range renderedViews {
			v.SetText("Rendering...")
			renderFailed[i] = false
		}
		updateTabColors()
		go func() {
			snapshot := append([]string(nil), hosts...)
			for i, h := range snapshot {
				result, err := RenderTemplate(stubPath, outputPath, h, rest)
				i, h, result, err := i, h, result, err
				app.QueueUpdateDraw(func() {
					applyRenderResult(i, h, result, err)
				})
			}
			app.QueueUpdateDraw(finishRendering)
		}()
	}
	renderHost := func(i int) {
		if rendering {
			pendingRerenderAll = true
			return
		}
		rendering = true
		host := hosts[i]
		renderedViews[i].SetText("Rendering...")
		renderFailed[i] = false
		updateTabColors()
		go func() {
			result, err := RenderTemplate(stubPath, outputPath, host, rest)
			app.QueueUpdateDraw(func() {
				applyRenderResult(i, host, result, err)
				finishRendering()
			})
		}()
	}
	renderAll() // renderAll itself is non-blocking - the actual
	// ansible-playbook invocations run on their own goroutine it spawns
	// internally, same as every other call site below.

	// Host-switch dialog: a single-field modal, same CenteredModal/Pages
	// overlay pattern NewLiveTUI's own filter/search dialogs use - first
	// iteration is plain text entry, per design-docs/Tangsible
	// template.md's own "Changing hosts" section (a picker list gleaned
	// from the inventory is a natural later improvement, not required
	// here); autocomplete against hostCandidates (literal hostnames only)
	// is wired below.
	hostInput := tview.NewInputField().SetLabel("Host: ")
	hostInput.SetAutocompleteFunc(func(text string) []string {
		return uikit.MatchSingleValue(hostCandidates, text)
	}).SetAutocompleteUseTags(false). // plain hostnames/group names, and
		// avoids a literal '[' in one being misread as a color tag.
		SetAutocompletedFunc(func(text string, index, source int) bool {
			if source == tview.AutocompletedNavigate {
				return false // preview-on-navigate only moves the
				// highlight, doesn't write into the field itself - same
				// reasoning as the re-run dialog's own wireAutocomplete.
			}
			hostInput.SetText(text)
			return true
		})
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
	pages.AddPage("host", uikit.CenteredModal(hostFlex, 50, 7), true, false)

	hostDialogOpen := false
	// hostDialogTarget is the index into hosts this dialog is currently
	// editing - snapshotted from lastActiveHostIndex at open time, not
	// re-read at apply time, so it can't drift if the active tab happens
	// to change while the dialog is up (it can't today - the dialog is
	// modal - but there's no reason to depend on that).
	hostDialogTarget := 0
	openHostDialog := func() {
		hostDialogOpen = true
		hostDialogTarget = lastActiveHostIndex
		if hostDialogTarget < 0 || hostDialogTarget >= len(hosts) {
			hostDialogTarget = 0
		}
		hostInput.SetText(hosts[hostDialogTarget])
		pages.ShowPage("host")
		app.SetFocus(hostInput)
	}
	closeHostDialog := func() {
		hostDialogOpen = false
		pages.HidePage("host")
		app.SetFocus(tabs.Primitive())
	}
	// applyHostChange is the Enter/Apply-button shared body - renames
	// hostDialogTarget's own tab to whatever's currently typed into
	// hostInput (a no-op if it's empty or unchanged) and re-renders just
	// that one host, but does not itself close the dialog: both callers
	// below do that as their own last step. If the typed host is already
	// open under a different tab, this switches to that existing tab
	// instead of creating a duplicate - uikit.TabbedPane's own tab names
	// double as tview.Pages page names, so they must stay unique.
	applyHostChange := func() {
		newHost := strings.TrimSpace(hostInput.GetText())
		if newHost == "" || newHost == hosts[hostDialogTarget] {
			return
		}
		for _, h := range hosts {
			if h == newHost {
				tabs.SetActiveByName(newHost)
				return
			}
		}
		hosts[hostDialogTarget] = newHost
		rebuildTabs()
		tabs.SetActiveByName(newHost)
		renderHost(hostDialogTarget)
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
		// design-docs/Search.md, same reasoning as tui.go's identically-
		// shaped branch: bypasses normal focus-driven dispatch (unlike
		// hostDialogOpen's own plain "return event" above, which is
		// shallow enough - a direct sibling page of the root Pages - not
		// to need it; searchBar's own prompt sits one Flex layer deeper).
		if searchBar.IsComposing() {
			searchBar.HandleComposingKey(event)
			return nil
		}
		switch {
		// Esc clears an active search first (design-docs/Search.md);
		// otherwise deliberately still no KeyEscape case here (unlike
		// Ctrl-C) - Esc used to quit identically to q/Ctrl-C, but that's
		// an easy accidental hit while just browsing the rendered/source
		// tabs; only q and Ctrl-C quit now. Falls through to `return
		// event` at the bottom, which tview.TextView (sourceView/
		// renderedView) with no SetDoneFunc of its own just no-ops on -
		// not a dead binding, just genuinely inert.
		case event.Key() == tcell.KeyEscape && searchBar.HasActive():
			searchBar.Clear()
			return nil
		case event.Key() == tcell.KeyCtrlC:
			searchBar.CloseComposing()
			app.Stop()
			return nil
		case event.Rune() == 'q':
			app.Stop()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'N':
			searchBar.Prev()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'n':
			searchBar.Next()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == '/':
			searchBar.Open()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'y':
			searchBar.ShowMessage(uikit.CopyActiveTabStatus(tabs))
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
				cmd := exec.Command(PreferredEditor(), templatePath)
				cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
				_ = cmd.Run()
			})
			refreshSource()
			renderAll() // every open host tab, not just the active one -
			// the template file just changed, so every tab's own rendered
			// content is now stale, not just whichever happens to be
			// visible.
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
			// hostFlex has its own bare tview.NewBox() top margin (see its
			// own construction above) - Box.MouseHandler only ever consumes
			// the MouseLeftDown action, never MouseLeftClick, so a click
			// landing on that margin row went unconsumed and leaked straight
			// through to the tab bar underneath via Pages' own topmost-first,
			// falls-through-on-non-consumption dispatch (confirmed against
			// tview's own source, and against the identical, live-reproduced
			// bug in session/tui.go's rerunDialogOpen/filterDialogOpen/
			// searchDialogOpen). Fix: dispatch a click-type action directly
			// to hostFlex's own MouseHandler (still reaches hostInput's own
			// native click-to-position-cursor handling exactly as normal
			// Pages dispatch would) and unconditionally swallow it, so a
			// click hostFlex itself doesn't consume can never fall through.
			// MouseMove/MouseLeftDown/MouseLeftUp still pass through
			// unchanged when inside the box - MouseLeftUp in particular must
			// stay non-nil, or tview's own fireMouseActions (application.go)
			// never synthesizes the following MouseLeftClick at all.
			x, y := event.Position()
			if !uikit.InRect(x, y, hostFlex) {
				return nil, action
			}
			switch action {
			case tview.MouseLeftClick, tview.MouseLeftDoubleClick,
				tview.MouseMiddleClick, tview.MouseMiddleDoubleClick,
				tview.MouseRightClick, tview.MouseRightDoubleClick:
				hostFlex.MouseHandler()(action, event, func(p tview.Primitive) { app.SetFocus(p) })
				return nil, action
			default:
				return event, action
			}
		}
		// header/footer are plain, non-interactive TextViews - swallow a
		// click there before it can reach TextView's own default
		// MouseLeftDown handling, which would otherwise silently move
		// keyboard focus onto a one-line status bar (same focus-steal
		// issue as tui.go's topBar/bottomBar/outputTopBar/
		// outputBottomBar - confirmed live there that Escape/Enter/arrow
		// navigation then silently stop reaching the real content).
		if x, y := event.Position(); uikit.InRect(x, y, header) || uikit.InRect(x, y, searchBar.Primitive()) {
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
