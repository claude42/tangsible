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
	"testing"

	"code.aw.net/claude/tangsible/internal/config"
)

// exitCodePtr is duplicated from internal/revisit's own test helper of the
// same name (unexported test helpers aren't visible across a package
// boundary, and _test.go files are never importable at all regardless of
// export status).
func exitCodePtr(n int) *int { return &n }

func TestCsvSetEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"a,b", "b,a", true},   // order-insensitive
		{"a, b", "b,a", true},  // whitespace trimmed
		{"a,b", "a", false},    // subset is not equal
		{"a", "a,b", false},    // superset is not equal
		{"a,a,b", "a,b", true}, // duplicates collapsed
		{"", "", true},         // both empty
		{"", "a", false},
		{"a", "", false},
	}
	for _, c := range cases {
		if got := csvSetEqual(c.a, c.b); got != c.want {
			t.Errorf("csvSetEqual(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestResolveDiffCandidatesExactTagsAndHosts(t *testing.T) {
	cfg := config.StateConfig{History: []config.PlaybookHistory{
		{Playbook: "site.yml", Invocations: []config.InvocationRecord{
			{Args: "-l zen --tags foo,bar", Time: "2026-08-23T10:00:00Z", ExitCode: exitCodePtr(0), RunID: "exact-match"},
			{Args: "-l zen --tags foo", Time: "2026-08-23T11:00:00Z", ExitCode: exitCodePtr(0), RunID: "partial-tags"},
			{Args: "-l zen,other --tags foo,bar", Time: "2026-08-23T12:00:00Z", ExitCode: exitCodePtr(0), RunID: "different-hosts"},
		}},
	}}
	got := resolveDiffCandidates("site.yml", "", "foo,bar", "zen", "", cfg)
	if len(got) != 1 || got[0].RunID != "exact-match" {
		t.Errorf("resolveDiffCandidates() = %+v, want just the exact tags/hosts match", got)
	}
}

func TestResolveDiffCandidatesExcludesCurrentRun(t *testing.T) {
	cfg := config.StateConfig{History: []config.PlaybookHistory{
		{Playbook: "site.yml", Invocations: []config.InvocationRecord{
			{Args: "", Time: "2026-08-23T10:00:00Z", ExitCode: exitCodePtr(0), RunID: "self"},
			{Args: "", Time: "2026-08-23T11:00:00Z", ExitCode: exitCodePtr(0), RunID: "other"},
		}},
	}}
	got := resolveDiffCandidates("site.yml", "", "", "", "self", cfg)
	if len(got) != 1 || got[0].RunID != "other" {
		t.Errorf("resolveDiffCandidates() = %+v, want the current run's own RunID excluded", got)
	}
}

func TestResolveDiffCandidatesDifferentTargetExcluded(t *testing.T) {
	cfg := config.StateConfig{History: []config.PlaybookHistory{
		{Playbook: "site.yml", Invocations: []config.InvocationRecord{{Time: "2026-08-23T10:00:00Z", ExitCode: exitCodePtr(0), RunID: "site"}}},
		{Role: "myrole", Invocations: []config.InvocationRecord{{Time: "2026-08-23T11:00:00Z", ExitCode: exitCodePtr(0), RunID: "role"}}},
		{Playbook: "other.yml", Invocations: []config.InvocationRecord{{Time: "2026-08-23T12:00:00Z", ExitCode: exitCodePtr(0), RunID: "other"}}},
	}}
	got := resolveDiffCandidates("site.yml", "", "", "", "", cfg)
	if len(got) != 1 || got[0].RunID != "site" {
		t.Errorf("resolveDiffCandidates(site.yml) = %+v, want only site.yml's own entry", got)
	}
}

func TestResolveDiffCandidatesIgnoresEntriesWithNoRunID(t *testing.T) {
	cfg := config.StateConfig{History: []config.PlaybookHistory{
		{Playbook: "site.yml", Invocations: []config.InvocationRecord{
			{Time: "2026-08-23T10:00:00Z", ExitCode: exitCodePtr(0), RunID: ""},
		}},
	}}
	if got := resolveDiffCandidates("site.yml", "", "", "", "", cfg); len(got) != 0 {
		t.Errorf("resolveDiffCandidates() = %+v, want no candidates for an entry with no saved data", got)
	}
}
