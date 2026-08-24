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

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// TangsibleStatePath is .tangsible/state.toml - see stateConfig's own doc
// comment. Shared by this file's own read/write functions and main.go's
// three appendInvocation call sites plus the "rerun" verb's cfg read.
var TangsibleStatePath = filepath.Join(TangsibleDir, "state.toml")

// MaxHistoryPerPlaybook bounds how many past invocations are kept per
// playbook in state.toml's [[history]] table - arbitrary, per-project
// discussion (there's no principled number here, just "enough to be
// useful without growing the file forever").
const MaxHistoryPerPlaybook = 20

// InvocationRecord is one recorded invocation of a playbook or role - one
// entry in playbookHistory.Invocations. Args is unchanged from before
// design-docs/Revisit.md: the single space-joined string form of that run's
// passthrough args (see argsToHistoryString/historyStringToArgs in
// rerunargs.go), the same shape the user would have typed after the
// playbook name (or role name) on the command line, including an empty
// string for "no extra args at all" - still all `rerun`'s own pre-fill
// (lastInvocation) ever reads.
//
// Time/ExitCode/RunID exist for design-docs/Revisit.md's "revisit" verb.
// appendInvocation sets Args and Time (RFC3339, this generation's own start)
// immediately, before the generation is even spawned - "an invocation is an
// invocation," same reasoning as before this existed, independent of
// whether the run goes on to succeed, fail, or gets killed outright.
// ExitCode/RunID are only known once the generation actually finishes, so
// they start unset and are filled in later by finalizeInvocation - ExitCode
// is a pointer specifically so "not finished yet" (nil) can never be
// confused with "finished, exit code 0" (the two would be indistinguishable
// as a plain int, and revisit's own list view needs to tell them apart:
// see design-docs/Revisit.md's list-building pruning). RunID names the
// sibling .tangsible/runs/<RunID>.jsonl/.stderr files this generation's own
// raw event log and stderr were saved under (runlog.go) - empty if that
// save was never attempted, never succeeded, or (later) if revisit's own
// pruning found the files gone and cleared it back out.
type InvocationRecord struct {
	Args     string `toml:"args"`
	Time     string `toml:"time,omitempty"`
	ExitCode *int   `toml:"exit_code,omitempty"`
	RunID    string `toml:"run_id,omitempty"`
}

// PlaybookHistory is one [[history]] entry in state.toml: either a
// playbook path or a role name (design-docs/Tangsible role.md) - exactly
// one of Playbook/Role is ever set on a given entry, never both - and its
// past invocations, oldest first.
type PlaybookHistory struct {
	Playbook    string             `toml:"playbook,omitempty"`
	Role        string             `toml:"role,omitempty"`
	Invocations []InvocationRecord `toml:"invocations"`
}

// StateConfig is the shape of .tangsible/state.toml - entirely app-owned:
// Tangsible reads AND writes this file, on every invocation of every verb
// (see appendInvocation), the same full-rewrite approach the old single
// .tangsible file used for everything before
// design-docs/Dottangsible-directory.md's split. Safe here specifically
// because nothing in state.toml is ever meant to be hand-edited - unlike
// the separate, user-authored settingsConfig (resolve.go), there's no
// user content a rewrite could ever clobber.
type StateConfig struct {
	General struct {
		// Last is the playbook path or role name (see "tangsible role",
		// design-docs/Tangsible role.md) of the most recent invocation of
		// *any* verb, updated by appendInvocation on every one - what
		// "tangsible rerun" (no playbook given) resolves to (Rerun.md).
		// One field for both, not two parallel ones: a playbook path and
		// a role name are never really ambiguous in practice (a playbook
		// almost always has a .yml/.yaml extension or a path separator; a
		// role name is a bare identifier), and whichever of a
		// playbookHistory entry's own Playbook/Role fields matches this
		// value at lookup time (see lastTarget) says which kind it was -
		// no separate flag needed. Distinct from History's own
		// per-playbook/per-role ordering: History preserves insertion
		// order of *which targets have ever been seen*, not *when* each
		// was last touched relative to the others, so it can't answer
		// "what ran most recently" on its own - this one field can,
		// without restructuring History itself or adding per-invocation
		// timestamps.
		Last string `toml:"last"`
	} `toml:"general"`
	History []PlaybookHistory `toml:"history"`
}

