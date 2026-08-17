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
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAppendCapped(t *testing.T) {
	cases := []struct {
		name     string
		existing []string
		next     string
		max      int
		want     []string
	}{
		{
			name:     "under the cap just appends",
			existing: []string{"a", "b"},
			next:     "c",
			max:      5,
			want:     []string{"a", "b", "c"},
		},
		{
			name:     "at the cap drops the oldest",
			existing: []string{"a", "b", "c"},
			next:     "d",
			max:      3,
			want:     []string{"b", "c", "d"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := appendCapped(c.existing, c.next, c.max); !slices.Equal(got, c.want) {
				t.Errorf("appendCapped(%v, %q, %d) = %v, want %v", c.existing, c.next, c.max, got, c.want)
			}
		})
	}
}

func TestAppendInvocationAndLastInvocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	if err := appendInvocation(path, "site.yml", "", "-l somehost"); err != nil {
		t.Fatalf("appendInvocation() first call: %v", err)
	}
	if err := appendInvocation(path, "site.yml", "", "--tags foo,bar"); err != nil {
		t.Fatalf("appendInvocation() second call: %v", err)
	}
	if err := appendInvocation(path, "other.yml", "", "-l otherhost"); err != nil {
		t.Fatalf("appendInvocation() for a second playbook: %v", err)
	}

	cfg := readState(path)

	if got, ok := lastInvocation(cfg, "site.yml"); !ok || got != "--tags foo,bar" {
		t.Errorf("lastInvocation(site.yml) = (%q, %v), want (%q, true)", got, ok, "--tags foo,bar")
	}
	if got, ok := lastInvocation(cfg, "other.yml"); !ok || got != "-l otherhost" {
		t.Errorf("lastInvocation(other.yml) = (%q, %v), want (%q, true)", got, ok, "-l otherhost")
	}
	if _, ok := lastInvocation(cfg, "unknown.yml"); ok {
		t.Error("lastInvocation(unknown.yml) ok = true, want false")
	}
	// other.yml was the most recent of the three appendInvocation calls
	// above, regardless of it being a different playbook than the first
	// two - General.Last tracks invocation recency, not History's own
	// per-playbook insertion order.
	if entry, ok := lastTarget(cfg); !ok || entry.Playbook != "other.yml" {
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
		if err := appendInvocation(statePath, "site.yml", "", "-l somehost"); err != nil {
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
	if readDefaultPlaybook(configPath) != "site.yml" {
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
	err := appendInvocation(statePath, "site.yml", "", "")
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

	if err := appendInvocation(statePath, "site.yml", "", "-l somehost"); err != nil {
		t.Fatalf("appendInvocation() with no sibling config.toml: %v", err)
	}
	if got, ok := lastInvocation(readState(statePath), "site.yml"); !ok || got != "-l somehost" {
		t.Errorf("lastInvocation(site.yml) = (%q, %v), want (%q, true)", got, ok, "-l somehost")
	}
}

func TestAppendInvocationCapsAtMaxHistoryPerPlaybook(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	for i := 0; i < maxHistoryPerPlaybook+5; i++ {
		if err := appendInvocation(path, "site.yml", "", string(rune('a'+i%26))); err != nil {
			t.Fatalf("appendInvocation() call %d: %v", i, err)
		}
	}

	cfg := readState(path)
	var entry *playbookHistory
	for i := range cfg.History {
		if cfg.History[i].Playbook == "site.yml" {
			entry = &cfg.History[i]
		}
	}
	if entry == nil {
		t.Fatal("no history entry for site.yml")
	}
	if len(entry.Invocations) != maxHistoryPerPlaybook {
		t.Errorf("len(Invocations) = %d, want %d", len(entry.Invocations), maxHistoryPerPlaybook)
	}
	// The very first invocation ("a") should have been dropped as the
	// oldest, in favor of the 5 later ones pushing it out.
	if entry.Invocations[0] == "a" {
		t.Error("oldest invocation was not dropped once the cap was exceeded")
	}
}

func TestAppendInvocationForRoleAndLastTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	if err := appendInvocation(path, "site.yml", "", "-l somehost"); err != nil {
		t.Fatalf("appendInvocation() for a playbook: %v", err)
	}
	if err := appendInvocation(path, "", "myrole", "-l nirvana"); err != nil {
		t.Fatalf("appendInvocation() for a role: %v", err)
	}

	cfg := readState(path)

	// myrole was the most recent of the two calls above - lastTarget
	// should resolve to it, reporting it as a role (Role set, Playbook
	// empty) rather than a playbook.
	entry, ok := lastTarget(cfg)
	if !ok || entry.Role != "myrole" || entry.Playbook != "" {
		t.Errorf("lastTarget() = (%+v, %v), want (Role=\"myrole\" Playbook=\"\", true)", entry, ok)
	}

	// Recording a playbook invocation afterward moves General.Last back
	// to it, without disturbing myrole's own history entry.
	if err := appendInvocation(path, "site.yml", "", "--tags again"); err != nil {
		t.Fatalf("appendInvocation() for the playbook again: %v", err)
	}
	cfg = readState(path)
	entry, ok = lastTarget(cfg)
	if !ok || entry.Playbook != "site.yml" || entry.Role != "" {
		t.Errorf("lastTarget() after re-running the playbook = (%+v, %v), want (Playbook=\"site.yml\" Role=\"\", true)", entry, ok)
	}
	if got, ok := lastInvocation(cfg, "site.yml"); !ok || got != "--tags again" {
		t.Errorf("lastInvocation(site.yml) = (%q, %v), want (%q, true)", got, ok, "--tags again")
	}
}

func TestLastInvocationEntryWithNoInvocations(t *testing.T) {
	cfg := stateConfig{History: []playbookHistory{{Playbook: "site.yml"}}}
	if _, ok := lastInvocation(cfg, "site.yml"); ok {
		t.Error("lastInvocation() on an entry with no Invocations, ok = true, want false")
	}
}
