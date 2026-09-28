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

// Implements the "host" Verb (design-docs/HostVerb.md): a standalone,
// five-tab program showing everything Tangsible can determine about one
// host - live gathered facts, inventory group membership, which plays
// would run for it, its own host_vars files, and the raw
// "ansible-inventory --host" dump - entirely separate from the
// run/rerun/role verbs' own live tree UI, the same way "template" is
// (template.go).
package host

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/execerr"
	"code.aw.net/claude/tangsible/internal/inventory"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ParseHostArgs splits args (everything after the "host" Verb) into the
// required hostname, an optional playbook, and everything else as
// passthrough args - "tangsible host <hostname> [<playbook>] [-i ...]
// [-e ...]" (design-docs/HostVerb.md), the same shape parseTemplateArgs
// (template.go) already uses for "<path> [<hostname>]", just with the two
// positionals' roles swapped: hostname is only recognized when it's the
// *first* leading positional; playbook only when it's the *second*,
// immediately after hostname and before any flag-shaped token. ok is
// false if no hostname was given at all (a missing or flag-shaped first
// argument).
func ParseHostArgs(args []string) (hostname, playbookArg string, rest []string, ok bool) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", "", nil, false
	}
	hostname = args[0]
	remaining := args[1:]
	if len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") {
		playbookArg = remaining[0]
		remaining = remaining[1:]
	}
	return hostname, playbookArg, remaining, true
}

// RunHostVerb is "tangsible host <hostname> [<playbook>]"'s own entry
// point - resolves the playbook the same cascade "run" uses when it
// isn't given explicitly (a missing/unresolved playbook isn't fatal here:
// only the Plays tab actually needs one, and reports its own absence
// gracefully - see fetchHostPlays), creates the one stub playbook the
// Summary tab's live fact-gathering needs, and shows the standalone
// detail view for the process's entire lifetime.
func RunHostVerb(args []string) int {
	hostname, playbookArg, rest, ok := ParseHostArgs(args)
	if !ok {
		fmt.Fprintf(os.Stderr, "usage: %s host <hostname> [<playbook>] [ansible-playbook args...]\n", os.Args[0])
		return 2
	}
	playbook := playbookArg
	if playbook == "" {
		playbook, _ = config.ResolvePlaybook()
	}

	stubPath, err := WriteHostSummaryStub()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook: %v\n", err)
		return 1
	}
	defer os.Remove(stubPath)

	RunHostDetailStandalone(hostname, playbook, rest, stubPath)
	return 0
}

// RunHostsVerb is "tangsible hosts [<playbook>]"'s own entry point -
// lists every inventory host up front (ansible-inventory --list, the
// same call template.go's resolveInventoryHost already makes for its own
// single-host resolution) and shows the list-then-detail flow.
func RunHostsVerb(args []string) int {
	playbookArg, rest, _ := config.SplitPlaybookArgs(args)
	playbook := playbookArg
	if playbook == "" {
		playbook, _ = config.ResolvePlaybook()
	}

	hosts, err := inventory.ListInventoryHosts(rest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't list inventory hosts: %v\n", err)
		return 1
	}
	if len(hosts) == 0 {
		fmt.Fprintln(os.Stderr, "tangsible: no hosts found in the inventory")
		return 1
	}

	stubPath, err := WriteHostSummaryStub()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook: %v\n", err)
		return 1
	}
	defer os.Remove(stubPath)

	pingStubPath, err := WriteHostPingStub()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook: %v\n", err)
		return 1
	}
	defer os.Remove(pingStubPath)

	RunHostsListTUI(hosts, playbook, rest, stubPath, pingStubPath)
	return 0
}

// GroupMembership is one entry in a host's own transitive group chain
// (hostGroupChain) - Via is "" for a group the host is a *direct* member
// of (listed under that group's own "hosts:" in the inventory), or the
// name of the child group through which this ancestor group was reached
// otherwise.
type GroupMembership struct {
	Group string
	Via   string
}

// HostGroupChain returns every group hostname transitively belongs to,
// per design-docs/HostVerb.md's own decision to show the full chain, not
// just direct membership: direct groups first (alphabetically, for
// determinism), then each further ancestor layer outward, also
// alphabetically within its own layer. raw is `ansible-inventory --list`'s
// own decoded JSON (see inventory.AnsibleInventoryGroup, internal/inventory) - the same
// source inventory.FlattenInventoryHosts already reads, just walked in the opposite
// direction: that function walks group→hosts to build one flat host set;
// this one needs host→ancestor-groups, which the JSON's own "children:"
// pointers don't give directly (only parent→children is stored, never
// child→parent) - so this builds its own reverse (child→parents) index
// first, then works outward from the host via BFS.
func HostGroupChain(raw map[string]json.RawMessage, hostname string) []GroupMembership {
	groups := make(map[string]inventory.AnsibleInventoryGroup, len(raw))
	for name, data := range raw {
		if name == "_meta" {
			continue
		}
		var g inventory.AnsibleInventoryGroup
		if err := json.Unmarshal(data, &g); err != nil {
			continue
		}
		groups[name] = g
	}

	parentsOf := map[string][]string{}
	for name, g := range groups {
		for _, child := range g.Children {
			parentsOf[child] = append(parentsOf[child], name)
		}
	}

	var direct []string
	for name, g := range groups {
		for _, h := range g.Hosts {
			if h == hostname {
				direct = append(direct, name)
				break
			}
		}
	}
	sort.Strings(direct)

	var chain []GroupMembership
	seen := map[string]bool{}
	queue := make([]string, len(direct))
	for i, name := range direct {
		chain = append(chain, GroupMembership{Group: name})
		seen[name] = true
		queue[i] = name
	}

	for len(queue) > 0 {
		nextVia := map[string]string{}
		var next []string
		for _, child := range queue {
			parents := append([]string(nil), parentsOf[child]...)
			sort.Strings(parents)
			for _, parent := range parents {
				if seen[parent] {
					continue
				}
				seen[parent] = true
				nextVia[parent] = child
				next = append(next, parent)
			}
		}
		sort.Strings(next)
		for _, parent := range next {
			chain = append(chain, GroupMembership{Group: parent, Via: nextVia[parent]})
		}
		queue = next
	}

	return chain
}

// FetchHostGroups runs `ansible-inventory --list` and renders hostname's
// own full transitive group chain (hostGroupChain), one line per group,
// left-aligned to the widest group name so the "(direct)"/"(via ...)"
// annotations line up.
func FetchHostGroups(hostname string, rest []string) (string, error) {
	cmd := exec.Command("ansible-inventory", append([]string{"--list"}, rest...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := execerr.NotFoundMessage("ansible-inventory", err); msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("ansible-inventory --list failed: %s", msg)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		return "", fmt.Errorf("ansible-inventory --list didn't produce valid JSON: %v", err)
	}

	chain := HostGroupChain(raw, hostname)
	if len(chain) == 0 {
		return fmt.Sprintf("host %q is not a member of any inventory group", hostname), nil
	}

	width := 0
	for _, m := range chain {
		if l := len([]rune(m.Group)); l > width {
			width = l
		}
	}
	var b strings.Builder
	for _, m := range chain {
		pad := width - len([]rune(m.Group))
		// "all" is the universal group every host belongs to by
		// definition - a "(via <child>)" annotation there is always
		// technically true (some child led the BFS to it) but never
		// actually informative, since it'd be true of literally any
		// child group whether or not this specific host used it - so it
		// reads as a claim about *why* this host is in "all" that isn't
		// real. No annotation at all for "all"; every other group still
		// gets its own "(direct)"/"(via ...)" detail.
		if m.Group == "all" {
			fmt.Fprintf(&b, "[green]%s[-]\n", tview.Escape(m.Group))
			continue
		}
		detail := "(direct)"
		if m.Via != "" {
			detail = fmt.Sprintf("(via %s)", m.Via)
		}
		fmt.Fprintf(&b, "[green]%s[-]%s  [lightsteelblue]%s[-]\n", tview.Escape(m.Group), strings.Repeat(" ", pad), tview.Escape(detail))
	}
	return b.String(), nil
}

// ExtractInventoryDirs pulls every -i/--inventory value out of rest
// (both "--flag value" and "--flag=value" long forms, same convention
// ParsePassthroughArgs uses in rerunargs.go for --tags/--limit) and
// returns the directory each one lives in, for any that resolve to a
// real file or directory on disk - silently skipping anything else (a
// bare comma-list like "web1,web2,", a nonexistent path) since neither has
// a meaningful directory to look for a sibling host_vars/ under.
func ExtractInventoryDirs(rest []string) []string {
	var dirs []string
	addIfReal := func(path string) {
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		if info.IsDir() {
			dirs = append(dirs, path)
			return
		}
		dirs = append(dirs, filepath.Dir(path))
	}
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		switch {
		case arg == "-i" || arg == "--inventory" || arg == "--inventory-file":
			if i+1 < len(rest) {
				addIfReal(rest[i+1])
				i++
			}
		case strings.HasPrefix(arg, "--inventory="):
			addIfReal(strings.TrimPrefix(arg, "--inventory="))
		case strings.HasPrefix(arg, "--inventory-file="):
			addIfReal(strings.TrimPrefix(arg, "--inventory-file="))
		}
	}
	return dirs
}

// DiscoverHostVarsFiles returns every host_vars file for hostname found
// under any of dirs, sorted for determinism - Ansible looks for host_vars
// as a sibling of both the inventory source and the playbook (ansible-core's
// own documented behavior), so dirs is expected to already carry both
// candidates by the time this is called (extractInventoryDirs plus the
// playbook's own directory - see fetchHostVars). Matches both shapes
// Ansible itself recognizes: a single host_vars/<hostname>.yml (or .yaml)
// file, and a host_vars/<hostname>/ directory of multiple files.
// Deduplicated by absolute path, since the playbook and an inventory
// source can easily share the same directory.
func DiscoverHostVarsFiles(hostname string, dirs []string) []string {
	seen := map[string]bool{}
	var files []string
	addIfNew := func(path string) {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		files = append(files, path)
	}
	for _, dir := range dirs {
		for _, ext := range []string{".yml", ".yaml"} {
			p := filepath.Join(dir, "host_vars", hostname+ext)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				addIfNew(p)
			}
		}
		groupDir := filepath.Join(dir, "host_vars", hostname)
		entries, err := os.ReadDir(groupDir)
		if err != nil {
			continue
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			addIfNew(filepath.Join(groupDir, name))
		}
	}
	return files
}