// EnsureParentDir creates path's parent directory (and any missing
// ancestors) if it doesn't already exist. In practice this only ever
// needs to create .tangsible/ itself, on state.toml's first write in a
// project - config.toml is user-authored and Tangsible never creates it
// (see settingsConfig's own doc comment), so nothing else in this program
// ever needs to create anything under .tangsible/.
//
// Handles exactly one legibility problem, deliberately not more (per
// design-docs/Dottangsible-directory.md: no migration code, just make the
// failure mode legible): a pre-upgrade project's .tangsible was a plain
// file, not a directory. If that old file is still sitting at this exact
// path when os.MkdirAll tries to create the new directory over it, the
// resulting error is a bare "mkdir .tangsible: not a directory" - not
// wrong, but gives no hint why, especially to someone hitting this cold
// after an upgrade with no memory of this design doc. Detected here
// explicitly (stat first, check !IsDir) rather than left to MkdirAll's own
// text, so the message can say the one thing MkdirAll's own error can't:
// that this is a known upgrade artifact and the fix is to delete it.
func EnsureParentDir(path string) error {
	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		return fmt.Errorf(
			"%s exists as a plain file, not a directory - this looks like a pre-upgrade .tangsible file (see design-docs/Dottangsible-directory.md); it's safe to delete (Tangsible only ever used it for local invocation history), and Tangsible will recreate %s/ on its own next time",
			dir, dir,
		)
	}
	return os.MkdirAll(dir, 0o755)
}

// WriteState writes cfg to path (state.toml) as TOML, overwriting whatever
// was there and creating .tangsible/ first if necessary (see
// ensureParentDir). This is a full rewrite, not an in-place edit - safe
// here, unlike the old single .tangsible file this replaces, because
// state.toml is entirely app-owned (see stateConfig's own doc comment):
// there is no user-authored content left in this file for a rewrite to
// lose.
func WriteState(path string, cfg StateConfig) error {
	if err := EnsureParentDir(path); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}

// ReadState reads path (state.toml) via readTOMLFile (resolve.go). Shared
// by appendInvocation (this file) and main.go's "rerun" verb, which reads
// it fresh once per invocation to build the cfg resolveRerun needs.
func ReadState(path string) StateConfig {
	return ReadTOMLFile[StateConfig](path)
}

// AppendInvocation records one new invocation (argsString, as produced by
// argsToHistoryString) under the matching [[history]] entry in path,
// creating that entry if it doesn't exist yet, and capping it at
// maxHistoryPerPlaybook (dropping the oldest once that's exceeded - see
// appendCapped, which also reports what got dropped so this can delete any
// saved run-log files, runlog.go, that go with it). Exactly one of
// playbook/role is expected non-empty, matching playbookHistory's own shape
// - a plain "tangsible run"/"tangsible rerun" of a playbook passes
// (playbook, "") and "tangsible role" (or a rerun that resolves to one -
// see resolveRerun) passes ("", role). Also updates General.Last to
// whichever of the two was given - every invocation of every verb funnels
// through here, which is what makes this the single place that keeps it
// current (see stateConfig's own doc comment). Reads the file fresh and
// writes the whole thing back - state.toml isn't expected to be edited
// concurrently by more than one tangsible process at this project's target
// scale, so no locking is attempted.
//
// The new record's own Time is stamped here (RFC3339, this generation's own
// start) rather than threaded in from the caller - every call site wants
// "now," so there's nothing a parameter would add. ExitCode/RunID start
// unset; see finalizeInvocation for when those are filled in.
func AppendInvocation(path, playbook, role, argsString string) error {
	cfg := ReadState(path)

	next := InvocationRecord{Args: argsString, Time: time.Now().UTC().Format(time.RFC3339)}

	found := false
	for i := range cfg.History {
		h := &cfg.History[i]
		if (playbook != "" && h.Playbook == playbook) || (role != "" && h.Role == role) {
			var evicted []InvocationRecord
			h.Invocations, evicted = AppendCapped(h.Invocations, next, MaxHistoryPerPlaybook)
			for _, e := range evicted {
				DeleteRunLog(path, e.RunID)
			}
			found = true
			break
		}
	}
	if !found {
		cfg.History = append(cfg.History, PlaybookHistory{
			Playbook:    playbook,
			Role:        role,
			Invocations: []InvocationRecord{next},
		})
	}
	if playbook != "" {
		cfg.General.Last = playbook
	} else {
		cfg.General.Last = role
	}

	return WriteState(path, cfg)
}

// FinalizeInvocation fills in the two fields appendInvocation's own call
// (always made first, right before that generation was spawned) couldn't
// know yet: the process's actual exit code, and runID - the id of the
// sibling .tangsible/runs/<runID>.{jsonl,stderr} files this generation's
// raw event log and stderr were saved under (runlog.go), or "" if that save
// was never attempted or didn't succeed. Updates the *last* invocation
// recorded for playbook/role, which is always the one this call is meant
// for: only one generation is ever in flight for a given playbook/role at a
// time, and nothing else appends to that same target's history between this
// generation's own appendInvocation call and it actually finishing.
//
// Best-effort, same tolerance appendInvocation itself has for its own
// failures: a write failure here just means this one generation's history
// entry keeps an unset exit code and isn't revisitable, never fatal to the
// run itself.
func FinalizeInvocation(path, playbook, role string, exitCode int, runID string) error {
	cfg := ReadState(path)

	for i := range cfg.History {
		h := &cfg.History[i]
		if (playbook != "" && h.Playbook == playbook) || (role != "" && h.Role == role) {
			if n := len(h.Invocations); n > 0 {
				code := exitCode
				h.Invocations[n-1].ExitCode = &code
				h.Invocations[n-1].RunID = runID
			}
			break
		}
	}

	return WriteState(path, cfg)
}

