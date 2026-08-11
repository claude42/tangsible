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
	"os"

	"github.com/BurntSushi/toml"
)

// maxHistoryPerPlaybook bounds how many past invocations are kept per
// playbook in .tangsible's [[history]] table - arbitrary, per-project
// discussion (there's no principled number here, just "enough to be
// useful without growing the file forever").
const maxHistoryPerPlaybook = 20

// playbookHistory is one [[history]] entry in .tangsible: either a
// playbook path or a role name (design-docs/Tangsible role.md) - exactly
// one of Playbook/Role is ever set on a given entry, never both - and its
// past invocations, oldest first. Each entry in Invocations is the single
// space-joined string form of that run's passthrough args (see
// argsToHistoryString/historyStringToArgs in rerunargs.go) - the same
// shape the user would have typed after the playbook name (or role name)
// on the command line, including an empty string for "no extra args at
// all".
type playbookHistory struct {
	Playbook    string   `toml:"playbook,omitempty"`
	Role        string   `toml:"role,omitempty"`
	Invocations []string `toml:"invocations"`
}

// writeTangsibleConfig writes cfg to path as TOML, overwriting whatever
// was there. This is a full rewrite, not an in-place edit - any comments
// or formatting a user hand-added to .tangsible are lost once it starts
// accumulating history, same tradeoff this project already accepts
// elsewhere for "good enough, not chased further" machinery.
func writeTangsibleConfig(path string, cfg playbookConfig) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// appendInvocation records one new invocation (argsString, as produced by
// argsToHistoryString) under the matching [[history]] entry in path,
// creating that entry if it doesn't exist yet, and capping it at
// maxHistoryPerPlaybook (dropping the oldest once that's exceeded).
// Exactly one of playbook/role is expected non-empty, matching
// playbookHistory's own shape - a plain "tangsible run"/"tangsible rerun"
// of a playbook passes (playbook, "") and "tangsible role" (or a rerun
// that resolves to one - see resolveRerun) passes ("", role). Also updates
// General.Last to whichever of the two was given - every invocation of
// every verb funnels through here, which is what makes this the single
// place that keeps it current (see playbookConfig's own doc comment).
// Reads the file fresh and writes the whole thing back - .tangsible isn't
// expected to be edited concurrently by more than one tangsible process at
// this project's target scale, so no locking is attempted.
func appendInvocation(path, playbook, role, argsString string) error {
	cfg := readTangsibleConfig(path)

	found := false
	for i := range cfg.History {
		h := &cfg.History[i]
		if (playbook != "" && h.Playbook == playbook) || (role != "" && h.Role == role) {
			h.Invocations = appendCapped(h.Invocations, argsString, maxHistoryPerPlaybook)
			found = true
			break
		}
	}
	if !found {
		cfg.History = append(cfg.History, playbookHistory{
			Playbook:    playbook,
			Role:        role,
			Invocations: []string{argsString},
		})
	}
	if playbook != "" {
		cfg.General.Last = playbook
	} else {
		cfg.General.Last = role
	}

	return writeTangsibleConfig(path, cfg)
}

// lastTarget resolves cfg.General.Last - the playbook path or role name of
// the most recently invoked verb, of either kind, see playbookConfig's own
// doc comment - against cfg.History, returning the matching entry itself
// (which carries its own Invocations to pre-fill a rerun from) so the
// caller can tell a playbook rerun from a role rerun by checking which of
// the returned entry's own Playbook/Role fields is non-empty. What
// "tangsible rerun" with no playbook (or role) given resolves to
// (Rerun.md, design-docs/Tangsible role.md).
//
// Falls back to cfg.History's own single entry, of either kind, when Last
// is unset but there's exactly one entry on record: a real gap this
// covers, not a hypothetical - a .tangsible written entirely by a build
// that predates this field (or its own predecessor, LastPlaybook) has
// [[history]] entries but no general.last at all, and the common case
// there is a single-target project, where "what ran last" isn't actually
// ambiguous even without a recorded answer - there's only one candidate.
// Two or more entries with no Last genuinely can't be disambiguated this
// way (History preserves first-seen order, not recency - see
// playbookConfig's own doc comment), so that case still falls through to
// "not ok."
//
// A playbook and a role sharing the exact same literal name would make
// this ambiguous too - vanishingly unlikely (see playbookConfig's own doc
// comment) and not worth more machinery than picking one deterministically
// if it ever happens: a role match wins over a playbook match.
func lastTarget(cfg playbookConfig) (playbookHistory, bool) {
	if cfg.General.Last != "" {
		var playbookMatch, roleMatch *playbookHistory
		for i := range cfg.History {
			h := &cfg.History[i]
			switch {
			case h.Role != "" && h.Role == cfg.General.Last:
				roleMatch = h
			case h.Playbook != "" && h.Playbook == cfg.General.Last:
				playbookMatch = h
			}
		}
		if roleMatch != nil {
			return *roleMatch, true
		}
		if playbookMatch != nil {
			return *playbookMatch, true
		}
	}
	if len(cfg.History) == 1 {
		return cfg.History[0], true
	}
	return playbookHistory{}, false
}

// appendCapped appends next to existing, dropping from the front (oldest
// first) until the result is at most max entries long.
func appendCapped(existing []string, next string, max int) []string {
	out := append(existing, next)
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

// lastInvocation returns the most recently recorded invocation string for
// playbook out of cfg, and false if there's no history entry for it (or
// that entry has no invocations recorded).
func lastInvocation(cfg playbookConfig, playbook string) (string, bool) {
	for _, h := range cfg.History {
		if h.Playbook != playbook {
			continue
		}
		if len(h.Invocations) == 0 {
			return "", false
		}
		return h.Invocations[len(h.Invocations)-1], true
	}
	return "", false
}
