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

// Implements the "host" verb (design-docs/HostVerb.md): a standalone,
// five-tab program showing everything Tangsible can determine about one
// host - live gathered facts, inventory group membership, which plays
// would run for it, its own host_vars files, and the raw
// "ansible-inventory --host" dump - entirely separate from the
// run/rerun/role verbs' own live tree UI, the same way "template" is
// (template.go).
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

// parseHostArgs splits args (everything after the "host" verb) into the
// required hostname, an optional playbook, and everything else as
// passthrough args - "tangsible host <hostname> [<playbook>] [-i ...]
// [-e ...]" (design-docs/HostVerb.md), the same shape parseTemplateArgs
// (template.go) already uses for "<path> [<hostname>]", just with the two
// positionals' roles swapped: hostname is only recognized when it's the
// *first* leading positional; playbook only when it's the *second*,
// immediately after hostname and before any flag-shaped token. ok is
// false if no hostname was given at all (a missing or flag-shaped first
// argument).
func parseHostArgs(args []string) (hostname, playbookArg string, rest []string, ok bool) {
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

// runHostVerb is "tangsible host <hostname> [<playbook>]"'s own entry
// point - resolves the playbook the same cascade "run" uses when it
// isn't given explicitly (a missing/unresolved playbook isn't fatal here:
// only the Plays tab actually needs one, and reports its own absence
// gracefully - see fetchHostPlays), creates the one stub playbook the
// Summary tab's live fact-gathering needs, and shows the standalone
// detail view for the process's entire lifetime.
func runHostVerb(args []string) int {
	hostname, playbookArg, rest, ok := parseHostArgs(args)
	if !ok {
		fmt.Fprintf(os.Stderr, "usage: %s host <hostname> [<playbook>] [ansible-playbook args...]\n", os.Args[0])
		return 2
	}
	playbook := playbookArg
	if playbook == "" {
		playbook, _ = resolvePlaybook()
	}

	stubPath, err := writeHostSummaryStub()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook: %v\n", err)
		return 1
	}
	defer os.Remove(stubPath)

	runHostDetailStandalone(hostname, playbook, rest, stubPath)
	return 0
}

// runHostsVerb is "tangsible hosts [<playbook>]"'s own entry point -
// lists every inventory host up front (ansible-inventory --list, the
// same call template.go's resolveInventoryHost already makes for its own
// single-host resolution) and shows the list-then-detail flow.
func runHostsVerb(args []string) int {
	playbookArg, rest, _ := splitPlaybookArgs(args)
	playbook := playbookArg
	if playbook == "" {
		playbook, _ = resolvePlaybook()
	}

	hosts, err := listInventoryHosts(rest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't list inventory hosts: %v\n", err)
		return 1
	}
	if len(hosts) == 0 {
		fmt.Fprintln(os.Stderr, "tangsible: no hosts found in the inventory")
		return 1
	}

	stubPath, err := writeHostSummaryStub()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook: %v\n", err)
		return 1
	}
	defer os.Remove(stubPath)

	runHostsListTUI(hosts, playbook, rest, stubPath)
	return 0
}

