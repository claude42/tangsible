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

import "testing"

func exitCodePtr(n int) *int { return &n }

func TestResolveRevisitEntriesFiltersEntriesWithNoRunID(t *testing.T) {
	cfg := stateConfig{History: []playbookHistory{
		{Playbook: "site.yml", Invocations: []invocationRecord{
			{Args: "never saved", Time: "2026-08-23T10:00:00Z"},                                     // no RunID at all
			{Args: "saved", Time: "2026-08-23T11:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-1"}, // revisitable
			{Args: "pruned", Time: "2026-08-23T12:00:00Z", ExitCode: exitCodePtr(0), RunID: ""},     // RunID cleared by pruneMissingRunLogs
		}},
	}}
	got := resolveRevisitEntries(nil, cfg)
	if len(got) != 1 || got[0].Args != "saved" {
		t.Errorf("resolveRevisitEntries() = %+v, want exactly the one entry with a RunID", got)
	}
}

func TestResolveRevisitEntriesSortsNewestFirst(t *testing.T) {
	cfg := stateConfig{History: []playbookHistory{
		{Playbook: "site.yml", Invocations: []invocationRecord{
			{Args: "oldest", Time: "2026-08-23T10:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-1"},
			{Args: "newest", Time: "2026-08-23T12:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-2"},
			{Args: "middle", Time: "2026-08-23T11:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-3"},
		}},
	}}
	got := resolveRevisitEntries(nil, cfg)
	if len(got) != 3 || got[0].Args != "newest" || got[1].Args != "middle" || got[2].Args != "oldest" {
		t.Errorf("resolveRevisitEntries() order = %v, want newest, middle, oldest", got)
	}
}

func TestResolveRevisitEntriesUnparseableTimeSortsLast(t *testing.T) {
	cfg := stateConfig{History: []playbookHistory{
		{Playbook: "site.yml", Invocations: []invocationRecord{
			{Args: "bad time", Time: "not a real timestamp", ExitCode: exitCodePtr(0), RunID: "run-1"},
			{Args: "good time", Time: "2026-08-23T10:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-2"},
		}},
	}}
	got := resolveRevisitEntries(nil, cfg)
	if len(got) != 2 || got[0].Args != "good time" || got[1].Args != "bad time" {
		t.Errorf("resolveRevisitEntries() order = %v, want the parseable time first", got)
	}
}

func TestResolveRevisitEntriesFiltersByExplicitTarget(t *testing.T) {
	cfg := stateConfig{History: []playbookHistory{
		{Playbook: "site.yml", Invocations: []invocationRecord{
			{Args: "", Time: "2026-08-23T10:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-1"},
		}},
		{Role: "myrole", Invocations: []invocationRecord{
			{Args: "", Time: "2026-08-23T11:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-2"},
		}},
	}}
	got := resolveRevisitEntries([]string{"site.yml"}, cfg)
	if len(got) != 1 || got[0].Playbook != "site.yml" {
		t.Errorf("resolveRevisitEntries([site.yml]) = %+v, want only the site.yml entry", got)
	}
	got = resolveRevisitEntries([]string{"myrole"}, cfg)
	if len(got) != 1 || got[0].Role != "myrole" {
		t.Errorf("resolveRevisitEntries([myrole]) = %+v, want only the myrole entry", got)
	}
}

func TestResolveRevisitEntriesFiltersByHostsAndTags(t *testing.T) {
	cfg := stateConfig{History: []playbookHistory{
		{Playbook: "site.yml", Invocations: []invocationRecord{
			{Args: "-l zen --tags foo,bar", Time: "2026-08-23T10:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-1"},
			{Args: "-l other --tags baz", Time: "2026-08-23T11:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-2"},
		}},
	}}
	got := resolveRevisitEntries([]string{"-l", "zen"}, cfg)
	if len(got) != 1 || got[0].Args != "-l zen --tags foo,bar" {
		t.Errorf("resolveRevisitEntries([-l zen]) = %+v, want just the matching host entry", got)
	}
	got = resolveRevisitEntries([]string{"--tags", "bar"}, cfg)
	if len(got) != 1 || got[0].Args != "-l zen --tags foo,bar" {
		t.Errorf("resolveRevisitEntries([--tags bar]) = %+v, want just the matching tag entry (OR-overlap on the comma list)", got)
	}
	got = resolveRevisitEntries([]string{"--tags", "nomatch"}, cfg)
	if len(got) != 0 {
		t.Errorf("resolveRevisitEntries([--tags nomatch]) = %+v, want no entries", got)
	}
}

func TestResolveRevisitEntriesNoArgsMeansNoFiltering(t *testing.T) {
	cfg := stateConfig{History: []playbookHistory{
		{Playbook: "a.yml", Invocations: []invocationRecord{{Time: "2026-08-23T10:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-1"}}},
		{Playbook: "b.yml", Invocations: []invocationRecord{{Time: "2026-08-23T11:00:00Z", ExitCode: exitCodePtr(0), RunID: "run-2"}}},
	}}
	if got := resolveRevisitEntries(nil, cfg); len(got) != 2 {
		t.Errorf("resolveRevisitEntries(nil) = %+v, want both entries with no filter applied", got)
	}
}

func TestCsvOverlap(t *testing.T) {
	cases := []struct {
		want, have string
		wantMatch  bool
	}{
		{"a", "a,b,c", true},
		{"a,z", "b,c", false},
		{"a, b", "b", true}, // whitespace around a value is trimmed
		{"", "a", false},
		{"a", "", false},
	}
	for _, c := range cases {
		if got := csvOverlap(c.want, c.have); got != c.wantMatch {
			t.Errorf("csvOverlap(%q, %q) = %v, want %v", c.want, c.have, got, c.wantMatch)
		}
	}
}