// LastTarget resolves cfg.General.Last - the playbook path or role name of
// the most recently invoked verb, of either kind, see stateConfig's own
// doc comment - against cfg.History, returning the matching entry itself
// (which carries its own Invocations to pre-fill a rerun from) so the
// caller can tell a playbook rerun from a role rerun by checking which of
// the returned entry's own Playbook/Role fields is non-empty. What
// "tangsible rerun" with no playbook (or role) given resolves to
// (Rerun.md, design-docs/Tangsible role.md).
//
// Falls back to cfg.History's own single entry, of either kind, when Last
// is unset but there's exactly one entry on record: a real gap this
// covers, not a hypothetical - a state.toml written entirely by a build
// that predates this field (or its own predecessor, LastPlaybook) has
// [[history]] entries but no general.last at all, and the common case
// there is a single-target project, where "what ran last" isn't actually
// ambiguous even without a recorded answer - there's only one candidate.
// Two or more entries with no Last genuinely can't be disambiguated this
// way (History preserves first-seen order, not recency - see
// stateConfig's own doc comment), so that case still falls through to
// "not ok."
//
// A playbook and a role sharing the exact same literal name would make
// this ambiguous too - vanishingly unlikely (see stateConfig's own doc
// comment) and not worth more machinery than picking one deterministically
// if it ever happens: a role match wins over a playbook match.
func LastTarget(cfg StateConfig) (PlaybookHistory, bool) {
	if cfg.General.Last != "" {
		var playbookMatch, roleMatch *PlaybookHistory
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
	return PlaybookHistory{}, false
}

// AppendCapped appends next to existing, dropping from the front (oldest
// first) until the result is at most max entries long. evicted is whatever
// got dropped, oldest first, so a caller with side effects to clean up
// per-entry (appendInvocation deleting a dropped entry's own saved run-log
// files) has something to iterate.
func AppendCapped(existing []InvocationRecord, next InvocationRecord, max int) (result, evicted []InvocationRecord) {
	out := append(existing, next)
	if len(out) > max {
		evicted = out[:len(out)-max]
		out = out[len(out)-max:]
	}
	return out, evicted
}

// LastInvocation returns the most recently recorded invocation string
// (invocationRecord.Args) for playbook out of cfg, and false if there's no
// history entry for it (or that entry has no invocations recorded).
func LastInvocation(cfg StateConfig, playbook string) (string, bool) {
	for _, h := range cfg.History {
		if h.Playbook != playbook {
			continue
		}
		if len(h.Invocations) == 0 {
			return "", false
		}
		return h.Invocations[len(h.Invocations)-1].Args, true
	}
	return "", false
}

// PruneMissingRunLogs reads path (state.toml), clears the RunID of every
// invocationRecord whose saved .jsonl file (runlog.go's runLogPaths) no
// longer exists on disk, writes the result back if anything changed, and
// returns the resulting cfg either way - design-docs/Revisit.md's own
// answer to "what if the manifest says a run is revisitable but its data
// is actually gone" (hand-deleted, disk cleanup, ...): rather than showing
// an error when the user picks such an entry, it's simply never offered in
// the first place, self-healing on every "revisit" invocation. Only the
// .jsonl file is checked - .stderr is supplementary (not yet surfaced
// anywhere - see writeRunStderr's own doc comment), so its absence alone
// doesn't disqualify an otherwise-good entry.
//
// Deliberately clears RunID only, leaving Args/Time/ExitCode in place: that
// same Invocations list also feeds "rerun"'s own tags/hosts pre-fill
// (lastInvocation), a use that has nothing to do with whether this run's
// saved *data* still exists - clearing the whole record would quietly
// degrade that unrelated feature as a side effect.
func PruneMissingRunLogs(path string) (StateConfig, error) {
	cfg := ReadState(path)

	changed := false
	for i := range cfg.History {
		h := &cfg.History[i]
		for j := range h.Invocations {
			inv := &h.Invocations[j]
			if inv.RunID == "" {
				continue
			}
			jsonlPath, _ := RunLogPaths(path, inv.RunID)
			if _, err := os.Stat(jsonlPath); os.IsNotExist(err) {
				inv.RunID = ""
				changed = true
			}
		}
	}

	if !changed {
		return cfg, nil
	}
	return cfg, WriteState(path, cfg)
}
