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

import (
	"reflect"
	"testing"
)

func TestResolvedRerunHosts(t *testing.T) {
	failed := []string{"web2", "web1"}
	unreachable := []string{"db1", "web1"}

	tests := []struct {
		name            string
		onlyFailed      bool
		onlyUnreachable bool
		want            []string
	}{
		{
			name: "neither checked yields no hosts",
			want: nil,
		},
		{
			name:       "only failed checked yields failed hosts, sorted",
			onlyFailed: true,
			want:       []string{"web1", "web2"},
		},
		{
			name:            "only unreachable checked yields unreachable hosts, sorted",
			onlyUnreachable: true,
			want:            []string{"db1", "web1"},
		},
		{
			name:            "both checked union without duplicating a host present in both",
			onlyFailed:      true,
			onlyUnreachable: true,
			want:            []string{"db1", "web1", "web2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvedRerunHosts(tt.onlyFailed, tt.onlyUnreachable, failed, unreachable)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resolvedRerunHosts(%v, %v, %v, %v) = %v, want %v",
					tt.onlyFailed, tt.onlyUnreachable, failed, unreachable, got, tt.want)
			}
		})
	}
}

func TestResolvedRerunHosts_NoFailedOrUnreachableHosts(t *testing.T) {
	// A checkbox can be checked with an empty candidate list e.g. right
	// after a run with no failures at all still has "Only unreachable"
	// checked from a prior toggle - must not panic, must yield no hosts.
	got := resolvedRerunHosts(true, true, nil, nil)
	if len(got) != 0 {
		t.Errorf("resolvedRerunHosts with no candidate hosts = %v, want empty", got)
	}
}

func TestResolvedRerunHosts_UnionIsNotAffectedByArgOrder(t *testing.T) {
	// The same host appearing in both candidate lists must still surface
	// exactly once regardless of which checkbox contributed it first -
	// regression guard for the "plain append double-lists a host present
	// in both buckets" failure mode this function exists to avoid.
	got := resolvedRerunHosts(true, true, []string{"web1"}, []string{"web1"})
	want := []string{"web1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolvedRerunHosts = %v, want %v", got, want)
	}
}

func TestResumablePlayName(t *testing.T) {
	knownPlayNames := []string{"first play", "second play"}

	tests := []struct {
		name      string
		candidate string
		want      string
	}{
		{
			name:      "candidate matches a known top-level play name",
			candidate: "second play",
			want:      "second play",
		},
		{
			name:      "candidate empty (no failure at all) stays empty",
			candidate: "",
			want:      "",
		},
		{
			name: "candidate is a synthesized jsonl name for an unnamed " +
				"play, absent from the static YAML scan - the exact " +
				"situation that made confirming 'Resume where failed' " +
				"fail the whole rerun before this guard existed " +
				"(confirmed live against testdata/outcomes.yml)",
			candidate: "localhost",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resumablePlayName(tt.candidate, knownPlayNames); got != tt.want {
				t.Errorf("resumablePlayName(%q, %v) = %q, want %q", tt.candidate, knownPlayNames, got, tt.want)
			}
		})
	}
}

func TestResumablePlayName_NoKnownPlayNamesAtAll(t *testing.T) {
	// A playbook with zero named top-level plays (source.
	// ListTopLevelPlayNames returns nil) must never panic and must never
	// treat an empty knownPlayNames slice as "anything goes."
	if got := resumablePlayName("localhost", nil); got != "" {
		t.Errorf("resumablePlayName with no known play names = %q, want empty", got)
	}
}