// FetchHostVars renders every host_vars file found for hostname verbatim
// (design-docs/HostVerb.md's own "Findings from discussion": raw file
// content, one section per file, not a merged key-value view - preserves
// comments/formatting and needs no variable-precedence logic), one
// SectionLabel-headed section per file, in discoverHostVarsFiles' own
// sorted order.
func FetchHostVars(hostname, playbook string, rest []string) (string, error) {
	var dirs []string
	if playbook != "" {
		if abs, err := filepath.Abs(playbook); err == nil {
			dirs = append(dirs, filepath.Dir(abs))
		}
	}
	dirs = append(dirs, ExtractInventoryDirs(rest)...)

	files := DiscoverHostVarsFiles(hostname, dirs)
	if len(files) == 0 {
		return fmt.Sprintf("no host_vars files found for host %q", hostname), nil
	}

	var b strings.Builder
	for _, path := range files {
		data, err := os.ReadFile(path)
		content := string(data)
		if err != nil {
			content = fmt.Sprintf("(couldn't read: %v)", err)
		}
		b.WriteString(hostSectionHeading(path))
		b.WriteString(highlightYAMLKeys(content))
		b.WriteString("\n\n")
	}
	return b.String(), nil
}

// FetchHostPlays runs "ansible-playbook <playbook> <rest...> --limit
// <hostname> --list-tasks --list-hosts" and groups the flattened
// ProgressEntry sequence ParseListTasksOutput (progress.go) already
// produces back into per-play sections, reusing that parser directly
// rather than reimplementing it - narrowed to exactly this host via the
// same --limit flag progress.go's own doc comment already explains is
// required alongside --list-hosts for a limit to actually apply at all.
// Unlike BuildProgressSkeleton (progress.go), which is always
// best-effort and swallows every failure silently (fine for an optional
// progress indicator riding on top of an already-working run), this
// surfaces a real failure as err, since an empty Plays tab needs to stay
// distinguishable from "the whole invocation failed."
func FetchHostPlays(playbook string, rest []string, hostname string) (string, error) {
	if playbook == "" {
		return "no playbook specified, and none could be resolved - can't determine which plays would run", nil
	}
	args := append([]string{playbook}, rest...)
	args = append(args, "--limit", hostname, "--list-tasks", "--list-hosts")
	cmd := exec.Command("ansible-playbook", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = execerr.Fallback("ansible-playbook", err)
		}
		return "", fmt.Errorf("%s", msg)
	}

	// nil, not source.FreeStrategyPlayNames(playbook): this tab answers
	// "which plays/tasks would actually run" (CLAUDE.md's own Host/hosts
	// section) - a free-strategy play's tasks genuinely do run for this
	// host, so excluding them here (the way BuildProgressSkeleton's live
	// progress-fill prediction has to, design-docs/StrategyFree.md) would
	// wrongly drop real, correct entries from a static preview that was
	// never trying to predict live event matching in the first place.
	entries := runner.ParseListTasksOutput(stdout.String(), nil)
	if len(entries) == 0 {
		return fmt.Sprintf("no plays would run for host %q", hostname), nil
	}

	var b strings.Builder
	currentPlay := ""
	for _, e := range entries {
		if e.Play != currentPlay {
			if currentPlay != "" {
				b.WriteString("\n")
			}
			b.WriteString(hostSectionHeading(e.Play))
			currentPlay = e.Play
		}
		fmt.Fprintf(&b, "  %s\n", highlightRolePrefix(e.Task))
	}
	return b.String(), nil
}

// highlightRolePrefix highlights a task line's own leading "<role> : "
// prefix green, dropping the rest of the line (the task name itself) to
// lightsteelblue - ansible's own --list-tasks output prefixes a
// role-sourced task with its role's name and " : " (confirmed
// empirically, --list-tasks against a real role-using playbook); a task
// defined directly in the play has no such prefix at all. Splits on the
// first " : " only - a task's own name containing that exact substring is
// vanishingly unlikely, and would just mean this heuristic highlights a
// bit too much, not a crash.
func highlightRolePrefix(task string) string {
	if idx := strings.Index(task, " : "); idx != -1 {
		return "[green]" + tview.Escape(task[:idx]) + "[-]" + "[lightsteelblue]" + tview.Escape(task[idx:]) + "[-]"
	}
	return tview.Escape(task)
}

// RunAnsibleInventoryHost runs "ansible-inventory --host <hostname>" and
// returns its raw stdout - the one subprocess invocation shared by
// fetchHostEverythingKnown (shown verbatim) and fetchHostSummary's own
// cache-first check (parsed into a map, see below), so the command is
// only ever built and run in one place.
//
// --host itself never connects to the host - it only ever reads local
// state (inventory/group_vars/host_vars, merged by ansible's own
// precedence rules, plus whatever's on disk already) - but "never
// connects" turned out not to mean "never shows gathered facts" the way
// an earlier version of this comment claimed: confirmed empirically
// (after a live report caught this exact discrepancy) that when
// `fact_caching` is configured in ansible.cfg *and* that cache already
// holds an entry for the host - written by any prior real gather, not
// necessarily one this session ran - ansible-inventory merges those
// cached facts straight into --host's own output too, same as it merges
// host_vars/group_vars, and drops them again once fact_caching_timeout
// expires (also confirmed empirically) - so a hit here is guaranteed
// fresh within whatever window the user's own ansible.cfg configures,
// never arbitrarily stale. fetchHostSummary uses this to skip an actual
// connection entirely when a fresh cache entry already exists.
func RunAnsibleInventoryHost(hostname string, rest []string) ([]byte, error) {
	cmd := exec.Command("ansible-inventory", append([]string{"--host", hostname}, rest...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := execerr.NotFoundMessage("ansible-inventory", err); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ansible-inventory --host failed: %s", msg)
	}
	return out, nil
}

// FetchHostEverythingKnownRaw is design-docs/HostVerb.md's own fifth tab -
// runAnsibleInventoryHost's own output, trimmed but not yet rendered
// (BuildHostDetailPrimitive handles that, via highlightJSONKeys, both for
// this raw form and again for mergeFactsIntoEverythingKnown's own merged
// result - see that function's own doc comment for why this needed
// splitting out of what used to be one single fetchHostEverythingKnown).
// That tool's default output is already pretty-printed JSON - confirmed
// empirically, no reformatting needed for this raw form. So this tab's
// own base content is: always declared inventory data, plus whichever
// gathered facts happen to be sitting in the fact cache already, if any -
// not fetched live either way, but not guaranteed static either (see
// runAnsibleInventoryHost's own doc comment).
func FetchHostEverythingKnownRaw(hostname string, rest []string) ([]byte, error) {
	out, err := RunAnsibleInventoryHost(hostname, rest)
	if err != nil {
		return nil, err
	}
	return bytes.TrimRight(out, "\n"), nil
}

// mergeFactsIntoEverythingKnown overlays facts (Summary's own decoded
// ansible_facts, from FetchHostFacts) onto raw's own JSON object, facts
// winning on any key collision - design-docs/HostVerb.md's own "Everything
// known doesn't see what Summary gathered" finding: the two tabs' own
// independent fetches otherwise race, with Everything Known's quick,
// cache-only ansible-inventory --host read finishing well before
// Summary's live gather (if it needed one) ever does, so within a single
// view-open Everything Known could show no facts at all even while
// Summary, moments later, shows real ones. facts is the freshest,
// most-authoritative data available regardless of that race (a real live
// gather this exact session, or a confirmed-fresh cache hit) - merging it
// in directly means Everything Known no longer depends on a second,
// independently-timed cache read to ever show what this session itself
// already found. Re-marshaled with json.MarshalIndent, matching
// ansible-inventory's own default 4-space indent - key order ends up
// alphabetical (encoding/json's own map-marshaling behavior), not
// ansible-inventory's original order, an accepted cosmetic difference for
// what was already just a raw data dump.
func mergeFactsIntoEverythingKnown(raw []byte, facts map[string]interface{}) ([]byte, error) {
	var merged map[string]interface{}
	if err := json.Unmarshal(raw, &merged); err != nil {
		return nil, err
	}
	if merged == nil {
		merged = map[string]interface{}{}
	}
	for k, v := range facts {
		merged[k] = v
	}
	return json.MarshalIndent(merged, "", "    ")
}

// jsonKeyLine matches one line of pretty-printed JSON's own "key": value
// shape - runAnsibleInventoryHost's own output (fetchHostEverythingKnown)
// is JSON, not YAML, so uikit.YamlKeyLine's own unquoted-key pattern
// doesn't apply here; same "good enough, line-based, not a real parser"
// approach otherwise (a line with no matching key - a closing brace, an
// array element - just renders unstyled).
var jsonKeyLine = regexp.MustCompile(`^(\s*)("(?:[^"\\]|\\.)*")(:)(\s.*|)$`)

// jsonValueLine matches a line that's nothing but a bare JSON value, no
// key at all - the shape a line inside a JSON array takes (a string,
// number, or true/false/null element, optionally trailing-comma'd).
// jsonKeyLine's own "key": value shape never matches these (no colon at
// all), which otherwise left every array element fully unstyled - a real,
// reported gap: only an array's own opening "[" (part of its owning
// key's own value, jsonKeyLine's m[4]) was ever colored, never the
// elements themselves.
var jsonValueLine = regexp.MustCompile(`^(\s*)("(?:[^"\\]|\\.)*"|-?\d+(?:\.\d+)?|true|false|null)(,?)$`)

// highlightJSONKeys highlights each line's own quoted key green, and
// every value - whether it's a key's own "key": value (jsonKeyLine) or a
// bare array element with no key of its own (jsonValueLine) -
// lightsteelblue. A structural-only line (an opening/closing brace or
// bracket with nothing else on it) matches neither and stays unstyled.
// Different color pairing than uikit.ColorizeYAML's own orange-plus-bold
// convention for these pages. design-docs/HostVerb.md's own cosmetic pass
// tried bold-only, then dim, then bright-white/silver, before landing
// here per live feedback each time.
func highlightJSONKeys(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		if m := jsonKeyLine.FindStringSubmatch(line); m != nil {
			lines[i] = tview.Escape(m[1]) + "[green]" + tview.Escape(m[2]+m[3]) + "[-]" + "[lightsteelblue]" + tview.Escape(m[4]) + "[-]"
			continue
		}
		if m := jsonValueLine.FindStringSubmatch(line); m != nil {
			lines[i] = tview.Escape(m[1]) + "[lightsteelblue]" + tview.Escape(m[2]) + "[-]" + tview.Escape(m[3])
			continue
		}
		lines[i] = tview.Escape(line)
	}
	return strings.Join(lines, "\n")
}

