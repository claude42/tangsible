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

package main

import "strings"

// csvSetEqual reports whether a and b represent the same SET of comma-
// separated values (whitespace-trimmed per entry, order-insensitive,
// duplicates collapsed) - csvOverlap's own sibling (revisitresolve.go),
// but requiring equality rather than merely an overlap. Backs
// resolveDiffCandidates' own "did this past run use the exact same tags/
// hosts as the current session" check (design-docs/Diff.md: "a different
// set of hosts" disqualifies a candidate outright, not just a partial
// mismatch - diffing two runs that didn't actually execute the same tasks
// wouldn't mean much).
func csvSetEqual(a, b string) bool {
	toSet := func(s string) map[string]bool {
		set := map[string]bool{}
		for _, v := range strings.Split(s, ",") {
			if v = strings.TrimSpace(v); v != "" {
				set[v] = true
			}
		}
		return set
	}
	setA, setB := toSet(a), toSet(b)
	if len(setA) != len(setB) {
		return false
	}
	for v := range setA {
		if !setB[v] {
			return false
		}
	}
	return true
}

// resolveDiffCandidates flattens cfg's history into the runs offerable as
// a diff comparison target for the current session (design-docs/Diff.md):
// same playbook/role, and the exact same tags/hosts as the current
// session's own (csvSetEqual, not csvOverlap - unlike resolveRevisitEntries'
// own CLI-driven filter, a partial tag/host match here would silently
// compare two runs that didn't actually execute the same set of tasks).
// excludeRunID (the session currently on screen) is never offered against
// itself. Deliberately ignores every other passthrough arg (-e, -i, -vvv,
// ...) - only playbook/role and tags/hosts are checked, per design-docs/
// Diff.md's own answer. Same "has a RunID" revisitability precondition and
// newest-first ordering as resolveRevisitEntries - pruneMissingRunLogs is
// expected to have already run; this function has no I/O of its own.
func resolveDiffCandidates(currentPlaybook, currentRole, currentTags, currentHosts, excludeRunID string, cfg stateConfig) []revisitEntry {
	var entries []revisitEntry
	for _, h := range cfg.History {
		if h.Playbook != currentPlaybook || h.Role != currentRole {
			continue
		}
		for _, inv := range h.Invocations {
			if inv.RunID == "" || inv.RunID == excludeRunID {
				continue
			}
			invArgs := parsePassthroughArgs(historyStringToArgs(inv.Args))
			if !csvSetEqual(invArgs.Tags, currentTags) || !csvSetEqual(invArgs.Hosts, currentHosts) {
				continue
			}
			entries = append(entries, newRevisitEntry(h, inv))
		}
	}
	sortRevisitEntriesNewestFirst(entries)
	return entries
}
