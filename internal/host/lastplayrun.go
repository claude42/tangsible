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

package host

import (
	"path/filepath"
	"sort"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
)

// The Plays tab's "last ran" line (design-docs/HostVerb.md, "Plays tab"):
// for every play listed, when it last ran on this host, found by replaying
// the playbook's own saved run logs newest first.

// lastPlayRun is one play's most recent run on the host.
type lastPlayRun struct {
	time   string // InvocationRecord.Time (RFC3339)
	failed bool   // a task failed for the host (ignored failures don't count)
	check  bool   // the run was a --check dry run
}

// hostPlayRuns reports, per play name in state, whether the play ran on host
// and whether it failed there. "Ran" means at least one task has an outcome
// for the host other than unreachable - an unreachable host never executed
// anything, and a play whose tasks were all filtered out (--tags) has no
// task rows at all. Plays sharing a name are merged.
func hostPlayRuns(state *playbook.PlaybookState, host string) map[string]lastPlayRun {
	out := map[string]lastPlayRun{}
	for _, play := range state.Plays {
		r, seen := out[play.Name]
		for _, task := range play.Tasks {
			o, present := task.Hosts[host]
			if !present || o == playbook.OutcomeUnreachable {
				continue
			}
			seen = true
			if o == playbook.OutcomeFailed && !task.Ignored[host] {
				r.failed = true
			}
		}
		if seen {
			out[play.Name] = r
		}
	}
	return out
}

// replaySavedRun loads one saved run's events (.tangsible/runs/<runID>.jsonl).
func replaySavedRun(runID string) (*playbook.PlaybookState, error) {
	jsonlPath, _ := config.RunLogPaths(config.TangsibleStatePath, runID)
	return runner.ReplayRunLog(jsonlPath)
}

// samePlaybookPath compares two playbook paths as given, so "site.yml" and
// "./site.yml" (or an absolute spelling) are the same one.
func samePlaybookPath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	aa, errA := filepath.Abs(a)
	ab, errB := filepath.Abs(b)
	return errA == nil && errB == nil && aa == ab
}

// LastPlayRuns finds, for each play name in wanted, the newest saved run of
// playbookPath in which that play ran on host. Plays with no such run are
// absent from the result (the history is capped, so "no result" means "not
// in the retained runs"). replay loads one saved run by RunID; a run it
// can't load is skipped. Stops as soon as every wanted play is found.
func LastPlayRuns(cfg config.StateConfig, playbookPath, host string, wanted map[string]bool, replay func(runID string) (*playbook.PlaybookState, error)) map[string]lastPlayRun {
	var runs []config.InvocationRecord
	for _, h := range cfg.History {
		if h.Playbook == "" || !samePlaybookPath(h.Playbook, playbookPath) {
			continue
		}
		for _, inv := range h.Invocations {
			if inv.RunID != "" {
				runs = append(runs, inv)
			}
		}
	}
	sort.SliceStable(runs, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, runs[i].Time)
		tj, _ := time.Parse(time.RFC3339, runs[j].Time)
		return ti.After(tj)
	})

	found := map[string]lastPlayRun{}
	for _, inv := range runs {
		if len(found) >= len(wanted) {
			break
		}
		state, err := replay(inv.RunID)
		if err != nil {
			continue
		}
		check := config.HasCheckFlag(config.HistoryStringToArgs(inv.Args))
		for name, r := range hostPlayRuns(state, host) {
			if _, done := found[name]; done || !wanted[name] {
				continue
			}
			r.time, r.check = inv.Time, check
			found[name] = r
		}
	}
	return found
}

// formatLastPlayRun renders the text in the brackets after a play's
// heading: "last ran 2026-10-20 14:03 · ok", with "failed" and/or ", check"
// as they apply. A play with no retained run gets no text at all.
func formatLastPlayRun(r lastPlayRun) string {
	status := "ok"
	if r.failed {
		status = "failed"
	}
	if r.check {
		status += ", check"
	}
	return "last ran " + formatRecentTime(r.time) + " · " + status
}
