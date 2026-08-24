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

import (
	"sort"
	"strings"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
)

// revisitEntry is one flattened, revisitable InvocationRecord - one
// generation whose saved run data still exists (see InvocationRecord's own
// doc comment, history.go: RunID is only ever non-empty when it does).
// Exactly one of Playbook/Role is set, mirroring PlaybookHistory's own
// shape.
type revisitEntry struct {
	Playbook string
	Role     string
	Args     string
	Time     string
	ExitCode int
	RunID    string
}

// resolveRevisitEntries flattens every revisitable invocation across cfg's
// whole history into one list, filtered by the "revisit" Verb's own args
// (design-docs/Revisit.md: an optional positional playbook/role, and/or
// -l/--tags) and sorted newest-first. Pure - no I/O of its own, so directly
// testable. The actual pruning of entries whose saved files have gone
// missing (turning a once-real RunID back to "" - see InvocationRecord's
// own doc comment) happens earlier, in PruneMissingRunLogs (history.go),
// which is what makes "has a RunID" alone a reliable revisitability filter
// here, with no filesystem access needed in this function at all.
//
// Filtering: an explicit positional playbook/role (same shape
// SplitPlaybookArgs already uses for "run") keeps only that one target's
// own history. -l/--tags, parsed the same way ParsePassthroughArgs already
// does elsewhere, keep only entries whose own recorded Args overlap - at
// least one requested host, or one requested tag, is also present in that
// entry's own comma-separated set (csvOverlap) - matching ansible's own
// --tags/-l OR semantics for a comma list, not a strict subset requirement.
// Everything unset (a bare "tangsible revisit") means no filtering at all.
func resolveRevisitEntries(args []string, cfg config.StateConfig) []revisitEntry {
	target, rest, explicit := config.SplitPlaybookArgs(args)
	filter := config.ParsePassthroughArgs(rest)

	var entries []revisitEntry
	for _, h := range cfg.History {
		if explicit && h.Playbook != target && h.Role != target {
			continue
		}
		for _, inv := range h.Invocations {
			if inv.RunID == "" {
				continue // nothing left to revisit - see PruneMissingRunLogs
			}
			invArgs := config.ParsePassthroughArgs(config.HistoryStringToArgs(inv.Args))
			if filter.Hosts != "" && !csvOverlap(filter.Hosts, invArgs.Hosts) {
				continue
			}
			if filter.Tags != "" && !csvOverlap(filter.Tags, invArgs.Tags) {
				continue
			}
			entries = append(entries, newRevisitEntry(h, inv))
		}
	}

	sortRevisitEntriesNewestFirst(entries)
	return entries
}

// newRevisitEntry builds one revisitEntry from a PlaybookHistory entry and
// one of its own invocationRecords - shared by resolveRevisitEntries and
// design-docs/Diff.md's own resolveDiffCandidates (diffresolve.go), so the
// two can't silently drift on what a revisitEntry's fields mean.
func newRevisitEntry(h config.PlaybookHistory, inv config.InvocationRecord) revisitEntry {
	code := 0
	if inv.ExitCode != nil {
		code = *inv.ExitCode
	}
	return revisitEntry{
		Playbook: h.Playbook,
		Role:     h.Role,
		Args:     inv.Args,
		Time:     inv.Time,
		ExitCode: code,
		RunID:    inv.RunID,
	}
}

// sortRevisitEntriesNewestFirst sorts entries in place, newest Time first -
// shared by resolveRevisitEntries and resolveDiffCandidates. A Time that
// fails to parse (shouldn't happen for anything AppendInvocation itself
// ever wrote, but not trusted blindly - same caveat this project applies
// to every other event-derived field) falls back to time.Time's own zero
// value, which naturally sorts to the end - no separate fallback branch
// needed.
func sortRevisitEntriesNewestFirst(entries []revisitEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, entries[i].Time)
		tj, _ := time.Parse(time.RFC3339, entries[j].Time)
		return ti.After(tj)
	})
}

// csvOverlap reports whether any comma-separated value in want also
// appears in have (whitespace-trimmed per entry).
func csvOverlap(want, have string) bool {
	haveSet := map[string]bool{}
	for _, h := range strings.Split(have, ",") {
		if h = strings.TrimSpace(h); h != "" {
			haveSet[h] = true
		}
	}
	for _, w := range strings.Split(want, ",") {
		if w = strings.TrimSpace(w); w != "" && haveSet[w] {
			return true
		}
	}
	return false
}