// listInventoryHosts runs `ansible-inventory --list`, forwarding
// passthroughArgs verbatim, and returns every host it finds
// (flattenInventoryHosts, template.go) - shared by the "hosts" verb's own
// full listing and, via resolveInventoryHost, "template"'s single-host
// resolution.
func listInventoryHosts(passthroughArgs []string) ([]string, error) {
	cmd := exec.Command("ansible-inventory", append([]string{"--list"}, passthroughArgs...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ansible-inventory --list failed: %s", msg)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
		return nil, fmt.Errorf("ansible-inventory --list didn't produce valid JSON (%v) - it printed:\n%s", err, snippet)
	}
	return flattenInventoryHosts(raw), nil
}

// groupMembership is one entry in a host's own transitive group chain
// (hostGroupChain) - Via is "" for a group the host is a *direct* member
// of (listed under that group's own "hosts:" in the inventory), or the
// name of the child group through which this ancestor group was reached
// otherwise.
type groupMembership struct {
	Group string
	Via   string
}

// hostGroupChain returns every group hostname transitively belongs to,
// per design-docs/HostVerb.md's own decision to show the full chain, not
// just direct membership: direct groups first (alphabetically, for
// determinism), then each further ancestor layer outward, also
// alphabetically within its own layer. raw is `ansible-inventory --list`'s
// own decoded JSON (see ansibleInventoryGroup, template.go) - the same
// source flattenInventoryHosts already reads, just walked in the opposite
// direction: that function walks group→hosts to build one flat host set;
// this one needs host→ancestor-groups, which the JSON's own "children:"
// pointers don't give directly (only parent→children is stored, never
// child→parent) - so this builds its own reverse (child→parents) index
// first, then works outward from the host via BFS.
func hostGroupChain(raw map[string]json.RawMessage, hostname string) []groupMembership {
	groups := make(map[string]ansibleInventoryGroup, len(raw))
	for name, data := range raw {
		if name == "_meta" {
			continue
		}
		var g ansibleInventoryGroup
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

	var chain []groupMembership
	seen := map[string]bool{}
	queue := make([]string, len(direct))
	for i, name := range direct {
		chain = append(chain, groupMembership{Group: name})
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
			chain = append(chain, groupMembership{Group: parent, Via: nextVia[parent]})
		}
		queue = next
	}

	return chain
}

// fetchHostGroups runs `ansible-inventory --list` and renders hostname's
// own full transitive group chain (hostGroupChain), one line per group,
// left-aligned to the widest group name so the "(direct)"/"(via ...)"
// annotations line up.
func fetchHostGroups(hostname string, rest []string) (string, error) {
	cmd := exec.Command("ansible-inventory", append([]string{"--list"}, rest...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
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

	chain := hostGroupChain(raw, hostname)
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
			fmt.Fprintf(&b, "%s\n", tview.Escape(m.Group))
			continue
		}
		detail := "(direct)"
		if m.Via != "" {
			detail = fmt.Sprintf("(via %s)", m.Via)
		}
		fmt.Fprintf(&b, "%s%s  %s\n", tview.Escape(m.Group), strings.Repeat(" ", pad), tview.Escape(detail))
	}
	return b.String(), nil
}

// extractInventoryDirs pulls every -i/--inventory value out of rest
// (both "--flag value" and "--flag=value" long forms, same convention
// parsePassthroughArgs uses in rerunargs.go for --tags/--limit) and
// returns the directory each one lives in, for any that resolve to a
// real file or directory on disk - silently skipping anything else (a
// bare comma-list like "web1,web2,", a nonexistent path) since neither has
// a meaningful directory to look for a sibling host_vars/ under.
func extractInventoryDirs(rest []string) []string {
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

// discoverHostVarsFiles returns every host_vars file for hostname found
// under any of dirs, sorted for determinism - Ansible looks for host_vars
// as a sibling of both the inventory source and the playbook (ansible-core's
// own documented behavior), so dirs is expected to already carry both
// candidates by the time this is called (extractInventoryDirs plus the
// playbook's own directory - see fetchHostVars). Matches both shapes
// Ansible itself recognizes: a single host_vars/<hostname>.yml (or .yaml)
// file, and a host_vars/<hostname>/ directory of multiple files.
// Deduplicated by absolute path, since the playbook and an inventory
// source can easily share the same directory.
func discoverHostVarsFiles(hostname string, dirs []string) []string {
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

// fetchHostVars renders every host_vars file found for hostname verbatim
// (design-docs/HostVerb.md's own "Findings from discussion": raw file
// content, one section per file, not a merged key-value view - preserves
// comments/formatting and needs no variable-precedence logic), one
// sectionLabel-headed section per file, in discoverHostVarsFiles' own
// sorted order.
func fetchHostVars(hostname, playbook string, rest []string) (string, error) {
	var dirs []string
	if playbook != "" {
		if abs, err := filepath.Abs(playbook); err == nil {
			dirs = append(dirs, filepath.Dir(abs))
		}
	}
	dirs = append(dirs, extractInventoryDirs(rest)...)

	files := discoverHostVarsFiles(hostname, dirs)
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
		b.WriteString(sectionLabel("orange", path))
		b.WriteString(tview.Escape(content))
		b.WriteString("\n\n")
	}
	return b.String(), nil
}

// fetchHostPlays runs "ansible-playbook <playbook> <rest...> --limit
// <hostname> --list-tasks --list-hosts" and groups the flattened
// progressEntry sequence parseListTasksOutput (progress.go) already
// produces back into per-play sections, reusing that parser directly
// rather than reimplementing it - narrowed to exactly this host via the
// same --limit flag progress.go's own doc comment already explains is
// required alongside --list-hosts for a limit to actually apply at all.
// Unlike buildProgressSkeleton (progress.go), which is always
// best-effort and swallows every failure silently (fine for an optional
// progress indicator riding on top of an already-working run), this
// surfaces a real failure as err, since an empty Plays tab needs to stay
// distinguishable from "the whole invocation failed."
func fetchHostPlays(playbook string, rest []string, hostname string) (string, error) {
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
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}

	entries := parseListTasksOutput(stdout.String())
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
			b.WriteString(sectionLabel("orange", e.Play))
			currentPlay = e.Play
		}
		fmt.Fprintf(&b, "  %s\n", tview.Escape(e.Task))
	}
	return b.String(), nil
}

