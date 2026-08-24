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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAppendCapped(t *testing.T) {
	rec := func(s string) InvocationRecord { return InvocationRecord{Args: s} }
	cases := []struct {
		name        string
		existing    []InvocationRecord
		next        InvocationRecord
		max         int
		want        []InvocationRecord
		wantEvicted []InvocationRecord
	}{
		{
			name:     "under the cap just appends",
			existing: []InvocationRecord{rec("a"), rec("b")},
			next:     rec("c"),
			max:      5,
			want:     []InvocationRecord{rec("a"), rec("b"), rec("c")},
		},
		{
			name:        "at the cap drops the oldest",
			existing:    []InvocationRecord{rec("a"), rec("b"), rec("c")},
			next:        rec("d"),
			max:         3,
			want:        []InvocationRecord{rec("b"), rec("c"), rec("d")},
			wantEvicted: []InvocationRecord{rec("a")},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, evicted := AppendCapped(c.existing, c.next, c.max)
			if !slices.Equal(got, c.want) {
				t.Errorf("appendCapped() result = %v, want %v", got, c.want)
			}
			if !slices.Equal(evicted, c.wantEvicted) {
				t.Errorf("appendCapped() evicted = %v, want %v", evicted, c.wantEvicted)
			}
		})
	}
}

func TestAppendInvocationAndLastInvocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	if err := AppendInvocation(path, "site.yml", "", "-l somehost"); err != nil {
		t.Fatalf("appendInvocation() first call: %v", err)
	}
	if err := AppendInvocation(path, "site.yml", "", "--tags foo,bar"); err != nil {
		t.Fatalf("appendInvocation() second call: %v", err)
	}
	if err := AppendInvocation(path, "other.yml", "", "-l otherhost"); err != nil {
		t.Fatalf("appendInvocation() for a second playbook: %v", err)
	}

	cfg := ReadState(path)

	if got, ok := LastInvocation(cfg, "site.yml"); !ok || got != "--tags foo,bar" {
		t.Errorf("lastInvocation(site.yml) = (%q, %v), want (%q, true)", got, ok, "--tags foo,bar")
	}
	if got, ok := LastInvocation(cfg, "other.yml"); !ok || got != "-l otherhost" {
		t.Errorf("lastInvocation(other.yml) = (%q, %v), want (%q, true)", got, ok, "-l otherhost")
	}
	if _, ok := LastInvocation(cfg, "unknown.yml"); ok {
		t.Error("lastInvocation(unknown.yml) ok = true, want false")
	}
	// other.yml was the most recent of the three appendInvocation calls
	// above, regardless of it being a different playbook than the first
	// two - General.Last tracks invocation recency, not History's own
	// per-playbook insertion order.
	if entry, ok := LastTarget(cfg); !ok || entry.Playbook != "other.yml" {
		t.Errorf("lastTarget() = (%+v, %v), want (Playbook=\"other.yml\", true)", entry, ok)
	}
}

// TestAppendInvocationNeverTouchesConfigFile proves the actual guarantee
// the .tangsible/config.toml + state.toml split
// (design-docs/Dottangsible-directory.md) exists to provide: config.toml
// is user-authored, and nothing appendInvocation/writeState does can ever
// touch it - not just "still parses the same values", but byte-for-byte
// untouched, comments and all. This replaces the old, weaker
// TestAppendInvocationPreservesGeneralSection, whose exact scenario (a
// hand-written [general] section sharing a file with History) is now
// structurally impossible rather than merely fixed, since stateConfig has
// no DefaultPlaybook field at all to lose.
func TestAppendInvocationNeverTouchesConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".tangsible", "config.toml")
	statePath := filepath.Join(dir, ".tangsible", "state.toml")

	configContent := "# my own settings, please leave this comment alone\n[general]\ndefault_playbook = \"site.yml\"\n"
	mustWriteFile(t, configPath, configContent)

	for i := 0; i < 3; i++ {
		if err := AppendInvocation(statePath, "site.yml", "", "-l somehost"); err != nil {
			t.Fatalf("appendInvocation() call %d: %v", i, err)
		}
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config.toml back: %v", err)
	}
	if string(got) != configContent {
		t.Errorf("config.toml was modified by appendInvocation:\n got: %q\nwant: %q", got, configContent)
	}
	if ReadDefaultPlaybook(configPath) != "site.yml" {
		t.Error("config.toml's own settings are no longer readable after appendInvocation")
	}
}