// yamlListItemLine matches a bare block-sequence item with no key of its
// own - "- <value>" (any leading indentation) - the shape a YAML list
// holds when its entries are plain scalars or flow-lists ("- alpha",
// "- [a, b]") rather than nested mappings. A mapping entry ("- key:
// value") is already handled by uikit.YamlKeyLine itself, whose own
// leading-marker group already accepts an optional "- " prefix - this
// one only ever gets a chance on a line YamlKeyLine has already failed to
// match, i.e. one with no "key:" shape at all. Left unstyled before this,
// a real, reported gap: only a list's own flow-style opening (attached to
// its owning key, e.g. "mylist: [") was ever colored, never a block-style
// item on its own line.
var yamlListItemLine = regexp.MustCompile(`^(\s*-\s+)(.+)$`)

// yamlBlockScalarOpener matches a key/list-item's own value when it's
// nothing but a block-scalar indicator - "|" or ">", optionally
// chomp-marked ("+"/"-") and/or preceded by a YAML tag (most commonly
// "!vault" - a real, reported gap: only that "!vault |" opener itself was
// ever colored, never the raw multi-line blob underneath it) - meaning
// every subsequent, more-indented (or blank) line is that one value's own
// raw continuation text, not a further nested key/list-item of its own.
var yamlBlockScalarOpener = regexp.MustCompile(`^(![^\s]+\s+)?[|>][+-]?\d*$`)

// highlightYAMLKeys highlights each line's own "key:" portion (or, for a
// bare list item, its whole value - yamlListItemLine) green for the key,
// lightsteelblue for the value - the same regex-based, "good enough, not
// a real YAML parser" approach uikit.ColorizeYAML already uses for the
// drill-down's own Task section (uikit.YamlKeyLine), just without that
// helper's own orange-plus-bold coloring - see highlightJSONKeys' own doc
// comment for the styling history. blockScalarIndent tracks whether the
// previous key/list-item's own value opened a raw multi-line scalar
// (yamlBlockScalarOpener) - every line more indented than that (or blank)
// is colored as that same value's own continuation instead of being
// re-matched against the key/list-item patterns below, until indentation
// drops back to the opener's own level or shallower.
func highlightYAMLKeys(raw string) string {
	lines := strings.Split(raw, "\n")
	blockScalarIndent := -1
	for i, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)

		if blockScalarIndent != -1 {
			if trimmed == "" || indent > blockScalarIndent {
				lines[i] = "[lightsteelblue]" + tview.Escape(line) + "[-]"
				continue
			}
			blockScalarIndent = -1
		}

		if m := uikit.YamlKeyLine.FindStringSubmatch(line); m != nil {
			lines[i] = tview.Escape(m[1]) + "[green]" + tview.Escape(m[2]+m[3]) + "[-]" + "[lightsteelblue]" + tview.Escape(m[4]) + "[-]"
			if yamlBlockScalarOpener.MatchString(strings.TrimSpace(m[4])) {
				blockScalarIndent = indent
			}
			continue
		}
		if m := yamlListItemLine.FindStringSubmatch(line); m != nil {
			lines[i] = tview.Escape(m[1]) + "[lightsteelblue]" + tview.Escape(m[2]) + "[-]"
			if yamlBlockScalarOpener.MatchString(strings.TrimSpace(m[2])) {
				blockScalarIndent = indent
			}
			continue
		}
		lines[i] = tview.Escape(line)
	}
	return strings.Join(lines, "\n")
}

// hostSectionHeading renders a host.go-local section header - the same
// heading-plus-"="-underline shape as uikit.SectionLabel, but without
// that helper's own deliberate color-coding (its own doc comment: Task/
// Output/Errors/Details in the drill-down are colored so they're never
// mistaken for an outcome color) - these pages have no outcome palette to
// avoid colliding with. Green, grouped with every other "key" this
// styling pass highlights (design-docs/HostVerb.md's own "Headlines and
// keys" grouping); the "=" underline shares the same color tag as the
// heading text above it, one single [green]...[-] span covering both
// lines rather than two separately-colored pieces.
func hostSectionHeading(label string) string {
	return fmt.Sprintf("[green]%s\n%s[-]\n\n", tview.Escape(label), strings.Repeat("=", len([]rune(label))))
}

// HostSummaryStubYAML is the play design-docs/HostVerb.md's Summary tab
// uses to gather live facts for one host: an *explicit* `ansible.builtin.
// setup:` task, not the play-level `gather_facts: true` shorthand this
// originally used. That original version had a real, reported bug: with
// `gathering = smart` configured in ansible.cfg (a common setup) and a
// fact cache already warm for the host - populated by any prior playbook
// run at all, not necessarily this one - the *implicit* "Gathering
// Facts" task `gather_facts: true` inserts is silently skipped
// altogether, producing zero jsonl output for it: no task-start, no
// runner event, nothing - confirmed empirically by reproducing the exact
// reported symptom (a real ansible.cfg with `gathering = smart` +
// `fact_caching = jsonfile`, cache warmed by a separate, unrelated
// playbook run first). `gather_facts: true`'s own doc-comment claim that
// this transparently respects the fact cache "with zero special-casing"
// was simply wrong: smart gathering doesn't mean "consult the cache, but
// still report"; it means "skip entirely, sometimes with no observable
// event at all." An *explicit* task calling the `setup` module directly
// doesn't have this problem - it's an ordinary task like any other, always
// executes, always fires a real event (confirmed the same way, against
// the same warmed cache) - `gathering`'s smart-skip logic only ever
// applies to the auto-inserted implicit task the `gather_facts:` play
// keyword creates, never to a task the playbook actually writes out
// itself. This still benefits from a configured fact cache exactly as
// intended, just at the module's own internal level (a fresh `setup` run
// still writes/reads the cache) rather than by skipping the task
// pre-emptively. ignore_unreachable matches the "template" Verb's own
// stub (template.go/writeTemplateStub) - moot in practice since --limit
// always narrows this to exactly one host, but harmless and consistent.
const HostSummaryStubYAML = "- hosts: all\n  gather_facts: false\n  ignore_unreachable: true\n  tasks:\n    - name: gather facts\n      ansible.builtin.setup:\n"

// HostPingStubYAML backs the host list's own connectivity dot
// (design-docs/HostVerb.md's "New ideas": green if ansible.builtin.ping
// succeeds, red otherwise) - a real `ansible.builtin.ping` task, run
// through the same "ansible-playbook + bundled callback plugin" path as
// every other live fetch in this file, deliberately not a raw ICMP/DNS/
// SSH-port check from wherever tangsible itself happens to run: those
// bypass Ansible's own connection plugin entirely (jump hosts,
// non-standard ports, connection: local/docker/winrm) and can't tell you
// anything about whether Ansible itself could actually manage the host.
// ignore_unreachable matches hostSummaryStubYAML's own stub for the
// identical reason (moot given --limit always narrows to one host, but
// harmless and consistent).
const HostPingStubYAML = "- hosts: all\n  gather_facts: false\n  ignore_unreachable: true\n  tasks:\n    - name: ping\n      ansible.builtin.ping:\n"

// writeHostStub writes yamlContent to a fresh temp file matching pattern -
// shared by WriteHostSummaryStub/WriteHostPingStub. Reused, unchanged,
// across every fetch of its own kind in one tangsible session (the
// "hosts" Verb's own list-then-detail flow can view many hosts one after
// another), same "one stable scratch file for the whole session"
// convention as the "template" Verb's own stub/output pair.
func writeHostStub(yamlContent, pattern string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(yamlContent); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func WriteHostSummaryStub() (string, error) {
	return writeHostStub(HostSummaryStubYAML, "tangsible-host-summary-*.yml")
}

func WriteHostPingStub() (string, error) {
	return writeHostStub(HostPingStubYAML, "tangsible-host-ping-*.yml")
}

// FetchHostPing runs hostPingStubYAML against hostname and reports whether
// it came back with a genuine v2_runner_on_ok - the host list's own
// connectivity dot (design-docs/HostVerb.md). Deliberately always a live
// attempt, never a fact-cache shortcut the way fetchHostSummary's own live
// path has one: a cached fact proves nothing about whether the host is
// reachable right now. Green-or-red only, matching that same decision -
// a failure, an unreachable result, and an inability to even run
// ansible-playbook at all (bad inventory, missing binary) all collapse to
// the same "false", with no separate error surfaced for this dot's own
// purposes (the five detail tabs already report a real error for exactly
// this host if there's one to see).
func FetchHostPing(stubPath, hostname string, rest []string) bool {
	pluginEnv, err := runner.CallbackPluginEnv()
	if err != nil {
		return false
	}
	args := append([]string{stubPath, "--limit", hostname}, rest...)
	cmd := exec.Command("ansible-playbook", args...)
	cmd.Env = append(os.Environ(), pluginEnv...)
	out, _ := cmd.Output()

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
		if ev.Event != "v2_runner_on_ok" {
			continue
		}
		if _, ok := ev.Hosts[hostname]; ok {
			return true
		}
	}
	return false
}

// FactCacheCanaryKey is checked against runAnsibleInventoryHost's own
// output to decide whether a usable, fresh fact-cache entry already
// exists for this host - "ansible_architecture" is part of ansible's own
// default (min) gather_subset and essentially always present whenever
// any real gather has ever happened at all, regardless of which further,
// more specific subsets (hardware/network/virtual/...) were also
// gathered - a reasonable single presence check rather than requiring
// every individual field formatHostSummary might want.
const FactCacheCanaryKey = "ansible_architecture"

// hostFactsResult is FetchHostFacts' own return shape. characteristics is
// always the Characteristics section's own eventual body - either
// formatCharacteristics' normal field list (the common case, with facts
// then set too, backing Keys/HostKeyLines) or a pre-rendered note in its
// place: a live Unreachable/Failed result, or (skipLiveGather) a note
// that the live gather was skipped entirely. Never both - facts is nil
// whenever characteristics is pre-rendered instead of the normal list.
type hostFactsResult struct {
	prefix          string
	characteristics string
	facts           map[string]interface{}
}

