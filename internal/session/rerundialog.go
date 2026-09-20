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

import "sort"

// resolvedRerunHosts computes the re-run dialog's "Limit hosts to" field
// contents from the Only-failed/Only-unreachable checkboxes' current
// checked state, per design-docs/Rerun.md's "Extend rerun dialog": the
// union of whichever checkbox(es) are checked, deduplicated (a host can
// genuinely be both failed, on one task, and unreachable, on another,
// within the same run - a plain concatenation would list it twice when
// both checkboxes are checked) and sorted for a stable, predictable field
// value regardless of which checkbox was toggled most recently.
//
// Pulled out of NewLiveTUI's setHostsFieldFromCheckboxes closure so the
// actual host-selection logic can be tested without constructing any
// tview widgets - the checkbox callback ordering that closure also has to
// handle (tview.Checkbox.SetChecked invokes its own changed-callback
// before updating its internal checked field) is a real tview interaction
// and stays there, tested via e2e instead; this function only owns "given
// the resolved checked state, which hosts."
func resolvedRerunHosts(onlyFailed, onlyUnreachable bool, failedHosts, unreachableHosts []string) []string {
	seen := map[string]bool{}
	var hosts []string
	add := func(hs []string) {
		for _, h := range hs {
			if !seen[h] {
				seen[h] = true
				hosts = append(hosts, h)
			}
		}
	}
	if onlyFailed {
		add(failedHosts)
	}
	if onlyUnreachable {
		add(unreachableHosts)
	}
	sort.Strings(hosts)
	return hosts
}

// resumablePlayName reconciles two different play-name namespaces that
// "Resume where failed" (design-docs/Rerun.md) sits between. candidate is
// PlaybookState.EarliestFailingPlay()'s (or a replayed run's) play.Name,
// sourced from the live jsonl stream - Ansible always populates this,
// synthesizing a default (e.g. from the play's own hosts: pattern) for a
// play with no explicit name: key at all. knownPlayNames is
// source.ListTopLevelPlayNames's static YAML scan, which - per
// design-docs/StartWithPlay.md's own deliberate v1 scope decision - only
// ever lists plays that DO have an explicit name: key, since there's
// nothing else a user could type or autocomplete to target one.
//
// Pre-filling "Start with play" with a synthesized name Start-with-play's
// own TrimPlaybookToPlay can never match would check "Resume where
// failed," look correct, and then fail the whole rerun the moment it's
// confirmed (TrimPlaybookToPlay finds no matching top-level play) -
// confirmed live: exactly this happened against testdata/outcomes.yml's
// unnamed play before this guard existed. Returning "" here instead makes
// rebuildRerunForm omit the "Resume where failed" checkbox entirely for
// such a play, the same "nothing to show" treatment used everywhere else
// in this app when a feature's data isn't actually usable.
func resumablePlayName(candidate string, knownPlayNames []string) string {
	if candidate == "" {
		return ""
	}
	for _, n := range knownPlayNames {
		if n == candidate {
			return candidate
		}
	}
	return ""
}