// TestAppendInvocationCreatesTangsibleDirCollision proves the one
// legibility guarantee design-docs/Dottangsible-directory.md asks for
// without any real migration code: a pre-upgrade flat .tangsible file
// sitting where the new .tangsible/ directory needs to go produces a
// clear, actionable error - not a bare "mkdir: not a directory".
func TestAppendInvocationCreatesTangsibleDirCollision(t *testing.T) {
	dir := t.TempDir()
	staleFile := filepath.Join(dir, ".tangsible")
	mustWriteFile(t, staleFile, "[general]\ndefault_playbook = \"site.yml\"\n")

	statePath := filepath.Join(staleFile, "state.toml")
	err := AppendInvocation(statePath, "site.yml", "", "")
	if err == nil {
		t.Fatal("appendInvocation() with a stale .tangsible file in the way, err = nil, want an error")
	}
	if !strings.Contains(err.Error(), "not a directory") || !strings.Contains(err.Error(), "delete") {
		t.Errorf("appendInvocation() error = %q, want it to mention \"not a directory\" and suggest deleting the stale file", err)
	}
}

// TestReadStateWithNoConfigFile proves state.toml's own read/write path
// works standalone - config.toml is never required to exist alongside it,
// since the two are read/written entirely independently.
func TestReadStateWithNoConfigFile(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	if err := AppendInvocation(statePath, "site.yml", "", "-l somehost"); err != nil {
		t.Fatalf("appendInvocation() with no sibling config.toml: %v", err)
	}
	if got, ok := LastInvocation(ReadState(statePath), "site.yml"); !ok || got != "-l somehost" {
		t.Errorf("lastInvocation(site.yml) = (%q, %v), want (%q, true)", got, ok, "-l somehost")
	}
}

func TestAppendInvocationCapsAtMaxHistoryPerPlaybook(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	for i := 0; i < MaxHistoryPerPlaybook+5; i++ {
		if err := AppendInvocation(path, "site.yml", "", string(rune('a'+i%26))); err != nil {
			t.Fatalf("appendInvocation() call %d: %v", i, err)
		}
	}

	cfg := ReadState(path)
	var entry *PlaybookHistory
	for i := range cfg.History {
		if cfg.History[i].Playbook == "site.yml" {
			entry = &cfg.History[i]
		}
	}
	if entry == nil {
		t.Fatal("no history entry for site.yml")
	}
	if len(entry.Invocations) != MaxHistoryPerPlaybook {
		t.Errorf("len(Invocations) = %d, want %d", len(entry.Invocations), MaxHistoryPerPlaybook)
	}
	// The very first invocation ("a") should have been dropped as the
	// oldest, in favor of the 5 later ones pushing it out.
	if entry.Invocations[0].Args == "a" {
		t.Error("oldest invocation was not dropped once the cap was exceeded")
	}
}

func TestAppendInvocationForRoleAndLastTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	if err := AppendInvocation(path, "site.yml", "", "-l somehost"); err != nil {
		t.Fatalf("appendInvocation() for a playbook: %v", err)
	}
	if err := AppendInvocation(path, "", "myrole", "-l nirvana"); err != nil {
		t.Fatalf("appendInvocation() for a role: %v", err)
	}

	cfg := ReadState(path)

	// myrole was the most recent of the two calls above - lastTarget
	// should resolve to it, reporting it as a role (Role set, Playbook
	// empty) rather than a playbook.
	entry, ok := LastTarget(cfg)
	if !ok || entry.Role != "myrole" || entry.Playbook != "" {
		t.Errorf("lastTarget() = (%+v, %v), want (Role=\"myrole\" Playbook=\"\", true)", entry, ok)
	}

	// Recording a playbook invocation afterward moves General.Last back
	// to it, without disturbing myrole's own history entry.
	if err := AppendInvocation(path, "site.yml", "", "--tags again"); err != nil {
		t.Fatalf("appendInvocation() for the playbook again: %v", err)
	}
	cfg = ReadState(path)
	entry, ok = LastTarget(cfg)
	if !ok || entry.Playbook != "site.yml" || entry.Role != "" {
		t.Errorf("lastTarget() after re-running the playbook = (%+v, %v), want (Playbook=\"site.yml\" Role=\"\", true)", entry, ok)
	}
	if got, ok := LastInvocation(cfg, "site.yml"); !ok || got != "--tags again" {
		t.Errorf("lastInvocation(site.yml) = (%q, %v), want (%q, true)", got, ok, "--tags again")
	}
}