// runAnsibleInventoryHost runs "ansible-inventory --host <hostname>" and
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
func runAnsibleInventoryHost(hostname string, rest []string) ([]byte, error) {
	cmd := exec.Command("ansible-inventory", append([]string{"--host", hostname}, rest...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ansible-inventory --host failed: %s", msg)
	}
	return out, nil
}

// fetchHostEverythingKnown is design-docs/HostVerb.md's own fifth tab -
// runAnsibleInventoryHost's own output shown verbatim (that tool's
// default output is already pretty-printed JSON - confirmed empirically,
// no reformatting needed). So this tab's content is: always declared
// inventory data, plus whichever gathered facts happen to be sitting in
// the fact cache already, if any - not fetched live either way, but not
// guaranteed static either (see runAnsibleInventoryHost's own doc
// comment). The Summary tab (fetchHostSummary) is the one that forces a
// fresh, live gather when the cache doesn't already have what it needs.
func fetchHostEverythingKnown(hostname string, rest []string) (string, error) {
	out, err := runAnsibleInventoryHost(hostname, rest)
	if err != nil {
		return "", err
	}
	return tview.Escape(strings.TrimRight(string(out), "\n")), nil
}

// hostSummaryStubYAML is the play design-docs/HostVerb.md's Summary tab
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
// pre-emptively. ignore_unreachable matches the "template" verb's own
// stub (template.go/writeTemplateStub) - moot in practice since --limit
// always narrows this to exactly one host, but harmless and consistent.
const hostSummaryStubYAML = "- hosts: all\n  gather_facts: false\n  ignore_unreachable: true\n  tasks:\n    - name: gather facts\n      ansible.builtin.setup:\n"

// writeHostSummaryStub writes hostSummaryStubYAML to a fresh temp file -
// reused, unchanged, across every host summary fetch in one tangsible
// session (the "hosts" verb's own list-then-detail flow can view many
// hosts one after another), same "one stable scratch file for the whole
// session" convention as the "template" verb's own stub/output pair.
func writeHostSummaryStub() (string, error) {
	f, err := os.CreateTemp("", "tangsible-host-summary-*.yml")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(hostSummaryStubYAML); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// factCacheCanaryKey is checked against runAnsibleInventoryHost's own
// output to decide whether a usable, fresh fact-cache entry already
// exists for this host - "ansible_architecture" is part of ansible's own
// default (min) gather_subset and essentially always present whenever
// any real gather has ever happened at all, regardless of which further,
// more specific subsets (hardware/network/virtual/...) were also
// gathered - a reasonable single presence check rather than requiring
// every individual field formatHostSummary might want.
const factCacheCanaryKey = "ansible_architecture"

// fetchHostSummary first checks whether a fresh fact-cache entry already
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
// Falling through to the live path: stubPath runs synchronously, narrowed
// to hostname via --limit, and this extracts that host's own
// ansible_facts from the resulting jsonl stream - the same
// scan-for-one-host's-own-event pattern renderTemplate (template.go)
// already uses. err is non-nil only when nothing usable could be
// determined at all (a bad inventory/host); an unreachable/failed host is
// reported as ordinary displayable text instead, not err, since that's
// expected, common content for this tab, not a tool failure.
func fetchHostSummary(stubPath, hostname string, rest []string) (string, error) {
	if out, err := runAnsibleInventoryHost(hostname, rest); err == nil {
		var cached map[string]interface{}
		if json.Unmarshal(out, &cached) == nil {
			if _, ok := cached[factCacheCanaryKey]; ok {
				return "[gray](from fact cache)[-]\n\n" + formatHostSummary(hostname, cached), nil
			}
		}
	}

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
		var ev rawEvent
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
			msg = runErr.Error()
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
		return "", fmt.Errorf("%s", msg)
	}

	var decoded map[string]interface{}
	_ = json.Unmarshal(raw, &decoded)
	result := decodeHostResult(raw)
	if result.Unreachable {
		return fmt.Sprintf("[maroon::b]Unreachable[-::-]\n\n%s", tview.Escape(result.Msg)), nil
	}
	if result.Failed {
		return fmt.Sprintf("[red::b]Failed[-::-]\n\n%s", tview.Escape(result.Msg)), nil
	}
	facts, _ := decoded["ansible_facts"].(map[string]interface{})
	return formatHostSummary(hostname, facts), nil
}

// factString/factStringList pull a string/[]string field out of a decoded
// ansible_facts map, tolerating an absent or wrongly-shaped key the same
// way decodeHostResult tolerates a malformed payload elsewhere - "" / nil
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
func factString(facts map[string]interface{}, key string) string {
	if v, ok := facts["ansible_"+key].(string); ok {
		return v
	}
	return ""
}

func factStringList(facts map[string]interface{}, key string) []string {
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

// joinNonEmpty joins only parts that are actually non-empty - used for
// "OS"/"Distribution" summary fields, each built from two separate facts
// that could individually be missing (e.g. ansible_kernel absent on a
// platform that doesn't report one).
func joinNonEmpty(sep string, parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, sep)
}

// dedupProcessorModels extracts unique processor model-name strings out
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
func dedupProcessorModels(raw interface{}) []string {
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

// formatRAM renders ansible_facts' own "memtotal_mb" (always a JSON
// number, so always a float64 once decoded into interface{}) as
// "N.N GB", one decimal place.
func formatRAM(raw interface{}) string {
	mb, ok := raw.(float64)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.1f GB", mb/1024)
}

// virtualizationContainerTechs/virtualizationVMTechs categorize
// ansible_facts' own "virtualization_type" (and, as a fallback,
// "virtualization_tech_guest" entries) into design-docs/HostVerb.md's own
// VM/Container/Bare Metal buckets - a documented heuristic, not an
// exhaustive list of every virtualization technology Ansible can report,
// same "good enough, not chased further" style as this project's other
// text-classification heuristics (e.g. taskLabel's truncation,
// primaryOutputField's stdout-vs-msg choice). An unrecognized-but-real
// type falls back to showing the raw value rather than a wrong bucket.
var virtualizationContainerTechs = map[string]bool{
	"docker": true, "lxc": true, "lxd": true, "podman": true,
	"container": true, "openvz": true, "jail": true, "chroot": true, "zone": true,
}
var virtualizationVMTechs = map[string]bool{
	"kvm": true, "qemu": true, "vmware": true, "virtualbox": true,
	"xen": true, "hyperv": true, "parallels": true, "bhyve": true, "uml": true,
}

func classifyVirtualization(facts map[string]interface{}) string {
	role := factString(facts, "virtualization_role")
	if role != "guest" {
		return "Bare Metal"
	}
	vtype := strings.ToLower(factString(facts, "virtualization_type"))
	if virtualizationContainerTechs[vtype] {
		return "Container"
	}
	if virtualizationVMTechs[vtype] {
		return "VM"
	}
	for _, tech := range factStringList(facts, "virtualization_tech_guest") {
		tech = strings.ToLower(tech)
		if virtualizationContainerTechs[tech] {
			return "Container"
		}
		if virtualizationVMTechs[tech] {
			return "VM"
		}
	}
	if vtype != "" {
		return vtype
	}
	return "Guest (unknown type)"
}

// filterLinkLocal drops fe80::/10 link-local IPv6 addresses - present on
// essentially every interface and not generally useful for identifying a
// host, so they'd otherwise clutter the IPv6 summary line on any
// multi-interface host.
func filterLinkLocal(addrs []string) []string {
	var out []string
	for _, a := range addrs {
		if strings.HasPrefix(a, "fe80:") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// hostKeyLine is one rendered "Host key (<type>):" line - label already
// includes its own trailing colon, so formatHostSummary's own padding
// logic can treat it exactly like every other field label.
type hostKeyLine struct {
	label string
	value string
}

// hostKeyTypeOrder is modern-to-legacy, matching design-docs/HostVerb.md's
// own "show all key types present" decision - every type the host
// actually has is shown, in this fixed order, rather than picking one.
var hostKeyTypeOrder = []string{"ed25519", "ecdsa", "rsa", "dsa"}

// hostKeyLines builds one line per SSH host key type ansible_facts
// actually gathered for this host - `ansible_ssh_host_key_<type>_public`
// holds the raw base64 key material, `..._public_keytype` the matching
// wire-format prefix (e.g. "ssh-ed25519") - confirmed empirically that
// these are two separate facts, not one combined line the way `ssh-keyscan`
// or an authorized_keys file would show it.
func hostKeyLines(facts map[string]interface{}) []hostKeyLine {
	var lines []hostKeyLine
	for _, kt := range hostKeyTypeOrder {
		pub := factString(facts, "ssh_host_key_"+kt+"_public")
		if pub == "" {
			continue
		}
		prefix := factString(facts, "ssh_host_key_"+kt+"_public_keytype")
		if prefix == "" {
			prefix = "ssh-" + kt
		}
		lines = append(lines, hostKeyLine{
			label: fmt.Sprintf("Host key (%s):", kt),
			value: prefix + " " + pub,
		})
	}
	return lines
}

// formatHostSummary renders design-docs/HostVerb.md's own "Content
// summary page" draft from a decoded ansible_facts map - a fixed field
// list, label-padded to line up, plus one further, separately-padded
// block of Host key lines (see hostKeyLines) since that block's own
// labels ("Host key (ed25519):") are a different width than the main
// field labels and pooling them into one shared width would either
// under-pad the main fields or over-pad them for no reason.
func formatHostSummary(hostname string, facts map[string]interface{}) string {
	type field struct{ label, value string }
	fields := []field{
		{"Host", hostname},
		{"FQDN", factString(facts, "fqdn")},
		{"OS", joinNonEmpty(", ", factString(facts, "system"), factString(facts, "kernel"))},
		{"Distribution", joinNonEmpty(", ", factString(facts, "distribution"), factString(facts, "distribution_version"))},
		{"Architecture", factString(facts, "architecture")},
		{"Processor", strings.Join(dedupProcessorModels(facts["ansible_processor"]), ", ")},
		{"RAM", formatRAM(facts["ansible_memtotal_mb"])},
		{"Virtualization", classifyVirtualization(facts)},
		{"IPv4", strings.Join(factStringList(facts, "all_ipv4_addresses"), ", ")},
		{"IPv6", strings.Join(filterLinkLocal(factStringList(facts, "all_ipv6_addresses")), ", ")},
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
		fmt.Fprintf(&b, "%s%s%s\n", label, strings.Repeat(" ", width-len(label)+1), tview.Escape(value))
	}

	keyLines := hostKeyLines(facts)
	if len(keyLines) > 0 {
		b.WriteString("\n")
		keyWidth := 0
		for _, k := range keyLines {
			if l := len(k.label); l > keyWidth {
				keyWidth = l
			}
		}
		for _, k := range keyLines {
			fmt.Fprintf(&b, "%s%s%s\n", k.label, strings.Repeat(" ", keyWidth-len(k.label)+1), tview.Escape(k.value))
		}
	}
	return b.String()
}

// hostDetailTabNames is the fixed tab order buildHostDetailPrimitive
// always builds a fresh tabbedPane in - shared with runHostsListTUI's own
// n/p host-navigation (tabIndexByName), which needs to know this order to
// restore the same tab after rebuilding a brand new tabbedPane for the
// newly-selected host, since tabbedPane itself has no "jump to tab by
// name" method, only relative Next()/Prev().
var hostDetailTabNames = []string{"Summary", "Groups", "Plays", "host_vars", "Everything known"}

// tabIndexByName finds name's own index in names, defaulting to 0 (the
// first tab) if it's ever not found - shouldn't happen in practice, since
// every caller passes back a name tabbedPane.ActiveName() itself
// produced, but a silent, harmless fallback is better than a panic over a
// cosmetic detail like which tab a host-switch happens to land on.
func tabIndexByName(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return 0
}

// buildHostDetailPrimitive builds design-docs/HostVerb.md's five-tab host
// detail view - a thin header (hostname + playbook, mirroring the
// "template" verb's own header pattern) and a five-tab body (tabbedPane,
// tabs.go) - shared unchanged between the standalone "host <name>" verb
// (runHostDetailStandalone) and the "hosts" verb's own list-then-detail
// flow (runHostsListTUI); the two differ only in what Esc does, which the
// caller wires itself via its own SetInputCapture, not this function.
// Returns the built Flex alongside the tabbedPane/header/footer so the
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
func buildHostDetailPrimitive(app *tview.Application, stubPath, hostname, playbook string, rest []string, footerText string) (tview.Primitive, *tabbedPane, *tview.TextView, *tview.TextView) {
	header := tview.NewTextView().SetDynamicColors(true)
	header.SetTextStyle(barStyle)
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

	tabs := newTabbedPane()
	tabs.SetTabs(
		hostDetailTabNames,
		[]tview.Primitive{summaryView, groupsView, playsView, hostVarsView, everythingView},
	)

	footer := tview.NewTextView().SetDynamicColors(true).SetText(footerText)
	footer.SetTextStyle(barStyle)

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(tabs.Primitive(), 0, 1, true).
		AddItem(footer, 1, 0, false)

	fetch := func(view *tview.TextView, do func() (string, error)) {
		go func() {
			text, err := do()
			app.QueueUpdateDraw(func() {
				if err != nil {
					view.SetText("[red::b]Error[-::-]\n\n" + tview.Escape(err.Error()))
				} else {
					view.SetText(text)
				}
				view.ScrollToBeginning()
			})
		}()
	}

	fetch(summaryView, func() (string, error) { return fetchHostSummary(stubPath, hostname, rest) })
	fetch(groupsView, func() (string, error) { return fetchHostGroups(hostname, rest) })
	fetch(playsView, func() (string, error) { return fetchHostPlays(playbook, rest, hostname) })
	fetch(hostVarsView, func() (string, error) { return fetchHostVars(hostname, playbook, rest) })
	fetch(everythingView, func() (string, error) { return fetchHostEverythingKnown(hostname, rest) })

	return flex, tabs, header, footer
}

// runHostDetailStandalone builds and runs "tangsible host <hostname>"'s
// own standalone program (design-docs/HostVerb.md) - a single
// tview.Application showing exactly one host's detail view for the
// process's entire lifetime, no list to go back to. Esc is deliberately
// inert here, same reasoning and precedent as the "template" verb's own
// view (template.go/runTemplateTUI): only q/Ctrl-C quit, so idly browsing
// tabs can never close the whole thing by reflex.
func runHostDetailStandalone(hostname, playbook string, rest []string, stubPath string) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	footerText := " tab/shift-tab: switch tab  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom "
	detail, tabs, header, footer := buildHostDetailPrimitive(app, stubPath, hostname, playbook, rest, footerText)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyCtrlC:
			app.Stop()
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
		}
		return event
	})

	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if event == nil {
			return nil, action
		}
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

	app.SetRoot(detail, true).SetFocus(tabs.Primitive())
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
	}
}