// FetchHostFacts first checks whether a fresh fact-cache entry already
// covers this host (runAnsibleInventoryHost, gated on factCacheCanaryKey)
// and uses that directly, with no connection to the host at all, when it
// does - restoring the original design intent ("if fact caching is
// activated, retrieve from there, otherwise retrieve from host") that
// got lost when hostSummaryStubYAML's own fix (see its doc comment)
// switched to an explicit `setup:` task that always connects: that fix
// was necessary for correctness (the play-level `gather_facts: true`
// shorthand could silently skip with zero jsonl output at all under
// `gathering = smart`), but it also meant paying for a live connection
// on every view, even when a perfectly fresh cache already existed.
// Checking the cache first, explicitly, in application code rather than
// leaning on ansible's own smart-gathering skip, gets the speed back
// without reintroducing the silent-failure bug: a cache hit here is a
// real, parsed, guaranteed-fresh result (runAnsibleInventoryHost's own
// doc comment - confirmed empirically that ansible-inventory --host
// stops showing a host's cached facts once fact_caching_timeout expires),
// never a guess.
//
// Any problem with the cache-first check itself (the command fails, its
// output isn't valid JSON, or the canary key just isn't there) falls
// straight through to the live path below without comment - all three
// are ordinary, expected cases (no fact_caching configured at all is the
// common one), not worth surfacing as their own error when the live path
// is about to attempt the exact same thing anyway and will report its
// own error if that fails too.
//
// skipLiveGather (design-docs/HostVerb.md's own "New ideas": a host the
// hosts list's own ping dot already found unreachable shouldn't pay for a
// second, doomed connection attempt just to open its Summary tab) only
// ever short-circuits the live path below, never the cache check above a
// stale-but-real cached fact is still worth showing even for a host
// that's down right now.
//
// Falling through to the live path: stubPath runs synchronously, narrowed
// to hostname via --limit, and this extracts that host's own
// ansible_facts from the resulting jsonl stream - the same
// scan-for-one-host's-own-event pattern renderTemplate (template.go)
// already uses. err is non-nil only when nothing usable could be
// determined at all (a bad inventory/host); an unreachable/failed host is
// reported as ordinary displayable text instead, not err, since that's
// expected, common content for this tab, not a tool failure.
func FetchHostFacts(stubPath, hostname string, rest []string, skipLiveGather bool) (hostFactsResult, error) {
	if out, err := RunAnsibleInventoryHost(hostname, rest); err == nil {
		var cached map[string]interface{}
		if json.Unmarshal(out, &cached) == nil {
			if _, ok := cached[FactCacheCanaryKey]; ok {
				return hostFactsResult{
					prefix:          "[gray](from fact cache)[-]\n\n",
					characteristics: formatCharacteristics(hostname, cached),
					facts:           cached,
				}, nil
			}
		}
	}

	if skipLiveGather {
		return hostFactsResult{
			characteristics: "[maroon]Host unavailable.[-]\n",
		}, nil
	}

	pluginEnv, err := runner.CallbackPluginEnv()
	if err != nil {
		return hostFactsResult{}, err
	}
	args := append([]string{stubPath, "--limit", hostname}, rest...)
	cmd := exec.Command("ansible-playbook", args...)
	cmd.Env = append(os.Environ(), pluginEnv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()

	var raw json.RawMessage
	// otherHosts/eventCount are diagnostic-only, gathered regardless of
	// whether they end up needed: if hostname's own event never turns up,
	// knowing what *did* show up (every other hostname seen in any event's
	// own "hosts" map, and how many events were parsed at all) turns "no
	// result reported" from a dead end into an actionable clue - e.g. a
	// hostname that resolves via ansible-inventory but never matches any
	// runner event (a real, reported case - see design-docs/HostVerb.md's
	// own "Findings from discussion") shows up here as "0 events parsed"
	// or "events were seen, but only for: <other names>", either of which
	// points straight at the real cause instead of leaving it a mystery.
	otherHosts := map[string]bool{}
	eventCount := 0
	nonJSONLines := 0
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev playbook.RawEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			nonJSONLines++
			continue
		}
		eventCount++
		for h := range ev.Hosts {
			if h != hostname {
				otherHosts[h] = true
			}
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
			detail := fmt.Sprintf("%d jsonl events were parsed, none for this host", eventCount)
			if len(otherHosts) > 0 {
				names := make([]string, 0, len(otherHosts))
				for h := range otherHosts {
					names = append(names, h)
				}
				sort.Strings(names)
				detail = fmt.Sprintf("%d jsonl events were parsed, but only ever for: %s", eventCount, strings.Join(names, ", "))
			}
			if nonJSONLines > 0 {
				detail += fmt.Sprintf("; %d non-JSON line(s) on stdout were skipped", nonJSONLines)
			}
			msg = fmt.Sprintf("no result reported for host %q - check that it resolves in the inventory (%s)", hostname, detail)
		}
		return hostFactsResult{}, fmt.Errorf("%s", msg)
	}

	var decoded map[string]interface{}
	_ = json.Unmarshal(raw, &decoded)
	result := playbook.DecodeHostResult(raw)
	if result.Unreachable {
		return hostFactsResult{characteristics: fmt.Sprintf("[maroon::b]Unreachable[-::-]\n\n%s\n", tview.Escape(result.Msg))}, nil
	}
	if result.Failed {
		return hostFactsResult{characteristics: fmt.Sprintf("[red::b]Failed[-::-]\n\n%s\n", tview.Escape(result.Msg))}, nil
	}
	facts, _ := decoded["ansible_facts"].(map[string]interface{})
	return hostFactsResult{characteristics: formatCharacteristics(hostname, facts), facts: facts}, nil
}

// FactString/factStringList pull a string/[]string field out of a decoded
// ansible_facts map, tolerating an absent or wrongly-shaped key the same
// way DecodeHostResult tolerates a malformed payload elsewhere - "" / nil
// rather than a panic or an error, since a field simply not being
// gathered on a given platform is normal, not exceptional. key is the
// short fact name (e.g. "fqdn", "distribution") - both functions add the
// "ansible_" prefix themselves. Confirmed empirically (a real gather_facts
// run, not assumed from the `debug: var: ansible_facts` shape used
// elsewhere in this project, which is templated/prefix-stripped and
// looks different): the *task result JSON* this whole file reads
// (`v2_runner_on_ok`'s own `hosts.<host>.ansible_facts`) nests every
// fact under its full "ansible_<name>" key, not the short name - e.g.
// "ansible_fqdn", "ansible_processor", not "fqdn"/"processor".
func FactString(facts map[string]interface{}, key string) string {
	if v, ok := facts["ansible_"+key].(string); ok {
		return v
	}
	return ""
}

