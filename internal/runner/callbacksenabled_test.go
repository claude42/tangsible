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

package runner

import (
	"slices"
	"testing"
)

// TestParseCallbacksEnabled uses real ansible-config dump --only-changed
// --format json output, captured live against ansible-core 2.19 (see
// design-docs/OwnCallbackPlugin.md's step 6) - including the one observed
// anomaly (a bare {"GALAXY_SERVERS": {}} entry with no name/value at all)
// so the parser's tolerance of it is locked in, not just assumed.
func TestParseCallbacksEnabled(t *testing.T) {
	t.Run("callbacks_enabled configured in ansible.cfg", func(t *testing.T) {
		input := `[
			{
				"name": "CALLBACKS_ENABLED",
				"origin": "/some/path/ansible.cfg",
				"value": ["profile_tasks", "timer"]
			},
			{
				"name": "EDITOR",
				"origin": "env: EDITOR",
				"value": "/usr/bin/vim"
			},
			{
				"GALAXY_SERVERS": {}
			}
		]`
		got := parseCallbacksEnabled([]byte(input))
		if want := []string{"profile_tasks", "timer"}; !slices.Equal(got, want) {
			t.Errorf("parseCallbacksEnabled() = %v, want %v", got, want)
		}
	})

	t.Run("nothing configured at all", func(t *testing.T) {
		input := `[
			{"name": "CONFIG_FILE", "origin": "", "value": null},
			{"name": "EDITOR", "origin": "env: EDITOR", "value": "/usr/bin/vim"},
			{"GALAXY_SERVERS": {}}
		]`
		if got := parseCallbacksEnabled([]byte(input)); got != nil {
			t.Errorf("parseCallbacksEnabled() = %v, want nil", got)
		}
	})

	t.Run("malformed JSON falls back to nil, not an error", func(t *testing.T) {
		if got := parseCallbacksEnabled([]byte("not json at all")); got != nil {
			t.Errorf("parseCallbacksEnabled() = %v, want nil", got)
		}
	})

	t.Run("CALLBACKS_ENABLED present but not a string list falls back to nil", func(t *testing.T) {
		input := `[{"name": "CALLBACKS_ENABLED", "origin": "", "value": "not-a-list"}]`
		if got := parseCallbacksEnabled([]byte(input)); got != nil {
			t.Errorf("parseCallbacksEnabled() = %v, want nil", got)
		}
	})
}

func TestUnionCallbacksEnabled(t *testing.T) {
	t.Run("nothing configured - just our own plugin", func(t *testing.T) {
		if got, want := unionCallbacksEnabled(nil, "tangsible_jsonl"), "tangsible_jsonl"; got != want {
			t.Errorf("unionCallbacksEnabled(nil, ...) = %q, want %q", got, want)
		}
	})

	t.Run("existing callbacks are preserved, ours appended", func(t *testing.T) {
		got := unionCallbacksEnabled([]string{"profile_tasks", "timer"}, "tangsible_jsonl")
		if want := "profile_tasks,timer,tangsible_jsonl"; got != want {
			t.Errorf("unionCallbacksEnabled() = %q, want %q", got, want)
		}
	})

	t.Run("already explicitly enabled - not duplicated", func(t *testing.T) {
		got := unionCallbacksEnabled([]string{"profile_tasks", "tangsible_jsonl"}, "tangsible_jsonl")
		if want := "profile_tasks,tangsible_jsonl"; got != want {
			t.Errorf("unionCallbacksEnabled() = %q, want %q (should not duplicate)", got, want)
		}
	})
}

func TestPrependCallbackPluginsPath(t *testing.T) {
	t.Run("no existing value", func(t *testing.T) {
		if got, want := PrependCallbackPluginsPath("/opt/tangsible/callback", ""), "/opt/tangsible/callback"; got != want {
			t.Errorf("PrependCallbackPluginsPath() = %q, want %q", got, want)
		}
	})

	t.Run("existing value is preserved, ours prepended", func(t *testing.T) {
		got := PrependCallbackPluginsPath("/opt/tangsible/callback", "/home/user/.ansible/plugins/callback")
		if want := "/opt/tangsible/callback:/home/user/.ansible/plugins/callback"; got != want {
			t.Errorf("PrependCallbackPluginsPath() = %q, want %q", got, want)
		}
	})
}