// hostRowText renders one row of "hosts"'s own list - plain white text
// normally, or black bold text on a light gray background when selected,
// the same "cursor row" convention every other selectable row in this
// app uses (playRowText/taskLabel/hostLabel's own selected parameter,
// tui.go).
func hostRowText(hostname string, selected bool) string {
	if selected {
		return fmt.Sprintf("[%s:lightgray:b]%s[-:-:-]", pureBlack, tview.Escape(hostname))
	}
	return "[white]" + tview.Escape(hostname) + "[-]"
}

// runHostsListTUI implements "tangsible hosts"'s own list-then-detail
// flow: a scrollable treeList (treelist.go - the same widget the main
// tree view uses) of every host, Enter opens the identical five-tab
// detail view "tangsible host <name>" would show for that same host
// (buildHostDetailPrimitive), Esc from the detail view returns to the
// list rather than quitting - the one behavioral difference from "host
// <name>"'s own standalone Esc-is-inert view. While a detail view is
// open, n/p also jump straight to the next/previous host in the list
// (navigateHostDetail) without leaving the detail view at all - the same
// n/p convention the main tree's own drill-down view uses to hop between
// hosts for the same task, applied here to hopping between hosts
// directly. Built as a single tview.Application with a two-page Pages
// (list, detail); detail's own
// content is rebuilt fresh (buildHostDetailPrimitive called again) each
// time a different host is selected rather than kept alive/cached across
// selections - design-docs/HostVerb.md's own "Findings from discussion"
// never asked for cross-host caching, and a fresh five-way concurrent
// fetch per selection is cheap enough at this project's own ~10-host
// target scale.
func runHostsListTUI(hosts []string, playbook string, rest []string, stubPath string) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	list := newTreeList()
	pages := tview.NewPages()

	listHeader := tview.NewTextView().SetDynamicColors(true).
		SetText(fmt.Sprintf(" %d hosts ", len(hosts)))
	listHeader.SetTextStyle(barStyle)
	listFooter := tview.NewTextView().SetDynamicColors(true).
		SetText(" enter: open host  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom ")
	listFooter.SetTextStyle(barStyle)

	listFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(listHeader, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(listFooter, 1, 0, false)
	pages.AddPage("list", listFlex, true, true)

	var (
		onList          = true
		detailTabs      *tabbedPane
		detailHeader    *tview.TextView
		detailFooter    *tview.TextView
		currentHostname string
	)

	detailFooterText := " tab/shift-tab: switch tab  n/p: prev/next host  esc: back to list  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom "

	showDetail := func(hostname string) {
		detail, tabs, hdr, ftr := buildHostDetailPrimitive(app, stubPath, hostname, playbook, rest, detailFooterText)
		pages.RemovePage("detail")
		pages.AddPage("detail", detail, true, true)
		detailTabs, detailHeader, detailFooter = tabs, hdr, ftr
		currentHostname = hostname
		pages.SwitchToPage("detail")
		onList = false
		app.SetFocus(tabs.Primitive())
	}

	// navigateHostDetail switches the open detail view to the previous/
	// next host in the same order the list itself uses (hosts, already
	// alphabetically sorted - flattenInventoryHosts) - no wraparound at
	// either end, matching this app's own navigation convention
	// everywhere else (e.g. tui.go's navigateMainTask). The currently
	// active tab is preserved across the switch by name (tabIndexByName)
	// rather than always resetting to Summary: showDetail rebuilds a
	// brand new tabbedPane from scratch for the new host (there's no way
	// to just re-point an existing one at different content), so the old
	// tabbedPane's own active tab has to be looked up by name and
	// re-applied via repeated Next() calls on the new one.
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
		showDetail(hosts[newIdx])
		for i := 0; i < tabIndexByName(hostDetailTabNames, activeTabName); i++ {
			detailTabs.Next()
		}
	}

	// treeList (treelist.go), unlike tview.List, has no built-in "this is
	// the current row" highlighting at all - tui.go's own tree gets its
	// visible cursor purely by re-rendering whichever row is current with
	// different style tags on every change (see its own rebuild()), never
	// from the widget itself. A first version of this list added each
	// host's row once, plain, and never did that - a real, reported bug:
	// the cursor moved (Enter still opened the right host) but nothing
	// ever looked selected. Fixed the same way tui.go's own tree is: a
	// selectedIdx tracked here, and a full rebuildRows pass - re-adding
	// every row, this one row styled per hostRowText's own selected
	// variant - triggered on every genuine cursor move.
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
			list.AddItem(hostRowText(h, i == selectedIdx), func() { showDetail(h) })
		}
		list.SetCurrentItem(selectedIdx)
	}
	list.SetChangedFunc(func(index int) {
		if rebuilding {
			return
		}
		selectedIdx = index
		rebuildRows()
	})
	rebuildRows()

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyCtrlC, event.Rune() == 'q':
			app.Stop()
			return nil
		case event.Key() == tcell.KeyCtrlA:
			return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlE:
			return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
		}
		if onList {
			return event
		}
		switch {
		case event.Key() == tcell.KeyEscape:
			pages.SwitchToPage("list")
			onList = true
			app.SetFocus(list)
			return nil
		case event.Key() == tcell.KeyTab:
			detailTabs.Next()
			return nil
		case event.Key() == tcell.KeyBacktab:
			detailTabs.Prev()
			return nil
		case event.Rune() == 'n':
			navigateHostDetail(1)
			return nil
		case event.Rune() == 'p':
			navigateHostDetail(-1)
			return nil
		}
		return event
	})

	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if event == nil {
			return nil, action
		}
		if onList {
			if x, y := event.Position(); inRect(x, y, listHeader) || inRect(x, y, listFooter) {
				return nil, action
			}
			return event, action
		}
		if x, y := event.Position(); inRect(x, y, detailHeader) || inRect(x, y, detailFooter) {
			return nil, action
		}
		if action == tview.MouseLeftClick {
			if x, y := event.Position(); detailTabs.HandleClick(x, y) {
				return nil, action
			}
		}
		return event, action
	})

	app.SetRoot(pages, true).SetFocus(list)
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
	}
}