func TestLastInvocationEntryWithNoInvocations(t *testing.T) {
	cfg := StateConfig{History: []PlaybookHistory{{Playbook: "site.yml"}}}
	if _, ok := LastInvocation(cfg, "site.yml"); ok {
		t.Error("lastInvocation() on an entry with no Invocations, ok = true, want false")
	}
}

// TestFinalizeInvocation covers design-docs/Revisit.md's two-phase record:
// appendInvocation stamps Args/Time up front (before a generation is even
// spawned); finalizeInvocation fills in ExitCode/RunID once it's actually
// known, on the same (necessarily last) entry - without disturbing Args/
// Time, or any other target's own history.
func TestFinalizeInvocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	if err := AppendInvocation(path, "site.yml", "", "-l somehost"); err != nil {
		t.Fatalf("appendInvocation(): %v", err)
	}
	if err := AppendInvocation(path, "", "myrole", "-l nirvana"); err != nil {
		t.Fatalf("appendInvocation() for a role: %v", err)
	}

	if err := FinalizeInvocation(path, "site.yml", "", 0, "20260823T150000.000000000Z"); err != nil {
		t.Fatalf("finalizeInvocation(): %v", err)
	}

	cfg := ReadState(path)
	var site, role *PlaybookHistory
	for i := range cfg.History {
		switch cfg.History[i].Playbook {
		case "site.yml":
			site = &cfg.History[i]
		}
		if cfg.History[i].Role == "myrole" {
			role = &cfg.History[i]
		}
	}
	if site == nil || len(site.Invocations) != 1 {
		t.Fatalf("site.yml entry = %+v, want exactly one invocation", site)
	}
	got := site.Invocations[0]
	if got.Args != "-l somehost" {
		t.Errorf("finalizeInvocation() touched Args: got %q, want unchanged %q", got.Args, "-l somehost")
	}
	if got.Time == "" {
		t.Error("finalizeInvocation() cleared Time, want it left untouched")
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("got.ExitCode = %v, want a pointer to 0", got.ExitCode)
	}
	if got.RunID != "20260823T150000.000000000Z" {
		t.Errorf("got.RunID = %q, want the id passed to finalizeInvocation()", got.RunID)
	}

	// myrole's own entry must be untouched - finalizeInvocation only ever
	// updates the target it was called for.
	if role == nil || len(role.Invocations) != 1 {
		t.Fatalf("myrole entry = %+v, want exactly one invocation", role)
	}
	if role.Invocations[0].ExitCode != nil || role.Invocations[0].RunID != "" {
		t.Errorf("myrole's own invocation = %+v, want ExitCode/RunID still unset", role.Invocations[0])
	}
}