func FactStringList(facts map[string]interface{}, key string) []string {
	raw, ok := facts["ansible_"+key].([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// JoinNonEmpty joins only parts that are actually non-empty - used for
// "OS"/"Distribution" summary fields, each built from two separate facts
// that could individually be missing (e.g. ansible_kernel absent on a
// platform that doesn't report one).
func JoinNonEmpty(sep string, parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, sep)
}

// DedupProcessorModels extracts unique processor model-name strings out
// of ansible_facts' own "processor" field. Confirmed empirically (see
// host_test.go): that fact is a flat list repeating, once per logical
// core, a 3-element group of [core index, vendor id, model name] - e.g.
// ["0", "AuthenticAMD", "AMD Ryzen 5 3600 6-Core Processor", "1",
// "AuthenticAMD", "AMD Ryzen 5 3600 6-Core Processor", ...]. Rather than
// depend on that exact grouping (which could differ across platforms/
// ansible-core versions), this uses a simpler, platform-independent
// heuristic: a real model-name string always contains a space (e.g. "AMD
// Ryzen 5 3600 6-Core Processor"), while a core index ("0") or vendor id
// ("AuthenticAMD") never does - so filtering for entries containing a
// space and deduplicating, preserving first-seen order, reliably yields
// just the distinct model names (handling the rare heterogeneous-CPU case
// too) without needing to know the grouping width at all.
func DedupProcessorModels(raw interface{}) []string {
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var models []string
	for _, v := range list {
		s, ok := v.(string)
		if !ok || !strings.Contains(s, " ") {
			continue
		}
		if !seen[s] {
			seen[s] = true
			models = append(models, s)
		}
	}
	return models
}

// FormatRAM renders ansible_facts' own "memtotal_mb" (always a JSON
// number, so always a float64 once decoded into interface{}) as
// "N.N GB", one decimal place.
func FormatRAM(raw interface{}) string {
	mb, ok := raw.(float64)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.1f GB", mb/1024)
}

// VirtualizationContainerTechs/virtualizationVMTechs categorize
// ansible_facts' own "virtualization_type" (and, as a fallback,
// "virtualization_tech_guest" entries) into design-docs/HostVerb.md's own
// VM/Container/Bare Metal buckets - a documented heuristic, not an
// exhaustive list of every virtualization technology Ansible can report,
// same "good enough, not chased further" style as this project's other
// text-classification heuristics (e.g. TaskLabel's truncation,
// PrimaryOutputField's stdout-vs-msg choice). An unrecognized-but-real
// type falls back to showing the raw value rather than a wrong bucket.
var VirtualizationContainerTechs = map[string]bool{
	"docker": true, "lxc": true, "lxd": true, "podman": true,
	"container": true, "openvz": true, "jail": true, "chroot": true, "zone": true,
}
var VirtualizationVMTechs = map[string]bool{
	"kvm": true, "qemu": true, "vmware": true, "virtualbox": true,
	"xen": true, "hyperv": true, "parallels": true, "bhyve": true, "uml": true,
}

func ClassifyVirtualization(facts map[string]interface{}) string {
	role := FactString(facts, "virtualization_role")
	if role != "guest" {
		return "Bare Metal"
	}
	vtype := strings.ToLower(FactString(facts, "virtualization_type"))
	if VirtualizationContainerTechs[vtype] {
		return "Container"
	}
	if VirtualizationVMTechs[vtype] {
		return "VM"
	}
	for _, tech := range FactStringList(facts, "virtualization_tech_guest") {
		tech = strings.ToLower(tech)
		if VirtualizationContainerTechs[tech] {
			return "Container"
		}
		if VirtualizationVMTechs[tech] {
			return "VM"
		}
	}
	if vtype != "" {
		return vtype
	}
	return "Guest (unknown type)"
}

// FilterLinkLocal drops fe80::/10 link-local IPv6 addresses - present on
// essentially every interface and not generally useful for identifying a
// host, so they'd otherwise clutter the IPv6 summary line on any
// multi-interface host.
func FilterLinkLocal(addrs []string) []string {
	var out []string
	for _, a := range addrs {
		if strings.HasPrefix(a, "fe80:") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// HostKeyLine is one rendered "Host key (<type>):" line - label already
// includes its own trailing colon, so formatHostSummary's own padding
// logic can treat it exactly like every other field label.
type HostKeyLine struct {
	label string
	value string
}

// HostKeyTypeOrder is modern-to-legacy, matching design-docs/HostVerb.md's
// own "show all key types present" decision - every type the host
// actually has is shown, in this fixed order, rather than picking one.
var HostKeyTypeOrder = []string{"ed25519", "ecdsa", "rsa", "dsa"}

// HostKeyLines builds one line per SSH host key type ansible_facts
// actually gathered for this host - `ansible_ssh_host_key_<type>_public`
// holds the raw base64 key material, `..._public_keytype` the matching
// wire-format prefix (e.g. "ssh-ed25519") - confirmed empirically that
// these are two separate facts, not one combined line the way `ssh-keyscan`
// or an authorized_keys file would show it.
func HostKeyLines(facts map[string]interface{}) []HostKeyLine {
	var lines []HostKeyLine
	for _, kt := range HostKeyTypeOrder {
		pub := FactString(facts, "ssh_host_key_"+kt+"_public")
		if pub == "" {
			continue
		}
		prefix := FactString(facts, "ssh_host_key_"+kt+"_public_keytype")
		if prefix == "" {
			prefix = "ssh-" + kt
		}
		lines = append(lines, HostKeyLine{
			label: fmt.Sprintf("Host key (%s):", kt),
			value: prefix + " " + pub,
		})
	}
	return lines
}

// formatCharacteristics renders the Summary page's own fixed field list
// (design-docs/HostVerb.md's "Content summary page" draft, since restyled
// into the "Characteristics" section of the restructured page - see
// formatHostSummary), label-padded to line up.
func formatCharacteristics(hostname string, facts map[string]interface{}) string {
	type field struct{ label, value string }
	fields := []field{
		{"Host", hostname},
		{"FQDN", FactString(facts, "fqdn")},
		{"OS", JoinNonEmpty(", ", FactString(facts, "system"), FactString(facts, "kernel"))},
		{"Distribution", JoinNonEmpty(", ", FactString(facts, "distribution"), FactString(facts, "distribution_version"))},
		{"Architecture", FactString(facts, "architecture")},
		{"Processor", strings.Join(DedupProcessorModels(facts["ansible_processor"]), ", ")},
		{"RAM", FormatRAM(facts["ansible_memtotal_mb"])},
		{"Virtualization", ClassifyVirtualization(facts)},
		{"IPv4", strings.Join(FactStringList(facts, "all_ipv4_addresses"), ", ")},
		{"IPv6", strings.Join(FilterLinkLocal(FactStringList(facts, "all_ipv6_addresses")), ", ")},
	}

	width := 0
	for _, f := range fields {
		if l := len(f.label) + 1; l > width { // +1 for the trailing colon
			width = l
		}
	}

	var b strings.Builder
	for _, f := range fields {
		value := f.value
		if value == "" {
			value = "-"
		}
		label := f.label + ":"
		fmt.Fprintf(&b, "[green]%s[-]%s[lightsteelblue]%s[-]\n", label, strings.Repeat(" ", width-len(label)+1), tview.Escape(value))
	}
	return b.String()
}

// formatHostKeyLines renders hostKeyLines' own output, label-padded to
// line up - separately from formatCharacteristics' own padding width,
// since "Host key (ed25519):" is a different width than "IPv6:" and
// pooling the two into one shared width would either under- or over-pad
// one of the two blocks for no reason.
func formatHostKeyLines(keyLines []HostKeyLine) string {
	width := 0
	for _, k := range keyLines {
		if l := len(k.label); l > width {
			width = l
		}
	}
	var b strings.Builder
	for _, k := range keyLines {
		fmt.Fprintf(&b, "[green]%s[-]%s[lightsteelblue]%s[-]\n", k.label, strings.Repeat(" ", width-len(k.label)+1), tview.Escape(k.value))
	}
	return b.String()
}

// RecentRunsScanLimit/RecentRunsShowLimit bound fetchRecentRuns'
// (design-docs/HostVerb.md "New ideas") own state.toml scan: finding "the
// last RecentRunsShowLimit times this host was touched" means replaying
// candidate invocations, newest first, until that many matches turn up -
// unbounded, that could mean opening every invocation of every playbook
// ever run in the project for a rarely-targeted host. RecentRunsScanLimit
// caps how many of the most recent invocations project-wide are ever even
// looked at; RecentRunsShowLimit stops the scan early the moment enough
// matches are already found.
const (
	RecentRunsScanLimit = 20
	RecentRunsShowLimit = 5
)

// recentRunCandidate is one flattened invocation from state.toml's whole
// History - across every playbook/role ever recorded, not just the one
// this "host"/"hosts" invocation was itself given (design-docs/HostVerb.md's
// own decision: which playbooks have touched this host at all is itself
// useful information, kept even though it's inconsistent with the Plays
// tab's own single-playbook scope).
type recentRunCandidate struct {
	name string // PlaybookHistory.Playbook, or .Role if that's what was set
	config.InvocationRecord
}

// collectRecentRunCandidates flattens cfg's whole History into one
// newest-first list, already capped at RecentRunsScanLimit - an invocation
// with no RunID (never saved, or cleared by config.PruneMissingRunLogs) is
// dropped here, before any replay is attempted, matching every other
// run-log consumer's "best-effort, never an error" convention.
func collectRecentRunCandidates(cfg config.StateConfig) []recentRunCandidate {
	var all []recentRunCandidate
	for _, h := range cfg.History {
		name := h.Playbook
		if name == "" {
			name = h.Role
		}
		for _, inv := range h.Invocations {
			if inv.RunID == "" {
				continue
			}
			all = append(all, recentRunCandidate{name: name, InvocationRecord: inv})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, all[i].Time)
		tj, _ := time.Parse(time.RFC3339, all[j].Time)
		return ti.After(tj)
	})
	if len(all) > RecentRunsScanLimit {
		all = all[:RecentRunsScanLimit]
	}
	return all
}

// hostRunCounts mirrors internal/session/recap.go's own recapHostSummary,
// just the seven summary counts "Recent" shows, with none of that type's
// own Categories/TotalDuration tracking. Reimplemented locally rather than
// reused across the package boundary: recapForHost is unexported, and
// internal/session already imports internal/host (to dispatch the "host"/
// "hosts" verbs), so the reverse import would be a cycle.
type hostRunCounts struct {
	OK, Skipped, Changed, Unreachable, Failed, Warnings, Ignored int
}

// hostCountsFor scans every task across every play in state for host's own
// outcome, the same approach recapForHost uses for the live recap - found
// is false if host never appears in state.AllHosts at all (nothing to
// report, as opposed to a real zero-everywhere result).
func hostCountsFor(state *playbook.PlaybookState, host string) (hostRunCounts, bool) {
	idx := sort.SearchStrings(state.AllHosts, host)
	if idx >= len(state.AllHosts) || state.AllHosts[idx] != host {
		return hostRunCounts{}, false
	}
	var c hostRunCounts
	for _, play := range state.Plays {
		for _, task := range play.Tasks {
			o, present := task.Hosts[host]
			if !present {
				continue
			}
			switch o {
			case playbook.OutcomeOK:
				c.OK++
			case playbook.OutcomeChanged:
				c.Changed++
			case playbook.OutcomeSkipped:
				c.Skipped++
			case playbook.OutcomeUnreachable:
				c.Unreachable++
			case playbook.OutcomeFailed:
				c.Failed++
			}
			if task.Warnings[host] {
				c.Warnings++
			}
			if task.Ignored[host] {
				c.Ignored++
			}
		}
	}
	return c, true
}

// formatRecentTime renders InvocationRecord.Time (RFC3339 UTC, as
// config.AppendInvocation stamps it) in the local zone, readably -
// falling back to the raw stored string on a parse failure, the same
// caveat FormatRevisitTime (internal/revisit) already applies to the
// identical field for the identical reason.
func formatRecentTime(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.Local().Format("2006-01-02 15:04")
}

// recentRunLine is one already-matched "Recent" entry, ready to render.
type recentRunLine struct {
	name   string
	time   string
	counts hostRunCounts
}

// formatRecentRunLines renders lines' own "<time> <name>: ok=N ..." rows,
// the prefix padded to a shared width so the count fields line up across
// every row - the same label-padding convention formatCharacteristics/
// formatHostKeyLines already use, just against a prefix computed per row
// rather than a fixed label list.
func formatRecentRunLines(lines []recentRunLine) string {
	prefixes := make([]string, len(lines))
	width := 0
	for i, l := range lines {
		prefixes[i] = fmt.Sprintf("%s %s:", formatRecentTime(l.time), l.name)
		if w := len([]rune(prefixes[i])); w > width {
			width = w
		}
	}
	var b strings.Builder
	for i, l := range lines {
		pad := width - len([]rune(prefixes[i])) + 1
		fmt.Fprintf(&b, "[green]%s[-]%s[lightsteelblue]ok=%-4d skipped=%-4d changed=%-4d unreachable=%-4d failed=%-4d warnings=%-4d ignored=%d[-]\n",
			tview.Escape(prefixes[i]), strings.Repeat(" ", pad),
			l.counts.OK, l.counts.Skipped, l.counts.Changed, l.counts.Unreachable, l.counts.Failed, l.counts.Warnings, l.counts.Ignored)
	}
	return b.String()
}

// StreamRecentRuns renders design-docs/HostVerb.md's own "Recent" section
// incrementally: up to RecentRunsShowLimit matches, newest first, calling
// onMatch as each one is found rather than collecting everything before
// returning. This is the whole fix for a real, reported problem: scanning/
// replaying up to RecentRunsScanLimit run logs can genuinely take several
// seconds on a project with a long history (found live - roughly 8s on
// the reporting user's own hardware), and the Summary tab's other two
// sections (Characteristics, Keys) have nothing to do with this scan at
// all - blocking their own already-fast render on it, the way an earlier,
// all-at-once fetchRecentRuns did, made the whole tab feel hung for no
// reason. BuildHostDetailPrimitive's own Summary goroutine calls this
// after Characteristics/Keys are already showing, appending each match to
// the visible page as it arrives instead. Best-effort throughout, same as
// that earlier version: a missing/unparseable state.toml reads as "no
// history at all" (config.ReadState's own silent-on-missing behavior),
// and a candidate whose run log has since vanished from disk
// (runner.ReplayRunLog failing to open it) is silently skipped rather
// than surfaced as an error - the same "best-effort, never an error"
// treatment every other run-log consumer in this app already applies.
func StreamRecentRuns(hostname string, onMatch func(recentRunLine)) {
	cfg := config.ReadState(config.TangsibleStatePath)
	candidates := collectRecentRunCandidates(cfg)

	found := 0
	for _, c := range candidates {
		if found >= RecentRunsShowLimit {
			return
		}
		jsonlPath, _ := config.RunLogPaths(config.TangsibleStatePath, c.RunID)
		state, err := runner.ReplayRunLog(jsonlPath)
		if err != nil {
			continue
		}
		counts, matched := hostCountsFor(state, hostname)
		if !matched {
			continue
		}
		found++
		onMatch(recentRunLine{name: c.name, time: c.Time, counts: counts})
	}
}

// summaryDoc composes the Summary tab's own three sections
// (design-docs/HostVerb.md's restructured page) from whichever pieces are
// known so far - Characteristics and Keys are set once, immediately after
// the (possibly live) facts fetch completes; Recent grows one line at a
// time as StreamRecentRuns finds matches. render() is called again after
// every change, cheap since every piece is already-formatted, short text.
type summaryDoc struct {
	prefix          string // "(from fact cache)" note, or empty
	characteristics string // formatCharacteristics' own field list, or a
	// pre-rendered Unreachable/Failed/skipped note in its place (see
	// hostFactsResult) - either way, always the Characteristics section's
	// own body.
	recent string
	keys   string // empty when there's nothing to show - no Keys section at all then
}

func (d summaryDoc) render() string {
	var b strings.Builder
	b.WriteString(d.prefix)
	b.WriteString(hostSectionHeading("Characteristics"))
	b.WriteString(d.characteristics)

	b.WriteString("\n")
	b.WriteString(hostSectionHeading("Recent"))
	b.WriteString(d.recent)

	if d.keys != "" {
		b.WriteString("\n")
		b.WriteString(hostSectionHeading("Keys"))
		b.WriteString(d.keys)
	}
	return b.String()
}

// HostDetailTabNames is the fixed tab order buildHostDetailPrimitive
// always builds a fresh TabbedPane in - shared with runHostsListTUI's own
// n/N host-navigation (tabIndexByName), which needs to know this order to
// restore the same tab after rebuilding a brand new TabbedPane for the
// newly-selected host, since TabbedPane itself has no "jump to tab by
// name" method, only relative Next()/Prev().
var HostDetailTabNames = []string{"Summary", "Groups", "Plays", "host_vars", "Everything known"}

// TabIndexByName finds name's own index in names, defaulting to 0 (the
// first tab) if it's ever not found - shouldn't happen in practice, since
// every caller passes back a name TabbedPane.ActiveName() itself
// produced, but a silent, harmless fallback is better than a panic over a
// cosmetic detail like which tab a host-switch happens to land on.
func TabIndexByName(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return 0
}

// BuildHostDetailPrimitive builds design-docs/HostVerb.md's five-tab host
// detail view - a thin header (hostname + playbook, mirroring the
// "template" Verb's own header pattern) and a five-tab body (TabbedPane,
// tabs.go) - shared unchanged between the standalone "host <name>" Verb
// (runHostDetailStandalone) and the "hosts" Verb's own list-then-detail
// flow (runHostsListTUI); the two differ only in what Esc does, which the
// caller wires itself via its own SetInputCapture, not this function.
// Returns the built Flex alongside the TabbedPane/header/footer so the
// caller's own input/mouse capture can drive tab-switching and swallow
// clicks on the header/footer bars, the same way template.go's
// runTemplateTUI does inline for its own, simpler two-tab view.
//
// Every tab's own data is fetched concurrently, each on its own
// goroutine, starting the instant this function returns - not deferred
// until a tab is first viewed - per design-docs/HostVerb.md's own
// "Findings from discussion". The view itself (and each tab's own
// placeholder text) appears immediately; each goroutine updates its own
// tab via app.QueueUpdateDraw once its own fetch completes - the same
// async-update mechanism resolved.go/ansibledoc.go already use for the
// drill-down view's own Resolved/Docs tabs, just kicked off eagerly for
// every tab at once instead of lazily per tab-open.
// pingKnown/pingOK carry the hosts list's own already-known connectivity
// dot for this host, if any (false/false for the standalone "host" Verb,
// which has no ping subsystem at all - RunHostDetailStandalone always
// passes that) - see the Summary tab's own progressive-fetch goroutine
// below for what this changes.
func BuildHostDetailPrimitive(app *tview.Application, stubPath, hostname, playbook string, rest []string, footerText string, pingKnown, pingOK bool) (tview.Primitive, *uikit.TabbedPane, *tview.TextView, *uikit.TabSearchBar) {
	header := tview.NewTextView().SetDynamicColors(true)
	header.SetTextStyle(uikit.BarStyle)
	playbookLabel := playbook
	if playbookLabel == "" {
		playbookLabel = "(none)"
	}
	header.SetText(fmt.Sprintf(" Host: %s   Playbook: %s ", tview.Escape(hostname), tview.Escape(playbookLabel)))

	summaryView := tview.NewTextView().SetDynamicColors(true).SetText("Gathering facts...")
	groupsView := tview.NewTextView().SetDynamicColors(true).SetText("Loading...")
	playsView := tview.NewTextView().SetDynamicColors(true).SetText("Loading...")
	hostVarsView := tview.NewTextView().SetDynamicColors(true).SetText("Loading...")
	everythingView := tview.NewTextView().SetDynamicColors(true).SetText("Loading...")

	tabs := uikit.NewTabbedPane()
	tabs.SetTabs(
		HostDetailTabNames,
		[]tview.Primitive{summaryView, groupsView, playsView, hostVarsView, everythingView},
	)

	// searchBar (design-docs/Search.md) replaces the plain footer TextView
	// in flex's own bottom slot - it owns its own hint/search-prompt/
	// match-status states internally, see its own doc comment (uikit).
	// focus falls back to tabs.Primitive() once a search prompt closes,
	// matching this view's own initial SetFocus (both callers below).
	searchBar := uikit.NewTabSearchBar(app, tabs, footerText, tabs.Primitive())

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(tabs.Primitive(), 0, 1, true).
		AddItem(searchBar.Primitive(), 1, 0, false)

	fetch := func(view *tview.TextView, do func() (string, error)) {
		go func() {
			text, err := do()
			app.QueueUpdateDraw(func() {
				searchBar.ClearForView(view) // this tab's own content is
				// about to change out from under any search targeting it
				// specifically - design-docs/Search.md's "a search does
				// not survive content changing under it," scoped to just
				// this one tab so a fetch landing on a *different* tab
				// doesn't clear a search that's still perfectly valid.
				if err != nil {
					view.SetText("[red::b]Error[-::-]\n\n" + tview.Escape(err.Error()))
				} else {
					view.SetText(text)
				}
				view.ScrollToBeginning()
			})
		}()
	}

	// summaryFacts is how Summary's own goroutine hands its resolved
	// ansible_facts (if any) over to Everything Known's own goroutine
	// below, once - buffered so Summary's send never blocks regardless of
	// whether Everything Known has reached its own receive yet. See
	// mergeFactsIntoEverythingKnown's own doc comment for why this
	// handoff exists at all: without it, Everything Known's own quick,
	// cache-only read races Summary's own (possibly live-gathering) one
	// and almost always loses.
	summaryFacts := make(chan map[string]interface{}, 1)

	// Summary gets its own bespoke goroutine rather than the generic fetch
	// helper above: design-docs/HostVerb.md's own "New ideas" findings -
	// Characteristics/Keys (fast: one cache-check-or-live-gather call) show
	// immediately, then Recent streams in on top of that separately
	// (StreamRecentRuns), since its own state.toml/run-log scan can take
	// several seconds on a project with a long history and has nothing to
	// do with facts at all - blocking the whole tab on it made it feel
	// hung for no reason. skipLiveGather (a host the list's own ping dot
	// already found unreachable) avoids a second, doomed connection
	// attempt just to build this tab - see FetchHostFacts' own doc comment.
	go func() {
		res, err := FetchHostFacts(stubPath, hostname, rest, pingKnown && !pingOK)
		if err != nil {
			summaryFacts <- nil // never leave Everything Known's own
			// receive blocked forever just because Summary itself failed
			app.QueueUpdateDraw(func() {
				searchBar.ClearForView(summaryView)
				summaryView.SetText("[red::b]Error[-::-]\n\n" + tview.Escape(err.Error()))
				summaryView.ScrollToBeginning()
			})
			return
		}
		summaryFacts <- res.facts // nil for any of hostFactsResult's own
		// pre-rendered cases (Unreachable/Failed/skipped) - Everything
		// Known's own receiver already treats an empty map as "nothing to
		// merge," so a nil send here needs no special-casing there.
		doc := summaryDoc{prefix: res.prefix, characteristics: res.characteristics, recent: "scanning recent runs..."}
		if keyLines := HostKeyLines(res.facts); len(keyLines) > 0 {
			doc.keys = formatHostKeyLines(keyLines)
		}
		app.QueueUpdateDraw(func() {
			searchBar.ClearForView(summaryView)
			summaryView.SetText(doc.render())
			summaryView.ScrollToBeginning()
		})

		var lines []recentRunLine
		StreamRecentRuns(hostname, func(l recentRunLine) {
			lines = append(lines, l)
			app.QueueUpdateDraw(func() {
				doc.recent = formatRecentRunLines(lines)
				summaryView.SetText(doc.render()) // SetText alone doesn't
				// reset scroll position (tui_drilldown.go's own gotcha,
				// confirmed here too) - deliberately not calling
				// ScrollToBeginning() again on every one of these: doing
				// so would yank the view back to the top every time a new
				// Recent line lands, fighting anyone who scrolled down
				// while it was still streaming in.
			})
		})
		if len(lines) == 0 {
			app.QueueUpdateDraw(func() {
				doc.recent = "no recorded runs found that touched this host\n"
				summaryView.SetText(doc.render())
			})
		}
	}()

	fetch(groupsView, func() (string, error) { return FetchHostGroups(hostname, rest) })
	fetch(playsView, func() (string, error) { return FetchHostPlays(playbook, rest, hostname) })
	fetch(hostVarsView, func() (string, error) { return FetchHostVars(hostname, playbook, rest) })

	// Everything known also gets its own bespoke goroutine rather than the
	// generic fetch helper: it renders immediately from its own
	// ansible-inventory --host call exactly as before (unchanged speed),
	// then re-renders a second time once summaryFacts delivers whatever
	// Summary's own goroutine (above) found - merging those facts in
	// (mergeFactsIntoEverythingKnown) rather than leaving this tab
	// dependent on winning its own race against Summary's live gather.
	go func() {
		raw, err := FetchHostEverythingKnownRaw(hostname, rest)
		app.QueueUpdateDraw(func() {
			searchBar.ClearForView(everythingView)
			if err != nil {
				everythingView.SetText("[red::b]Error[-::-]\n\n" + tview.Escape(err.Error()))
			} else {
				everythingView.SetText(highlightJSONKeys(string(raw)))
			}
			everythingView.ScrollToBeginning()
		})
		if err != nil {
			return
		}
		facts := <-summaryFacts
		if len(facts) == 0 {
			return
		}
		merged, mergeErr := mergeFactsIntoEverythingKnown(raw, facts)
		if mergeErr != nil {
			return // best-effort - the plain ansible-inventory dump
			// already shown above stays as the final answer
		}
		app.QueueUpdateDraw(func() {
			everythingView.SetText(highlightJSONKeys(string(merged)))
		})
	}()

	return flex, tabs, header, searchBar
}

// RunHostDetailStandalone builds and runs "tangsible host <hostname>"'s
// own standalone program (design-docs/HostVerb.md) - a single
// tview.Application showing exactly one host's detail view for the
// process's entire lifetime, no list to go back to. Esc is deliberately
// inert here, same reasoning and precedent as the "template" Verb's own
// view (template.go/runTemplateTUI): only q/Ctrl-C quit, so idly browsing
// tabs can never close the whole thing by reflex.
func RunHostDetailStandalone(hostname, playbook string, rest []string, stubPath string) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	footerText := " tab/shift-tab: switch tab  /: search tab  y: copy tab  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom "
	detail, tabs, header, searchBar := BuildHostDetailPrimitive(app, stubPath, hostname, playbook, rest, footerText, false, false)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			searchBar.CloseComposing()
			app.Stop()
			return nil
		}
		// design-docs/Search.md, same reasoning as tui.go's identically-
		// shaped branch: bypasses normal focus-driven dispatch, confirmed
		// live to not reliably reach a primitive nested this deep.
		if searchBar.IsComposing() {
			searchBar.HandleComposingKey(event)
			return nil
		}
		switch {
		case event.Key() == tcell.KeyEscape && searchBar.HasActive():
			searchBar.Clear()
			return nil
		case event.Rune() == 'q':
			app.Stop()
			return nil
		case event.Key() == tcell.KeyCtrlA:
			return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlE:
			return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
		case event.Key() == tcell.KeyTab:
			tabs.Next()
			return nil
		case event.Key() == tcell.KeyBacktab:
			tabs.Prev()
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
		}
		return event
	})

	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if event == nil {
			return nil, action
		}
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

	app.SetRoot(detail, true).SetFocus(tabs.Primitive())
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
	}
}

