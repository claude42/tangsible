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
	"slices"
	"testing"
)

func TestHasDialogFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"absent", []string{"-i", "localhost,"}, false},
		{"dialog", []string{"-i", "localhost,", "--dialog"}, true},
		{"no-dialog", []string{"--no-dialog", "-v"}, true},
		{"empty args", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasDialogFlag(tt.args); got != tt.want {
				t.Errorf("HasDialogFlag(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestExtractDialogFlag(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantOverride DialogOverride
		wantRest     []string
		wantOK       bool
	}{
		{"absent", []string{"-i", "localhost,"}, DialogOverrideUnset, []string{"-i", "localhost,"}, true},
		{"dialog", []string{"-i", "localhost,", "--dialog"}, DialogOverrideShow, []string{"-i", "localhost,"}, true},
		{"no-dialog", []string{"--no-dialog", "-v"}, DialogOverrideHide, []string{"-v"}, true},
		{"repeated flag is a no-op, not tracked specially", []string{"--dialog", "--dialog"}, DialogOverrideShow, nil, true},
		{"both given is a conflict", []string{"--dialog", "-v", "--no-dialog"}, DialogOverrideUnset, []string{"--dialog", "-v", "--no-dialog"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOverride, gotRest, gotOK := ExtractDialogFlag(tt.args)
			if gotOverride != tt.wantOverride {
				t.Errorf("override = %v, want %v", gotOverride, tt.wantOverride)
			}
			if !slices.Equal(gotRest, tt.wantRest) {
				t.Errorf("rest = %v, want %v", gotRest, tt.wantRest)
			}
			if gotOK != tt.wantOK {
				t.Errorf("ok = %v, want %v", gotOK, tt.wantOK)
			}
		})
	}
}

func TestExtractDialogFlag_NoneFoundReturnsSameSlice(t *testing.T) {
	args := []string{"-i", "localhost,"}
	_, rest, ok := ExtractDialogFlag(args)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if &rest[0] != &args[0] {
		t.Error("rest should be the same underlying slice as args when nothing was found")
	}
}

func TestResolveDialogVisibility(t *testing.T) {
	tests := []struct {
		name        string
		override    DialogOverride
		pref        DialogPreference
		verbDefault bool
		want        bool
	}{
		{"no override, default pref, verb default false", DialogOverrideUnset, DialogPreferenceDefault, false, false},
		{"no override, default pref, verb default true", DialogOverrideUnset, DialogPreferenceDefault, true, true},
		{"no override, pref never overrides verb default true", DialogOverrideUnset, DialogPreferenceNever, true, false},
		{"no override, pref always overrides verb default false", DialogOverrideUnset, DialogPreferenceAlways, false, true},
		{"override show wins over pref never", DialogOverrideShow, DialogPreferenceNever, false, true},
		{"override hide wins over pref always", DialogOverrideHide, DialogPreferenceAlways, true, false},
		{"override show wins over verb default with default pref", DialogOverrideShow, DialogPreferenceDefault, false, true},
		{"override hide wins over verb default with default pref", DialogOverrideHide, DialogPreferenceDefault, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveDialogVisibility(tt.override, tt.pref, tt.verbDefault); got != tt.want {
				t.Errorf("ResolveDialogVisibility(%v, %v, %v) = %v, want %v", tt.override, tt.pref, tt.verbDefault, got, tt.want)
			}
		})
	}
}