// TestAppendInvocationEvictionDeletesRunLogFiles proves
// design-docs/Revisit.md's own retention story: when the
// maxHistoryPerPlaybook cap drops an old invocationRecord, any saved run-log
// files it references (runlog.go) are deleted right alongside it, so
// .tangsible/runs/ doesn't accumulate orphans forever.
func TestAppendInvocationEvictionDeletesRunLogFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	if err := AppendInvocation(path, "site.yml", "", "first"); err != nil {
		t.Fatalf("appendInvocation() first call: %v", err)
	}
	runID := "20260823T150000.000000000Z"
	if err := FinalizeInvocation(path, "site.yml", "", 0, runID); err != nil {
		t.Fatalf("finalizeInvocation(): %v", err)
	}
	jsonlPath, stderrPath := RunLogPaths(path, runID)
	mustWriteFile(t, jsonlPath, `{"_event":"v2_playbook_on_play_start"}`+"\n")
	mustWriteFile(t, stderrPath, "some stderr\n")

	// maxHistoryPerPlaybook more invocations for the same playbook push the
	// very first one (the only one with a RunID/saved files) out of the cap.
	for i := 0; i < MaxHistoryPerPlaybook; i++ {
		if err := AppendInvocation(path, "site.yml", "", "later"); err != nil {
			t.Fatalf("appendInvocation() call %d: %v", i, err)
		}
	}

	if _, err := os.Stat(jsonlPath); !os.IsNotExist(err) {
		t.Errorf("jsonl file for the evicted entry still exists (err=%v), want it deleted", err)
	}
	if _, err := os.Stat(stderrPath); !os.IsNotExist(err) {
		t.Errorf("stderr file for the evicted entry still exists (err=%v), want it deleted", err)
	}
}

// TestPruneMissingRunLogsClearsDanglingRunIDs covers design-docs/Revisit.md's
// own self-healing story: a RunID whose .jsonl file has gone missing (hand-
// deleted, disk cleanup, ...) gets cleared - and only that field - so the
// entry drops out of the revisit list without the write ever being able to
// happen again, while Args/Time/ExitCode (and any OTHER entry's own RunID)
// stay untouched.
func TestPruneMissingRunLogsClearsDanglingRunIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	if err := AppendInvocation(path, "site.yml", "", "-l somehost"); err != nil {
		t.Fatalf("appendInvocation() first: %v", err)
	}
	if err := FinalizeInvocation(path, "site.yml", "", 0, "dangling-run"); err != nil {
		t.Fatalf("finalizeInvocation() first: %v", err)
	}
	if err := AppendInvocation(path, "site.yml", "", "--tags foo"); err != nil {
		t.Fatalf("appendInvocation() second: %v", err)
	}
	realRunID := "real-run"
	if err := FinalizeInvocation(path, "site.yml", "", 0, realRunID); err != nil {
		t.Fatalf("finalizeInvocation() second: %v", err)
	}
	jsonlPath, _ := RunLogPaths(path, realRunID)
	mustWriteFile(t, jsonlPath, `{"_event":"v2_playbook_on_play_start"}`+"\n")
	// Deliberately no file written for "dangling-run" - that's the one
	// meant to be pruned.

	cfg, err := PruneMissingRunLogs(path)
	if err != nil {
		t.Fatalf("pruneMissingRunLogs(): %v", err)
	}
	if len(cfg.History) != 1 || len(cfg.History[0].Invocations) != 2 {
		t.Fatalf("cfg.History = %+v, want one entry with both invocations still present", cfg.History)
	}
	first, second := cfg.History[0].Invocations[0], cfg.History[0].Invocations[1]
	if first.RunID != "" {
		t.Errorf("first invocation's RunID = %q, want cleared (its file is missing)", first.RunID)
	}
	if first.Args != "-l somehost" || first.ExitCode == nil || *first.ExitCode != 0 {
		t.Errorf("first invocation = %+v, want Args/ExitCode left untouched by pruning", first)
	}
	if second.RunID != realRunID {
		t.Errorf("second invocation's RunID = %q, want %q (its file exists, must survive)", second.RunID, realRunID)
	}

	// Re-reading from disk confirms the prune was actually persisted, not
	// just returned in-memory.
	reread := ReadState(path)
	if reread.History[0].Invocations[0].RunID != "" {
		t.Error("pruneMissingRunLogs() didn't persist the cleared RunID to disk")
	}
}

func TestPruneMissingRunLogsNoOpWhenNothingIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tangsible", "state.toml")
	if err := AppendInvocation(path, "site.yml", "", ""); err != nil {
		t.Fatalf("appendInvocation(): %v", err)
	}
	// No RunID was ever set on this entry, so there's nothing for
	// pruneMissingRunLogs to check or clear - must not error either way.
	if _, err := PruneMissingRunLogs(path); err != nil {
		t.Errorf("pruneMissingRunLogs() on an entry with no RunID at all: %v", err)
	}
}