// HostRowText renders one row of "hosts"'s own list - plain white text
// normally, or black bold text on a light gray background when selected,
// the same "cursor row" convention every other selectable row in this
// app uses (PlayRowText/TaskLabel/HostLabel's own selected parameter,
// tui.go). pingKnown/pingOK back design-docs/HostVerb.md's own "New
// ideas" connectivity dot: a green "●" once this host's own
// ansible.builtin.ping has come back OK, red once it's come back
// anything else (failed/unreachable/couldn't even run), nothing at all
// while that row's own ping is still in flight - lazy per-row update, no
// placeholder spinner, matching that decision's own "row shows its dot
// the moment its own ping lands" wording.
func HostRowText(hostname string, selected bool, pingKnown, pingOK bool) string {
	color := "gray" // pending - that row's own ping hasn't landed yet
	if pingKnown {
		color = "red"
		if pingOK {
			color = "green"
		}
	}
	dot := fmt.Sprintf("[%s]●[-]  ", color)
	if selected {
		return dot + fmt.Sprintf("[%s:lightgray:b]%s[-:-:-]", uikit.PureBlack, tview.Escape(hostname))
	}
	return dot + "[white]" + tview.Escape(hostname) + "[-]"
}

// RunHostsListTUI implements "tangsible hosts"'s own list-then-detail
// flow: a scrollable TreeList (treelist.go - the same widget the main
// tree view uses) of every host, each row showing its own
// ansible.builtin.ping connectivity dot (design-docs/HostVerb.md's own
// "New ideas", hostPingStubYAML/FetchHostPing) once that host's own ping
// has come back. On a wide enough terminal, list and detail show side by
// side (splitMode below) with the detail pane live-syncing to whichever
// host the list's own cursor sits on, no Enter needed; on a narrower
// terminal, Enter opens the identical five-tab detail view "tangsible
// host <name>" would show for that host (buildHostDetailPrimitive), Esc
// closes it back to the list - the one behavioral difference from "host
// <name>"'s own standalone Esc-is-inert view. While a detail view is
// open (either layout), n/N also jump straight to the next/previous host
// in the list (navigateHostDetail) - the same n/N convention the main
// tree's own drill-down view uses to hop between hosts for the same
// task, applied here to hopping between hosts directly.
//
// Built as a single tview.Application with a three-page Pages ("list",
// "detail", "split") - "list"/detailFlex are built once and reused
// unchanged across page switches (the same "same primitive, multiple
// named pages, only one ever frontmost" trick tui.go's own s.treeBody/
// outputBody use for their three-page main/output/split split); "detail"
// is rebuilt fresh (buildHostDetailPrimitive called again) each time a
// different host is selected rather than kept alive/cached across
// selections - design-docs/HostVerb.md's own "Findings from discussion"
// never asked for cross-host caching, and a fresh five-way concurrent
// fetch per selection is cheap enough at this project's own ~10-host
// target scale; "split"'s own outer wrapper Flex, in contrast, is
// rebuilt on every layout() call regardless of whether the host changed -
// cheap, since it only ever re-arranges already-built primitives, never
// re-fetches anything.
func RunHostsListTUI(hosts []string, playbook string, rest []string, stubPath, pingStubPath string) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	list := uikit.NewTreeList()
	pages := tview.NewPages()

	listHeader := tview.NewTextView().SetDynamicColors(true).
		SetText(fmt.Sprintf(" %d hosts ", len(hosts)))
	listHeader.SetTextStyle(uikit.BarStyle)
	listFooter := tview.NewTextView().SetDynamicColors(true).
		SetText(" enter: open host  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom ")
	listFooter.SetTextStyle(uikit.BarStyle)

	listFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(listHeader, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(listFooter, 1, 0, false)
	pages.AddPage("list", listFlex, true, true)

	// splitDivider is design-docs/TwoPanedLayout.md's own one-column
	// vertical rule, reused unchanged here for this "New ideas" two-pane
	// hosts layout - a bare Box whose Draw() fills its own rect with this
	// color, no content needed for a solid separator line. tcell.ColorNavy
	// matches uikit.BarStyle's own background, the same chrome color
	// listHeader/listFooter/the detail header already use.
	splitDivider := tview.NewBox().SetBackgroundColor(tcell.ColorNavy)

	var (
		viewingDetail   bool // a detail view (either layout) is showing at all
		splitMode       bool // meaningful only while viewingDetail: side-by-side vs full-screen
		detailTabs      *uikit.TabbedPane
		detailHeader    *tview.TextView
		detailSearch    *uikit.TabSearchBar
		detailPrimitive tview.Primitive
		currentHostname string
		lastTotalWidth  int
		quitting        atomic.Bool
	)

	detailFooterText := " tab/shift-tab: switch tab  n/N: next/prev host  /: search tab  y: copy tab  esc: back to list  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom "
	// splitDetailFooterText drops "esc: back to list" - split mode has no
	// separate list page to go back to, the list is already visible right
	// alongside. A known, accepted simplification: this text is fixed at
	// BuildHostDetailPrimitive's own construction time and TabSearchBar
	// has no way to update it later, so a detail view opened in one
	// layout keeps that layout's own footer text even if a mid-session
	// resize (layout, below) later switches which layout is actually
	// showing it.
	splitDetailFooterText := " tab/shift-tab: switch tab  n/N: next/prev host  /: search tab  y: copy tab  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom "

	var showDetail func(hostname string)
	var layout func()

	// pingKnown/pingOK back each row's own connectivity dot (HostRowText) -
	// populated lazily, one goroutine per host, kicked off below.
	pingKnown := map[string]bool{}
	pingOK := map[string]bool{}

	// TreeList (treelist.go), unlike tview.List, has no built-in "this is
	// the current row" highlighting at all - tui.go's own tree gets its
	// visible cursor purely by re-rendering whichever row is current with
	// different style tags on every change (see its own rebuild()), never
	// from the widget itself. A first version of this list added each
	// host's row once, plain, and never did that - a real, reported bug:
	// the cursor moved (Enter still opened the right host) but nothing
	// ever looked selected. Fixed the same way tui.go's own tree is: a
	// selectedIdx tracked here, and a full rebuildRows pass - re-adding
	// every row, this one row styled per hostRowText's own selected
	// variant (now also carrying that row's own ping dot) - triggered on
	// every genuine cursor move.
	//
	// rebuilding guards against the same self-triggering hazard tui.go's
	// own rebuild() documents: list.Clear() followed by re-AddItem()
	// fires the list's own SetChangedFunc the instant the first row lands
	// back in the now-empty list (index -1 -> 0), which would otherwise
	// immediately re-enter rebuildRows recursively.
	selectedIdx := 0
	rebuilding := false
	var rebuildRows func()
	rebuildRows = func() {
		rebuilding = true
		defer func() { rebuilding = false }()
		list.Clear()
		for i, h := range hosts {
			h := h
			list.AddItem(HostRowText(h, i == selectedIdx, pingKnown[h], pingOK[h]), func() { showDetail(h) })
		}
		list.SetCurrentItem(selectedIdx)
	}

	// navigateHostDetail switches the open detail view to the previous/
	// next host in the same order the list itself uses (hosts, already
	// alphabetically sorted - inventory.FlattenInventoryHosts) - no
	// wraparound at either end, matching this app's own navigation
	// convention everywhere else (e.g. tui.go's navigateMainTask). Also
	// moves the list's own cursor (selectedIdx/rebuildRows), not just the
	// detail pane, so the two can never disagree about which host is
	// "open" regardless of layout - a real gap in the pre-two-pane
	// version, which left the list cursor stale after a plain n/N hop
	// since the list was never visible at the same time anyway. The
	// currently active tab is preserved across the switch by name
	// (tabIndexByName) rather than always resetting to Summary -
	// showDetail rebuilds a brand new TabbedPane from scratch for the new
	// host (there's no way to just re-point an existing one at different
	// content), so the old TabbedPane's own active tab has to be looked
	// up by name and re-applied via repeated Next() calls on the new one.
	navigateHostDetail := func(delta int) {
		idx := -1
		for i, h := range hosts {
			if h == currentHostname {
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
		activeTabName := detailTabs.ActiveName()
		selectedIdx = newIdx
		rebuildRows()
		showDetail(hosts[newIdx])
		for i := 0; i < TabIndexByName(HostDetailTabNames, activeTabName); i++ {
			detailTabs.Next()
		}
	}

	showDetail = func(hostname string) {
		_, _, totalWidth, _ := pages.GetInnerRect()
		footer := detailFooterText
		if totalWidth >= uikit.SplitMinTotalWidth {
			footer = splitDetailFooterText
		}
		detail, tabs, hdr, bar := BuildHostDetailPrimitive(app, stubPath, hostname, playbook, rest, footer, pingKnown[hostname], pingOK[hostname])
		pages.RemovePage("detail")
		pages.AddPage("detail", detail, true, true)
		detailTabs, detailHeader, detailSearch, detailPrimitive = tabs, hdr, bar, detail
		currentHostname = hostname
		viewingDetail = true
		layout()
	}

	// layout decides, on every call, whether the terminal is currently
	// wide enough for the side-by-side two-pane view (design-docs/
	// HostVerb.md's "New ideas") and arranges pages accordingly - called
	// after every host selection (showDetail) and, since nothing else in
	// this standalone view already re-evaluates this on a timer, by a
	// dedicated resize-watcher goroutine below (mirroring tui.go's own
	// startResizeWatcher, needed for the identical reason: nothing else
	// notices a bare terminal resize with no other event to piggyback
	// on).
	layout = func() {
		_, _, totalWidth, _ := pages.GetInnerRect()
		lastTotalWidth = totalWidth
		splitMode = totalWidth >= uikit.SplitMinTotalWidth
		if splitMode {
			if !viewingDetail {
				// Wide enough but nothing ever opened yet - split mode
				// has no "nothing open" state at all (the detail pane is
				// always right there alongside the list), so this
				// auto-opens whatever the list's own cursor already sits
				// on, matching design-docs/HostVerb.md's own "moving the
				// cursor should immediately update the detail pane".
				// Recurses exactly once (showDetail sets viewingDetail
				// before calling back into layout), never deeper.
				showDetail(hosts[selectedIdx])
				return
			}
			splitFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
				AddItem(listFlex, uikit.SplitTreeWidth(totalWidth), 0, false).
				AddItem(splitDivider, uikit.SplitDividerWidth, 0, false).
				AddItem(detailPrimitive, 0, 1, true)
			pages.RemovePage("split")
			pages.AddPage("split", splitFlex, true, true)
			pages.SwitchToPage("split")
			// detailTabs, not list - the same real focus target as the
			// full-screen "detail" page just below, so ↑/↓ scroll the
			// active tab's own content in split mode too, matching how it
			// already works in full-screen mode. A real, reported bug
			// otherwise: with focus left on list, ↑/↓ only ever moved the
			// host cursor, and there was no way to scroll a tab's own
			// content at all. Host-to-host navigation while split still
			// works via n/N (handleKey intercepts those regardless of
			// real focus) and via a mouse click/wheel on the list pane
			// itself (TreeList's own MouseHandler moves real focus back
			// onto list when clicked, same as any other widget).
			app.SetFocus(detailTabs.Primitive())
			return
		}
		if viewingDetail {
			pages.SwitchToPage("detail")
			app.SetFocus(detailTabs.Primitive())
		} else {
			pages.SwitchToPage("list")
			app.SetFocus(list)
		}
	}

	list.SetChangedFunc(func(index int) {
		if rebuilding {
			return
		}
		selectedIdx = index
		rebuildRows()
		if splitMode {
			// Live-sync (design-docs/HostVerb.md's own "New ideas"): in
			// split mode, moving the list's own cursor is what opens/
			// retargets the detail pane, no Enter needed. Deliberately no
			// debounce on rapid movement (e.g. holding Down) - accepted
			// as a real cost (each move fires a fresh concurrent 5-tab
			// fetch for whichever host the cursor lands on next) until
			// live use actually shows it's a problem, not guessed at up
			// front.
			showDetail(hosts[index])
		}
	})
	rebuildRows()

	// Every row's own connectivity dot (design-docs/HostVerb.md) fires
	// the instant the list itself renders - one goroutine per host, the
	// same "kick off eagerly, populate via app.QueueUpdateDraw once done"
	// pattern BuildHostDetailPrimitive's own fetch helper already uses for
	// the five detail tabs, just one goroutine per row instead of one per
	// tab. A row shows its dot the moment its own ping lands, independent
	// of every other row's timing - no batching, no "wait for all to
	// finish" gate.
	for _, h := range hosts {
		h := h
		go func() {
			ok := FetchHostPing(pingStubPath, h, rest)
			app.QueueUpdateDraw(func() {
				pingKnown[h] = true
				pingOK[h] = ok
				rebuildRows()
			})
		}()
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			if viewingDetail {
				detailSearch.CloseComposing()
			}
			quitting.Store(true)
			app.Stop()
			return nil
		}
		// design-docs/Search.md, same reasoning as tui.go's identically-
		// shaped branch: bypasses normal focus-driven dispatch, confirmed
		// live to not reliably reach a primitive nested this deep.
		if viewingDetail && detailSearch.IsComposing() {
			detailSearch.HandleComposingKey(event)
			return nil
		}
		switch {
		case event.Rune() == 'q':
			quitting.Store(true)
			app.Stop()
			return nil
		case event.Key() == tcell.KeyCtrlA:
			return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlE:
			return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
		}
		// Detail-specific bindings apply whenever a detail view exists at
		// all - both the full-screen "detail" page and the split-mode
		// "split" page show one, unlike the old onList-only gate this
		// replaced, which only ever had to distinguish two pages, not
		// three.
		if viewingDetail {
			switch {
			case event.Key() == tcell.KeyEscape && detailSearch.HasActive():
				detailSearch.Clear()
				return nil
			case event.Key() == tcell.KeyEscape && !splitMode:
				pages.SwitchToPage("list")
				viewingDetail = false
				app.SetFocus(list)
				return nil
			case event.Key() == tcell.KeyTab:
				detailTabs.Next()
				return nil
			case event.Key() == tcell.KeyBacktab:
				detailTabs.Prev()
				return nil
			// n/N are context-sensitive here, the same way tui.go's own
			// task-hop n/N become match-nav while a search is active: this
			// view already used n/N for host-hop (navigateHostDetail)
			// before search existed, and there's no separate key budget to
			// give search its own - see design-docs/Search.md's own
			// discussion of this exact collision.
			case event.Rune() == 'n':
				if detailSearch.HasActive() {
					detailSearch.Next()
				} else {
					navigateHostDetail(1)
				}
				return nil
			case event.Rune() == 'N':
				if detailSearch.HasActive() {
					detailSearch.Prev()
				} else {
					navigateHostDetail(-1)
				}
				return nil
			case event.Rune() == '/':
				detailSearch.Open()
				return nil
			case event.Rune() == 'y':
				detailSearch.ShowMessage(uikit.CopyActiveTabStatus(detailTabs))
				return nil
			}
		}
		return event
	})

	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if event == nil {
			return nil, action
		}
		if !viewingDetail {
			if x, y := event.Position(); uikit.InRect(x, y, listHeader) || uikit.InRect(x, y, listFooter) {
				return nil, action
			}
			return event, action
		}
		if x, y := event.Position(); uikit.InRect(x, y, detailHeader) || uikit.InRect(x, y, detailSearch.Primitive()) {
			return nil, action
		}
		if action == tview.MouseLeftClick {
			if x, y := event.Position(); detailTabs.HandleClick(x, y) {
				return nil, action
			}
		}
		if splitMode {
			// The list pane is also on screen in split mode and stays
			// clickable/scrollable exactly as it is in full-screen list
			// mode - list's own row-select callback (showDetail) is what
			// live-syncs the detail pane, so clicking a row does the same
			// thing n/N already does. Real keyboard focus defaults to
			// detailTabs.Primitive() in split mode now (layout, above),
			// not list - so ↑/↓ scroll the active tab's own content by
			// default, matching full-screen mode; clicking a list row
			// still moves real focus onto list for as long as the user
			// keeps navigating there (TreeList.MouseHandler's own
			// unconditional setFocus on click), reverting back to
			// detailTabs on the next host selection (showDetail/layout).
			if x, y := event.Position(); uikit.InRect(x, y, listHeader) || uikit.InRect(x, y, listFooter) {
				return nil, action
			}
		}
		return event, action
	})

	// A dedicated resize-watcher goroutine, mirroring tui.go's own
	// startResizeWatcher: this view has no other ticker at all (fully
	// event-driven, no live jsonl stream to piggyback a periodic rebuild
	// on), so a bare terminal resize with no other keyboard/mouse event
	// needs its own watcher to ever notice split mode should change.
	go func() {
		ticker := time.NewTicker(uikit.SpinnerInterval)
		defer ticker.Stop()
		for range ticker.C {
			if quitting.Load() {
				return
			}
			app.QueueUpdate(func() { // NOT QueueUpdateDraw - avoid forcing
				// a real screen redraw on every tick when nothing changed.
				_, _, totalWidth, _ := pages.GetInnerRect()
				if totalWidth != lastTotalWidth {
					layout()
					// app.Draw() would deadlock here - see tui.go's own
					// startResizeWatcher for why ForceDraw() is the one
					// safe way to force a redraw from inside a queued
					// update.
					app.ForceDraw()
				}
			})
		}
	}()

	app.SetRoot(pages, true).SetFocus(list)
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
	}
}
