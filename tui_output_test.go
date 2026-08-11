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
	"slices"
	"testing"
)

func TestRoleFromPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "role-sourced task",
			path: "/home/user/project/roles/myrole/tasks/main.yml:1",
			want: "myrole",
		},
		{
			name: "role-sourced handler",
			path: "/home/user/project/roles/myrole/handlers/main.yml:3",
			want: "myrole",
		},
		{
			name: "play-level task, not role-sourced",
			path: "/home/user/project/site.yml:8",
			want: "",
		},
		{
			name: "included task file outside any role",
			path: "/home/user/project/tasks/setup.yml:1",
			want: "",
		},
		{
			name: "empty path",
			path: "",
			want: "",
		},
		{
			name: "role directory in the name but not the expected layout",
			path: "/home/user/project/not-roles/myrole/tasks/main.yml:1",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := roleFromPath(c.path); got != c.want {
				t.Errorf("roleFromPath(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

func TestSkipOutputText(t *testing.T) {
	cases := []struct {
		name    string
		decoded map[string]interface{}
		want    string
	}{
		{
			name:    "reason and a string condition",
			decoded: map[string]interface{}{"skip_reason": "Conditional result was False", "false_condition": "my_var"},
			want:    "Conditional result was False: my_var",
		},
		{
			name: "false_condition not a string (e.g. when: false serializes as JSON false)",
			decoded: map[string]interface{}{
				"skip_reason":     "Conditional result was False",
				"false_condition": false,
			},
			want: "Conditional result was False",
		},
		{
			name:    "no skip_reason at all",
			decoded: map[string]interface{}{},
			want:    "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := skipOutputText(c.decoded); got != c.want {
				t.Errorf("skipOutputText(%v) = %q, want %q", c.decoded, got, c.want)
			}
		})
	}
}

func TestLoopItemLabels(t *testing.T) {
	t.Run("no results key at all - not a looped task", func(t *testing.T) {
		got := loopItemLabels(map[string]interface{}{"changed": false})
		if got != nil {
			t.Errorf("loopItemLabels() = %v, want nil", got)
		}
	})

	t.Run("string item labels, the common case", func(t *testing.T) {
		decoded := map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{"_ansible_item_label": "foo", "item": "foo"},
				map[string]interface{}{"_ansible_item_label": "bar", "item": "bar"},
			},
		}
		got := loopItemLabels(decoded)
		want := []string{"foo", "bar"}
		if !slices.Equal(got, want) {
			t.Errorf("loopItemLabels() = %v, want %v", got, want)
		}
	})

	t.Run("dict item label falls back to compact JSON", func(t *testing.T) {
		decoded := map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{
					"_ansible_item_label": map[string]interface{}{"name": "foo", "val": 1.0},
					"item":                map[string]interface{}{"name": "foo", "val": 1.0},
				},
			},
		}
		got := loopItemLabels(decoded)
		if len(got) != 1 {
			t.Fatalf("loopItemLabels() = %v, want exactly 1 entry", got)
		}
		if got[0] != `{"name":"foo","val":1}` {
			t.Errorf("loopItemLabels()[0] = %q, want compact JSON of the dict item", got[0])
		}
	})

	t.Run("missing _ansible_item_label falls back to item", func(t *testing.T) {
		decoded := map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{"item": "onlyitem"},
			},
		}
		got := loopItemLabels(decoded)
		want := []string{"onlyitem"}
		if !slices.Equal(got, want) {
			t.Errorf("loopItemLabels() = %v, want %v", got, want)
		}
	})

	t.Run("empty results is not nil but also not shown", func(t *testing.T) {
		got := loopItemLabels(map[string]interface{}{"results": []interface{}{}})
		if len(got) != 0 {
			t.Errorf("loopItemLabels() = %v, want empty", got)
		}
	})
}
